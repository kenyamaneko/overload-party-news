package port

import (
	"context"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

// MockNewsRepo は port の 2 インタフェースをすべて実装する 1 個のモック。
// 必要な Fn フィールドだけ埋めて使う。未設定の呼び出しは panic で意図しない呼び出しを検出する。
type MockNewsRepo struct {
	ListPublishedFn                func(ctx context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error)
	GetPublishedByIDFn             func(ctx context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error)
	InsertArticleWithTranslationFn func(ctx context.Context, article domain.Article, lang, title, summary, body string) (bool, error)
}

var (
	_ PublicNewsQuerier = (*MockNewsRepo)(nil)
	_ NewsIngestWriter  = (*MockNewsRepo)(nil)
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

func (m *MockNewsRepo) InsertArticleWithTranslation(ctx context.Context, article domain.Article, lang, title, summary, body string) (bool, error) {
	if m.InsertArticleWithTranslationFn == nil {
		panic("MockNewsRepo.InsertArticleWithTranslation called without Fn")
	}
	return m.InsertArticleWithTranslationFn(ctx, article, lang, title, summary, body)
}
