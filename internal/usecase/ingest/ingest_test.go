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

func TestInsert(t *testing.T) {
	t.Run("イベントの取込", func(t *testing.T) {
		validCases := []struct {
			name   string
			mutate func(*apinews.ArticleCollectedEvent)
		}{
			{
				name:   "完全なイベント (ja 1 件) のとき、記事と翻訳が INSERT される",
				mutate: func(_ *apinews.ArticleCollectedEvent) {},
			},
			{
				name:   "source_published_at が null でも、INSERT される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.SourcePublishedAt = nil },
			},
			{
				name:   "tags が nil でも、INSERT される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Tags = nil },
			},
			{
				name:   "article_id が 26 文字のとき、INSERT される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.ArticleID = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" },
			},
			{
				name:   "source が 20 文字のとき、INSERT される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Source = "12345678901234567890" },
			},
		}
		for _, tc := range validCases {
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

				require.NoError(t, err)
				assert.Equal(t, 1, articleCalls)
				assert.Equal(t, 1, transCalls)
			})
		}

		invalidCases := []struct {
			name   string
			mutate func(*apinews.ArticleCollectedEvent)
		}{
			{
				name: "翻訳が ja + en の 2 件のとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) {
					e.Translations = append(e.Translations, apinews.EventTranslation{Lang: domain.LangEn, Title: "T", Summary: "S", Body: "B"})
				},
			},
			{
				name:   "article_id 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.ArticleID = "" },
			},
			{
				name:   "article_id が 27 文字のとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.ArticleID = "ABCDEFGHIJKLMNOPQRSTUVWXYZ7" },
			},
			{
				name:   "source 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Source = "" },
			},
			{
				name:   "source が 21 文字のとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Source = "123456789012345678901" },
			},
			{
				name:   "source_url 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.SourceURL = "" },
			},
			{
				name:   "translations 空のとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations = nil },
			},
			{
				name:   "translations[0].lang が en (ja 以外) のとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = domain.LangEn },
			},
			{
				name:   "translations[0].lang 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Lang = "" },
			},
			{
				name:   "translations[0].title 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Title = "" },
			},
			{
				name:   "translations[0].summary 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Summary = "" },
			},
			{
				name:   "translations[0].body 欠けのとき、拒否される",
				mutate: func(e *apinews.ArticleCollectedEvent) { e.Translations[0].Body = "" },
			},
		}
		for _, tc := range invalidCases {
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

				assert.ErrorIs(t, err, ingest.ErrInvalidEventPayload)
				assert.Equal(t, 0, articleCalls)
				assert.Equal(t, 0, transCalls)
			})
		}

		dbErr := errors.New("db lost")
		propagationValidCases := []struct {
			name          string
			articleResult bool
			wantInserted  bool
		}{
			{
				name:          "記事が新規挿入のとき、inserted=true を返す",
				articleResult: true,
				wantInserted:  true,
			},
			{
				name:          "記事が重複 (記事・翻訳とも既存) のとき、inserted=false を返す",
				articleResult: false,
				wantInserted:  false,
			},
		}
		for _, tc := range propagationValidCases {
			t.Run(tc.name, func(t *testing.T) {
				repo := &port.MockNewsRepo{
					InsertArticleFn: func(_ context.Context, _ domain.Article) (bool, error) {
						return tc.articleResult, nil
					},
					InsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
						return nil
					},
				}

				inserted, err := ingest.New(repo).Insert(context.Background(), validEvent())

				require.NoError(t, err)
				assert.Equal(t, tc.wantInserted, inserted)
			})
		}

		propagationErrorCases := []struct {
			name          string
			articleResult bool
			articleErr    error
			transErr      error
		}{
			{
				name:       "記事 INSERT で DB 障害のとき、そのエラーが伝播する",
				articleErr: dbErr,
			},
			{
				name:          "翻訳 INSERT で DB 障害のとき、そのエラーが伝播する",
				articleResult: true,
				transErr:      dbErr,
			},
		}
		for _, tc := range propagationErrorCases {
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

				assert.ErrorIs(t, err, dbErr)
				assert.False(t, inserted)
			})
		}
	})
}
