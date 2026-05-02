package admin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
)

// errorStatus はusecase 層のエラーを HTTP ステータスにマップする。
func errorStatus(err error) int {
	switch {
	case errors.Is(err, port.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, review.ErrInvalidField), errors.Is(err, port.ErrInvalidPersistedValue):
		return http.StatusBadRequest
	case errors.Is(err, ErrMissingIAPHeader):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// respondError は管理 UI のエラーをテキストで返す。
// HTMX は 4xx/5xx 時に hx-swap を抑止するため、人間可読なメッセージだけを返せばよい。
func respondError(c *gin.Context, err error) {
	c.String(errorStatus(err), "%s", err.Error())
}
