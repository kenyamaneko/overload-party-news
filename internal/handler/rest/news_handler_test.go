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
	t.Run("公開記事一覧API", func(t *testing.T) {
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
			wantErrHas   string
		}{
			{
				name:         "lang=ja + limit=10で0件のとき、200と空配列を返す",
				query:        "?lang=ja&limit=10",
				repoReturn:   nil,
				wantStatus:   http.StatusOK,
				wantArticles: []apinews.NewsListItem{},
				wantLang:     "ja",
			},
			{
				name:         "lang=en + limit=10で1件のとき、200と当該記事を返す",
				query:        "?lang=en&limit=10",
				repoReturn:   successRows,
				wantStatus:   http.StatusOK,
				wantArticles: []apinews.NewsListItem{{ArticleID: "01A", Source: apinews.SourceAws, Title: "T", Summary: "S", Tags: []string{"compute"}, PublishedAt: pub}},
				wantLang:     "en",
			},
			{
				name:       "lang未指定のとき、400 + lang必須のエラーメッセージを返す",
				query:      "?limit=10",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "lang is required",
			},
			{
				name:       "limit未指定のとき、400 + limit不正のエラーメッセージを返す (デフォルト値へフォールバックしない)",
				query:      "?lang=ja",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "invalid limit",
			},
			{
				name:       "クエリ全未指定のとき、400 + limit不正のエラーメッセージを返す",
				query:      "",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "invalid limit",
			},
			{
				name:       "lang対応外のとき、400 + lang非対応のエラーメッセージを返す",
				query:      "?lang=fr&limit=10",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "unsupported lang",
			},
			{
				name:       "limit=0のとき、400 + limit不正のエラーメッセージを返す",
				query:      "?lang=ja&limit=0",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "invalid limit",
			},
			{
				name:       "limit=101 (上限超過)のとき、400 + limit不正のエラーメッセージを返す",
				query:      "?lang=ja&limit=101",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "invalid limit",
			},
			{
				name:       "limitが非整数のとき、400 + limit不正のエラーメッセージを返す",
				query:      "?lang=ja&limit=abc",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "invalid limit",
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

				var resp apinews.NewsListResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				// wantArticles が空配列のケースは nil でないこと (0 件でも null を返さない仕様) も検証する。
				assert.Equal(t, tc.wantArticles, resp.Articles)

				var errBody struct {
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errBody))
				assert.Contains(t, errBody.Error, tc.wantErrHas)
			})
		}
	})
}

func TestGetDetail(t *testing.T) {
	t.Run("公開記事詳細API", func(t *testing.T) {
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
			wantBody   apinews.NewsDetail
			wantLang   string
			wantErrHas string
		}{
			{
				name:       "存在する記事のとき、body / source_urlを含むJSONを返す",
				query:      "?lang=ja",
				repoReturn: successRow,
				wantStatus: http.StatusOK,
				wantBody:   wantSuccess,
				wantLang:   "ja",
			},
			{
				name:       "not foundのとき、404 + 記事が見つからないエラーメッセージを返す",
				query:      "?lang=ja",
				repoErr:    port.ErrNotFound,
				wantStatus: http.StatusNotFound,
				wantLang:   "ja",
				wantErrHas: "article not found",
			},
			{
				name:       "その他エラーのとき、500 + repoのエラーメッセージを返す",
				query:      "?lang=ja",
				repoErr:    otherErr,
				wantStatus: http.StatusInternalServerError,
				wantLang:   "ja",
				wantErrHas: "db lost",
			},
			{
				name:       "lang未指定のとき、400 + lang必須のエラーメッセージを返す",
				query:      "",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "lang is required",
			},
			{
				name:       "lang対応外のとき、400 + lang非対応のエラーメッセージを返す",
				query:      "?lang=fr",
				wantStatus: http.StatusBadRequest,
				wantErrHas: "unsupported lang",
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

				var got apinews.NewsDetail
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.Equal(t, tc.wantBody, got)

				var errBody struct {
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errBody))
				assert.Contains(t, errBody.Error, tc.wantErrHas)
			})
		}
	})
}
