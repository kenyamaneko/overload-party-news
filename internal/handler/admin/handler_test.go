package admin_test

import (
	"context"
	"errors"
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

// errListArticlesFailed は記事一覧取得の失敗を模すためにテストが注入するセンチネル。
var errListArticlesFailed = errors.New("list articles failed")

func newAdminServer(t *testing.T, repo *port.MockNewsRepo) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	uc := review.New(repo, repo, func() time.Time { return time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC) })
	h, err := admin.NewHandler(uc)
	require.NoError(t, err)

	r := gin.New()
	g := r.Group("/admin", admin.NewAuthMiddleware(config.EnvLocal))
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
// usecase 層が両者を取得して結合する契約に合わせるためのテスト用ヘルパー。
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
	t.Run("記事一覧の表示", func(t *testing.T) {
		cases := []struct {
			name        string
			query       string
			stubItems   []domain.ArticleWithTranslations
			listErr     error
			wantStatus  int
			wantBodyHas []string
			wantPubBtn  bool
			wantRejBtn  bool
		}{
			{
				name:       "status未指定 + limit=50のとき、200になる",
				query:      "?limit=50",
				wantStatus: http.StatusOK,
			},
			{
				name:       "status=all + limit=50のとき、200になる",
				query:      "?status=all&limit=50",
				wantStatus: http.StatusOK,
			},
			{
				name:       "status=pending + limit=50のとき、200になる",
				query:      "?status=pending&limit=50",
				wantStatus: http.StatusOK,
			},
			{
				name:       "status=published + limit=50のとき、200になる",
				query:      "?status=published&limit=50",
				wantStatus: http.StatusOK,
			},
			{
				name:       "status=rejected + limit=50のとき、200になる",
				query:      "?status=rejected&limit=50",
				wantStatus: http.StatusOK,
			},
			{
				name:       "limit未指定のとき、400になる (デフォルト値へフォールバックしない)",
				query:      "?status=pending",
				wantStatus: http.StatusBadRequest,
			},
			{
				name:       "未知のstatusのとき、400になる",
				query:      "?status=unknown&limit=50",
				wantStatus: http.StatusBadRequest,
			},
			{
				name:       "limitが非整数のとき、400になる",
				query:      "?limit=xxx",
				wantStatus: http.StatusBadRequest,
			},
			{
				name:        "ja翻訳ありのとき、jaタイトルが表示される",
				query:       "?limit=50",
				stubItems:   []domain.ArticleWithTranslations{{Article: sampleArticle("01", domain.StatusPending), Translations: []domain.Translation{{Lang: domain.LangJa, Title: "ja-title"}}}},
				wantStatus:  http.StatusOK,
				wantBodyHas: []string{"ja-title"},
				wantPubBtn:  true,
				wantRejBtn:  true,
			},
			{
				name:        "en翻訳のみのとき、[ja未作成]プレースホルダが表示される",
				query:       "?limit=50",
				stubItems:   []domain.ArticleWithTranslations{{Article: sampleArticle("01", domain.StatusPending), Translations: []domain.Translation{{Lang: domain.LangEn, Title: "en-title"}}}},
				wantStatus:  http.StatusOK,
				wantBodyHas: []string{"[ja 未作成]"},
				wantPubBtn:  true,
				wantRejBtn:  true,
			},
			{
				name:        "翻訳なしのとき、[ja未作成]プレースホルダが表示される",
				query:       "?limit=50",
				stubItems:   []domain.ArticleWithTranslations{{Article: sampleArticle("01", domain.StatusPending)}},
				wantStatus:  http.StatusOK,
				wantBodyHas: []string{"[ja 未作成]"},
				wantPubBtn:  true,
				wantRejBtn:  true,
			},
			{
				name:       "pending記事のとき、承認ボタンと却下ボタンが両方表示される",
				query:      "?limit=50",
				stubItems:  []domain.ArticleWithTranslations{sampleArticleWithJa("01", domain.StatusPending)},
				wantStatus: http.StatusOK,
				wantPubBtn: true,
				wantRejBtn: true,
			},
			{
				name:       "published記事のとき、却下ボタンのみ表示される",
				query:      "?limit=50",
				stubItems:  []domain.ArticleWithTranslations{sampleArticleWithJa("01", domain.StatusPublished)},
				wantStatus: http.StatusOK,
				wantPubBtn: false,
				wantRejBtn: true,
			},
			{
				name:       "rejected記事のとき、承認ボタンのみ表示される",
				query:      "?limit=50",
				stubItems:  []domain.ArticleWithTranslations{sampleArticleWithJa("01", domain.StatusRejected)},
				wantStatus: http.StatusOK,
				wantPubBtn: true,
				wantRejBtn: false,
			},
			{
				name:  "jaとenの翻訳がある記事のとき、一覧の言語欄にja, enと表示される",
				query: "?limit=50",
				stubItems: []domain.ArticleWithTranslations{{
					Article: sampleArticle("01", domain.StatusPending),
					Translations: []domain.Translation{
						{Lang: domain.LangJa, Title: "ja-title"},
						{Lang: domain.LangEn, Title: "en-title"},
					},
				}},
				wantStatus:  http.StatusOK,
				wantBodyHas: []string{"<small>ja, en</small>"},
				wantPubBtn:  true,
				wantRejBtn:  true,
			},
			{
				name:       "記事一覧の取得が予期しないエラーになるとき、500になる",
				query:      "?limit=50",
				listErr:    errListArticlesFailed,
				wantStatus: http.StatusInternalServerError,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				repo := &port.MockNewsRepo{}
				stubList(repo, tc.stubItems)
				baseListArticles := repo.ListArticlesFn
				repo.ListArticlesFn = func(ctx context.Context, limit int) ([]domain.Article, error) {
					articles, _ := baseListArticles(ctx, limit)
					return articles, tc.listErr
				}
				req := httptest.NewRequest(http.MethodGet, "/admin/articles"+tc.query, nil)
				w := httptest.NewRecorder()
				newAdminServer(t, repo).ServeHTTP(w, req)

				assert.Equal(t, tc.wantStatus, w.Code)
				body := w.Body.String()
				for _, s := range tc.wantBodyHas {
					assert.Contains(t, body, s)
				}
				assert.Equal(t, tc.wantPubBtn, strings.Contains(body, `/publish"`), "承認ボタンの存在")
				assert.Equal(t, tc.wantRejBtn, strings.Contains(body, `/reject"`), "却下ボタンの存在")
			})
		}
	})

	t.Run("statusフィルタの絞り込みとタブ選択表示", func(t *testing.T) {
		stubItems := []domain.ArticleWithTranslations{
			sampleArticleWithJa("01", domain.StatusPending),
			sampleArticleWithJa("02", domain.StatusPublished),
			sampleArticleWithJa("03", domain.StatusRejected),
		}

		filterCases := []struct {
			name           string
			query          string
			wantBodyHas    []string
			wantBodyNotHas []string
			wantRowCount   int
		}{
			{
				name:           "pending/published/rejectedが混在するとき、status=pendingではpending記事の行だけが表示される",
				query:          "?status=pending&limit=50",
				wantBodyHas:    []string{"ja-title-01"},
				wantBodyNotHas: []string{"ja-title-02", "ja-title-03"},
				wantRowCount:   1,
			},
			{
				name:           "status=pending&status=publishedのとき、両statusの記事の行が表示される",
				query:          "?status=pending&status=published&limit=50",
				wantBodyHas:    []string{"ja-title-01", "ja-title-02"},
				wantBodyNotHas: []string{"ja-title-03"},
				wantRowCount:   2,
			},
		}

		for _, tc := range filterCases {
			t.Run(tc.name, func(t *testing.T) {
				repo := &port.MockNewsRepo{}
				stubList(repo, stubItems)
				req := httptest.NewRequest(http.MethodGet, "/admin/articles"+tc.query, nil)
				w := httptest.NewRecorder()
				newAdminServer(t, repo).ServeHTTP(w, req)

				body := w.Body.String()
				for _, s := range tc.wantBodyHas {
					assert.Contains(t, body, s)
				}
				for _, s := range tc.wantBodyNotHas {
					assert.NotContains(t, body, s)
				}
				assert.Equal(t, tc.wantRowCount, strings.Count(body, `id="row-`))
			})
		}

		tabCases := []struct {
			name        string
			query       string
			wantBodyHas string
		}{
			{
				name:        "status=pendingのとき、pendingタブが選択中として表示される",
				query:       "?status=pending&limit=50",
				wantBodyHas: `href="/admin/articles?status=pending" aria-current="page"`,
			},
			{
				name:        "status未指定のとき、allタブが選択中として表示される",
				query:       "?limit=50",
				wantBodyHas: `href="/admin/articles" aria-current="page"`,
			},
			{
				name:        "同じstatusを重複指定したとき、重複が除かれpendingタブが選択中として表示される",
				query:       "?status=pending&status=pending&limit=50",
				wantBodyHas: `href="/admin/articles?status=pending" aria-current="page"`,
			},
		}

		for _, tc := range tabCases {
			t.Run(tc.name, func(t *testing.T) {
				repo := &port.MockNewsRepo{}
				stubList(repo, stubItems)
				req := httptest.NewRequest(http.MethodGet, "/admin/articles"+tc.query, nil)
				w := httptest.NewRecorder()
				newAdminServer(t, repo).ServeHTTP(w, req)

				assert.Contains(t, w.Body.String(), tc.wantBodyHas)
			})
		}
	})
}

