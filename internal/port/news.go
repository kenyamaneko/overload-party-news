package port

import (
	"context"
	"time"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

// PublicNewsQuerier は公開 API (gateway → news) が必要とする read-only 操作。
// 公開可能条件 (status = 'published' かつ指定 lang の翻訳が存在) を満たす記事のみを返す。
// 返却型はドメイン DTO で、apinews への射影は usecase 層が行う。
type PublicNewsQuerier interface {
	// ListPublished は published な記事を published_at DESC で最新 limit 件返す。
	// 指定 lang の翻訳が無い記事は除外 (フォールバックしない)。
	ListPublished(ctx context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error)
	// GetPublishedByID は published かつ指定 lang の翻訳がある単一記事を返す。
	// 非存在 / 非公開 / 当該 lang 翻訳なしなら ErrNotFound。
	GetPublishedByID(ctx context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error)
}

// AdminNewsQuerier は管理 UI が必要とする read 操作。
// 記事と翻訳の取得・status の絞り込みはすべて usecase 層の責務とし、
// repo は status 概念を一切知らない (純粋な永続層)。
type AdminNewsQuerier interface {
	// ListArticles は記事を ingested_at DESC で limit 件返す。フィルタは行わない。
	// status による絞り込みは取得後に usecase 層で実施する契約。
	ListArticles(ctx context.Context, limit int) ([]domain.Article, error)
	// GetArticleByID は記事を返す。非存在なら ErrNotFound。
	GetArticleByID(ctx context.Context, articleID string) (*domain.Article, error)
	// ListTranslationsByArticleIDs は指定 article_id 群の翻訳を 1 クエリで返す。
	// 並びは article_id, lang。グループ化は呼び出し側で行う。
	ListTranslationsByArticleIDs(ctx context.Context, articleIDs []string) ([]domain.Translation, error)
}

// NewsIngestWriter は Pub/Sub subscriber が記事と翻訳を永続化するための write 操作。
// 記事と翻訳の挿入は独立した冪等操作として定義し、tx はアダプタ側でも張らない。
// 「記事だけ入って翻訳なし」の中間状態は管理 UI の [ja 未作成] で扱える設計 (FEATURE_SPEC)。
type NewsIngestWriter interface {
	// InsertArticle は記事行を挿入する。既存なら inserted=false で no-op。
	InsertArticle(ctx context.Context, article domain.Article) (inserted bool, err error)
	// InsertTranslation はインジェスト経路の翻訳挿入。既存翻訳は上書きしない (DO NOTHING)。
	// 校閲後の編集を newsfeed 再送で壊さない契約 (FEATURE_SPEC)。
	InsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error
}

// NewsReviewWriter は校閲ユースケース (承認・却下・翻訳 upsert) の write 操作。
// 各操作のカラム更新仕様は FEATURE_SPEC を参照。
type NewsReviewWriter interface {
	// Publish は published 状態への遷移。再承認 (既に published) でも冪等。
	// 非存在記事なら ErrNotFound。
	Publish(ctx context.Context, articleID string, reviewer string, now time.Time) error
	// Reject は rejected 状態への遷移。非存在記事なら ErrNotFound。
	Reject(ctx context.Context, articleID string, reviewer string, now time.Time) error
	// UpsertTranslation は指定言語の翻訳を追加または更新する。
	// news_articles には触れない (翻訳編集はレビュー判断と区別する)。
	// 親記事が非存在の場合は ErrNotFound (FK 違反を変換して返す)。
	UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error
}
