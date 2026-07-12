package news_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// validLimit は境界を狙わない「任意の有効な limit」を表す。
const validLimit = 10

func TestList(t *testing.T) {
	t.Run("公開記事一覧の取得", func(t *testing.T) {
		pub := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		rows := []domain.PublishedArticleSummary{
			{ArticleID: "a1", Source: string(apinews.SourceAws), Title: "T1", Summary: "S1", Tags: []string{"x"}, PublishedAt: pub},
			{ArticleID: "a2", Source: string(apinews.SourceGoogleCloud), Title: "T2", Summary: "S2", Tags: []string{"y"}, PublishedAt: pub},
		}

		validCases := []struct {
			name  string
			lang  string
			limit int
		}{
			{
				name:  "lang=ja + limit=1 (下限) のとき、querier へ lang/limit を転送し、返った行を射影して返す",
				lang:  domain.LangJa,
				limit: 1,
			},
			{
				name:  "lang=ja + limit が上限のとき、querier へ lang/limit を転送し、返った行を射影して返す",
				lang:  domain.LangJa,
				limit: news.ListLimitMax,
			},
			{
				name:  "lang=en のとき、querier へ lang/limit を転送し、返った行を射影して返す",
				lang:  domain.LangEn,
				limit: validLimit,
			},
		}
		for _, tc := range validCases {
			t.Run(tc.name, func(t *testing.T) {
				var gotLang string
				var gotLimit int
				repo := &port.MockNewsRepo{
					ListPublishedFn: func(_ context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error) {
						gotLang, gotLimit = lang, limit
						return rows, nil
					},
				}

				got, err := news.New(repo).List(context.Background(), tc.lang, tc.limit)

				require.NoError(t, err)
				assert.Equal(t, tc.lang, gotLang)
				assert.Equal(t, tc.limit, gotLimit)
				assert.Len(t, got, len(rows))
				assert.Equal(t, "a1", got[0].ArticleID)
				assert.Equal(t, "T1", got[0].Title)
				assert.Equal(t, apinews.SourceAws, got[0].Source)
				assert.Equal(t, "a2", got[1].ArticleID)
				assert.Equal(t, apinews.SourceGoogleCloud, got[1].Source)
			})
		}

		invalidCases := []struct {
			name    string
			lang    string
			limit   int
			wantErr error
		}{
			{
				name:    "lang 未指定のとき、querier を呼ばず ErrLangRequired になる",
				lang:    "",
				limit:   news.ListLimitMax,
				wantErr: news.ErrLangRequired,
			},
			{
				name:    "lang 対応外のとき、querier を呼ばず ErrUnsupportedLang になる",
				lang:    "fr",
				limit:   news.ListLimitMax,
				wantErr: news.ErrUnsupportedLang,
			},
			{
				name:    "lang=JA (大文字) のとき、querier を呼ばず ErrUnsupportedLang になる",
				lang:    "JA",
				limit:   news.ListLimitMax,
				wantErr: news.ErrUnsupportedLang,
			},
			{
				name:    "limit=0 のとき、querier を呼ばず ErrInvalidLimit になる",
				lang:    domain.LangJa,
				limit:   0,
				wantErr: news.ErrInvalidLimit,
			},
			{
				name:    "limit=-1 (負値) のとき、querier を呼ばず ErrInvalidLimit になる",
				lang:    domain.LangJa,
				limit:   -1,
				wantErr: news.ErrInvalidLimit,
			},
			{
				name:    "limit が上限+1 のとき、querier を呼ばず ErrInvalidLimit になる",
				lang:    domain.LangJa,
				limit:   news.ListLimitMax + 1,
				wantErr: news.ErrInvalidLimit,
			},
		}
		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				// ListPublishedFn を未設定にすることで、入力検証を抜けて querier に到達したら
				// MockNewsRepo が panic する = querier 未到達を強制する。
				repo := &port.MockNewsRepo{}

				got, err := news.New(repo).List(context.Background(), tc.lang, tc.limit)

				assert.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, got)
			})
		}

		t.Run("querier がエラーを返すとき、List はそのままエラーを伝播する", func(t *testing.T) {
			wantErr := errors.New("querier: db connection lost")
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]domain.PublishedArticleSummary, error) {
					return nil, wantErr
				},
			}

			got, err := news.New(repo).List(context.Background(), domain.LangJa, validLimit)

			assert.ErrorIs(t, err, wantErr)
			assert.Nil(t, got)
		})
	})
}

func TestGetDetail(t *testing.T) {
	t.Run("公開記事詳細の取得", func(t *testing.T) {
		dbErr := errors.New("db connection lost")
		pub := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		repoSuccess := &domain.PublishedArticleDetail{
			ArticleID: "abc", Source: "aws", Title: "T", Summary: "S",
			Body: "B", Tags: []string{"x"}, SourceURL: "https://example.com/abc",
			PublishedAt: pub,
		}
		wantSuccess := &apinews.NewsDetail{
			ArticleID: "abc", Source: apinews.SourceAws, Title: "T", Summary: "S",
			Body: "B", Tags: []string{"x"}, SourceURL: "https://example.com/abc",
			PublishedAt: pub,
		}

		cases := []struct {
			name        string
			lang        string
			repoReturn  *domain.PublishedArticleDetail
			repoErr     error
			wantDetail  *apinews.NewsDetail
			wantErr     error
			wantRepoHit bool
		}{
			{
				name:        "成功時は domain DTO を apinews.NewsDetail に射影して返す",
				lang:        domain.LangJa,
				repoReturn:  repoSuccess,
				wantDetail:  wantSuccess,
				wantRepoHit: true,
			},
			{
				name:        "repo が ErrNotFound のとき、そのまま返る",
				lang:        domain.LangJa,
				repoErr:     port.ErrNotFound,
				wantErr:     port.ErrNotFound,
				wantRepoHit: true,
			},
			{
				name:        "repo が DB 障害のとき、そのまま返る",
				lang:        domain.LangJa,
				repoErr:     dbErr,
				wantErr:     dbErr,
				wantRepoHit: true,
			},
			{
				name:        "lang 未指定のとき、repo を呼ばず ErrLangRequired になる",
				lang:        "",
				wantErr:     news.ErrLangRequired,
				wantRepoHit: false,
			},
			{
				name:        "lang 対応外のとき、repo を呼ばず ErrUnsupportedLang になる",
				lang:        "fr",
				wantErr:     news.ErrUnsupportedLang,
				wantRepoHit: false,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var repoCalled bool
				repo := &port.MockNewsRepo{
					GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*domain.PublishedArticleDetail, error) {
						repoCalled = true
						return tc.repoReturn, tc.repoErr
					},
				}
				got, err := news.New(repo).GetDetail(context.Background(), "abc", tc.lang)

				assert.ErrorIs(t, err, tc.wantErr)
				assert.Equal(t, tc.wantDetail, got)
				assert.Equal(t, tc.wantRepoHit, repoCalled)
			})
		}
	})
}
