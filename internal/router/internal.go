package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
)

// NewInternal は gateway 向け内部 REST API のルータを構築する。news 側では追加認証を行わない (gateway で完了済前提)。
func NewInternal(newsH *rest.NewsHandler) *gin.Engine {
	r := gin.New()
	r.Use(requestLogger(), gin.Recovery())

	r.GET("/health", healthHandler)

	v1 := r.Group("/internal/v1")
	{
		v1.GET("/news", newsH.List)
		v1.GET("/news/:articleId", newsH.GetDetail)
	}
	return r
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// requestLogger は構造化ログを出力する共通ミドルウェア。
func requestLogger() gin.HandlerFunc {
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
