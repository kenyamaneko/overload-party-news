package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

func TestDeriveStatus(t *testing.T) {
	t.Run("記事ステータスの導出", func(t *testing.T) {
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
		earlier := now.Add(-1 * time.Hour)

		cases := []struct {
			name        string
			reviewedAt  *time.Time
			publishedAt *time.Time
			want        domain.Status
		}{
			{
				name:       "未校閲のとき、pendingになる",
				reviewedAt: nil,
				want:       domain.StatusPending,
			},
			{
				name:        "reviewed_at = published_atのとき、publishedになる",
				reviewedAt:  &now,
				publishedAt: &now,
				want:        domain.StatusPublished,
			},
			{
				name:        "published_at < reviewed_atのとき、rejectedになる",
				reviewedAt:  &now,
				publishedAt: &earlier,
				want:        domain.StatusRejected,
			},
			{
				name:        "publish経験なしで校閲済み (published_atがnil)のとき、rejectedになる",
				reviewedAt:  &now,
				publishedAt: nil,
				want:        domain.StatusRejected,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := domain.DeriveStatus(domain.Article{
					ReviewedAt:  tc.reviewedAt,
					PublishedAt: tc.publishedAt,
				})
				assert.Equal(t, tc.want, got)
			})
		}
	})
}
