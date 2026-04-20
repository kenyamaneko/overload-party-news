// Package rest は gateway 向け内部 REST API の delivery 層。
// service 層のセンチネルエラーを HTTP ステータスに変換する責務を持つ。
package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/news"
)

// errorStatus はサービス層のエラーを HTTP ステータスにマップする。
func errorStatus(err error) int {
	switch {
	case errors.Is(err, port.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, news.ErrInvalidLimit),
		errors.Is(err, news.ErrLangRequired),
		errors.Is(err, news.ErrUnsupportedLang):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func respondError(c *gin.Context, err error) {
	c.JSON(errorStatus(err), gin.H{"error": err.Error()})
}
