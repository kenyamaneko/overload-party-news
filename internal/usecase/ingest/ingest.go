// Package ingest は news-article-collected Pub/Sub イベントを自スキーマに永続化する usecase。
package ingest

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/presenter"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ErrInvalidEventPayload は再送しても永続化できない payload に対するセンチネル。
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
// 戻り値 inserted は「新規に取り込んだか」。同一 article_id の再送も、同一 source_url を別 article_id で
// 送り直した取り直しも、既に取り込み済みとして inserted=false になり何も挿入しない。
// 必須フィールド欠落・ja 以外の lang・translations 不正・列幅超過は ErrInvalidEventPayload。
func (uc *Interactor) Insert(ctx context.Context, event apinews.ArticleCollectedEvent) (inserted bool, err error) {
	if err := validateEvent(event); err != nil {
		return false, err
	}

	t := event.Translations[0]
	return uc.writer.InsertArticleWithTranslation(
		ctx, presenter.ArticleFromCollectedEvent(event), t.Lang, t.Title, t.Summary, t.Body)
}

// validateEvent は必須フィールドが揃い、列幅に収まり、translations が ja 1 件ちょうどであることを確認する。
func validateEvent(e apinews.ArticleCollectedEvent) error {
	if e.ArticleID == "" {
		return fmt.Errorf("%w: article_id is empty", ErrInvalidEventPayload)
	}
	if n := utf8.RuneCountInString(e.ArticleID); n > domain.MaxArticleIDLength {
		return fmt.Errorf("%w: article_id exceeds %d characters (got %d)", ErrInvalidEventPayload, domain.MaxArticleIDLength, n)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: source is empty", ErrInvalidEventPayload)
	}
	if n := utf8.RuneCountInString(e.Source); n > domain.MaxSourceLength {
		return fmt.Errorf("%w: source exceeds %d characters (got %d)", ErrInvalidEventPayload, domain.MaxSourceLength, n)
	}
	if e.SourceURL == "" {
		return fmt.Errorf("%w: source_url is empty", ErrInvalidEventPayload)
	}
	if e.Tags == nil {
		return fmt.Errorf("%w: tags is missing", ErrInvalidEventPayload)
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
