package port

import (
	"context"
	"time"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// PublicNewsQuerier は公開 API (gateway → news) が必要とする read-only 操作。
// 公開可能条件 (status = 'published' かつ指定 lang の翻訳が存在) を満たす記事のみを返す。
type PublicNewsQuerier interface {
	// ListPublished は published な記事を published_at DESC で最新 limit 件返す。
	// 指定 lang の翻訳が無い記事は除外 (フォールバックしない)。
	ListPublished(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error)
	// GetPublishedByID は published かつ指定 lang の翻訳がある単一記事を返す。
	// 非存在 / 非公開 / 当該 lang 翻訳なしなら ErrNotFound。
	GetPublishedByID(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error)
}

// AdminNewsQuerier は管理 UI が必要とする read 操作。
// 記事とその全翻訳をまとめて取得する (編集画面で言語タブを表示するため)。
type AdminNewsQuerier interface {
	// ListByStatus は status でフィルタされた記事 + その翻訳群を ingested_at DESC で limit 件返す。
	// statusFilter が nil のとき全件。
	ListByStatus(ctx context.Context, statusFilter *apinews.Status, limit int) ([]apinews.ArticleWithTranslations, error)
	// GetByID は status を問わず記事 + 全翻訳を返す。非存在なら ErrNotFound。
	GetByID(ctx context.Context, articleID string) (*apinews.ArticleWithTranslations, error)
}

// NewsIngester は Pub/Sub subscriber が記事と翻訳を永続化するための write 操作。
// 記事と翻訳の挿入は独立した冪等操作として定義し、tx はアダプタ側でも張らない。
// 「記事だけ入って翻訳なし」の中間状態は管理 UI の [ja 未作成] で扱える設計 (FEATURE_SPEC §7)。
type NewsIngester interface {
	// InsertArticle は記事行を挿入する。既存なら inserted=false で no-op。
	InsertArticle(ctx context.Context, article apinews.Article) (inserted bool, err error)
	// InsertTranslation はインジェスト経路の翻訳挿入。既存翻訳は上書きしない (DO NOTHING)。
	// 校閲後の編集を newsfeed 再送で壊さない契約 (FEATURE_SPEC §3.2)。
	InsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error
}

// NewsReviewer は校閲ユースケース (承認・却下・翻訳 upsert) の write 操作。
type NewsReviewer interface {
	// Publish は status=published に遷移させ、published_at / reviewed_at / reviewer を now にセットする。
	// 再承認 (既に published) でも published_at を更新する。
	// 非存在記事なら ErrNotFound。
	Publish(ctx context.Context, articleID string, reviewer string, now time.Time) error
	// Reject は status=rejected に遷移させ、reviewed_at / reviewer を now にセットする。published_at は保持。
	// 非存在記事なら ErrNotFound。
	Reject(ctx context.Context, articleID string, reviewer string, now time.Time) error
	// UpsertTranslation は指定言語の翻訳を追加または更新する。
	// news_articles には触れない (status / reviewed_at 等を変更しない)。
	// 親記事が非存在の場合は ErrNotFound (FK 違反を変換して返す)。
	UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error
}
