package ingest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func validEvent() apinews.ArticleCollectedEvent {
	pub := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	return apinews.ArticleCollectedEvent{
		ArticleID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Source:            "aws",
		SourceURL:         "https://aws.amazon.com/blogs/aws/foo",
		Tags:              []string{"compute"},
		SourcePublishedAt: &pub,
		Translations: []apinews.EventTranslation{
			{Lang: domain.LangJa, Title: "タイトル", Summary: "要約", Body: "本文"},
		},
	}
}

func TestInsert_Validation(t *testing.T) {
	cases := []struct {
		name                 string
		mutate               func(*apinews.ArticleCollectedEvent)
		wantErr              error
		wantArticleCallCount int
		wantTransCallCount   int
	}{
		{
			name:                 "完全なイベント (ja 1 件)",
			mutate:               func(_ *apinews.ArticleCollectedEvent) {},
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name:                 "source_published_at null でも許容",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.SourcePublishedAt = nil },
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name:                 "tags nil でも許容",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Tags = nil },
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name: "ja + en の 2 件は MVP 仕様違反で拒否",
			mutate: func(e *apinews.ArticleCollectedEvent) {
				e.Translations = append(e.Translations, apinews.EventTranslation{Lang: domain.LangEn, Title: "T", Summary: "S", Body: "B"})
			},
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "article_id 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.ArticleID = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "source 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Source = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "source_url 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.SourceURL = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "translations 空",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Translations = nil },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "translations[0].lang が en (ja 以外) は拒否",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = domain.LangEn },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "translations[0].lang 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "translations[0].title 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Title = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "translations[0].summary 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Summary = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
		{
			name:    "translations[0].body 欠け",
			mutate:  func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Body = "" },
			wantErr: ingest.ErrInvalidEventPayload,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var articleCalls, transCalls int
			repo := &port.MockNewsRepo{
				InsertArticleFn: func(_ context.Context, _ domain.Article) (bool, error) {
					articleCalls++
					return true, nil
				},
				InsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
					transCalls++
					return nil
				},
			}
			event := validEvent()
			tc.mutate(&event)

			_, err := ingest.New(repo).Insert(context.Background(), event)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantArticleCallCount, articleCalls)
			assert.Equal(t, tc.wantTransCallCount, transCalls)
		})
	}
}

func TestInsert_RepoResultPropagation(t *testing.T) {
	dbErr := errors.New("db lost")

	cases := []struct {
		name          string
		articleResult bool
		articleErr    error
		transErr      error
		wantInserted  bool
		wantErr       error
	}{
		{
			name:          "新規挿入",
			articleResult: true,
			wantInserted:  true,
		},
		{
			name:          "重複は no-op (記事・翻訳とも既存)",
			articleResult: false,
			wantInserted:  false,
		},
		{
			name:       "記事 INSERT で DB 障害",
			articleErr: dbErr,
			wantErr:    dbErr,
		},
		{
			name:          "翻訳 INSERT で DB 障害",
			articleResult: true,
			transErr:      dbErr,
			wantErr:       dbErr,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				InsertArticleFn: func(_ context.Context, _ domain.Article) (bool, error) {
					return tc.articleResult, tc.articleErr
				},
				InsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
					return tc.transErr
				},
			}

			inserted, err := ingest.New(repo).Insert(context.Background(), validEvent())

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantInserted, inserted)
		})
	}
}

// TestInsert_EventToRepoMapping は ArticleCollectedEvent の各フィールドが
// repo 層 (InsertArticle / InsertTranslation) の引数に正しいフィールドへ
// マッピングされることを担保する (フィールドの取り違え検出)。
func TestInsert_EventToRepoMapping(t *testing.T) {
	var gotArticle domain.Article
	var gotArticleID, gotLang, gotTitle, gotSummary, gotBody string
	repo := &port.MockNewsRepo{
		InsertArticleFn: func(_ context.Context, a domain.Article) (bool, error) {
			gotArticle = a
			return true, nil
		},
		InsertTranslationFn: func(_ context.Context, articleID, lang, title, summary, body string) error {
			gotArticleID = articleID
			gotLang = lang
			gotTitle = title
			gotSummary = summary
			gotBody = body
			return nil
		},
	}
	event := validEvent()

	_, err := ingest.New(repo).Insert(context.Background(), event)
	require.NoError(t, err)

	// Article (status は永続化されず派生するため検証対象外)
	assert.Equal(t, event.ArticleID, gotArticle.ArticleID)
	assert.Equal(t, event.Source, gotArticle.Source)
	assert.Equal(t, event.SourceURL, gotArticle.SourceURL)
	assert.Equal(t, event.Tags, gotArticle.Tags)
	assert.Equal(t, event.SourcePublishedAt, gotArticle.SourcePublishedAt)

	// ja Translation
	assert.Equal(t, event.ArticleID, gotArticleID)
	assert.Equal(t, domain.LangJa, gotLang)
	assert.Equal(t, "タイトル", gotTitle)
	assert.Equal(t, "要約", gotSummary)
	assert.Equal(t, "本文", gotBody)
}
