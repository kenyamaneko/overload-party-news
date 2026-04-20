// Package subscriber は Pub/Sub 購読の delivery 層。
// adapter/pubsub から受け取った生バイト列を deserialize し、service 層に委譲する。
package subscriber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kenyamaneko/overload-party-news/internal/service/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ArticleCollectedHandler は news-article-collected トピックの購読 handler。
// service/ingest に委譲するだけで、ビジネスロジックは持たない。
type ArticleCollectedHandler struct {
	svc *ingest.Service
}

// NewArticleCollectedHandler は Handler を生成する。
func NewArticleCollectedHandler(svc *ingest.Service) *ArticleCollectedHandler {
	return &ArticleCollectedHandler{svc: svc}
}

// Handle は 1 メッセージの処理 entrypoint。adapter から MessageHandler として登録する。
// 返り値 nil → ACK、non-nil → NACK の契約:
//   - JSON デコード失敗: ACK (deterministic、再送無意味)
//   - ingest.ErrInvalidEventPayload (必須フィールド欠け): ACK
//   - 上記以外 (DB 障害等): NACK (Pub/Sub 側リトライ)
func (h *ArticleCollectedHandler) Handle(ctx context.Context, data []byte) error {
	var event apinews.ArticleCollectedEvent
	if err := json.Unmarshal(data, &event); err != nil {
		slog.WarnContext(ctx, "article-collected: json decode failed, acking", "error", err)
		return nil
	}

	inserted, err := h.svc.Insert(ctx, event)
	if err != nil {
		if errors.Is(err, ingest.ErrInvalidEventPayload) {
			slog.WarnContext(ctx, "article-collected: invalid payload, acking", "error", err, "article_id", event.ArticleID)
			return nil
		}
		return fmt.Errorf("article-collected insert: %w", err)
	}

	if inserted {
		slog.InfoContext(ctx, "article-collected: inserted", "article_id", event.ArticleID)
	} else {
		slog.DebugContext(ctx, "article-collected: duplicate, no-op", "article_id", event.ArticleID)
	}
	return nil
}
