package review_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/review"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
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

// 仕様 (FEATURE_SPEC §6.4): UpsertTranslation は title / summary / body の長さを検証してから repo に転送する。
// 不正入力は ErrInvalidField で repo に到達せず、正常入力時は repo にすべての値がそのまま渡る。
func TestUpsertTranslation_仕様_バリデーション(t *testing.T) {
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
			wantErr: nil,
			wantCall: &upsertCall{
				articleID: articleID, lang: apinews.LangJa,
				title: "a", summary: "b", body: "c",
			},
		},
		{
			name:    "境界上限 (マルチバイト)",
			title:   strings.Repeat("あ", review.TitleMaxLen),
			summary: strings.Repeat("い", review.SummaryMaxLen),
			body:    "本文",
			wantErr: nil,
			wantCall: &upsertCall{
				articleID: articleID, lang: apinews.LangJa,
				title:   strings.Repeat("あ", review.TitleMaxLen),
				summary: strings.Repeat("い", review.SummaryMaxLen),
				body:    "本文",
			},
		},
		{
			name:     "タイトル空",
			title:    "",
			summary:  "b",
			body:     "c",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
		},
		{
			name:     "タイトル超過",
			title:    strings.Repeat("a", review.TitleMaxLen+1),
			summary:  "b",
			body:     "c",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
		},
		{
			name:     "要約空",
			title:    "a",
			summary:  "",
			body:     "c",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
		},
		{
			name:     "要約超過",
			title:    "a",
			summary:  strings.Repeat("b", review.SummaryMaxLen+1),
			body:     "c",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
		},
		{
			name:     "本文空",
			title:    "a",
			summary:  "b",
			body:     "",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
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
			err := newService(repo).UpsertTranslation(context.Background(), articleID, apinews.LangJa, tc.title, tc.summary, tc.body)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCall, got)
		})
	}
}

