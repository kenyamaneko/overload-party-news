package review_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
)

var fixedNow = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

// 依存先 (querier / writer) が返したエラーを usecase がそのまま呼び出し元へ伝播することを
// 検証するために注入するセンチネル。
var (
	errListArticles     = errors.New("list articles failed")
	errListTranslations = errors.New("list translations failed")
	errReject           = errors.New("reject failed")
)

func newInteractor(repo *port.MockNewsRepo) *review.Interactor {
	return review.New(repo, repo, func() time.Time { return fixedNow })
}

// toIntPtr は int リテラルのアドレスを直接取れない Go の制約を補うテストヘルパー。
func toIntPtr(n int) *int { return &n }

// upsertCall / reviewCall は repo に渡された値の集合。
// テスト期待値として「正常時に何が渡るか」「失敗時は呼ばれない (= nil)」を 1 つの値で表現する。
type upsertCall struct {
	articleID string
	lang      string
	title     string
	summary   string
	body      string
}

type reviewCall struct {
	articleID string
	reviewer  string
	now       time.Time
}

func TestUpsertTranslation(t *testing.T) {
	const articleID = "article-1"
	cases := []struct {
		name     string
		title    string
		summary  string
		body     string
		wantErr  error
		wantCall *upsertCall
	}{
		{
			name:    "正常",
			title:   "a",
			summary: "b",
			body:    "c",
			wantCall: &upsertCall{
				articleID: articleID, lang: domain.LangJa,
				title: "a", summary: "b", body: "c",
			},
		},
		{
			name:    "境界上限 (マルチバイト)",
			title:   strings.Repeat("あ", review.TitleMaxLen),
			summary: strings.Repeat("い", review.SummaryMaxLen),
			body:    "本文",
			wantCall: &upsertCall{
				articleID: articleID, lang: domain.LangJa,
				title:   strings.Repeat("あ", review.TitleMaxLen),
				summary: strings.Repeat("い", review.SummaryMaxLen),
				body:    "本文",
			},
		},
		{
			name:    "タイトル空",
			title:   "",
			summary: "b",
			body:    "c",
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "タイトル超過",
			title:   strings.Repeat("a", review.TitleMaxLen+1),
			summary: "b",
			body:    "c",
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "要約空",
			title:   "a",
			summary: "",
			body:    "c",
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "要約超過",
			title:   "a",
			summary: strings.Repeat("b", review.SummaryMaxLen+1),
			body:    "c",
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "本文空",
			title:   "a",
			summary: "b",
			body:    "",
			wantErr: review.ErrInvalidField,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got *upsertCall
			repo := &port.MockNewsRepo{
				UpsertTranslationFn: func(_ context.Context, articleID string, lang, title, summary, body string) error {
					got = &upsertCall{articleID, lang, title, summary, body}
					return nil
				},
			}
			err := newInteractor(repo).UpsertTranslation(context.Background(), articleID, domain.LangJa, tc.title, tc.summary, tc.body)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCall, got)
		})
	}
}

func TestPublish(t *testing.T) {
	const articleID = "article-1"
	cases := []struct {
		name     string
		reviewer string
		wantErr  error
		wantCall *reviewCall
	}{
		{
			name:     "正常 reviewer は repo に articleID/reviewer/now が渡る",
			reviewer: "alice@example.com",
			wantCall: &reviewCall{articleID: articleID, reviewer: "alice@example.com", now: fixedNow},
		},
		{
			name:     "空 reviewer は ErrInvalidField で repo に到達しない",
			reviewer: "",
			wantErr:  review.ErrInvalidField,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got *reviewCall
			repo := &port.MockNewsRepo{
				PublishFn: func(_ context.Context, id string, reviewer string, now time.Time) error {
					got = &reviewCall{articleID: id, reviewer: reviewer, now: now}
					return nil
				},
			}
			err := newInteractor(repo).Publish(context.Background(), articleID, tc.reviewer)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCall, got)
		})
	}
}