func TestGetEdit(t *testing.T) {
	t.Run("記事編集画面の表示", func(t *testing.T) {
		existing := sampleArticleWithJa("01", domain.StatusPending)

		cases := []struct {
			name        string
			repoReturn  *domain.ArticleWithTranslations
			repoErr     error
			wantStatus  int
			wantBodyHas []string
		}{
			{
				name:       "存在する記事のとき、200になる",
				repoReturn: &existing,
				wantStatus: http.StatusOK,
			},
			{
				name:       "存在しない記事のとき、404になる",
				repoErr:    port.ErrNotFound,
				wantStatus: http.StatusNotFound,
			},
			{
				name: "jaのみ作成済みのとき、全言語タブと既存翻訳が埋め込まれる",
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
			{
				name:        "ja翻訳がある記事のとき、ページタイトルにjaタイトルが表示される",
				repoReturn:  &existing,
				wantStatus:  http.StatusOK,
				wantBodyHas: []string{"<title>News Admin — ja-title-01</title>"},
			},
			{
				name: "ja翻訳が無い記事 (enのみ)のとき、ページタイトルに記事IDが表示される",
				repoReturn: &domain.ArticleWithTranslations{
					Article:      sampleArticle("01", domain.StatusPending),
					Translations: []domain.Translation{{Lang: domain.LangEn, Title: "en-title"}},
				},
				wantStatus:  http.StatusOK,
				wantBodyHas: []string{"<title>News Admin — 01</title>"},
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
	})
}

// upsertArgs は UpsertTranslation がリポジトリへ転送した引数を捕捉する。
type upsertArgs struct {
	articleID, lang, title, summary, body string
}

func TestUpsertTranslation(t *testing.T) {
	t.Run("翻訳の登録・更新", func(t *testing.T) {
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
			wantUpsert   upsertArgs
		}{
			{
				name:         "HTMXリクエストでjaを登録すると、200 + HX-Redirectで編集画面へ誘導する",
				lang:         "ja",
				title:        "新タイトル",
				summary:      "新要約",
				body:         "新本文",
				extraHeaders: map[string]string{"HX-Request": "true"},
				wantStatus:   http.StatusOK,
				wantHXHeader: "/admin/articles/01",
				wantUpsert:   upsertArgs{articleID: "01", lang: "ja", title: "新タイトル", summary: "新要約", body: "新本文"},
			},
			{
				name:         "非HTMXリクエストでjaを登録すると、303 + Locationで編集画面へ誘導する",
				lang:         "ja",
				title:        "新タイトル",
				summary:      "新要約",
				body:         "新本文",
				wantStatus:   http.StatusSeeOther,
				wantLocation: "/admin/articles/01",
				wantUpsert:   upsertArgs{articleID: "01", lang: "ja", title: "新タイトル", summary: "新要約", body: "新本文"},
			},
			{
				name:         "HTMXリクエストでenを登録すると、200 + HX-Redirectで編集画面へ誘導する",
				lang:         "en",
				title:        "t",
				summary:      "s",
				body:         "b",
				extraHeaders: map[string]string{"HX-Request": "true"},
				wantStatus:   http.StatusOK,
				wantHXHeader: "/admin/articles/01",
				wantUpsert:   upsertArgs{articleID: "01", lang: "en", title: "t", summary: "s", body: "b"},
			},
			{
				name:         "titleが空のとき、400になる",
				lang:         "ja",
				title:        "",
				summary:      "s",
				body:         "b",
				extraHeaders: map[string]string{"HX-Request": "true"},
				wantStatus:   http.StatusBadRequest,
			},
			{
				name:         "summaryが空のとき、400になる",
				lang:         "ja",
				title:        "t",
				summary:      "",
				body:         "b",
				extraHeaders: map[string]string{"HX-Request": "true"},
				wantStatus:   http.StatusBadRequest,
			},
			{
				name:         "bodyが空のとき、400になる",
				lang:         "ja",
				title:        "t",
				summary:      "s",
				body:         "",
				extraHeaders: map[string]string{"HX-Request": "true"},
				wantStatus:   http.StatusBadRequest,
			},
			{
				name:         "未対応langがDB CHECK違反 (ErrInvalidPersistedValue)になるとき、400になる",
				lang:         "fr",
				title:        "t",
				summary:      "s",
				body:         "b",
				extraHeaders: map[string]string{"HX-Request": "true"},
				repoErr:      fmt.Errorf("lang=%q: %w", "fr", port.ErrInvalidPersistedValue),
				wantStatus:   http.StatusBadRequest,
				// usecase バリデーションは通過し lang はリポジトリ (DB CHECK) まで届く。
				wantUpsert: upsertArgs{articleID: "01", lang: "fr", title: "t", summary: "s", body: "b"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var gotUpsert upsertArgs
				repo := &port.MockNewsRepo{
					UpsertTranslationFn: func(_ context.Context, articleID, lang, title, summary, body string) error {
						gotUpsert = upsertArgs{articleID, lang, title, summary, body}
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
				// バリデーション 400 ケースは writer 未到達のため wantUpsert はゼロ値。
				assert.Equal(t, tc.wantUpsert, gotUpsert)
			})
		}
	})
}

func TestPublish(t *testing.T) {
	const articleID = "01"
	t.Run("記事の承認", func(t *testing.T) {
		successAW := sampleArticleWithJa(articleID, domain.StatusPublished)

		cases := []struct {
			name          string
			path          string
			hxTarget      string
			repoErr       error
			wantStatus    int
			wantHXRedir   string
			wantBodyHas   string
			wantBodyEmpty bool
		}{
			{
				name:        "HX-Targetがrow-{id} のとき、200 + 行フラグメントを返す",
				path:        "/admin/articles/" + articleID + "/publish",
				hxTarget:    "row-" + articleID,
				wantStatus:  http.StatusOK,
				wantBodyHas: `id="row-` + articleID + `"`,
			},
			{
				name:          "HX-Targetがrow以外のとき、HX-Redirectでリストへ誘導し本文は返さない",
				path:          "/admin/articles/" + articleID + "/publish",
				hxTarget:      "body",
				wantStatus:    http.StatusOK,
				wantHXRedir:   "/admin/articles",
				wantBodyEmpty: true,
			},
			{
				name:        "存在しない記事のとき、404 + エラーメッセージを返す",
				path:        "/admin/articles/ghost/publish",
				repoErr:     fmt.Errorf("not found: %w", port.ErrNotFound),
				wantStatus:  http.StatusNotFound,
				wantBodyHas: "article not found",
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
				assert.Equal(t, tc.wantBodyEmpty, w.Body.Len() == 0)
				assert.Contains(t, w.Body.String(), tc.wantBodyHas)
			})
		}
	})
}

func TestReject(t *testing.T) {
	t.Run("記事の却下", func(t *testing.T) {
		t.Run("HX-Targetがrow-01のとき、200で行フラグメントを返しrejectedを含む", func(t *testing.T) {
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
		})
	})
}
