package router

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
)

func newTestPublicRouter(t *testing.T, repo *port.MockNewsRepo, verifier internalauth.Verifier, pushHandle func(ctx context.Context, data []byte) error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	newsH := rest.NewNewsHandler(news.New(repo))
	pushH := pubsubpush.NewHandler(pushHandle)
	return NewPublic(newsH, verifier, pushH)
}

func acceptingVerifier() *internalauth.MockVerifier {
	return &internalauth.MockVerifier{
		VerifyFn: func(_ string) (string, error) {
			return "player-001", nil
		},
	}
}

func TestNewPublic(t *testing.T) {
	t.Run("[公開ルータ]公開ルータの配線", func(t *testing.T) {
		t.Run("GET /healthは認証ヘッダが無くても200を返す", func(t *testing.T) {
			r := newTestPublicRouter(t, &port.MockNewsRepo{}, acceptingVerifier(), func(context.Context, []byte) error { return nil })

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		missingHeaderTests := []struct {
			name   string
			target string
		}{
			{"GET /api/v1/newsに認証ヘッダを付けずにリクエストすると", "/api/v1/news?lang=ja&limit=10"},
			{"GET /api/v1/news/{articleId}に認証ヘッダを付けずにリクエストすると", "/api/v1/news/article-001?lang=ja"},
		}
		for _, tt := range missingHeaderTests {
			t.Run(tt.name+"、401と、応答本文にheader is requiredを含む内容を返す", func(t *testing.T) {
				r := newTestPublicRouter(t, &port.MockNewsRepo{}, acceptingVerifier(), func(context.Context, []byte) error { return nil })

				req := httptest.NewRequest(http.MethodGet, tt.target, nil)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code)
				assert.Contains(t, w.Body.String(), "header is required")
			})
		}

		t.Run("認証ヘッダを付けてリクエストしたが検証に失敗するとき、401と、応答本文にinvalid internal auth tokenを含む内容を返す", func(t *testing.T) {
			verifier := &internalauth.MockVerifier{
				VerifyFn: func(_ string) (string, error) {
					return "", assert.AnError
				},
			}
			r := newTestPublicRouter(t, &port.MockNewsRepo{}, verifier, func(context.Context, []byte) error { return nil })

			req := httptest.NewRequest(http.MethodGet, "/api/v1/news?lang=ja&limit=10", nil)
			req.Header.Set(internalauth.HeaderName, "some-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "invalid internal auth token")
		})

		t.Run("認証ヘッダを付けてリクエストし検証に成功するとき、一覧取得が実行され200を返す", func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
					return []domain.PublishedArticleSummary{}, nil
				},
			}
			r := newTestPublicRouter(t, repo, acceptingVerifier(), func(context.Context, []byte) error { return nil })

			req := httptest.NewRequest(http.MethodGet, "/api/v1/news?lang=ja&limit=10", nil)
			req.Header.Set(internalauth.HeaderName, "valid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("POST /internal/v1/pubsub/news-article-collectedは認証ヘッダを付けずにリクエストしても受理され、push envelopeの内容が後続の処理に渡る", func(t *testing.T) {
			var got []byte
			r := newTestPublicRouter(t, &port.MockNewsRepo{}, acceptingVerifier(), func(_ context.Context, data []byte) error {
				got = data
				return nil
			})

			body := `{"message":{"data":"cGF5bG9hZA=="}}`
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/pubsub/news-article-collected", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "payload", string(got))
		})

		t.Run("登録されていないPub/Sub pushのパスにリクエストすると、404を返す", func(t *testing.T) {
			r := newTestPublicRouter(t, &port.MockNewsRepo{}, acceptingVerifier(), func(context.Context, []byte) error { return nil })

			req := httptest.NewRequest(http.MethodPost, "/internal/v1/pubsub/unknown-event", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	})
}

type capturingSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
	return nil
}

func (h *capturingSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingSlogHandler) WithGroup(_ string) slog.Handler      { return h }

func captureRequestLog(t *testing.T, statusToWrite int) slog.Record {
	t.Helper()
	prev := slog.Default()
	handler := &capturingSlogHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() {
		slog.SetDefault(prev)
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(newRequestLogger())
	r.GET("/probe", func(c *gin.Context) {
		c.Status(statusToWrite)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, statusToWrite, w.Code)

	require.Len(t, handler.records, 1)
	return handler.records[0]
}

func recordAttr(t *testing.T, record slog.Record, key string) (string, bool) {
	t.Helper()
	var value string
	found := false
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			value = a.Value.String()
			found = true
			return false
		}
		return true
	})
	return value, found
}

func TestNewRequestLogger(t *testing.T) {
	t.Run("[リクエストログ]リクエストログの重大度分類", func(t *testing.T) {
		severityTests := []struct {
			name      string
			status    int
			wantLevel slog.Level
		}{
			{"応答ステータスが200のとき、INFOレベルで記録される", http.StatusOK, slog.LevelInfo},
			{"応答ステータスが399のとき、INFOレベルで記録される", 399, slog.LevelInfo},
			{"応答ステータスが400のとき、WARNレベルで記録される", http.StatusBadRequest, slog.LevelWarn},
			{"応答ステータスが499のとき、WARNレベルで記録される", 499, slog.LevelWarn},
			{"応答ステータスが500のとき、ERRORレベルで記録される", http.StatusInternalServerError, slog.LevelError},
		}
		for _, tt := range severityTests {
			t.Run(tt.name, func(t *testing.T) {
				record := captureRequestLog(t, tt.status)

				assert.Equal(t, tt.wantLevel, record.Level)
			})
		}

		t.Run("記録される内容に、リクエストのHTTPメソッド・パス・応答ステータスが含まれる", func(t *testing.T) {
			record := captureRequestLog(t, http.StatusOK)

			method, ok := recordAttr(t, record, "method")
			require.True(t, ok)
			assert.Equal(t, http.MethodGet, method)

			path, ok := recordAttr(t, record, "path")
			require.True(t, ok)
			assert.Equal(t, "/probe", path)

			status, ok := recordAttr(t, record, "status")
			require.True(t, ok)
			assert.Equal(t, "200", status)
		})
	})
}
