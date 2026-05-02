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
	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/admin"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
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

func sampleArticle(id string, status domain.Status) domain.Article {
	a := domain.Article{
		ArticleID: id,
		Source:    "aws",
		SourceURL: "https://example.com/" + id,
		Status:    status,
		Tags:      []string{},
	}
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-1 * time.Hour)
	switch status {
	case domain.StatusPublished:
		a.ReviewedAt = &now
		a.PublishedAt = &now
	case domain.StatusRejected:
		a.ReviewedAt = &now
		a.PublishedAt = &earlier // earlier publish が後で却下された履歴
	}
	return a
}

func sampleArticleWithJa(id string, status domain.Status) domain.ArticleWithTranslations {
	return domain.ArticleWithTranslations{
		Article: sampleArticle(id, status),
		Translations: []domain.Translation{
			{ArticleID: id, Lang: domain.LangJa, Title: "ja-title-" + id, Summary: "要約", Body: "本文"},
		},
	}
}

// stubList は ArticleWithTranslations 群を MockNewsRepo の 2 メソッド (記事 + 翻訳) に分解してセットする。
// service 層が両者を取得して結合する契約に合わせるためのテスト用ヘルパー。
// 翻訳の ArticleID は親記事の値で自動補完する (テスト記述を簡潔にするため)。
func stubList(repo *port.MockNewsRepo, items []domain.ArticleWithTranslations) {
	articles := make([]domain.Article, len(items))
	var translations []domain.Translation
	for i, it := range items {
		articles[i] = it.Article
		for _, tr := range it.Translations {
			tr.ArticleID = it.Article.ArticleID
			translations = append(translations, tr)
		}
	}
	repo.ListArticlesFn = func(_ context.Context, _ int) ([]domain.Article, error) {
		return articles, nil
	}
	repo.ListTranslationsByArticleIDsFn = func(_ context.Context, _ []string) ([]domain.Translation, error) {
		return translations, nil
	}
}

// stubGet は単一 ArticleWithTranslations を mock の GetArticleByID + ListTranslationsByArticleIDs に分解してセットする。
// repoErr が非 nil なら GetArticleByID がそれを返し、翻訳取得は呼ばれないことを期待する。
func stubGet(repo *port.MockNewsRepo, aw *domain.ArticleWithTranslations, repoErr error) {
	repo.GetArticleByIDFn = func(_ context.Context, _ string) (*domain.Article, error) {
		if repoErr != nil {
			return nil, repoErr
		}
		a := aw.Article
		return &a, nil
	}
	repo.ListTranslationsByArticleIDsFn = func(_ context.Context, _ []string) ([]domain.Translation, error) {
		if aw == nil {
			return nil, nil
		}
		return aw.Translations, nil
	}
}

