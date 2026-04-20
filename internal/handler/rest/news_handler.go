package rest

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/service/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// NewsHandler は gateway 経由で呼ばれる公開配信 API のエントリポイント。
type NewsHandler struct {
	svc *news.Service
}

// NewNewsHandler は NewsHandler を生成する。
func NewNewsHandler(svc *news.Service) *NewsHandler {
	return &NewsHandler{svc: svc}
}

// List は GET /internal/v1/news。lang (必須) と limit を解釈し、公開中記事の一覧を返す。
// lang は必須 (未指定は 400)、limit 未指定は news.ListLimitDefault、範囲外は 400。
func (h *NewsHandler) List(c *gin.Context) {
	lang := c.Query("lang")
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		respondError(c, err)
		return
	}

	items, err := h.svc.List(c.Request.Context(), lang, limit)
	if err != nil {
		respondError(c, err)
		return
	}
	if items == nil {
		items = []apinews.NewsListItem{}
	}
	c.JSON(http.StatusOK, apinews.NewsListResponse{Articles: items})
}

// GetDetail は GET /internal/v1/news/:articleId。指定 lang の公開中記事の詳細を返す。
// 非公開 / 非存在 / 当該 lang 翻訳なしは 404。
func (h *NewsHandler) GetDetail(c *gin.Context) {
	articleID := c.Param("articleId")
	lang := c.Query("lang")
	detail, err := h.svc.GetDetail(c.Request.Context(), articleID, lang)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// parseLimit は limit クエリを int に変換する。空なら default を返す。
// 値の範囲バリデーションは service 層が行うため、ここでは整数変換のみ責任を持つ。
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return news.ListLimitDefault, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, news.ErrInvalidLimit
	}
	return n, nil
}
