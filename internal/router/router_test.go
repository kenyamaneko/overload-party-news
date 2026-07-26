package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"

	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// noopPushHandle は push 経路を対象としないテストで使う no-op の port.MessageHandler。
func noopPushHandle(context.Context, []byte) error { return nil }

// newTestPublicRouter は記事なしを返す MockNewsRepo で組んだ実 usecase / handler で公開 router を構築する。
func newTestPublicRouter(verifier internalauth.Verifier, pushHandle port.MessageHandler) *gin.Engine {
	querier := &port.MockNewsRepo{
		ListPublishedFn: func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
			return []domain.PublishedArticleSummary{}, nil
		},
	}
	return NewPublic(rest.NewNewsHandler(news.New(querier)), verifier, pubsubpush.NewHandler(pushHandle))
}

// pushEnvelopeBody は data を base64 化した push envelope の JSON body を返す。
func pushEnvelopeBody(data string) *strings.Reader {
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	return strings.NewReader(`{"message":{"data":"` + encoded + `"}}`)
}

// TestNewPublic_HealthEndpoint は /health が auth middleware を通らず 200 を返すことを確かめる。
func TestNewPublic_HealthEndpoint(t *testing.T) {
	// VerifyFn 未設定: /health が verifier に到達しないことの検出を兼ねる
	r := newTestPublicRouter(&internalauth.MockVerifier{}, noopPushHandle)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestNewPublic_ApiRouteRequiresInternalAuth は /api/v1/news 配下が auth header 欠落で
// 401 を返し handler に到達しないことを確かめる。
func TestNewPublic_ApiRouteRequiresInternalAuth(t *testing.T) {
	// VerifyFn 未設定: header 欠落時は middleware が verifier に到達しないことの検出を兼ねる
	r := newTestPublicRouter(&internalauth.MockVerifier{}, noopPushHandle)

	cases := []struct {
		name string
		path string
	}{
		{
			name: "一覧は認証ヘッダ欠落で 401 を返し、応答に認証ヘッダ必須の内容が含まれる",
			path: "/api/v1/news?lang=ja&limit=10",
		},
		{
			name: "詳細は認証ヘッダ欠落で 401 を返し、応答に認証ヘッダ必須の内容が含まれる",
			path: "/api/v1/news/ART-TST-0001?lang=ja",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "header is required")
		})
	}
}

// TestNewPublic_ApiRouteRejectsVerifierError は verifier が error を返すと 401 を返し
// handler に到達しないことを確かめる。
func TestNewPublic_ApiRouteRejectsVerifierError(t *testing.T) {
	r := newTestPublicRouter(&internalauth.MockVerifier{
		VerifyFn: func(string) (string, error) { return "", errors.New("invalid token") },
	}, noopPushHandle)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news?lang=ja&limit=10", nil)
	req.Header.Set(internalauth.HeaderName, "any.token")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid internal auth token")
}

// TestNewPublic_ApiRouteWithValidTokenReachesHandler は verifier を通過したリクエストが
// handler の成功応答まで到達することを確かめる。
func TestNewPublic_ApiRouteWithValidTokenReachesHandler(t *testing.T) {
	r := newTestPublicRouter(&internalauth.MockVerifier{
		VerifyFn: func(string) (string, error) { return "TST-PLAYER-1", nil },
	}, noopPushHandle)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news?lang=ja&limit=10", nil)
	req.Header.Set(internalauth.HeaderName, "any.token")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestNewPublic_PubsubPushRouteSkipsAuth は /internal/v1/pubsub/news-article-collected が
// auth header 無しでも push handler まで到達することを確かめる。
func TestNewPublic_PubsubPushRouteSkipsAuth(t *testing.T) {
	var gotData []byte
	// VerifyFn 未設定: verifier に到達しないことの検出を兼ねる
	r := newTestPublicRouter(&internalauth.MockVerifier{}, func(_ context.Context, data []byte) error {
		gotData = data
		return nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/pubsub/news-article-collected", pushEnvelopeBody(`{"article_id":"TST-0001"}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"article_id":"TST-0001"}`, string(gotData))
}

// TestNewPublic_PubsubPushRouteRejectsUnknownEventPath は未登録のイベント名パスが
// 404 を返し push handler に到達しないことを確かめる。
func TestNewPublic_PubsubPushRouteRejectsUnknownEventPath(t *testing.T) {
	var called bool
	r := newTestPublicRouter(&internalauth.MockVerifier{}, func(context.Context, []byte) error {
		called = true
		return nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/pubsub/unknown-event", pushEnvelopeBody(`{}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.False(t, called)
}

func TestRequestLogger(t *testing.T) {
	t.Run("リクエストログのレベル分類", func(t *testing.T) {
		cases := []struct {
			name      string
			status    int
			wantLevel string
		}{
			{name: "status 200 のとき、INFO レベルで記録される", status: http.StatusOK, wantLevel: "INFO"},
			{name: "status 399 のとき、INFO レベルで記録される", status: 399, wantLevel: "INFO"},
			{name: "status 400 のとき、WARN レベルで記録される", status: http.StatusBadRequest, wantLevel: "WARN"},
			{name: "status 499 のとき、WARN レベルで記録される", status: 499, wantLevel: "WARN"},
			{name: "status 500 のとき、ERROR レベルで記録される", status: http.StatusInternalServerError, wantLevel: "ERROR"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				original := slog.Default()
				slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
				t.Cleanup(func() { slog.SetDefault(original) })

				r := gin.New()
				r.Use(newRequestLogger())
				r.GET("/probe", func(c *gin.Context) { c.Status(tc.status) })

				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))

				var record map[string]any
				require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
				assert.Equal(t, tc.wantLevel, record["level"])
				assert.Equal(t, http.MethodGet, record["method"])
				assert.Equal(t, "/probe", record["path"])
				assert.Equal(t, float64(tc.status), record["status"])
			})
		}
	})
}
