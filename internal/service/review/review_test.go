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

// 仕様 (FEATURE_SPEC §6.4): UpsertTranslation は title / summary / body / lang を検証する。
// 範囲外は ErrInvalidField を返し、repo.UpsertTranslation を呼ばない。
func TestUpsertTranslation_仕様_バリデーション(t *testing.T) {
	cases := []struct {
		name          string
		lang          string
		title         string
		summary       string
		body          string
		wantErr       error
		wantCallCount int
	}{
		{
			name:          "ja 正常",
			lang:          apinews.LangJa,
			title:         "a",
			summary:       "b",
			body:          "c",
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "en 正常",
			lang:          apinews.LangEn,
			title:         "T",
			summary:       "S",
			body:          "B",
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "境界上限 (マルチバイト)",
			lang:          apinews.LangJa,
			title:         strings.Repeat("あ", review.TitleMaxLen),
			summary:       strings.Repeat("い", review.SummaryMaxLen),
			body:          "本文",
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "lang 空",
			lang:          "",
			title:         "a",
			summary:       "b",
			body:          "c",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "lang 対応外",
			lang:          "fr",
			title:         "a",
			summary:       "b",
			body:          "c",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "タイトル空",
			lang:          apinews.LangJa,
			title:         "",
			summary:       "b",
			body:          "c",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "タイトル超過",
			lang:          apinews.LangJa,
			title:         strings.Repeat("a", review.TitleMaxLen+1),
			summary:       "b",
			body:          "c",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "要約空",
			lang:          apinews.LangJa,
			title:         "a",
			summary:       "",
			body:          "c",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "要約超過",
			lang:          apinews.LangJa,
			title:         "a",
			summary:       strings.Repeat("b", review.SummaryMaxLen+1),
			body:          "c",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "本文空",
			lang:          apinews.LangJa,
			title:         "a",
			summary:       "b",
			body:          "",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			var gotLang string
			repo := &port.MockNewsRepo{
				UpsertTranslationFn: func(_ context.Context, _ string, lang, _, _, _ string) error {
					callCount++
					gotLang = lang
					return nil
				},
			}
			err := newService(repo).UpsertTranslation(context.Background(), "article-1", tc.lang, tc.title, tc.summary, tc.body)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
			// repo に渡る lang が入力と一致する (呼ばれたケースのみ)
			assert.Equal(t, map[int]string{0: "", 1: tc.lang}[callCount], gotLang)
		})
	}
}

// 仕様: Publish / Reject は reviewer 必須、正常時は repo に注入した now を渡す。
func TestPublishReject_仕様_reviewer必須_nowを注入(t *testing.T) {
	cases := []struct {
		name          string
		call          func(svc *review.Service, reviewer string) error
		reviewer      string
		wantErr       error
		wantCallCount int
	}{
		{
			name: "Publish 正常",
			call: func(s *review.Service, r string) error {
				return s.Publish(context.Background(), "article-1", r)
			},
			reviewer:      "alice@example.com",
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name: "Publish 空 reviewer",
			call: func(s *review.Service, r string) error {
				return s.Publish(context.Background(), "article-1", r)
			},
			reviewer:      "",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name: "Reject 正常",
			call: func(s *review.Service, r string) error {
				return s.Reject(context.Background(), "article-1", r)
			},
			reviewer:      "alice@example.com",
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name: "Reject 空 reviewer",
			call: func(s *review.Service, r string) error {
				return s.Reject(context.Background(), "article-1", r)
			},
			reviewer:      "",
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			var gotNow time.Time
			var gotReviewer string
			recordCall := func(_ context.Context, _ string, reviewer string, now time.Time) error {
				callCount++
				gotNow = now
				gotReviewer = reviewer
				return nil
			}
			repo := &port.MockNewsRepo{PublishFn: recordCall, RejectFn: recordCall}
			svc := newService(repo)

			err := tc.call(svc, tc.reviewer)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
			assert.Equal(t, map[int]string{0: "", 1: tc.reviewer}[callCount], gotReviewer)
			assert.Equal(t, map[int]time.Time{0: {}, 1: fixedNow}[callCount], gotNow)
		})
	}
}

// 仕様: List は statusFilter = nil (全件) と Status 3 種で repo.ListByStatus を呼び分ける。
func TestList_仕様_statusフィルタ(t *testing.T) {
	pending := apinews.StatusPending
	published := apinews.StatusPublished
	rejected := apinews.StatusRejected

	cases := []struct {
		name          string
		filter        *apinews.Status
		limit         int
		wantErr       error
		wantCallCount int
	}{
		{
			name:          "全件 (nil)",
			filter:        nil,
			limit:         50,
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "pending のみ",
			filter:        &pending,
			limit:         50,
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "published のみ",
			filter:        &published,
			limit:         50,
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "rejected のみ",
			filter:        &rejected,
			limit:         50,
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "下限未満 limit",
			filter:        nil,
			limit:         review.AdminListLimitMin - 1,
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
		{
			name:          "上限超過 limit",
			filter:        nil,
			limit:         review.AdminListLimitMax + 1,
			wantErr:       review.ErrInvalidField,
			wantCallCount: 0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			var gotFilter *apinews.Status
			repo := &port.MockNewsRepo{
				ListByStatusFn: func(_ context.Context, f *apinews.Status, _ int) ([]apinews.ArticleWithTranslations, error) {
					callCount++
					gotFilter = f
					return nil, nil
				},
			}
			_, err := newService(repo).List(context.Background(), tc.filter, tc.limit)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
			assert.Equal(t, map[int]*apinews.Status{0: nil, 1: tc.filter}[callCount], gotFilter)
		})
	}
}

// 仕様: Get は repo.GetByID を呼び、ArticleWithTranslations を透過する。
func TestGet_仕様_ArticleWithTranslations透過(t *testing.T) {
	want := &apinews.ArticleWithTranslations{
		Article: apinews.Article{ArticleID: "01", Status: apinews.StatusPending},
		Translations: []apinews.Translation{
			{Lang: apinews.LangJa, Title: "ja title"},
		},
	}
	repo := &port.MockNewsRepo{
		GetByIDFn: func(_ context.Context, _ string) (*apinews.ArticleWithTranslations, error) {
			return want, nil
		},
	}

	got, err := newService(repo).Get(context.Background(), "01")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
