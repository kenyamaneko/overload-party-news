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

func TestAuthMiddleware_production_IAP_ヘッダ要求(t *testing.T) {
	cases := []struct {
		name         string
		header       string
		wantStatus   int
		wantReviewer string
	}{
		{name: "プロバイダープレフィックス付き", header: "accounts.google.com:alice@example.com", wantStatus: http.StatusOK, wantReviewer: "alice@example.com"},
		{name: "プレフィックスなし", header: "bob@example.com", wantStatus: http.StatusOK, wantReviewer: "bob@example.com"},
		{name: "ヘッダ不在は401", header: "", wantStatus: http.StatusUnauthorized, wantReviewer: ""},
		{name: "プレフィックスのみ (email 空) は401", header: "accounts.google.com:", wantStatus: http.StatusUnauthorized, wantReviewer: ""},
	}

	for _, tc := range cases {
		tc := tc
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
			if tc.header != "" {
				req.Header.Set("X-Goog-Authenticated-User-Email", tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantReviewer, seenReviewer)
		})
	}
}

func TestAuthMiddleware_local_ヘッダ不要で固定値注入(t *testing.T) {
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
