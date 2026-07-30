package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/config"
	"github.com/kenyamaneko/overload-party-news/internal/handler/admin"
)

func TestAuthMiddleware(t *testing.T) {
	t.Run("管理画面の認証ミドルウェア", func(t *testing.T) {
		t.Run("production 環境", func(t *testing.T) {
			cases := []struct {
				name         string
				headers      map[string]string
				wantStatus   int
				wantReviewer string
			}{
				{
					name:         "プロバイダープレフィックス付きヘッダのとき、prefix を除いた email が reviewer になる",
					headers:      map[string]string{"X-Goog-Authenticated-User-Email": "accounts.google.com:alice@example.com"},
					wantStatus:   http.StatusOK,
					wantReviewer: "alice@example.com",
				},
				{
					name:         "プレフィックスなしヘッダのとき、email がそのまま reviewer になる",
					headers:      map[string]string{"X-Goog-Authenticated-User-Email": "bob@example.com"},
					wantStatus:   http.StatusOK,
					wantReviewer: "bob@example.com",
				},
				{
					name:         "ヘッダが無いとき、401 になる",
					headers:      nil,
					wantStatus:   http.StatusUnauthorized,
					wantReviewer: "",
				},
				{
					name:         "プレフィックスのみで email が空のとき、401 になる",
					headers:      map[string]string{"X-Goog-Authenticated-User-Email": "accounts.google.com:"},
					wantStatus:   http.StatusUnauthorized,
					wantReviewer: "",
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					gin.SetMode(gin.TestMode)
					var seenReviewer string
					r := gin.New()
					r.Use(admin.NewAuthMiddleware(config.EnvProduction))
					r.GET("/_probe", func(c *gin.Context) {
						seenReviewer = admin.Reviewer(c)
						c.Status(http.StatusOK)
					})

					req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
					for k, v := range tc.headers {
						req.Header.Set(k, v)
					}
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)

					assert.Equal(t, tc.wantStatus, w.Code)
					assert.Equal(t, tc.wantReviewer, seenReviewer)
				})
			}
		})

		t.Run("local 環境ではヘッダが無くても固定の reviewer が注入される", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var seenReviewer string
			r := gin.New()
			r.Use(admin.NewAuthMiddleware(config.EnvLocal))
			r.GET("/_probe", func(c *gin.Context) {
				seenReviewer = admin.Reviewer(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "local-dev@example.com", seenReviewer)
		})
	})
}
