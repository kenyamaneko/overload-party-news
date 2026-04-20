package admin_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/config"
	"github.com/kenyamaneko/overload-party-news/internal/handler/admin"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/review"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func newAdminServer(t *testing.T, repo *port.MockNewsRepo) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	svc := review.New(repo, repo, func() time.Time { return time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC) })
	h, err := admin.NewHandler(svc)
	require.NoError(t, err)

	r := gin.New()
	g := r.Group("/admin", admin.AuthMiddleware(config.EnvLocal))
	g.GET("/articles", h.List)
	g.GET("/articles/:articleId", h.GetEdit)
	g.POST("/articles/:articleId/translations/:lang", h.UpsertTranslation)
	g.POST("/articles/:articleId/publish", h.Publish)
	g.POST("/articles/:articleId/reject", h.Reject)
	return r
}

func sampleArticle(id string, status apinews.Status) apinews.Article {
	return apinews.Article{
		ArticleID: id,
		Source:    "aws",
		SourceURL: "https://example.com/" + id,
		Status:    status,
		Tags:      []string{},
	}
}

func sampleArticleWithJa(id string, status apinews.Status) apinews.ArticleWithTranslations {
	return apinews.ArticleWithTranslations{
		Article: sampleArticle(id, status),
		Translations: []apinews.Translation{
			{ArticleID: id, Lang: apinews.LangJa, Title: "ja-title-" + id, Summary: "要約", Body: "本文"},
		},
	}
}

// 仕様 (API_REFERENCE): GET /admin/articles は status クエリの受理範囲で 200 / 400 を返す。
func TestList_仕様_statusクエリの受理範囲(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{name: "未指定は全件", query: "", wantStatus: http.StatusOK},
		{name: "all は全件", query: "?status=all", wantStatus: http.StatusOK},
		{name: "pending", query: "?status=pending", wantStatus: http.StatusOK},
		{name: "published", query: "?status=published", wantStatus: http.StatusOK},
		{name: "rejected", query: "?status=rejected", wantStatus: http.StatusOK},
		{name: "未知値は 400", query: "?status=unknown", wantStatus: http.StatusBadRequest},
		{name: "limit 非整数は 400", query: "?limit=xxx", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListByStatusFn: func(_ context.Context, _ *apinews.Status, _ int) ([]apinews.ArticleWithTranslations, error) {
					return []apinews.ArticleWithTranslations{}, nil
				},
			}
			req := httptest.NewRequest(http.MethodGet, "/admin/articles"+tc.query, nil)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

// 仕様: 一覧画面は常に ja タイトルで表示する。ja 翻訳があれば title を、なければ [ja 未作成] プレースホルダ。
func TestList_仕様_jaタイトル表示(t *testing.T) {
	cases := []struct {
		name        string
		translations []apinews.Translation
		wantBodyHas string
	}{
		{name: "ja あり", translations: []apinews.Translation{{Lang: apinews.LangJa, Title: "ja-title"}}, wantBodyHas: "ja-title"},
		{name: "en のみ", translations: []apinews.Translation{{Lang: apinews.LangEn, Title: "en-title"}}, wantBodyHas: "[ja 未作成]"},
		{name: "翻訳なし", translations: nil, wantBodyHas: "[ja 未作成]"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListByStatusFn: func(_ context.Context, _ *apinews.Status, _ int) ([]apinews.ArticleWithTranslations, error) {
					return []apinews.ArticleWithTranslations{{
						Article:      sampleArticle("01", apinews.StatusPending),
						Translations: tc.translations,
					}}, nil
				},
			}
			req := httptest.NewRequest(http.MethodGet, "/admin/articles", nil)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantBodyHas)
		})
	}
}

