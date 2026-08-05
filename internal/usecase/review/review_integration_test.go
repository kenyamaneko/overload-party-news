//go:build integration

package review_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
)

var (
	integrationFixedNow = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	// seedReviewedAt は ingested_at と独立に固定し、一覧順序へ影響させない (published 導出用)。
	seedReviewedAt = time.Date(2026, 4, 19, 9, 0, 0, 0, time.UTC)
	seedIngestBase = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
)

// seedSpec は 1 記事の seed 指定。
type seedSpec struct {
	id     string
	status domain.Status
}

// seedArticleAt は指定 status の記事を 1 件作り、その ingested_at を ingestedAt にピン留めする。
func seedArticleAt(t *testing.T, ctx context.Context, pg *postgrestest.Postgres, repo *postgres.NewsRepository, id string, status domain.Status, ingestedAt time.Time) {
	t.Helper()
	_, err := repo.InsertArticleWithTranslation(ctx, domain.Article{
		ArticleID: id,
		Source:    domain.SourceAws,
		SourceURL: "https://example.com/" + id,
		Tags:      []string{},
		Status:    domain.StatusPending,
	}, domain.LangJa, "ja-"+id, "s-"+id, "b-"+id)
	require.NoError(t, err)

	switch status {
	case domain.StatusPending:
		// reviewed_at 未設定が pending の定義 (domain.DeriveStatus) なので遷移しない。
	case domain.StatusPublished:
		require.NoError(t, repo.Publish(ctx, id, "seed@example.com", seedReviewedAt))
	default:
		t.Fatalf("unsupported seed status: %s", status)
	}

	// 一覧順序 (ListArticles の ingested_at DESC) を決定的にするため、status 遷移と独立に取込日時を直接ピン留めする。
	_, err = pg.Pool.Exec(ctx,
		`UPDATE news.news_articles SET ingested_at = $1 WHERE article_id = $2`,
		ingestedAt, id)
	require.NoError(t, err)
}

func TestListLimitWindow(t *testing.T) {
	ctx := context.Background()
	pg, err := postgrestest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pg.Close(ctx))
	})
	repo := postgres.NewNewsRepository(pg.Pool)

	t.Run("limit窓とstatusフィルタ", func(t *testing.T) {
		cases := []struct {
			name    string
			seeds   []seedSpec // ingested_at 昇順 (末尾が最新)
			filter  []domain.Status
			limit   int
			wantIDs []string // review.List の返却順 (ingested_at DESC) で期待値を並べる
		}{
			{
				name: "件数がlimitを超えるとき、最新limit件で打ち切られingested_at DESC上位が残る",
				seeds: []seedSpec{
					{"a1", domain.StatusPending},
					{"a2", domain.StatusPending},
					{"a3", domain.StatusPending},
					{"a4", domain.StatusPending},
					{"a5", domain.StatusPending},
				},
				filter:  domain.Statuses,
				limit:   3,
				wantIDs: []string{"a5", "a4", "a3"},
			},
			{
				name: "limit超過のとき、フィルタは最新limit件の窓内だけに効き窓外の該当statusは拾い直さない",
				seeds: []seedSpec{
					{"pub-out-a", domain.StatusPublished},
					{"pub-out-b", domain.StatusPublished},
					{"pub-in", domain.StatusPublished},
					{"pend-in-a", domain.StatusPending},
					{"pend-in-b", domain.StatusPending},
				},
				filter:  []domain.Status{domain.StatusPublished},
				limit:   3,
				wantIDs: []string{"pub-in"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				pg.Truncate(t)
				for i, s := range tc.seeds {
					seedArticleAt(t, ctx, pg, repo, s.id, s.status, seedIngestBase.Add(time.Duration(i)*time.Minute))
				}

				uc := review.New(repo, repo, func() time.Time { return integrationFixedNow })
				got, err := uc.List(ctx, tc.filter, tc.limit)
				require.NoError(t, err)

				gotIDs := make([]string, 0, len(got))
				for _, g := range got {
					gotIDs = append(gotIDs, g.Article.ArticleID)
				}
				assert.Equal(t, tc.wantIDs, gotIDs)
			})
		}
	})
}
