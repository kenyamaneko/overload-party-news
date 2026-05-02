package news_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func TestList(t *testing.T) {
	cases := []struct {
		name      string
		lang      string
		limit     int
		wantErr   error
		wantCount int
	}{
		{
			name:      "lang=ja + limit=1 (下限) で 1 件",
			lang:      domain.LangJa,
			limit:     1,
			wantCount: 1,
		},
		{
			name:      "lang=ja + limit=上限 で上限件数",
			lang:      domain.LangJa,
			limit:     news.ListLimitMax,
			wantCount: news.ListLimitMax,
		},
		{
			name:    "lang 未指定は ErrLangRequired",
			lang:    "",
			limit:   news.ListLimitMax,
			wantErr: news.ErrLangRequired,
		},
		{
			name:    "lang 対応外は ErrUnsupportedLang",
			lang:    "fr",
			limit:   news.ListLimitMax,
			wantErr: news.ErrUnsupportedLang,
		},
		{
			name:    "limit=0 は ErrInvalidLimit",
			lang:    domain.LangJa,
			limit:   0,
			wantErr: news.ErrInvalidLimit,
		},
		{
			name:    "limit 負値は ErrInvalidLimit",
			lang:    domain.LangJa,
			limit:   -1,
			wantErr: news.ErrInvalidLimit,
		},
		{
			name:    "limit 上限超過は ErrInvalidLimit",
			lang:    domain.LangJa,
			limit:   news.ListLimitMax + 1,
			wantErr: news.ErrInvalidLimit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, limit int) ([]domain.PublishedArticleSummary, error) {
					return make([]domain.PublishedArticleSummary, limit), nil
				},
			}
			got, err := news.New(repo).List(context.Background(), tc.lang, tc.limit)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Len(t, got, tc.wantCount)
		})
	}
}

func TestGetDetail(t *testing.T) {
	dbErr := errors.New("db connection lost")
	pub := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	repoSuccess := &domain.PublishedArticleDetail{
		ArticleID: "abc", Source: "aws", Title: "T", Summary: "S",
		Body: "B", Tags: []string{"x"}, SourceURL: "https://example.com/abc",
		PublishedAt: pub,
	}
	wantSuccess := &apinews.NewsDetail{
		ArticleID: "abc", Source: "aws", Title: "T", Summary: "S",
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
			name:        "repo の ErrNotFound がそのまま返る",
			lang:        domain.LangJa,
			repoErr:     port.ErrNotFound,
			wantErr:     port.ErrNotFound,
			wantRepoHit: true,
		},
		{
			name:        "repo の DB 障害がそのまま返る",
			lang:        domain.LangJa,
			repoErr:     dbErr,
			wantErr:     dbErr,
			wantRepoHit: true,
		},
		{
			name:        "lang 未指定は repo を呼ばず ErrLangRequired",
			lang:        "",
			wantErr:     news.ErrLangRequired,
			wantRepoHit: false,
		},
		{
			name:        "lang 対応外は repo を呼ばず ErrUnsupportedLang",
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
}
