// Package ingest は news-article-collected Pub/Sub イベントを自スキーマに永続化する usecase。
package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ErrInvalidEventPayload は必須フィールド欠けに対するセンチネル。
// subscriber は deterministic error として ACK する (再送しても結果が変わらない)。
var ErrInvalidEventPayload = errors.New("invalid event payload")

// Interactor は Pub/Sub event 1 件を受け取り DB 行 (記事 + ja 翻訳) に変換・永続化する。
type Interactor struct {
	writer port.NewsIngestWriter
}

// New は Interactor を生成する。
func New(writer port.NewsIngestWriter) *Interactor {
	return &Interactor{writer: writer}
}

// Insert はイベントを変換して記事と ja 翻訳を INSERT する。
// 戻り値 inserted は「記事行が新規に入ったか」(翻訳の有無は含めない)。
// 必須フィールド欠落・ja 以外の lang・translations 不正は ErrInvalidEventPayload。
func (uc *Interactor) Insert(ctx context.Context, event apinews.ArticleCollectedEvent) (inserted bool, err error) {
	if err := validateEvent(event); err != nil {
		return false, err
	}

	article := domain.Article{
		ArticleID:         event.ArticleID,
		Source:            event.Source,
		SourceURL:         event.SourceURL,
		Tags:              event.Tags,
		SourcePublishedAt: event.SourcePublishedAt,
	}
	inserted, err = uc.writer.InsertArticle(ctx, article)
	if err != nil {
		return false, err
	}

	t := event.Translations[0]
	if err := uc.writer.InsertTranslation(ctx, event.ArticleID, t.Lang, t.Title, t.Summary, t.Body); err != nil {
		return false, err
	}
	return inserted, nil
}

// validateEvent は必須フィールドが揃い translations が ja 1 件ちょうどであることを確認する。
func validateEvent(e apinews.ArticleCollectedEvent) error {
	if e.ArticleID == "" {
		return fmt.Errorf("%w: article_id is empty", ErrInvalidEventPayload)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: source is empty", ErrInvalidEventPayload)
	}
	if e.SourceURL == "" {
		return fmt.Errorf("%w: source_url is empty", ErrInvalidEventPayload)
	}
	if len(e.Translations) != 1 {
		return fmt.Errorf("%w: translations must contain exactly 1 entry (ja), got %d", ErrInvalidEventPayload, len(e.Translations))
	}
	t := e.Translations[0]
	if t.Lang != domain.LangJa {
		return fmt.Errorf("%w: translations[0].lang must be ja, got %q", ErrInvalidEventPayload, t.Lang)
	}
	if t.Title == "" {
		return fmt.Errorf("%w: translations[0].title is empty", ErrInvalidEventPayload)
	}
	if t.Summary == "" {
		return fmt.Errorf("%w: translations[0].summary is empty", ErrInvalidEventPayload)
	}
	if t.Body == "" {
		return fmt.Errorf("%w: translations[0].body is empty", ErrInvalidEventPayload)
	}
	return nil
}