func TestReject(t *testing.T) {
	const articleID = "article-1"
	cases := []struct {
		name      string
		reviewer  string
		writerErr error
		wantErr   error
		wantCall  *reviewCall
	}{
		{
			name:     "正常 reviewer は repo に articleID/reviewer/now が渡る",
			reviewer: "alice@example.com",
			wantCall: &reviewCall{articleID: articleID, reviewer: "alice@example.com", now: fixedNow},
		},
		{
			name:     "空 reviewer は ErrInvalidField で repo に到達しない",
			reviewer: "",
			wantErr:  review.ErrInvalidField,
		},
		{
			name:      "writer が失敗するとそのエラーがそのまま伝播し、repo には articleID/reviewer/now が渡る",
			reviewer:  "alice@example.com",
			writerErr: errReject,
			wantErr:   errReject,
			wantCall:  &reviewCall{articleID: articleID, reviewer: "alice@example.com", now: fixedNow},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got *reviewCall
			repo := &port.MockNewsRepo{
				RejectFn: func(_ context.Context, id string, reviewer string, now time.Time) error {
					got = &reviewCall{articleID: id, reviewer: reviewer, now: now}
					return tc.writerErr
				},
			}
			err := newInteractor(repo).Reject(context.Background(), articleID, tc.reviewer)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCall, got)
		})
	}
}

func TestList(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-1 * time.Hour)
	pendingArticle := domain.Article{ArticleID: "a-pending"}
	publishedArticle := domain.Article{ArticleID: "a-published", ReviewedAt: &now, PublishedAt: &now}
	rejectedArticle := domain.Article{ArticleID: "a-rejected", ReviewedAt: &now, PublishedAt: &earlier}
	threeArticles := []domain.Article{pendingArticle, publishedArticle, rejectedArticle}

	// limit の truncation と limit→filter の適用順は実 PostgreSQL を要するため TestListLimitWindow で検証する。
	cases := []struct {
		name             string
		articles         []domain.Article
		filter           []domain.Status
		limit            int
		listArticlesErr  error
		listTransErr     error
		wantErr          error
		wantIDs          []string
		wantTransOn      []string // nil 想定 = 翻訳取得呼び出しなし
		wantQueriedLimit *int     // nil 想定 = querier 未呼び出し
	}{
		{
			name:    "limit=0 は範囲 (0, Max] 違反で ErrInvalidField、querier に到達しない",
			limit:   0,
			filter:  domain.Statuses,
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "limit 負値は範囲 (0, Max] 違反で ErrInvalidField、querier に到達しない",
			limit:   -1,
			filter:  domain.Statuses,
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "limit 上限超過 (Max+1) は範囲 (0, Max] 違反で ErrInvalidField、querier に到達しない",
			limit:   review.AdminListLimitMax + 1,
			filter:  domain.Statuses,
			wantErr: review.ErrInvalidField,
		},
		{
			name:             "limit 上限ちょうど (Max) は境界内として受理され、その値が querier の取得件数に転送される",
			articles:         threeArticles,
			filter:           domain.Statuses,
			limit:            review.AdminListLimitMax,
			wantIDs:          []string{"a-pending", "a-published", "a-rejected"},
			wantTransOn:      []string{"a-pending", "a-published", "a-rejected"},
			wantQueriedLimit: toIntPtr(review.AdminListLimitMax),
		},
		{
			name:             "全 status 指定なら querier が返した記事を各 status を DeriveStatus で導出して全件返す",
			articles:         threeArticles,
			filter:           domain.Statuses,
			limit:            50,
			wantIDs:          []string{"a-pending", "a-published", "a-rejected"},
			wantTransOn:      []string{"a-pending", "a-published", "a-rejected"},
			wantQueriedLimit: toIntPtr(50),
		},
		{
			name:             "pending のみ指定なら pending と導出される記事だけ返す",
			articles:         threeArticles,
			filter:           []domain.Status{domain.StatusPending},
			limit:            50,
			wantIDs:          []string{"a-pending"},
			wantTransOn:      []string{"a-pending"},
			wantQueriedLimit: toIntPtr(50),
		},
		{
			name:             "published+rejected を指定なら該当する 2 件だけ返す",
			articles:         threeArticles,
			filter:           []domain.Status{domain.StatusPublished, domain.StatusRejected},
			limit:            50,
			wantIDs:          []string{"a-published", "a-rejected"},
			wantTransOn:      []string{"a-published", "a-rejected"},
			wantQueriedLimit: toIntPtr(50),
		},
		{
			name:             "該当 status が 0 件なら翻訳取得を呼ばずに空を返す",
			articles:         []domain.Article{publishedArticle},
			filter:           []domain.Status{domain.StatusPending},
			limit:            50,
			wantIDs:          nil,
			wantTransOn:      nil,
			wantQueriedLimit: toIntPtr(50),
		},
		{
			name:             "記事取得が失敗するとそのエラーがそのまま伝播し、翻訳取得には到達しない",
			articles:         threeArticles,
			filter:           domain.Statuses,
			limit:            50,
			listArticlesErr:  errListArticles,
			wantErr:          errListArticles,
			wantIDs:          nil,
			wantTransOn:      nil,
			wantQueriedLimit: toIntPtr(50),
		},
		{
			name:             "翻訳取得が失敗するとそのエラーがそのまま伝播し結果は返らない",
			articles:         threeArticles,
			filter:           domain.Statuses,
			limit:            50,
			listTransErr:     errListTranslations,
			wantErr:          errListTranslations,
			wantIDs:          nil,
			wantTransOn:      []string{"a-pending", "a-published", "a-rejected"},
			wantQueriedLimit: toIntPtr(50),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLimit *int
			var gotTransIDs []string
			repo := &port.MockNewsRepo{
				ListArticlesFn: func(_ context.Context, limit int) ([]domain.Article, error) {
					gotLimit = toIntPtr(limit)
					return tc.articles, tc.listArticlesErr
				},
				ListTranslationsByArticleIDsFn: func(_ context.Context, ids []string) ([]domain.Translation, error) {
					gotTransIDs = ids
					return nil, tc.listTransErr
				},
			}

			got, err := newInteractor(repo).List(context.Background(), tc.filter, tc.limit)
			assert.ErrorIs(t, err, tc.wantErr)

			var gotIDs []string
			for _, g := range got {
				gotIDs = append(gotIDs, g.Article.ArticleID)
			}
			assert.Equal(t, tc.wantQueriedLimit, gotLimit)
			assert.Equal(t, tc.wantIDs, gotIDs)
			assert.Equal(t, tc.wantTransOn, gotTransIDs)
		})
	}
}