func TestList(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		stubItems   []domain.ArticleWithTranslations
		wantStatus  int
		wantBodyHas []string
		wantPubBtn  bool
		wantRejBtn  bool
		checkBtns   bool
	}{
		// status クエリ受理範囲
		{
			name:       "limit のみで全件 (status 未指定 / all 同等)",
			query:      "?limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "status=all は全件",
			query:      "?status=all&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "status=pending",
			query:      "?status=pending&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "status=published",
			query:      "?status=published&limit=50",
			wantStatus: http.StatusOK,
		},
		{
			name:       "status=rejected",
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
		// ja タイトル表示
		{
			name:        "ja 翻訳ありは ja タイトルが表示される",
			query:       "?limit=50",
			stubItems:   []domain.ArticleWithTranslations{{Article: sampleArticle("01", domain.StatusPending), Translations: []domain.Translation{{Lang: domain.LangJa, Title: "ja-title"}}}},
			wantStatus:  http.StatusOK,
			wantBodyHas: []string{"ja-title"},
		},
		{
			name:        "en のみのとき [ja 未作成] プレースホルダ",
			query:       "?limit=50",
			stubItems:   []domain.ArticleWithTranslations{{Article: sampleArticle("01", domain.StatusPending), Translations: []domain.Translation{{Lang: domain.LangEn, Title: "en-title"}}}},
			wantStatus:  http.StatusOK,
			wantBodyHas: []string{"[ja 未作成]"},
		},
		{
			name:        "翻訳なしのとき [ja 未作成] プレースホルダ",
			query:       "?limit=50",
			stubItems:   []domain.ArticleWithTranslations{{Article: sampleArticle("01", domain.StatusPending)}},
			wantStatus:  http.StatusOK,
			wantBodyHas: []string{"[ja 未作成]"},
		},
		// status に応じたボタン表示 (FEATURE_SPEC)
		{
			name:       "pending は承認/却下ボタン両方",
			query:      "?limit=50",
			stubItems:  []domain.ArticleWithTranslations{sampleArticleWithJa("01", domain.StatusPending)},
			wantStatus: http.StatusOK,
			wantPubBtn: true,
			wantRejBtn: true,
			checkBtns:  true,
		},
		{
			name:       "published は却下ボタンのみ",
			query:      "?limit=50",
			stubItems:  []domain.ArticleWithTranslations{sampleArticleWithJa("01", domain.StatusPublished)},
			wantStatus: http.StatusOK,
			wantPubBtn: false,
			wantRejBtn: true,
			checkBtns:  true,
		},
		{
			name:       "rejected は承認ボタンのみ",
			query:      "?limit=50",
			stubItems:  []domain.ArticleWithTranslations{sampleArticleWithJa("01", domain.StatusRejected)},
			wantStatus: http.StatusOK,
			wantPubBtn: true,
			wantRejBtn: false,
			checkBtns:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			stubList(repo, tc.stubItems)
			req := httptest.NewRequest(http.MethodGet, "/admin/articles"+tc.query, nil)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			body := w.Body.String()
			for _, s := range tc.wantBodyHas {
				assert.Contains(t, body, s)
			}
			if tc.checkBtns {
				assert.Equal(t, tc.wantPubBtn, strings.Contains(body, `/publish"`), "承認ボタンの存在")
				assert.Equal(t, tc.wantRejBtn, strings.Contains(body, `/reject"`), "却下ボタンの存在")
			}
		})
	}
}

func TestGetEdit(t *testing.T) {
	existing := sampleArticleWithJa("01", domain.StatusPending)

	cases := []struct {
		name        string
		repoReturn  *domain.ArticleWithTranslations
		repoErr     error
		wantStatus  int
		wantBodyHas []string
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
		{
			name: "全言語タブ表示と既存翻訳の埋め込み (ja のみ作成済み)",
			repoReturn: &domain.ArticleWithTranslations{
				Article: sampleArticle("01", domain.StatusPending),
				Translations: []domain.Translation{
					{Lang: domain.LangJa, Title: "初期タイトル", Summary: "初期要約", Body: "初期本文"},
				},
			},
			wantStatus: http.StatusOK,
			wantBodyHas: []string{
				`value="初期タイトル"`,
				"初期要約",
				"初期本文",
				`hx-post="/admin/articles/01/translations/ja"`,
				`hx-post="/admin/articles/01/translations/en"`,
				"未作成", // en タブのプレースホルダ
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{}
			stubGet(repo, tc.repoReturn, tc.repoErr)
			req := httptest.NewRequest(http.MethodGet, "/admin/articles/01", nil)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			body := w.Body.String()
			for _, s := range tc.wantBodyHas {
				assert.Contains(t, body, s)
			}
		})
	}
}

