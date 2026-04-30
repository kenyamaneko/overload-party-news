package apinews_test

import (
	"testing"
	"time"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// 仕様: DeriveStatus は (reviewed_at, published_at) から status を一意に導出する。
// repo の WHERE 述語と 1:1 対応する SSoT。
func TestDeriveStatus_仕様(t *testing.T) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-1 * time.Hour)

	cases := []struct {
		name        string
		reviewedAt  *time.Time
		publishedAt *time.Time
		want        apinews.Status
	}{
		{
			name:       "未校閲は pending",
			reviewedAt: nil,
			want:       apinews.StatusPending,
		},
		{
			name:        "Publish 直後 (reviewed_at = published_at) は published",
			reviewedAt:  &now,
			publishedAt: &now,
			want:        apinews.StatusPublished,
		},
		{
			name:        "publish 後に Reject (published_at < reviewed_at) は rejected",
			reviewedAt:  &now,
			publishedAt: &earlier,
			want:        apinews.StatusRejected,
		},
		{
			name:        "publish 経験なしの Reject (published_at IS NULL ∧ reviewed_at IS NOT NULL) は rejected",
			reviewedAt:  &now,
			publishedAt: nil,
			want:        apinews.StatusRejected,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := apinews.DeriveStatus(apinews.Article{
				ReviewedAt:  tc.reviewedAt,
				PublishedAt: tc.publishedAt,
			})
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
