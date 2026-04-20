package port

import (
	"context"
	"time"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// MockNewsRepo は port の 4 インタフェースをすべて実装する 1 個のモック。
// テストでは必要な Fn フィールドだけ埋めて使い、未設定のメソッド呼び出しは panic で意図しない呼び出しを検出する。
type MockNewsRepo struct {
	ListPublishedFn     func(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error)
	GetPublishedByIDFn  func(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error)
	ListByStatusFn      func(ctx context.Context, statusFilter *apinews.Status, limit int) ([]apinews.ArticleWithTranslations, error)
	GetByIDFn           func(ctx context.Context, articleID string) (*apinews.ArticleWithTranslations, error)
	InsertFn            func(ctx context.Context, article apinews.Article, translations []apinews.Translation) (bool, error)
	ArticleExistsFn     func(ctx context.Context, articleID string) error
	PublishFn           func(ctx context.Context, articleID string, reviewer string, now time.Time) error
	RejectFn            func(ctx context.Context, articleID string, reviewer string, now time.Time) error
	UpsertTranslationFn func(ctx context.Context, articleID string, lang, title, summary, body string) error
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

func (m *MockNewsRepo) ListByStatus(ctx context.Context, statusFilter *apinews.Status, limit int) ([]apinews.ArticleWithTranslations, error) {
	if m.ListByStatusFn == nil {
		panic("MockNewsRepo.ListByStatus called without Fn")
	}
	return m.ListByStatusFn(ctx, statusFilter, limit)
}

func (m *MockNewsRepo) GetByID(ctx context.Context, articleID string) (*apinews.ArticleWithTranslations, error) {
	if m.GetByIDFn == nil {
		panic("MockNewsRepo.GetByID called without Fn")
	}
	return m.GetByIDFn(ctx, articleID)
}

func (m *MockNewsRepo) Insert(ctx context.Context, article apinews.Article, translations []apinews.Translation) (bool, error) {
	if m.InsertFn == nil {
		panic("MockNewsRepo.Insert called without Fn")
	}
	return m.InsertFn(ctx, article, translations)
}

func (m *MockNewsRepo) ArticleExists(ctx context.Context, articleID string) error {
	if m.ArticleExistsFn == nil {
		panic("MockNewsRepo.ArticleExists called without Fn")
	}
	return m.ArticleExistsFn(ctx, articleID)
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
