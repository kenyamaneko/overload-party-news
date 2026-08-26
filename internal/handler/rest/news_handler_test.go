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
)

func newNewsEngine(repo *port.MockNewsRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	uc := news.New(repo)
	h := rest.NewNewsHandler(uc)
	r := gin.New()
	r.GET("/api/v1/news", h.List)
	r.GET("/api/v1/news/:articleId", h.GetDetail)
	return r
}

func doGet(t *testing.T, r *gin.Engine, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestNewsHandlerList(t *testing.T) {
	t.Run("ニュース一覧の取得", func(t *testing.T) {
		t.Run("langにja、limitに10を指定し、取得元が0件を返すとき、200と空配列のarticlesを返す(nullにならない)", func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]domain.PublishedArticleSummary, error) {
					return []domain.PublishedArticleSummary{}, nil
				},
			}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news?lang=ja&limit=10")

			require.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, `{"articles":[]}`, w.Body.String())
		})

		t.Run("langにen、limitに10を指定し、取得元が記事を1件返すとき、200を返し、応答のarticlesにその記事の項目が含まれる", func(t *testing.T) {
			published := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error) {
					assert.Equal(t, "en", lang)
					assert.Equal(t, 10, limit)
					return []domain.PublishedArticleSummary{
						{
							ArticleID:   "article-list-001",
							Source:      "aws",
							Title:       "list title",
							Summary:     "list summary",
							Tags:        []string{"tag1"},
							PublishedAt: published,
						},
					}, nil
				},
			}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news?lang=en&limit=10")

			require.Equal(t, http.StatusOK, w.Code)
			var body struct {
				Articles []map[string]any `json:"articles"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Len(t, body.Articles, 1)
			item := body.Articles[0]
			assert.Equal(t, "article-list-001", item["article_id"])
			assert.Equal(t, "aws", item["source"])
			assert.Equal(t, "list title", item["title"])
			assert.Equal(t, "list summary", item["summary"])
			assert.Contains(t, item, "tags")
			assert.Contains(t, item, "published_at")
			assert.NotContains(t, item, "body")
			assert.NotContains(t, item, "source_url")
		})

		t.Run("取得元がエラーを返すとき、500と、そのエラーの内容を含む応答を返す", func(t *testing.T) {
			repoErr := errors.New("repository unavailable")
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]domain.PublishedArticleSummary, error) {
					return nil, repoErr
				},
			}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news?lang=ja&limit=10")

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), repoErr.Error())
		})

		t.Run("400になるクエリパラメータの組み合わせ", func(t *testing.T) {
			tests := []struct {
				name           string
				query          string
				wantErrContain string
			}{
				{
					name:           "limitに有効な値を指定した状態で、langを指定しないとき",
					query:          "limit=10",
					wantErrContain: "lang is required",
				},
				{
					name:           "limitに有効な値を指定した状態で、langに対応外の値(fr)を指定したとき",
					query:          "lang=fr&limit=10",
					wantErrContain: "unsupported lang",
				},
				{
					name:           "limitを指定しないとき(langは指定あり)",
					query:          "lang=ja",
					wantErrContain: "invalid limit",
				},
				{
					name:           "limitに整数でない値(abc)を指定したとき(langは指定あり)",
					query:          "lang=ja&limit=abc",
					wantErrContain: "invalid limit",
				},
				{
					name:           "langに有効な値を指定した状態で、limitに0を指定したとき",
					query:          "lang=ja&limit=0",
					wantErrContain: "invalid limit",
				},
				{
					name:           "langに有効な値を指定した状態で、limitに101(許容上限100の1つ上)を指定したとき",
					query:          "lang=ja&limit=101",
					wantErrContain: "invalid limit",
				},
				{
					name:           "langを指定せず、limitに0を指定したとき",
					query:          "limit=0",
					wantErrContain: "lang is required",
				},
				{
					name:           "lang・limitのいずれも指定しないとき",
					query:          "",
					wantErrContain: "invalid limit",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name+"、400と、応答本文に「"+tt.wantErrContain+"」を含む内容を返す", func(t *testing.T) {
					repo := &port.MockNewsRepo{}
					r := newNewsEngine(repo)

					target := "/api/v1/news"
					if tt.query != "" {
						target += "?" + tt.query
					}
					w := doGet(t, r, target)

					assert.Equal(t, http.StatusBadRequest, w.Code)
					assert.Contains(t, w.Body.String(), tt.wantErrContain)
				})
			}
		})
	})
}

func TestNewsHandlerGetDetail(t *testing.T) {
	t.Run("ニュース詳細の取得", func(t *testing.T) {
		t.Run("指定した記事が存在し取得できるとき、200を返し、応答に詳細項目が含まれる", func(t *testing.T) {
			published := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error) {
					assert.Equal(t, "article-detail-001", articleID)
					assert.Equal(t, "ja", lang)
					return &domain.PublishedArticleDetail{
						ArticleID:   "article-detail-001",
						Source:      "aws",
						Title:       "detail title",
						Summary:     "detail summary",
						Body:        "detail body",
						Tags:        []string{"tag1"},
						SourceURL:   "https://example.com/source",
						PublishedAt: published,
					}, nil
				},
			}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news/article-detail-001?lang=ja")

			require.Equal(t, http.StatusOK, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "article-detail-001", body["article_id"])
			assert.Equal(t, "aws", body["source"])
			assert.Equal(t, "detail title", body["title"])
			assert.Equal(t, "detail summary", body["summary"])
			assert.Equal(t, "detail body", body["body"])
			assert.Contains(t, body, "tags")
			assert.Equal(t, "https://example.com/source", body["source_url"])
			assert.Contains(t, body, "published_at")
		})

		t.Run("langを指定しないとき、400と、応答本文にlang is requiredを含む内容を返す", func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news/article-detail-001")

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "lang is required")
		})

		t.Run("langに対応外の値を指定したとき、400と、応答本文にunsupported langを含む内容を返す", func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news/article-detail-001?lang=fr")

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "unsupported lang")
		})

		t.Run("指定した記事が見つからないとき、404と、応答本文にarticle not foundを含む内容を返す", func(t *testing.T) {
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*domain.PublishedArticleDetail, error) {
					return nil, port.ErrNotFound
				},
			}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news/article-missing?lang=ja")

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Contains(t, w.Body.String(), "article not found")
		})

		t.Run("取得元がエラーを返すとき、500と、そのエラーの内容を含む応答を返す", func(t *testing.T) {
			repoErr := errors.New("repository unavailable")
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*domain.PublishedArticleDetail, error) {
					return nil, repoErr
				},
			}
			r := newNewsEngine(repo)

			w := doGet(t, r, "/api/v1/news/article-detail-001?lang=ja")

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), repoErr.Error())
		})
	})
}
