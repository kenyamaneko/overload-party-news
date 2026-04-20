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

// 仕様 (FEATURE_SPEC §3.1): 必須フィールド欠け・translations 空・対応外 lang は ErrInvalidEventPayload。
// 正常時は repo.Insert が呼ばれ、translations 配列が引き渡される。
func TestInsert_仕様_イベントバリデーション(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(*apinews.ArticleCollectedEvent)
		wantErr       error
		wantCallCount int
	}{
		{name: "完全なイベント (ja 1 件)", mutate: func(_ *apinews.ArticleCollectedEvent) {}, wantErr: nil, wantCallCount: 1},
		{name: "ja + en の 2 件", mutate: func(e *apinews.ArticleCollectedEvent) {
			e.Translations = append(e.Translations, apinews.EventTranslation{Lang: apinews.LangEn, Title: "T", Summary: "S", Body: "B"})
		}, wantErr: nil, wantCallCount: 1},
		{name: "source_published_at null でも許容", mutate: func(e *apinews.ArticleCollectedEvent) { e.SourcePublishedAt = nil }, wantErr: nil, wantCallCount: 1},
		{name: "tags nil でも許容", mutate: func(e *apinews.ArticleCollectedEvent) { e.Tags = nil }, wantErr: nil, wantCallCount: 1},

		{name: "article_id 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.ArticleID = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "source 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.Source = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "source_url 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.SourceURL = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},

		{name: "translations 空", mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations = nil }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "translations[0].lang 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "translations[0].lang 対応外", mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = "fr" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "translations[0].title 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Title = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "translations[0].summary 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Summary = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
		{name: "translations[0].body 欠け", mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Body = "" }, wantErr: ingest.ErrInvalidEventPayload, wantCallCount: 0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			var gotArticle apinews.Article
			var gotTranslationsLen int
			repo := &port.MockNewsRepo{
				InsertFn: func(_ context.Context, article apinews.Article, translations []apinews.Translation) (bool, error) {
					callCount++
					gotArticle = article
					gotTranslationsLen = len(translations)
					return true, nil
				},
			}
			event := validEvent()
			tc.mutate(&event)

			_, err := ingest.New(repo).Insert(context.Background(), event)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
			// 呼ばれたケースのみ status=pending で INSERT されるのを確認
			assert.Equal(t, map[int]apinews.Status{0: "", 1: apinews.StatusPending}[callCount], gotArticle.Status)
			assert.Equal(t, map[int]int{0: 0, 1: len(event.Translations)}[callCount], gotTranslationsLen)
		})
	}
}

// 仕様: Insert は repo の (inserted, err) を透過。重複は inserted=false で正常。
func TestInsert_仕様_repoの結果を透過(t *testing.T) {
	otherErr := errors.New("db lost")

	cases := []struct {
		name         string
		repoInserted bool
		repoErr      error
		wantInserted bool
		wantErr      error
	}{
		{name: "新規挿入", repoInserted: true, repoErr: nil, wantInserted: true, wantErr: nil},
		{name: "重複は no-op", repoInserted: false, repoErr: nil, wantInserted: false, wantErr: nil},
		{name: "DB 障害を透過", repoInserted: false, repoErr: otherErr, wantInserted: false, wantErr: otherErr},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				InsertFn: func(_ context.Context, _ apinews.Article, _ []apinews.Translation) (bool, error) {
					return tc.repoInserted, tc.repoErr
				},
			}

			inserted, err := ingest.New(repo).Insert(context.Background(), validEvent())

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantInserted, inserted)
		})
	}
}

// 仕様: 生成される Article / Translation[] は event のフィールドを忠実に写す。
func TestInsert_仕様_イベントから変換される値(t *testing.T) {
	var gotArticle apinews.Article
	var gotTranslations []apinews.Translation
	repo := &port.MockNewsRepo{
		InsertFn: func(_ context.Context, a apinews.Article, ts []apinews.Translation) (bool, error) {
			gotArticle = a
			gotTranslations = ts
			return true, nil
		},
	}
	event := validEvent()
	event.Translations = append(event.Translations, apinews.EventTranslation{Lang: apinews.LangEn, Title: "TEn", Summary: "SEn", Body: "BEn"})

	_, err := ingest.New(repo).Insert(context.Background(), event)
	require.NoError(t, err)

	assert.Equal(t, event.ArticleID, gotArticle.ArticleID)
	assert.Equal(t, event.Source, gotArticle.Source)
	assert.Equal(t, event.SourceURL, gotArticle.SourceURL)
	assert.Equal(t, event.Tags, gotArticle.Tags)
	assert.Equal(t, event.SourcePublishedAt, gotArticle.SourcePublishedAt)
	assert.Equal(t, apinews.StatusPending, gotArticle.Status)

	require.Len(t, gotTranslations, 2)
	assert.Equal(t, apinews.LangJa, gotTranslations[0].Lang)
	assert.Equal(t, "タイトル", gotTranslations[0].Title)
	assert.Equal(t, apinews.LangEn, gotTranslations[1].Lang)
	assert.Equal(t, "TEn", gotTranslations[1].Title)
	// Translation の article_id は Article.ArticleID と一致する
	assert.Equal(t, event.ArticleID, gotTranslations[0].ArticleID)
	assert.Equal(t, event.ArticleID, gotTranslations[1].ArticleID)
}
