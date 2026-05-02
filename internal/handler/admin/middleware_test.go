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

func TestAuthMiddleware_Production(t *testing.T) {
	cases := []struct {
		name         string
		headers      map[string]string
		wantStatus   int
		wantReviewer string
	}{
		{
			name:         "プロバイダープレフィックス付き",
			headers:      map[string]string{"X-Goog-Authenticated-User-Email": "accounts.google.com:alice@example.com"},
			wantStatus:   http.StatusOK,
			wantReviewer: "alice@example.com",
		},
		{
			name:         "プレフィックスなし",
			headers:      map[string]string{"X-Goog-Authenticated-User-Email": "bob@example.com"},
			wantStatus:   http.StatusOK,
			wantReviewer: "bob@example.com",
		},
		{
			name:         "ヘッダ不在は401",
			headers:      nil,
			wantStatus:   http.StatusUnauthorized,
			wantReviewer: "",
		},
		{
			name:         "プレフィックスのみ (email 空) は401",
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
			r.Use(admin.AuthMiddleware(config.EnvProduction))
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
}

func TestAuthMiddleware_Local(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var seenReviewer string
	r := gin.New()
	r.Use(admin.AuthMiddleware(config.EnvLocal))
	r.GET("/_probe", func(c *gin.Context) {
		seenReviewer = admin.Reviewer(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, seenReviewer, "local はヘッダ無しでも reviewer が注入されるべき")
}
