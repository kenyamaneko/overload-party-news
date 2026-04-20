package ingest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/ingest"
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
			{Lang: apinews.LangJa, Title: "タイトル", Summary: "要約", Body: "本文"},
		},
	}
}

// 仕様 (FEATURE_SPEC §3.1): MVP では translations は ja 1 件ちょうど。それ以外は ErrInvalidEventPayload。
// バリデーション失敗時は記事・翻訳のいずれも repo を呼ばない。
func TestInsert_仕様_イベントバリデーション(t *testing.T) {
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
			wantErr:              nil,
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name:                 "source_published_at null でも許容",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.SourcePublishedAt = nil },
			wantErr:              nil,
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name:                 "tags nil でも許容",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Tags = nil },
			wantErr:              nil,
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name: "ja + en の 2 件は MVP 仕様違反で拒否",
			mutate: func(e *apinews.ArticleCollectedEvent) {
				e.Translations = append(e.Translations, apinews.EventTranslation{Lang: apinews.LangEn, Title: "T", Summary: "S", Body: "B"})
			},
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "article_id 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.ArticleID = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "source 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Source = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "source_url 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.SourceURL = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "translations 空",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Translations = nil },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "translations[0].lang が en (ja 以外) は拒否",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = apinews.LangEn },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "translations[0].lang 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "translations[0].title 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Title = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "translations[0].summary 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Summary = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "translations[0].body 欠け",
			mutate:               func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Body = "" },
			wantErr:              ingest.ErrInvalidEventPayload,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var articleCalls, transCalls int
			var gotArticle apinews.Article
			repo := &port.MockNewsRepo{
				InsertArticleFn: func(_ context.Context, article apinews.Article) (bool, error) {
					articleCalls++
					gotArticle = article
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
			// 呼ばれたケースのみ status=pending で INSERT されるのを確認
			assert.Equal(t, map[int]apinews.Status{0: "", 1: apinews.StatusPending}[articleCalls], gotArticle.Status)
		})
	}
}

// 仕様: Insert は InsertArticle の inserted を透過。翻訳 INSERT は副次操作で inserted には影響しない。
func TestInsert_仕様_記事の結果を透過(t *testing.T) {
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
			wantErr:       nil,
		},
		{
			name:          "重複は no-op (記事・翻訳とも既存)",
			articleResult: false,
			wantInserted:  false,
			wantErr:       nil,
		},
		{
			name:          "記事 INSERT で DB 障害",
			articleResult: false,
			articleErr:    dbErr,
			wantInserted:  false,
			wantErr:       dbErr,
		},
		{
			name:          "翻訳 INSERT で DB 障害",
			articleResult: true,
			transErr:      dbErr,
			wantInserted:  false,
			wantErr:       dbErr,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				InsertArticleFn: func(_ context.Context, _ apinews.Article) (bool, error) {
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

// 仕様: event の全フィールドが InsertArticle / InsertTranslation に忠実に渡される。
func TestInsert_仕様_イベントから変換される値(t *testing.T) {
	var gotArticle apinews.Article
	var gotArticleID, gotLang, gotTitle, gotSummary, gotBody string
	repo := &port.MockNewsRepo{
		InsertArticleFn: func(_ context.Context, a apinews.Article) (bool, error) {
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

	// Article
	assert.Equal(t, event.ArticleID, gotArticle.ArticleID)
	assert.Equal(t, event.Source, gotArticle.Source)
	assert.Equal(t, event.SourceURL, gotArticle.SourceURL)
	assert.Equal(t, event.Tags, gotArticle.Tags)
	assert.Equal(t, event.SourcePublishedAt, gotArticle.SourcePublishedAt)
	assert.Equal(t, apinews.StatusPending, gotArticle.Status)

	// ja Translation
	assert.Equal(t, event.ArticleID, gotArticleID)
	assert.Equal(t, apinews.LangJa, gotLang)
	assert.Equal(t, "タイトル", gotTitle)
	assert.Equal(t, "要約", gotSummary)
	assert.Equal(t, "本文", gotBody)
}
