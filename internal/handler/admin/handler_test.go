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
	a := apinews.Article{
		ArticleID: id,
		Source:    "aws",
		SourceURL: "https://example.com/" + id,
		Status:    status,
		Tags:      []string{},
	}
	// status は apinews.DeriveStatus で reviewed_at / published_at から導出される設計のため、
	// テスト用にも該当する日付を埋めて状態を表現する。
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-1 * time.Hour)
	switch status {
	case apinews.StatusPublished:
		a.ReviewedAt = &now
		a.PublishedAt = &now
	case apinews.StatusRejected:
		a.ReviewedAt = &now
		a.PublishedAt = &earlier // earlier publish が後で却下された履歴
	}
	return a
}

func sampleArticleWithJa(id string, status apinews.Status) apinews.ArticleWithTranslations {
	return apinews.ArticleWithTranslations{
		Article: sampleArticle(id, status),
		Translations: []apinews.Translation{
			{ArticleID: id, Lang: apinews.LangJa, Title: "ja-title-" + id, Summary: "要約", Body: "本文"},
		},
	}
}

// stubList は ArticleWithTranslations 群を MockNewsRepo の 2 メソッド (記事 + 翻訳) に分解してセットする。
// service 層が両者を取得して結合する契約に合わせるためのテスト用ヘルパー。
// 翻訳の ArticleID は親記事の値で自動補完する (テスト記述を簡潔にするため)。
func stubList(repo *port.MockNewsRepo, items []apinews.ArticleWithTranslations) {
	articles := make([]apinews.Article, len(items))
	var translations []apinews.Translation
	for i, it := range items {
		articles[i] = it.Article
		for _, tr := range it.Translations {
			tr.ArticleID = it.Article.ArticleID
			translations = append(translations, tr)
		}
	}
	repo.ListArticlesFn = func(_ context.Context, _ int) ([]apinews.Article, error) {
		return articles, nil
	}
	repo.ListTranslationsByArticleIDsFn = func(_ context.Context, _ []string) ([]apinews.Translation, error) {
		return translations, nil
	}
}

// stubGet は単一 ArticleWithTranslations を mock の GetArticleByID + ListTranslationsByArticleIDs に分解してセットする。
// repoErr が非 nil なら GetArticleByID がそれを返し、翻訳取得は呼ばれないことを期待する。
func stubGet(repo *port.MockNewsRepo, aw *apinews.ArticleWithTranslations, repoErr error) {
	repo.GetArticleByIDFn = func(_ context.Context, _ string) (*apinews.Article, error) {
		if repoErr != nil {
			return nil, repoErr
		}
		a := aw.Article
		return &a, nil
	}
	repo.ListTranslationsByArticleIDsFn = func(_ context.Context, _ []string) ([]apinews.Translation, error) {
		if aw == nil {
			return nil, nil
		}
		return aw.Translations, nil
	}
}

