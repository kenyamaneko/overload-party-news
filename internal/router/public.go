package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"

	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
)

// NewPublic は gateway 経由の公開 REST API ルータを構築する。
// /api/v1/news/* は X-Internal-Auth (HMAC JWT) を必須とし、
// middleware が sub クレームを context に注入する。
func NewPublic(newsH *rest.NewsHandler, authVerifier internalauth.Verifier) *gin.Engine {
	r := gin.New()
	r.Use(newRequestLogger(), gin.Recovery())

	r.GET("/health", handleHealth)

	api := r.Group("/api/v1/news", internalauth.VerifyInternalAuth(authVerifier))
	{
		api.GET("", newsH.List)
		api.GET("/:articleId", newsH.GetDetail)
	}
	return r
}

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// newRequestLogger は構造化ログを出力する共通ミドルウェアを生成する。
func newRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}

		slog.LogAttrs(c.Request.Context(), level, "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
		)
	}
}
