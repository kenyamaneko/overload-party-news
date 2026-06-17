package admin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
)

// deriveErrorStatus は usecase 層のエラーを HTTP ステータスにマップする。
func deriveErrorStatus(err error) int {
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
// HTMX は 4xx/5xx で hx-swap を抑止するため人間可読なメッセージだけでよい。
func respondError(c *gin.Context, err error) {
	c.String(deriveErrorStatus(err), "%s", err.Error())
}
