package admin

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/service/review"
)

// adminListItem は一覧画面描画用に ArticleWithTranslations を展開したビューモデル。
type adminListItem struct {
	Article      domain.Article
	DisplayTitle string // ja タイトル (無ければ空 → テンプレートでプレースホルダ表示)
	LangsLabel   string // 存在する翻訳言語の表示 (例: "ja, en")
}

// Handler は管理 UI のすべての HTTP エンドポイントを提供する。
type Handler struct {
	svc *review.Service
	tpl *templates
}

// NewHandler は依存を受け取り Handler を生成する。テンプレート初期化もここで一度だけ行う。
func NewHandler(svc *review.Service) (*Handler, error) {
	tpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Handler{svc: svc, tpl: tpl}, nil
}

// List は GET /admin/articles。status フィルタ付きの一覧ページを返す。
// 各記事は ja タイトルで表示する (運用者が日本語話者前提)。
func (h *Handler) List(c *gin.Context) {
	statuses, statusParam, err := parseStatusFilter(c.QueryArray("status"))
	if err != nil {
		respondError(c, err)
		return
	}
	limit, err := parseAdminLimit(c.Query("limit"))
	if err != nil {
		respondError(c, err)
		return
	}

	articles, err := h.svc.List(c.Request.Context(), statuses, limit)
	if err != nil {
		respondError(c, err)
		return
	}

	items := make([]adminListItem, 0, len(articles))
	for _, a := range articles {
		items = append(items, toAdminListItem(a))
	}

	data := gin.H{
		"Title":        "記事一覧",
		"Reviewer":     Reviewer(c),
		"ActiveStatus": statusParam,
		"Items":        items,
	}
	renderPage(c, h.tpl.list, data)
}

// GetEdit は GET /admin/articles/:articleId。編集フォーム (ja/en タブ付き) のページを返す。
func (h *Handler) GetEdit(c *gin.Context) {
	articleID := c.Param("articleId")
	aw, err := h.svc.Get(c.Request.Context(), articleID)
	if err != nil {
		respondError(c, err)
		return
	}
	data := gin.H{
		"Title":        headerTitle(aw),
		"Reviewer":     Reviewer(c),
		"Article":      aw.Article,
		"Translations": aw.Translations,
		"Langs":        domain.SupportedLangs,
	}
	renderPage(c, h.tpl.edit, data)
}

// UpsertTranslation は POST /admin/articles/:articleId/translations/:lang。
// 翻訳の追加 / 更新を行い、編集画面に戻る。
func (h *Handler) UpsertTranslation(c *gin.Context) {
	articleID := c.Param("articleId")
	lang := c.Param("lang")
	title := c.PostForm("title")
	summary := c.PostForm("summary")
	body := c.PostForm("body")

	if err := h.svc.UpsertTranslation(c.Request.Context(), articleID, lang, title, summary, body); err != nil {
		respondError(c, err)
		return
	}
	redirectTo(c, "/admin/articles/"+articleID)
}

// Publish は POST /admin/articles/:articleId/publish。
func (h *Handler) Publish(c *gin.Context) {
	articleID := c.Param("articleId")
	if err := h.svc.Publish(c.Request.Context(), articleID, Reviewer(c)); err != nil {
		respondError(c, err)
		return
	}
	h.respondUpdatedRow(c, articleID)
}

// Reject は POST /admin/articles/:articleId/reject。
func (h *Handler) Reject(c *gin.Context) {
	articleID := c.Param("articleId")
	if err := h.svc.Reject(c.Request.Context(), articleID, Reviewer(c)); err != nil {
		respondError(c, err)
		return
	}
	h.respondUpdatedRow(c, articleID)
}