// 仕様: Publish は空 reviewer を ErrInvalidField で弾き、正常時は repo に articleID / reviewer / 注入された now が渡る。
func TestPublish_仕様_reviewerと注入now(t *testing.T) {
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
			wantErr:  nil,
			wantCall: &reviewCall{articleID: articleID, reviewer: "alice@example.com", now: fixedNow},
		},
		{
			name:     "空 reviewer は ErrInvalidField で repo に到達しない",
			reviewer: "",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
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

// 仕様: Reject は空 reviewer を ErrInvalidField で弾き、正常時は repo に articleID / reviewer / 注入された now が渡る。
func TestReject_仕様_reviewerと注入now(t *testing.T) {
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
			wantErr:  nil,
			wantCall: &reviewCall{articleID: articleID, reviewer: "alice@example.com", now: fixedNow},
		},
		{
			name:     "空 reviewer は ErrInvalidField で repo に到達しない",
			reviewer: "",
			wantErr:  review.ErrInvalidField,
			wantCall: nil,
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

// 仕様: List は limit を検証する。範囲外は ErrInvalidField、範囲内はエラーなし。
func TestList_仕様_limitバリデーション(t *testing.T) {
	cases := []struct {
		name    string
		limit   int
		wantErr error
	}{
		{
			name:    "正常 limit",
			limit:   50,
			wantErr: nil,
		},
		{
			name:    "0 はエラー",
			limit:   0,
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "負値はエラー",
			limit:   -1,
			wantErr: review.ErrInvalidField,
		},
		{
			name:    "上限超過はエラー",
			limit:   review.AdminListLimitMax + 1,
			wantErr: review.ErrInvalidField,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListArticlesFn: func(_ context.Context, _ int) ([]apinews.Article, error) {
					return nil, nil
				},
				ListTranslationsByArticleIDsFn: func(_ context.Context, _ []string) ([]apinews.Translation, error) {
					return nil, nil
				},
			}
			_, err := newService(repo).List(context.Background(), apinews.Statuses, tc.limit)

			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// 仕様: List は repo から取得した記事に DeriveStatus を適用し、要求 status 集合に含まれるものだけ返す。
// (repo は status 概念を持たないため、絞り込みは service の責務)
func TestList_仕様_statusフィルタはservice層で適用(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-1 * time.Hour)
	pendingArticle := apinews.Article{ArticleID: "a-pending"}
	publishedArticle := apinews.Article{ArticleID: "a-published", ReviewedAt: &now, PublishedAt: &now}
	rejectedArticle := apinews.Article{ArticleID: "a-rejected", ReviewedAt: &now, PublishedAt: &earlier}

	cases := []struct {
		name        string
		filter      []apinews.Status
		wantIDs     []string
		wantTransOn []string
	}{
		{
			name:        "全 status 列挙で 3 件",
			filter:      apinews.Statuses,
			wantIDs:     []string{"a-pending", "a-published", "a-rejected"},
			wantTransOn: []string{"a-pending", "a-published", "a-rejected"},
		},
		{
			name:        "pending のみで 1 件",
			filter:      []apinews.Status{apinews.StatusPending},
			wantIDs:     []string{"a-pending"},
			wantTransOn: []string{"a-pending"},
		},
		{
			name:        "published + rejected の複数指定で 2 件",
			filter:      []apinews.Status{apinews.StatusPublished, apinews.StatusRejected},
			wantIDs:     []string{"a-published", "a-rejected"},
			wantTransOn: []string{"a-published", "a-rejected"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var gotTransIDs []string
			repo := &port.MockNewsRepo{
				ListArticlesFn: func(_ context.Context, _ int) ([]apinews.Article, error) {
					return []apinews.Article{pendingArticle, publishedArticle, rejectedArticle}, nil
				},
				ListTranslationsByArticleIDsFn: func(_ context.Context, ids []string) ([]apinews.Translation, error) {
					gotTransIDs = ids
					return nil, nil
				},
			}

			got, err := newService(repo).List(context.Background(), tc.filter, 50)
			require.NoError(t, err)

			gotIDs := make([]string, len(got))
			for i, g := range got {
				gotIDs[i] = g.Article.ArticleID
			}
			assert.Equal(t, tc.wantIDs, gotIDs)
			assert.Equal(t, tc.wantTransOn, gotTransIDs, "翻訳取得は status フィルタ後の article_id でのみ行われる")
		})
	}
}

// 仕様: 全件 0 件 (フィルタ後) のとき翻訳取得は呼ばれない。
func TestList_仕様_フィルタ後ゼロ件で翻訳取得しない(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	publishedOnly := apinews.Article{ArticleID: "x", ReviewedAt: &now, PublishedAt: &now}

	var transCallCount int
	repo := &port.MockNewsRepo{
		ListArticlesFn: func(_ context.Context, _ int) ([]apinews.Article, error) {
			return []apinews.Article{publishedOnly}, nil
		},
		ListTranslationsByArticleIDsFn: func(_ context.Context, _ []string) ([]apinews.Translation, error) {
			transCallCount++
			return nil, nil
		},
	}
	got, err := newService(repo).List(context.Background(), []apinews.Status{apinews.StatusPending}, 50)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 0, transCallCount)
}

// 仕様: Get は記事 + その翻訳を取得して合成する。記事が無ければ翻訳取得は呼ばれない。
func TestGet_仕様_記事と翻訳を結合(t *testing.T) {
	article := &apinews.Article{ArticleID: "01", Status: apinews.StatusPending}
	translations := []apinews.Translation{
		{ArticleID: "01", Lang: apinews.LangJa, Title: "ja title"},
	}
	var transCallCount int
	repo := &port.MockNewsRepo{
		GetArticleByIDFn: func(_ context.Context, _ string) (*apinews.Article, error) {
			return article, nil
		},
		ListTranslationsByArticleIDsFn: func(_ context.Context, ids []string) ([]apinews.Translation, error) {
			transCallCount++
			assert.Equal(t, []string{"01"}, ids)
			return translations, nil
		},
	}

	got, err := newService(repo).Get(context.Background(), "01")
	require.NoError(t, err)
	assert.Equal(t, *article, got.Article)
	assert.Equal(t, translations, got.Translations)
	assert.Equal(t, 1, transCallCount)
}

// 仕様: Get は GetArticleByID が ErrNotFound のとき翻訳取得を呼ばない。
func TestGet_仕様_記事なしなら翻訳取得しない(t *testing.T) {
	var transCallCount int
	repo := &port.MockNewsRepo{
		GetArticleByIDFn: func(_ context.Context, _ string) (*apinews.Article, error) {
			return nil, port.ErrNotFound
		},
		ListTranslationsByArticleIDsFn: func(_ context.Context, _ []string) ([]apinews.Translation, error) {
			transCallCount++
			return nil, nil
		},
	}

	_, err := newService(repo).Get(context.Background(), "ghost")
	assert.ErrorIs(t, err, port.ErrNotFound)
	assert.Equal(t, 0, transCallCount)
}
