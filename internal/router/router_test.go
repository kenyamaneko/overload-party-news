package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"

	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestPublicRouter は記事なしを返す MockNewsRepo で組んだ実 usecase / handler で
// 公開 router を構築する。
func newTestPublicRouter(verifier internalauth.Verifier) *gin.Engine {
	querier := &port.MockNewsRepo{
		ListPublishedFn: func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
			return []domain.PublishedArticleSummary{}, nil
		},
	}
	return NewPublic(rest.NewNewsHandler(news.New(querier)), verifier)
}

// TestNewPublic_HealthEndpoint は /health が auth middleware を通らず 200 を返すことを確かめる。
func TestNewPublic_HealthEndpoint(t *testing.T) {
	// VerifyFn 未設定: /health が verifier に到達しないことの検出を兼ねる
	r := newTestPublicRouter(&internalauth.MockVerifier{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestNewPublic_ApiRouteRequiresInternalAuth は /api/v1/news 配下が auth header 欠落で
// 401 を返し handler に到達しないことを確かめる。
func TestNewPublic_ApiRouteRequiresInternalAuth(t *testing.T) {
	// VerifyFn 未設定: header 欠落時は middleware が verifier に到達しないことの検出を兼ねる
	r := newTestPublicRouter(&internalauth.MockVerifier{})

	cases := []struct {
		name string
		path string
	}{
		{
			name: "一覧は auth header 欠落で 401",
			path: "/api/v1/news?lang=ja&limit=10",
		},
		{
			name: "詳細は auth header 欠落で 401",
			path: "/api/v1/news/ART-TST-0001?lang=ja",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

// TestNewPublic_ApiRouteRejectsVerifierError は verifier が error を返すと 401 を返し
// handler に到達しないことを確かめる。
func TestNewPublic_ApiRouteRejectsVerifierError(t *testing.T) {
	r := newTestPublicRouter(&internalauth.MockVerifier{
		VerifyFn: func(string) (string, error) { return "", errors.New("invalid token") },
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news?lang=ja&limit=10", nil)
	req.Header.Set(internalauth.HeaderName, "any.token")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestNewPublic_ApiRouteWithValidTokenReachesHandler は verifier を通過したリクエストが
// handler の成功応答まで到達することを確かめる。
func TestNewPublic_ApiRouteWithValidTokenReachesHandler(t *testing.T) {
	r := newTestPublicRouter(&internalauth.MockVerifier{
		VerifyFn: func(string) (string, error) { return "TST-PLAYER-1", nil },
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news?lang=ja&limit=10", nil)
	req.Header.Set(internalauth.HeaderName, "any.token")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
