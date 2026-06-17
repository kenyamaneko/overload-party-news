package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
)

// deriveErrorStatus は usecase 層のエラーを HTTP ステータスにマップする。
func deriveErrorStatus(err error) int {
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
	c.JSON(deriveErrorStatus(err), gin.H{"error": err.Error()})
}