// 仕様 (FEATURE_SPEC §6.1): 未承認記事の行には承認/却下両方、published は却下のみ、rejected は承認のみ。
func TestList_仕様_statusに応じてボタン表示が変わる(t *testing.T) {
	cases := []struct {
		name    string
		status  apinews.Status
		wantPub bool
		wantRej bool
	}{
		{name: "pending は両方", status: apinews.StatusPending, wantPub: true, wantRej: true},
		{name: "published は却下のみ", status: apinews.StatusPublished, wantPub: false, wantRej: true},
		{name: "rejected は承認のみ", status: apinews.StatusRejected, wantPub: true, wantRej: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListByStatusFn: func(_ context.Context, _ *apinews.Status, _ int) ([]apinews.ArticleWithTranslations, error) {
					return []apinews.ArticleWithTranslations{sampleArticleWithJa("01", tc.status)}, nil
				},
			}
			req := httptest.NewRequest(http.MethodGet, "/admin/articles", nil)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			body := w.Body.String()
			assert.Equal(t, tc.wantPub, strings.Contains(body, `/publish"`), "承認ボタンの存在")
			assert.Equal(t, tc.wantRej, strings.Contains(body, `/reject"`), "却下ボタンの存在")
		})
	}
}

// 仕様: 編集画面は存在する記事なら 200、存在しなければ 404。
func TestGetEdit_仕様_404と200(t *testing.T) {
	cases := []struct {
		name       string
		repoErr    error
		wantStatus int
	}{
		{name: "存在する記事は 200", repoErr: nil, wantStatus: http.StatusOK},
		{name: "存在しない記事は 404", repoErr: port.ErrNotFound, wantStatus: http.StatusNotFound},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				GetByIDFn: func(_ context.Context, id string) (*apinews.ArticleWithTranslations, error) {
					if tc.repoErr != nil {
						return nil, tc.repoErr
					}
					aw := sampleArticleWithJa(id, apinews.StatusPending)
					return &aw, nil
				},
			}
			req := httptest.NewRequest(http.MethodGet, "/admin/articles/01", nil)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

// 仕様: 編集画面は対応全言語 (ja / en) のタブを持つ。既存翻訳のある言語は値が埋まり、
// 無い言語は空フォームと「未作成」ラベル。
func TestGetEdit_仕様_全言語タブと初期値(t *testing.T) {
	repo := &port.MockNewsRepo{
		GetByIDFn: func(_ context.Context, id string) (*apinews.ArticleWithTranslations, error) {
			aw := apinews.ArticleWithTranslations{
				Article: sampleArticle(id, apinews.StatusPending),
				Translations: []apinews.Translation{
					{Lang: apinews.LangJa, Title: "初期タイトル", Summary: "初期要約", Body: "初期本文"},
					// en はまだ作成されていない
				},
			}
			return &aw, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/articles/01", nil)
	w := httptest.NewRecorder()
	newAdminServer(t, repo).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	// ja タブには既存値が埋まる
	assert.Contains(t, body, `value="初期タイトル"`)
	assert.Contains(t, body, `初期要約`)
	assert.Contains(t, body, `初期本文`)

	// ja / en 両言語のフォーム submit URL が存在する
	assert.Contains(t, body, `hx-post="/admin/articles/01/translations/ja"`)
	assert.Contains(t, body, `hx-post="/admin/articles/01/translations/en"`)

	// 未作成 ラベルが en タブに表示される
	assert.Contains(t, body, "未作成")
}

// 仕様 (FEATURE_SPEC §6.2): UpsertTranslation は指定言語の翻訳を upsert、成功時は編集画面に戻る。
func TestUpsertTranslation_仕様_成功時は編集画面へ戻る(t *testing.T) {
	cases := []struct {
		name          string
		hxRequest     bool
		wantStatus    int
		wantHXHeader  string
		wantLocation  string
	}{
		{name: "HTMX は 200 + HX-Redirect (編集画面へ)", hxRequest: true, wantStatus: http.StatusOK, wantHXHeader: "/admin/articles/01"},
		{name: "非 HTMX は 303 + Location (編集画面へ)", hxRequest: false, wantStatus: http.StatusSeeOther, wantLocation: "/admin/articles/01"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var upsertCalled int
			var gotLang string
			repo := &port.MockNewsRepo{
				UpsertTranslationFn: func(_ context.Context, articleID string, lang, title, summary, body string) error {
					upsertCalled++
					gotLang = lang
					assert.Equal(t, "01", articleID)
					assert.Equal(t, "新タイトル", title)
					assert.Equal(t, "新要約", summary)
					assert.Equal(t, "新本文", body)
					return nil
				},
			}

			form := url.Values{"title": {"新タイトル"}, "summary": {"新要約"}, "body": {"新本文"}}
			req := httptest.NewRequest(http.MethodPost, "/admin/articles/01/translations/ja", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.hxRequest {
				req.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantHXHeader, w.Header().Get("HX-Redirect"))
			assert.Equal(t, tc.wantLocation, w.Header().Get("Location"))
			assert.Equal(t, 1, upsertCalled)
			assert.Equal(t, "ja", gotLang)
		})
	}
}

// 仕様: UpsertTranslation のバリデーションエラーは 400 (空 title / 未知 lang 等)。
func TestUpsertTranslation_仕様_バリデーション(t *testing.T) {
	cases := []struct {
		name       string
		lang       string
		title      string
		summary    string
		body       string
		wantStatus int
	}{
		{name: "正常 (ja)", lang: "ja", title: "t", summary: "s", body: "b", wantStatus: http.StatusOK},
		{name: "正常 (en)", lang: "en", title: "t", summary: "s", body: "b", wantStatus: http.StatusOK},
		{name: "未知 lang", lang: "fr", title: "t", summary: "s", body: "b", wantStatus: http.StatusBadRequest},
		{name: "空 title", lang: "ja", title: "", summary: "s", body: "b", wantStatus: http.StatusBadRequest},
		{name: "空 summary", lang: "ja", title: "t", summary: "", body: "b", wantStatus: http.StatusBadRequest},
		{name: "空 body", lang: "ja", title: "t", summary: "s", body: "", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				UpsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error { return nil },
			}
			form := url.Values{"title": {tc.title}, "summary": {tc.summary}, "body": {tc.body}}
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/articles/01/translations/%s", tc.lang), strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("HX-Request", "true")
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

// 仕様 (ARCHITECTURE §部分レスポンス契約): Publish は
//   - hx-target=#row-xxx → 200 + 行フラグメント
//   - それ以外 → HX-Redirect でリストへ
func TestPublish_仕様_HXTargetによる応答形式(t *testing.T) {
	articleID := "01"
	cases := []struct {
		name        string
		hxTarget    string
		wantStatus  int
		wantHXRedir string
		wantBodyHas string
	}{
		{name: "行ターゲット指定は行フラグメント", hxTarget: "row-" + articleID, wantStatus: http.StatusOK, wantHXRedir: "", wantBodyHas: `id="row-` + articleID + `"`},
		{name: "行ターゲット無指定は HX-Redirect (HTMX)", hxTarget: "body", wantStatus: http.StatusOK, wantHXRedir: "/admin/articles", wantBodyHas: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var publishCalled int
			repo := &port.MockNewsRepo{
				PublishFn: func(_ context.Context, _ string, _ string, _ time.Time) error {
					publishCalled++
					return nil
				},
				GetByIDFn: func(_ context.Context, id string) (*apinews.ArticleWithTranslations, error) {
					aw := sampleArticleWithJa(id, apinews.StatusPublished)
					return &aw, nil
				},
			}

			u := fmt.Sprintf("/admin/articles/%s/publish", articleID)
			req := httptest.NewRequest(http.MethodPost, u, nil)
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Target", tc.hxTarget)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantHXRedir, w.Header().Get("HX-Redirect"))
			assert.Equal(t, 1, publishCalled)
			assert.Equal(t, tc.wantBodyHas != "" && strings.Contains(w.Body.String(), tc.wantBodyHas), tc.wantBodyHas != "")
		})
	}
}

// 仕様: Reject も Publish と同じ契約。
func TestReject_仕様_HXTargetによる応答形式(t *testing.T) {
	repo := &port.MockNewsRepo{
		RejectFn: func(_ context.Context, _ string, _ string, _ time.Time) error { return nil },
		GetByIDFn: func(_ context.Context, id string) (*apinews.ArticleWithTranslations, error) {
			aw := sampleArticleWithJa(id, apinews.StatusRejected)
			return &aw, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/01/reject", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "row-01")
	w := httptest.NewRecorder()
	newAdminServer(t, repo).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `id="row-01"`)
	assert.Contains(t, w.Body.String(), "rejected")
}

// 仕様: 存在しない記事を publish すると 404。
func TestPublish_仕様_存在しない記事は404(t *testing.T) {
	repo := &port.MockNewsRepo{
		PublishFn: func(_ context.Context, _ string, _ string, _ time.Time) error {
			return fmt.Errorf("not found: %w", port.ErrNotFound)
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/ghost/publish", nil)
	w := httptest.NewRecorder()
	newAdminServer(t, repo).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
