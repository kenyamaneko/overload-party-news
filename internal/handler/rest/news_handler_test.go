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

	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func newEngine(h *rest.NewsHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/internal/v1/news", h.List)
	r.GET("/internal/v1/news/:articleId", h.GetDetail)
	return r
}

// 仕様 (API_REFERENCE): GET /internal/v1/news は lang 必須 + limit バリデーションで 200 / 400 を返す。
func TestList_仕様_クエリバリデーション(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{
			name:       "lang=ja 未 limit",
			query:      "?lang=ja",
			wantStatus: http.StatusOK,
		},
		{
			name:       "lang=en 未 limit",
			query:      "?lang=en",
			wantStatus: http.StatusOK,
		},
		{
			name:       "lang + limit 指定",
			query:      "?lang=ja&limit=10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "lang 未指定は 400",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "lang 未指定 (limit のみ) も 400",
			query:      "?limit=10",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "lang 対応外は 400",
			query:      "?lang=fr",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit 下限未満は 400",
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]apinews.NewsListItem, error) {
					return []apinews.NewsListItem{}, nil
				},
			}
			h := rest.NewNewsHandler(news.New(repo))

			req := httptest.NewRequest(http.MethodGet, "/internal/v1/news"+tc.query, nil)
			w := httptest.NewRecorder()
			newEngine(h).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

// 仕様: List レスポンスは {"articles": [...]} 形式、0 件でも nil ではなく空配列。
func TestList_仕様_空配列(t *testing.T) {
	repo := &port.MockNewsRepo{
		ListPublishedFn: func(_ context.Context, _ string, _ int) ([]apinews.NewsListItem, error) {
			return nil, nil
		},
	}
	h := rest.NewNewsHandler(news.New(repo))

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/news?lang=ja", nil)
	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp apinews.NewsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Articles)
	assert.Empty(t, resp.Articles)
}

// 仕様: List は repo の結果を JSON 化して返す (lang は指定通り repo に渡る)。
func TestList_仕様_レスポンス形(t *testing.T) {
	pub := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	items := []apinews.NewsListItem{
		{ArticleID: "01A", Source: "aws", Title: "T", Summary: "S", Tags: []string{"compute"}, PublishedAt: pub},
	}
	var gotLang string
	repo := &port.MockNewsRepo{
		ListPublishedFn: func(_ context.Context, lang string, _ int) ([]apinews.NewsListItem, error) {
			gotLang = lang
			return items, nil
		},
	}
	h := rest.NewNewsHandler(news.New(repo))

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/news?lang=en", nil)
	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "en", gotLang)

	var resp apinews.NewsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Articles, 1)
	assert.Equal(t, "01A", resp.Articles[0].ArticleID)
	assert.Equal(t, pub, resp.Articles[0].PublishedAt)
}

// 仕様 (FEATURE_SPEC §5): GetDetail の HTTP ステータスマッピング。
func TestGetDetail_仕様_HTTPマッピング(t *testing.T) {
	otherErr := errors.New("db lost")

	successDetail := &apinews.NewsDetail{ArticleID: "abc", PublishedAt: time.Now()}

	cases := []struct {
		name       string
		query      string
		repoReturn *apinews.NewsDetail
		repoErr    error
		wantStatus int
	}{
		{
			name:       "成功",
			query:      "?lang=ja",
			repoReturn: successDetail,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found は 404",
			query:      "?lang=ja",
			repoErr:    port.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "その他エラーは 500",
			query:      "?lang=ja",
			repoErr:    otherErr,
			wantStatus: http.StatusInternalServerError,
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*apinews.NewsDetail, error) {
					return tc.repoReturn, tc.repoErr
				},
			}
			h := rest.NewNewsHandler(news.New(repo))

			req := httptest.NewRequest(http.MethodGet, "/internal/v1/news/abc"+tc.query, nil)
			w := httptest.NewRecorder()
			newEngine(h).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

// 仕様: GetDetail は body / source_url / lang 特定の翻訳を含む JSON を返す。
func TestGetDetail_仕様_本文とsource_urlを返す(t *testing.T) {
	want := &apinews.NewsDetail{
		ArticleID: "01", Source: "oci", Title: "T", Summary: "S",
		Body: "本文", Tags: []string{"ai"},
		SourceURL:   "https://example.com/a",
		PublishedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	var gotLang string
	repo := &port.MockNewsRepo{
		GetPublishedByIDFn: func(_ context.Context, _ string, lang string) (*apinews.NewsDetail, error) {
			gotLang = lang
			return want, nil
		},
	}
	h := rest.NewNewsHandler(news.New(repo))

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/news/01?lang=ja", nil)
	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ja", gotLang)
	var got apinews.NewsDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, want.Body, got.Body)
	assert.Equal(t, want.SourceURL, got.SourceURL)
}
