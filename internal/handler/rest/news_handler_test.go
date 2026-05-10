package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func newEngine(h *rest.NewsHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/news", h.List)
	r.GET("/api/v1/news/:articleId", h.GetDetail)
	return r
}

func TestList(t *testing.T) {
	pub := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	successRows := []domain.PublishedArticleSummary{
		{ArticleID: "01A", Source: "aws", Title: "T", Summary: "S", Tags: []string{"compute"}, PublishedAt: pub},
	}

	cases := []struct {
		name         string
		query        string
		repoReturn   []domain.PublishedArticleSummary
		wantStatus   int
		wantArticles []apinews.NewsListItem
		wantLang     string
	}{
		{
			name:         "lang=ja + limit=10 で 200 (空配列)",
			query:        "?lang=ja&limit=10",
			repoReturn:   nil,
			wantStatus:   http.StatusOK,
			wantArticles: []apinews.NewsListItem{},
			wantLang:     "ja",
		},
		{
			name:         "lang=en + limit=10 で 200 (1 件)",
			query:        "?lang=en&limit=10",
			repoReturn:   successRows,
			wantStatus:   http.StatusOK,
			wantArticles: []apinews.NewsListItem{{ArticleID: "01A", Source: apinews.SourceAws, Title: "T", Summary: "S", Tags: []string{"compute"}, PublishedAt: pub}},
			wantLang:     "en",
		},
		{
			name:       "lang 未指定は 400",
			query:      "?limit=10",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit 未指定は 400 (デフォルト値へのフォールバックは行わない)",
			query:      "?lang=ja",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "クエリ全未指定は 400",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "lang 対応外は 400",
			query:      "?lang=fr&limit=10",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit=0 は 400",
			query:      "?lang=ja&limit=0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit 上限超過は 400",
			query:      "?lang=ja&limit=101",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit 非整数は 400",
			query:      "?lang=ja&limit=abc",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLang string
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, lang string, _ int) ([]domain.PublishedArticleSummary, error) {
					gotLang = lang
					return tc.repoReturn, nil
				},
			}
			h := rest.NewNewsHandler(news.New(repo))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/news"+tc.query, nil)
			w := httptest.NewRecorder()
			newEngine(h).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantLang, gotLang)

			if tc.wantStatus != http.StatusOK {
				return
			}
			var resp apinews.NewsListResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.NotNil(t, resp.Articles, "0 件でも nil ではなく空配列")
			assert.Equal(t, tc.wantArticles, resp.Articles)
		})
	}
}

func TestGetDetail(t *testing.T) {
	otherErr := errors.New("db lost")
	pub := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	successRow := &domain.PublishedArticleDetail{
		ArticleID: "01", Source: domain.SourceOci, Title: "T", Summary: "S",
		Body: "本文", Tags: []string{"ai"},
		SourceURL:   "https://example.com/a",
		PublishedAt: pub,
	}
	wantSuccess := apinews.NewsDetail{
		ArticleID: "01", Source: apinews.SourceOci, Title: "T", Summary: "S",
		Body: "本文", Tags: []string{"ai"},
		SourceURL:   "https://example.com/a",
		PublishedAt: pub,
	}

	cases := []struct {
		name       string
		query      string
		repoReturn *domain.PublishedArticleDetail
		repoErr    error
		wantStatus int
		wantBody   *apinews.NewsDetail
		wantLang   string
	}{
		{
			name:       "成功時は body / source_url を含む JSON を返す",
			query:      "?lang=ja",
			repoReturn: successRow,
			wantStatus: http.StatusOK,
			wantBody:   &wantSuccess,
			wantLang:   "ja",
		},
		{
			name:       "not found は 404",
			query:      "?lang=ja",
			repoErr:    port.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantLang:   "ja",
		},
		{
			name:       "その他エラーは 500",
			query:      "?lang=ja",
			repoErr:    otherErr,
			wantStatus: http.StatusInternalServerError,
			wantLang:   "ja",
		},
		{
			name:       "lang 未指定は 400",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "lang 対応外は 400",
			query:      "?lang=fr",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLang string
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, lang string) (*domain.PublishedArticleDetail, error) {
					gotLang = lang
					return tc.repoReturn, tc.repoErr
				},
			}
			h := rest.NewNewsHandler(news.New(repo))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/news/01"+tc.query, nil)
			w := httptest.NewRecorder()
			newEngine(h).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantLang, gotLang)

			if tc.wantBody == nil {
				return
			}
			var got apinews.NewsDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, *tc.wantBody, got)
		})
	}
}