// 仕様 (API_REFERENCE): GET /admin/articles は status クエリの受理範囲で 200 / 400 を返す。
func TestList_仕様_statusクエリの受理範囲(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{
			name:       "limit のみで全件 (status 未指定 / all 同等)",
			query:      "?limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "all は全件",
			query:      "?status=all&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "pending",
			query:      "?status=pending&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "published",
			query:      "?status=published&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "rejected",
			query:      "?status=rejected&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "limit 未指定は 400 (デフォルト値へのフォールバックは行わない)",
			query:      "?status=pending",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "未知 status は 400",
			query:      "?status=unknown&limit=50",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit 非整数は 400",
			query:      "?limit=xxx",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			stubList(repo, nil)
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
		name         string
		translations []apinews.Translation
		wantBodyHas  string
	}{
		{
			name:         "ja あり",
			translations: []apinews.Translation{{Lang: apinews.LangJa, Title: "ja-title"}},
			wantBodyHas:  "ja-title",
		},
		{
			name:         "en のみ",
			translations: []apinews.Translation{{Lang: apinews.LangEn, Title: "en-title"}},
			wantBodyHas:  "[ja 未作成]",
		},
		{
			name:         "翻訳なし",
			translations: nil,
			wantBodyHas:  "[ja 未作成]",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			stubList(repo, []apinews.ArticleWithTranslations{{
				Article:      sampleArticle("01", apinews.StatusPending),
				Translations: tc.translations,
			}})
			req := httptest.NewRequest(http.MethodGet, "/admin/articles?limit=50", nil)
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
		{
			name:    "pending は両方",
			status:  apinews.StatusPending,
			wantPub: true,
			wantRej: true,
		},
		{
			name:    "published は却下のみ",
			status:  apinews.StatusPublished,
			wantPub: false,
			wantRej: true,
		},
		{
			name:    "rejected は承認のみ",
			status:  apinews.StatusRejected,
			wantPub: true,
			wantRej: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			stubList(repo, []apinews.ArticleWithTranslations{sampleArticleWithJa("01", tc.status)})
			req := httptest.NewRequest(http.MethodGet, "/admin/articles?limit=50", nil)
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
	existing := sampleArticleWithJa("01", apinews.StatusPending)

	cases := []struct {
		name       string
		repoReturn *apinews.ArticleWithTranslations
		repoErr    error
		wantStatus int
	}{
		{
			name:       "存在する記事は 200",
			repoReturn: &existing,
			wantStatus: http.StatusOK,
		},
		{
			name:       "存在しない記事は 404",
			repoErr:    port.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			stubGet(repo, tc.repoReturn, tc.repoErr)
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
	repo := &port.MockNewsRepo{}
	stubGet(repo, &apinews.ArticleWithTranslations{
		Article: sampleArticle("01", apinews.StatusPending),
		Translations: []apinews.Translation{
			{Lang: apinews.LangJa, Title: "初期タイトル", Summary: "初期要約", Body: "初期本文"},
			// en はまだ作成されていない
		},
	}, nil)
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
		name         string
		extraHeaders map[string]string
		wantStatus   int
		wantHXHeader string
		wantLocation string
	}{
		{
			name:         "HTMX は 200 + HX-Redirect (編集画面へ)",
			extraHeaders: map[string]string{"HX-Request": "true"},
			wantStatus:   http.StatusOK,
			wantHXHeader: "/admin/articles/01",
		},
		{
			name:         "非 HTMX は 303 + Location (編集画面へ)",
			extraHeaders: nil,
			wantStatus:   http.StatusSeeOther,
			wantLocation: "/admin/articles/01",
		},
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
			for k, v := range tc.extraHeaders {
				req.Header.Set(k, v)
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
		{
			name:       "正常 (ja)",
			lang:       "ja",
			title:      "t",
			summary:    "s",
			body:       "b",
			wantStatus: http.StatusOK,
		},
		{
			name:       "正常 (en)",
			lang:       "en",
			title:      "t",
			summary:    "s",
			body:       "b",
			wantStatus: http.StatusOK,
		},
		{
			name:       "空 title",
			lang:       "ja",
			title:      "",
			summary:    "s",
			body:       "b",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "空 summary",
			lang:       "ja",
			title:      "t",
			summary:    "",
			body:       "b",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "空 body",
			lang:       "ja",
			title:      "t",
			summary:    "s",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
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

// 仕様: 未対応 lang (DB CHECK 違反) は repo が port.ErrInvalidPersistedValue を返し、handler が 400 にマップする。
// 許容値の SSoT は DB CHECK で、handler 単体テストでは repo がそのエラーを返すケースを擬似する。
func TestUpsertTranslation_仕様_未対応langは400(t *testing.T) {
	repo := &port.MockNewsRepo{
		UpsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
			return fmt.Errorf("lang=%q: %w", "fr", port.ErrInvalidPersistedValue)
		},
	}
	form := url.Values{"title": {"t"}, "summary": {"s"}, "body": {"b"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/01/translations/fr", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	newAdminServer(t, repo).ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
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
		{
			name:        "行ターゲット指定は行フラグメント",
			hxTarget:    "row-" + articleID,
			wantStatus:  http.StatusOK,
			wantHXRedir: "",
			wantBodyHas: `id="row-` + articleID + `"`,
		},
		{
			name:        "行ターゲット無指定は HX-Redirect (HTMX)",
			hxTarget:    "body",
			wantStatus:  http.StatusOK,
			wantHXRedir: "/admin/articles",
			wantBodyHas: "",
		},
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
			}
			aw := sampleArticleWithJa(articleID, apinews.StatusPublished)
			stubGet(repo, &aw, nil)

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
	}
	aw := sampleArticleWithJa("01", apinews.StatusRejected)
	stubGet(repo, &aw, nil)
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
