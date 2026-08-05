package port

import (
	"context"
	"time"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

// MockNewsRepo は port の 4 インタフェースをすべて実装する 1 個のモック。
// 必要な Fn フィールドだけ埋めて使う。未設定の呼び出しは panic で意図しない呼び出しを検出する。
type MockNewsRepo struct {
	ListPublishedFn                func(ctx context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error)
	GetPublishedByIDFn             func(ctx context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error)
	ListArticlesFn                 func(ctx context.Context, limit int) ([]domain.Article, error)
	GetArticleByIDFn               func(ctx context.Context, articleID string) (*domain.Article, error)
	ListTranslationsByArticleIDsFn func(ctx context.Context, articleIDs []string) ([]domain.Translation, error)
	InsertArticleWithTranslationFn func(ctx context.Context, article domain.Article, lang, title, summary, body string) (bool, error)
	PublishFn                      func(ctx context.Context, articleID string, reviewer string, now time.Time) error
	RejectFn                       func(ctx context.Context, articleID string, reviewer string, now time.Time) error
	UpsertTranslationFn            func(ctx context.Context, articleID string, lang, title, summary, body string) error
}

var (
	_ PublicNewsQuerier = (*MockNewsRepo)(nil)
	_ AdminNewsQuerier  = (*MockNewsRepo)(nil)
	_ NewsIngestWriter  = (*MockNewsRepo)(nil)
	_ NewsReviewWriter  = (*MockNewsRepo)(nil)
)

func (m *MockNewsRepo) ListPublished(ctx context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error) {
	if m.ListPublishedFn == nil {
		panic("MockNewsRepo.ListPublished called without Fn")
	}
	return m.ListPublishedFn(ctx, lang, limit)
}

func (m *MockNewsRepo) GetPublishedByID(ctx context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error) {
	if m.GetPublishedByIDFn == nil {
		panic("MockNewsRepo.GetPublishedByID called without Fn")
	}
	return m.GetPublishedByIDFn(ctx, articleID, lang)
}

func (m *MockNewsRepo) ListArticles(ctx context.Context, limit int) ([]domain.Article, error) {
	if m.ListArticlesFn == nil {
		panic("MockNewsRepo.ListArticles called without Fn")
	}
	return m.ListArticlesFn(ctx, limit)
}

func (m *MockNewsRepo) GetArticleByID(ctx context.Context, articleID string) (*domain.Article, error) {
	if m.GetArticleByIDFn == nil {
		panic("MockNewsRepo.GetArticleByID called without Fn")
	}
	return m.GetArticleByIDFn(ctx, articleID)
}

func (m *MockNewsRepo) ListTranslationsByArticleIDs(ctx context.Context, articleIDs []string) ([]domain.Translation, error) {
	if m.ListTranslationsByArticleIDsFn == nil {
		panic("MockNewsRepo.ListTranslationsByArticleIDs called without Fn")
	}
	return m.ListTranslationsByArticleIDsFn(ctx, articleIDs)
}

func (m *MockNewsRepo) InsertArticleWithTranslation(ctx context.Context, article domain.Article, lang, title, summary, body string) (bool, error) {
	if m.InsertArticleWithTranslationFn == nil {
		panic("MockNewsRepo.InsertArticleWithTranslation called without Fn")
	}
	return m.InsertArticleWithTranslationFn(ctx, article, lang, title, summary, body)
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
