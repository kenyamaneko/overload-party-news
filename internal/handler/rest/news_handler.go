package rest

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// NewsHandler は gateway 経由で呼ばれる公開配信 API のエントリポイント。
type NewsHandler struct {
	uc *news.Interactor
}

// NewNewsHandler は NewsHandler を生成する。
func NewNewsHandler(uc *news.Interactor) *NewsHandler {
	return &NewsHandler{uc: uc}
}

// List は GET /api/v1/news。lang / limit いずれも未指定または不正値は 400。
func (h *NewsHandler) List(c *gin.Context) {
	lang := c.Query("lang")
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		respondError(c, err)
		return
	}

	items, err := h.uc.List(c.Request.Context(), lang, limit)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, apinews.NewsListResponse{Articles: items})
}

// GetDetail は GET /api/v1/news/:articleId。非公開 / 非存在 / 当該 lang 翻訳なしは 404。
func (h *NewsHandler) GetDetail(c *gin.Context) {
	articleID := c.Param("articleId")
	lang := c.Query("lang")
	detail, err := h.uc.GetDetail(c.Request.Context(), articleID, lang)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// parseLimit は limit クエリを int に変換する。未指定 / 非整数は ErrInvalidLimit (デフォルト埋めはしない)。
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, news.ErrInvalidLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, news.ErrInvalidLimit
	}
	return n, nil
}
