package port

import (
	"context"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

// PublicNewsQuerier は公開 API (gateway → news) が必要とする read-only 操作。
type PublicNewsQuerier interface {
	// ListPublished は published な記事を published_at DESC で最新 limit 件返す。
	// 指定 lang の翻訳が無い記事は除外 (フォールバックしない)。
	ListPublished(ctx context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error)
	// GetPublishedByID は published かつ指定 lang の翻訳がある単一記事を返す。
	// 非存在 / 非公開 / 当該 lang 翻訳なしなら ErrNotFound。
	GetPublishedByID(ctx context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error)
}

// NewsIngestWriter は Pub/Sub subscriber が記事と翻訳を永続化するための write 操作。
type NewsIngestWriter interface {
	// InsertArticleWithTranslation は記事行と翻訳行を不可分に挿入する。
	// 記事が既に取り込み済み (同一 article_id、または同一 source_url の別 article_id) なら inserted=false で何も挿入しない。
	InsertArticleWithTranslation(ctx context.Context, article domain.Article, lang, title, summary, body string) (inserted bool, err error)
}
