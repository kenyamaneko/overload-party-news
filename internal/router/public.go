package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"

	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
)

// NewPublic は Cloud Run が公開する internal 向けルータを構築する。
// /api/v1/news/* は gateway 経由の配信 API で、X-Internal-Auth (RS256 JWT) を必須とし
// middleware が sub クレームを context に注入する。
// /internal/v1/pubsub/* は Pub/Sub push subscription の受け口。
func NewPublic(newsH *rest.NewsHandler, authVerifier internalauth.Verifier, articleCollectedPushH *pubsubpush.Handler) *gin.Engine {
	r := gin.New()
	r.Use(newRequestLogger(), gin.Recovery())

	r.GET("/health", handleHealth)

	api := r.Group("/api/v1/news", internalauth.VerifyInternalAuth(authVerifier))
	{
		api.GET("", newsH.List)
		api.GET("/:articleId", newsH.GetDetail)
	}

	internalGroup := r.Group("/internal/v1")
	{
		internalGroup.POST("/pubsub/news-article-collected", articleCollectedPushH.Handle)
	}
	return r
}

// handleHealth はヘルスチェック結果を返す gin ハンドラ。
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