func TestGet(t *testing.T) {
	article := &domain.Article{ArticleID: "01", Status: domain.StatusPending}
	translations := []domain.Translation{
		{ArticleID: "01", Lang: domain.LangJa, Title: "ja title"},
	}

	cases := []struct {
		name            string
		articleReturn   *domain.Article
		articleErr      error
		transErr        error
		wantErr         error
		want            *domain.ArticleWithTranslations
		wantTransCalled bool
	}{
		{
			name:            "記事ありなら記事 + 翻訳を結合して返す",
			articleReturn:   article,
			want:            &domain.ArticleWithTranslations{Article: *article, Translations: translations},
			wantTransCalled: true,
		},
		{
			name:            "記事が ErrNotFound のとき翻訳取得は呼ばれない",
			articleErr:      port.ErrNotFound,
			wantErr:         port.ErrNotFound,
			want:            nil,
			wantTransCalled: false,
		},
		{
			name:            "翻訳取得が失敗するとそのエラーがそのまま伝播し結果は返らない",
			articleReturn:   article,
			transErr:        errListTranslations,
			wantErr:         errListTranslations,
			want:            nil,
			wantTransCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var transCalled bool
			repo := &port.MockNewsRepo{
				GetArticleByIDFn: func(_ context.Context, _ string) (*domain.Article, error) {
					return tc.articleReturn, tc.articleErr
				},
				ListTranslationsByArticleIDsFn: func(_ context.Context, ids []string) ([]domain.Translation, error) {
					transCalled = true
					assert.Equal(t, []string{"01"}, ids)
					return translations, tc.transErr
				},
			}

			got, err := newInteractor(repo).Get(context.Background(), "01")
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantTransCalled, transCalled)
		})
	}
}
