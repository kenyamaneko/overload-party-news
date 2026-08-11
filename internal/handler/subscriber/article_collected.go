// Package subscriber は Pub/Sub 購読の delivery 層。生バイト列を deserialize して usecase に委譲する。
package subscriber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ArticleCollectedHandler は news-article-collected トピックの購読 handler。
type ArticleCollectedHandler struct {
	uc *ingest.Interactor
}

// NewArticleCollectedHandler は Handler を生成する。
func NewArticleCollectedHandler(uc *ingest.Interactor) *ArticleCollectedHandler {
	return &ArticleCollectedHandler{uc: uc}
}

// Handle は 1 メッセージの処理 entrypoint。adapter から MessageHandler として登録する。
// 返り値 nil → ACK、non-nil → NACK の契約:
//   - JSON デコード失敗: ACK (deterministic、再送無意味)
//   - ingest.ErrInvalidEventPayload (必須フィールド欠け・列幅超過): ACK (dead letter に送っても運用上得られる情報がないため)
//   - 上記以外 (DB 障害等): NACK (Pub/Sub 側リトライ)
func (h *ArticleCollectedHandler) Handle(ctx context.Context, data []byte) error {
	var event apinews.ArticleCollectedEvent
	if err := json.Unmarshal(data, &event); err != nil {
		slog.WarnContext(ctx, "article-collected: json decode failed, acking", "error", err)
		return nil
	}

	inserted, err := h.uc.Insert(ctx, event)
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
		slog.DebugContext(ctx, "article-collected: already ingested, no-op",
			"article_id", event.ArticleID, "source_url", event.SourceURL)
	}
	return nil
}
