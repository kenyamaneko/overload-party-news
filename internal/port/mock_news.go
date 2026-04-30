package port

import (
	"context"
	"time"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// MockNewsRepo は port の 4 インタフェースをすべて実装する 1 個のモック。
// テストでは必要な Fn フィールドだけ埋めて使い、未設定のメソッド呼び出しは panic で意図しない呼び出しを検出する。
type MockNewsRepo struct {
	ListPublishedFn                func(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error)
	GetPublishedByIDFn             func(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error)
	ListArticlesFn                 func(ctx context.Context, limit int) ([]apinews.Article, error)
	GetArticleByIDFn               func(ctx context.Context, articleID string) (*apinews.Article, error)
	ListTranslationsByArticleIDsFn func(ctx context.Context, articleIDs []string) ([]apinews.Translation, error)
	InsertArticleFn                func(ctx context.Context, article apinews.Article) (bool, error)
	InsertTranslationFn            func(ctx context.Context, articleID string, lang, title, summary, body string) error
	PublishFn                      func(ctx context.Context, articleID string, reviewer string, now time.Time) error
	RejectFn                       func(ctx context.Context, articleID string, reviewer string, now time.Time) error
	UpsertTranslationFn            func(ctx context.Context, articleID string, lang, title, summary, body string) error
}

var (
	_ PublicNewsQuerier = (*MockNewsRepo)(nil)
	_ AdminNewsQuerier  = (*MockNewsRepo)(nil)
	_ NewsIngester      = (*MockNewsRepo)(nil)
	_ NewsReviewer      = (*MockNewsRepo)(nil)
)

func (m *MockNewsRepo) ListPublished(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error) {
	if m.ListPublishedFn == nil {
		panic("MockNewsRepo.ListPublished called without Fn")
	}
	return m.ListPublishedFn(ctx, lang, limit)
}

func (m *MockNewsRepo) GetPublishedByID(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error) {
	if m.GetPublishedByIDFn == nil {
		panic("MockNewsRepo.GetPublishedByID called without Fn")
	}
	return m.GetPublishedByIDFn(ctx, articleID, lang)
}

func (m *MockNewsRepo) ListArticles(ctx context.Context, limit int) ([]apinews.Article, error) {
	if m.ListArticlesFn == nil {
		panic("MockNewsRepo.ListArticles called without Fn")
	}
	return m.ListArticlesFn(ctx, limit)
}

func (m *MockNewsRepo) GetArticleByID(ctx context.Context, articleID string) (*apinews.Article, error) {
	if m.GetArticleByIDFn == nil {
		panic("MockNewsRepo.GetArticleByID called without Fn")
	}
	return m.GetArticleByIDFn(ctx, articleID)
}

func (m *MockNewsRepo) ListTranslationsByArticleIDs(ctx context.Context, articleIDs []string) ([]apinews.Translation, error) {
	if m.ListTranslationsByArticleIDsFn == nil {
		panic("MockNewsRepo.ListTranslationsByArticleIDs called without Fn")
	}
	return m.ListTranslationsByArticleIDsFn(ctx, articleIDs)
}

func (m *MockNewsRepo) InsertArticle(ctx context.Context, article apinews.Article) (bool, error) {
	if m.InsertArticleFn == nil {
		panic("MockNewsRepo.InsertArticle called without Fn")
	}
	return m.InsertArticleFn(ctx, article)
}

func (m *MockNewsRepo) InsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	if m.InsertTranslationFn == nil {
		panic("MockNewsRepo.InsertTranslation called without Fn")
	}
	return m.InsertTranslationFn(ctx, articleID, lang, title, summary, body)
}

func (m *MockNewsRepo) Publish(ctx context.Context, articleID string, reviewer string, now time.Time) error {
	if m.PublishFn == nil {
		panic("MockNewsRepo.Publish called without Fn")
	}
	return m.PublishFn(ctx, articleID, reviewer, now)
}

func (m *MockNewsRepo) Reject(ctx context.Context, articleID string, reviewer string, now time.Time) error {
	if m.RejectFn == nil {
		panic("MockNewsRepo.Reject called without Fn")
	}
	return m.RejectFn(ctx, articleID, reviewer, now)
}

func (m *MockNewsRepo) UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	if m.UpsertTranslationFn == nil {
		panic("MockNewsRepo.UpsertTranslation called without Fn")
	}
	return m.UpsertTranslationFn(ctx, articleID, lang, title, summary, body)
}
