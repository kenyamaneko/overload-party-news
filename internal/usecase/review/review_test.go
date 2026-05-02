package review_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
)

var fixedNow = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

func newService(repo *port.MockNewsRepo) *review.Service {
	return review.New(repo, repo, func() time.Time { return fixedNow })
}

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
			err := newService(repo).UpsertTranslation(context.Background(), articleID, domain.LangJa, tc.title, tc.summary, tc.body)

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
			err := newService(repo).Publish(context.Background(), articleID, tc.reviewer)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCall, got)
		})
	}
}

func TestReject(t *testing.T) {
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
				RejectFn: func(_ context.Context, id string, reviewer string, now time.Time) error {
					got = &reviewCall{articleID: id, reviewer: reviewer, now: now}
					return nil
				},
			}
			err := newService(repo).Reject(context.Background(), articleID, tc.reviewer)

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

	cases := []struct {
		name        string
		articles    []domain.Article
		filter      []domain.Status
		limit       int
		wantErr     error
		wantIDs     []string
		wantTransOn []string // nil 想定 = 翻訳取得呼び出しなし
	}{
		{
			name:    "limit=0 は ErrInvalidField",
			limit:   0,
			filter:  domain.Statuses,
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "limit 負値は ErrInvalidField",
			limit:   -1,
			filter:  domain.Statuses,
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "limit 上限超過は ErrInvalidField",
			limit:   review.AdminListLimitMax + 1,
			filter:  domain.Statuses,
			wantErr: review.ErrInvalidField,
		},
		{
			name:        "全 status 列挙で 3 件 (DeriveStatus で各 status を導出)",
			articles:    threeArticles,
			filter:      domain.Statuses,
			limit:       50,
			wantIDs:     []string{"a-pending", "a-published", "a-rejected"},
			wantTransOn: []string{"a-pending", "a-published", "a-rejected"},
		},
		{
			name:        "pending のみで 1 件",
			articles:    threeArticles,
			filter:      []domain.Status{domain.StatusPending},
			limit:       50,
			wantIDs:     []string{"a-pending"},
			wantTransOn: []string{"a-pending"},
		},
		{
			name:        "published + rejected の複数指定で 2 件",
			articles:    threeArticles,
			filter:      []domain.Status{domain.StatusPublished, domain.StatusRejected},
			limit:       50,
			wantIDs:     []string{"a-published", "a-rejected"},
			wantTransOn: []string{"a-published", "a-rejected"},
		},
		{
			name:        "フィルタ後ゼロ件のとき翻訳取得は呼ばれない",
			articles:    []domain.Article{publishedArticle},
			filter:      []domain.Status{domain.StatusPending},
			limit:       50,
			wantIDs:     nil,
			wantTransOn: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotTransIDs []string
			var transCalled bool
			repo := &port.MockNewsRepo{
				ListArticlesFn: func(_ context.Context, _ int) ([]domain.Article, error) {
					return tc.articles, nil
				},
				ListTranslationsByArticleIDsFn: func(_ context.Context, ids []string) ([]domain.Translation, error) {
					transCalled = true
					gotTransIDs = ids
					return nil, nil
				},
			}

			got, err := newService(repo).List(context.Background(), tc.filter, tc.limit)
			assert.ErrorIs(t, err, tc.wantErr)

			var gotIDs []string
			for _, g := range got {
				gotIDs = append(gotIDs, g.Article.ArticleID)
			}
			assert.Equal(t, tc.wantIDs, gotIDs)
			assert.Equal(t, tc.wantTransOn != nil, transCalled, "翻訳取得呼び出し有無")
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
					return translations, nil
				},
			}

			got, err := newService(repo).Get(context.Background(), "01")
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantTransCalled, transCalled)
		})
	}
}