// respondUpdatedRow は承認・却下後に最新の行 HTML を返す。
// hx-target が #row-{articleId} のときだけ行フラグメントを返し、
// それ以外は HX-Redirect または 303 でリストへ戻す (edit ページからの呼び出し等)。
func (h *Handler) respondUpdatedRow(c *gin.Context, articleID string) {
	aw, err := h.svc.Get(c.Request.Context(), articleID)
	if err != nil {
		respondError(c, err)
		return
	}

	target := c.GetHeader("HX-Target")
	if target != fmt.Sprintf("row-%s", articleID) {
		redirectTo(c, "/admin/articles")
		return
	}

	item := toAdminListItem(*aw)
	var buf bytes.Buffer
	if err := h.tpl.row.ExecuteTemplate(&buf, "row", item); err != nil {
		respondError(c, fmt.Errorf("render row: %w", err))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
}

// redirectTo は HTMX リクエストなら HX-Redirect、通常リクエストなら 303 で指定 URL に遷移。
func redirectTo(c *gin.Context, url string) {
	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Redirect", url)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusSeeOther, url)
}

// renderPage はレイアウト込みのフルページ HTML を描画する。
// gin.H の data には必ず Title / Reviewer が含まれる前提 (layout テンプレートの契約)。
func renderPage(c *gin.Context, tpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		respondError(c, fmt.Errorf("render page: %w", err))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
}

// toAdminListItem は ArticleWithTranslations から一覧表示用のビューモデルを作る。
// ja タイトルを抽出し、存在する翻訳言語のラベルを組み立てる。
func toAdminListItem(aw domain.ArticleWithTranslations) adminListItem {
	var displayTitle string
	langs := make([]string, 0, len(aw.Translations))
	for _, t := range aw.Translations {
		langs = append(langs, t.Lang)
		if t.Lang == domain.LangJa {
			displayTitle = t.Title
		}
	}
	return adminListItem{
		Article:      aw.Article,
		DisplayTitle: displayTitle,
		LangsLabel:   strings.Join(langs, ", "),
	}
}

// headerTitle は編集画面の <title> に表示する文字列を決める。
// ja 翻訳があればそのタイトル、無ければ article_id を使う。
func headerTitle(aw *domain.ArticleWithTranslations) string {
	for _, t := range aw.Translations {
		if t.Lang == domain.LangJa {
			return t.Title
		}
	}
	return aw.Article.ArticleID
}

// parseStatusFilter は status クエリ群を Status 集合フィルタに変換する。
// 未指定 / 単一 "all" → domain.Statuses (全 status 列挙)、既知値群 → 対応 Status 列、未知値混在 → ErrInvalidField。
// 重複は除去する。複数指定 (?status=pending&status=published) は IN 句相当として扱う。
// 2 つ目の返り値はテンプレートで active タブをハイライトするための文字列
// (単一指定なら値そのもの、複数指定ならカンマ結合、全件なら空)。
func parseStatusFilter(raws []string) ([]domain.Status, string, error) {
	if len(raws) == 0 {
		return domain.Statuses, "", nil
	}
	if len(raws) == 1 && (raws[0] == "" || raws[0] == "all") {
		return domain.Statuses, "", nil
	}

	seen := make(map[domain.Status]struct{}, len(raws))
	statuses := make([]domain.Status, 0, len(raws))
	for _, raw := range raws {
		var s domain.Status
		switch raw {
		case string(domain.StatusPending):
			s = domain.StatusPending
		case string(domain.StatusPublished):
			s = domain.StatusPublished
		case string(domain.StatusRejected):
			s = domain.StatusRejected
		default:
			return nil, "", fmt.Errorf("%w: unknown status %q", review.ErrInvalidField, raw)
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		statuses = append(statuses, s)
	}

	parts := make([]string, len(statuses))
	for i, s := range statuses {
		parts[i] = string(s)
	}
	return statuses, strings.Join(parts, ","), nil
}

// parseAdminLimit は ?limit= クエリを int に変換する。
// 未指定や非整数は ErrInvalidField を返す (デフォルト値へのフォールバックを行わない方針)。
// 値の範囲バリデーションは service 層が行う。
func parseAdminLimit(raw string) (int, error) {
	if raw == "" {
		return 0, fmt.Errorf("%w: limit is required", review.ErrInvalidField)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: limit %q is not an integer", review.ErrInvalidField, raw)
	}
	return n, nil
}