func TestUpsertTranslation(t *testing.T) {
	cases := []struct {
		name         string
		lang         string
		title        string
		summary      string
		body         string
		extraHeaders map[string]string
		repoErr      error
		wantStatus   int
		wantHXHeader string
		wantLocation string
	}{
		{
			name:         "HTMX 正常 (ja) は 200 + HX-Redirect で編集画面へ",
			lang:         "ja",
			title:        "新タイトル",
			summary:      "新要約",
			body:         "新本文",
			extraHeaders: map[string]string{"HX-Request": "true"},
			wantStatus:   http.StatusOK,
			wantHXHeader: "/admin/articles/01",
		},
		{
			name:         "非 HTMX 正常 (ja) は 303 + Location で編集画面へ",
			lang:         "ja",
			title:        "新タイトル",
			summary:      "新要約",
			body:         "新本文",
			wantStatus:   http.StatusSeeOther,
			wantLocation: "/admin/articles/01",
		},
		{
			name:         "正常 (en)",
			lang:         "en",
			title:        "t",
			summary:      "s",
			body:         "b",
			extraHeaders: map[string]string{"HX-Request": "true"},
			wantStatus:   http.StatusOK,
			wantHXHeader: "/admin/articles/01",
		},
		{
			name:         "空 title は 400",
			lang:         "ja",
			title:        "",
			summary:      "s",
			body:         "b",
			extraHeaders: map[string]string{"HX-Request": "true"},
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "空 summary は 400",
			lang:         "ja",
			title:        "t",
			summary:      "",
			body:         "b",
			extraHeaders: map[string]string{"HX-Request": "true"},
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "空 body は 400",
			lang:         "ja",
			title:        "t",
			summary:      "s",
			body:         "",
			extraHeaders: map[string]string{"HX-Request": "true"},
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "未対応 lang (DB CHECK 違反 = ErrInvalidPersistedValue) は 400",
			lang:         "fr",
			title:        "t",
			summary:      "s",
			body:         "b",
			extraHeaders: map[string]string{"HX-Request": "true"},
			repoErr:      fmt.Errorf("lang=%q: %w", "fr", port.ErrInvalidPersistedValue),
			wantStatus:   http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				UpsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
					return tc.repoErr
				},
			}
			form := url.Values{"title": {tc.title}, "summary": {tc.summary}, "body": {tc.body}}
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/articles/01/translations/%s", tc.lang), strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for k, v := range tc.extraHeaders {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantHXHeader, w.Header().Get("HX-Redirect"))
			assert.Equal(t, tc.wantLocation, w.Header().Get("Location"))
		})
	}
}

func TestPublish(t *testing.T) {
	const articleID = "01"
	successAW := sampleArticleWithJa(articleID, domain.StatusPublished)

	cases := []struct {
		name        string
		path        string
		hxTarget    string
		repoErr     error
		wantStatus  int
		wantHXRedir string
		wantBodyHas string
	}{
		{
			name:        "HX-Target=row-{id} は 200 + 行フラグメント",
			path:        "/admin/articles/" + articleID + "/publish",
			hxTarget:    "row-" + articleID,
			wantStatus:  http.StatusOK,
			wantBodyHas: `id="row-` + articleID + `"`,
		},
		{
			name:        "HX-Target=row 以外は HX-Redirect でリストへ",
			path:        "/admin/articles/" + articleID + "/publish",
			hxTarget:    "body",
			wantStatus:  http.StatusOK,
			wantHXRedir: "/admin/articles",
		},
		{
			name:       "存在しない記事は 404",
			path:       "/admin/articles/ghost/publish",
			repoErr:    fmt.Errorf("not found: %w", port.ErrNotFound),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				PublishFn: func(_ context.Context, _ string, _ string, _ time.Time) error {
					return tc.repoErr
				},
			}
			stubGet(repo, &successAW, nil)

			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Target", tc.hxTarget)
			w := httptest.NewRecorder()
			newAdminServer(t, repo).ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantHXRedir, w.Header().Get("HX-Redirect"))
			if tc.wantBodyHas != "" {
				assert.Contains(t, w.Body.String(), tc.wantBodyHas)
			}
		})
	}
}

func TestReject(t *testing.T) {
	repo := &port.MockNewsRepo{
		RejectFn: func(_ context.Context, _ string, _ string, _ time.Time) error { return nil },
	}
	aw := sampleArticleWithJa("01", domain.StatusRejected)
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
