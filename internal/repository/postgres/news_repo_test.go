//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
)

var pg *postgrestest.Postgres

func TestMain(m *testing.M) {
	os.Exit(postgrestest.RunMain(m, &pg))
}

func timePtr(t time.Time) *time.Time { return &t }

func insertFixtureArticle(t *testing.T, articleID, source, sourceURL string, reviewedAt, publishedAt *time.Time) {
	t.Helper()
	_, err := pg.Pool.Exec(context.Background(), `
		INSERT INTO news.news_articles (article_id, source, source_url, reviewed_at, published_at)
		VALUES ($1, $2, $3, $4, $5)
	`, articleID, source, sourceURL, reviewedAt, publishedAt)
	require.NoError(t, err)
}

func insertFixtureTranslation(t *testing.T, articleID, lang, title, summary, body string) {
	t.Helper()
	_, err := pg.Pool.Exec(context.Background(), `
		INSERT INTO news.news_article_translations (article_id, lang, title, summary, body)
		VALUES ($1, $2, $3, $4, $5)
	`, articleID, lang, title, summary, body)
	require.NoError(t, err)
}

func TestNewsRepositoryListPublished(t *testing.T) {
	t.Run("公開記事の取得条件と並び順", func(t *testing.T) {
		t.Run("一覧の取得", func(t *testing.T) {
			t.Run("指定したlangの翻訳を持つ公開中の記事が複数あるとき、それらを返す", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "list-multi-001", "aws", "https://example.com/list-multi-001", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-multi-001", "ja", "title1", "summary1", "body1")
				insertFixtureArticle(t, "list-multi-002", "aws", "https://example.com/list-multi-002", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-multi-002", "ja", "title2", "summary2", "body2")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				gotIDs := make([]string, len(got))
				for i, a := range got {
					gotIDs[i] = a.ArticleID
				}
				assert.ElementsMatch(t, []string{"list-multi-001", "list-multi-002"}, gotIDs)
			})

			t.Run("公開中だが指定したlangの翻訳を持たない記事は、一覧から除外される", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "list-no-lang-001", "aws", "https://example.com/list-no-lang-001", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-no-lang-001", "en", "title-en", "summary-en", "body-en")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				assert.Empty(t, got)
			})

			t.Run("未校閲(承認も却下もされていない)の記事は、一覧から除外される", func(t *testing.T) {
				pg.Truncate(t)
				insertFixtureArticle(t, "list-unreviewed-001", "aws", "https://example.com/list-unreviewed-001", nil, nil)
				insertFixtureTranslation(t, "list-unreviewed-001", "ja", "title", "summary", "body")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				assert.Empty(t, got)
			})

			t.Run("校閲済みだが公開日時が設定されていない記事は、一覧から除外される", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "list-no-published-at-001", "aws", "https://example.com/list-no-published-at-001", timePtr(now), nil)
				insertFixtureTranslation(t, "list-no-published-at-001", "ja", "title", "summary", "body")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				assert.Empty(t, got)
			})

			t.Run("公開後に却下された(公開日時が校閲日時より前になった)記事は、一覧から除外される", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				reviewedAt := now
				publishedAt := now.Add(-time.Hour)
				insertFixtureArticle(t, "list-rejected-001", "aws", "https://example.com/list-rejected-001", timePtr(reviewedAt), timePtr(publishedAt))
				insertFixtureTranslation(t, "list-rejected-001", "ja", "title", "summary", "body")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				assert.Empty(t, got)
			})

			t.Run("承認直後(公開日時と校閲日時が同時刻)の記事は、一覧に含まれる", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "list-approved-just-now-001", "aws", "https://example.com/list-approved-just-now-001", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-approved-just-now-001", "ja", "title", "summary", "body")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				gotIDs := make([]string, len(got))
				for i, a := range got {
					gotIDs[i] = a.ArticleID
				}
				assert.Contains(t, gotIDs, "list-approved-just-now-001")
			})

			t.Run("公開日時が異なる公開中の記事が複数あるとき、公開日時の新しい順に並ぶ", func(t *testing.T) {
				pg.Truncate(t)
				base := time.Now().UTC().Truncate(time.Second)
				oldest := base.Add(-2 * time.Hour)
				middle := base.Add(-1 * time.Hour)
				newest := base
				insertFixtureArticle(t, "list-order-oldest", "aws", "https://example.com/list-order-oldest", timePtr(oldest), timePtr(oldest))
				insertFixtureTranslation(t, "list-order-oldest", "ja", "t", "s", "b")
				insertFixtureArticle(t, "list-order-middle", "aws", "https://example.com/list-order-middle", timePtr(middle), timePtr(middle))
				insertFixtureTranslation(t, "list-order-middle", "ja", "t", "s", "b")
				insertFixtureArticle(t, "list-order-newest", "aws", "https://example.com/list-order-newest", timePtr(newest), timePtr(newest))
				insertFixtureTranslation(t, "list-order-newest", "ja", "t", "s", "b")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				gotIDs := make([]string, len(got))
				for i, a := range got {
					gotIDs[i] = a.ArticleID
				}
				assert.Equal(t, []string{"list-order-newest", "list-order-middle", "list-order-oldest"}, gotIDs)
			})

			t.Run("公開日時が同時刻の公開中の記事が複数あるとき、article_idの降順で並ぶ", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "list-tie-aaa", "aws", "https://example.com/list-tie-aaa", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-tie-aaa", "ja", "t", "s", "b")
				insertFixtureArticle(t, "list-tie-zzz", "aws", "https://example.com/list-tie-zzz", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-tie-zzz", "ja", "t", "s", "b")
				insertFixtureArticle(t, "list-tie-mmm", "aws", "https://example.com/list-tie-mmm", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "list-tie-mmm", "ja", "t", "s", "b")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 10)

				require.NoError(t, err)
				gotIDs := make([]string, len(got))
				for i, a := range got {
					gotIDs[i] = a.ArticleID
				}
				assert.Equal(t, []string{"list-tie-zzz", "list-tie-mmm", "list-tie-aaa"}, gotIDs)
			})

			t.Run("公開中の記事がlimitの件数を超えるとき、公開日時の新しい方からlimit件だけが返る", func(t *testing.T) {
				pg.Truncate(t)
				base := time.Now().UTC().Truncate(time.Second)
				oldest := base.Add(-2 * time.Hour)
				middle := base.Add(-1 * time.Hour)
				newest := base
				insertFixtureArticle(t, "list-limit-oldest", "aws", "https://example.com/list-limit-oldest", timePtr(oldest), timePtr(oldest))
				insertFixtureTranslation(t, "list-limit-oldest", "ja", "t", "s", "b")
				insertFixtureArticle(t, "list-limit-middle", "aws", "https://example.com/list-limit-middle", timePtr(middle), timePtr(middle))
				insertFixtureTranslation(t, "list-limit-middle", "ja", "t", "s", "b")
				insertFixtureArticle(t, "list-limit-newest", "aws", "https://example.com/list-limit-newest", timePtr(newest), timePtr(newest))
				insertFixtureTranslation(t, "list-limit-newest", "ja", "t", "s", "b")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.ListPublished(context.Background(), "ja", 2)

				require.NoError(t, err)
				gotIDs := make([]string, len(got))
				for i, a := range got {
					gotIDs[i] = a.ArticleID
				}
				assert.Equal(t, []string{"list-limit-newest", "list-limit-middle"}, gotIDs)
			})
		})

		t.Run("詳細の取得", func(t *testing.T) {
			t.Run("指定した記事が公開中で、指定したlangの翻訳を持つとき、記事の詳細(body・source_urlを含む)を返す", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "detail-found-001", "aws", "https://example.com/detail-found-001", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "detail-found-001", "ja", "detail-title", "detail-summary", "detail-body")
				repo := postgres.NewNewsRepository(pg.Pool)

				got, err := repo.GetPublishedByID(context.Background(), "detail-found-001", "ja")

				require.NoError(t, err)
				require.NotNil(t, got)
				assert.Equal(t, "detail-found-001", got.ArticleID)
				assert.Equal(t, "detail-body", got.Body)
				assert.Equal(t, "https://example.com/detail-found-001", got.SourceURL)
			})

			t.Run("指定した記事が公開中だが、指定したlangの翻訳を持たないとき、見つからない扱いになる", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				insertFixtureArticle(t, "detail-no-lang-001", "aws", "https://example.com/detail-no-lang-001", timePtr(now), timePtr(now))
				insertFixtureTranslation(t, "detail-no-lang-001", "en", "title-en", "summary-en", "body-en")
				repo := postgres.NewNewsRepository(pg.Pool)

				_, err := repo.GetPublishedByID(context.Background(), "detail-no-lang-001", "ja")

				assert.ErrorIs(t, err, port.ErrNotFound)
			})

			t.Run("指定した記事が未校閲のとき、見つからない扱いになる", func(t *testing.T) {
				pg.Truncate(t)
				insertFixtureArticle(t, "detail-unreviewed-001", "aws", "https://example.com/detail-unreviewed-001", nil, nil)
				insertFixtureTranslation(t, "detail-unreviewed-001", "ja", "title", "summary", "body")
				repo := postgres.NewNewsRepository(pg.Pool)

				_, err := repo.GetPublishedByID(context.Background(), "detail-unreviewed-001", "ja")

				assert.ErrorIs(t, err, port.ErrNotFound)
			})

			t.Run("指定した記事が却下されているとき、見つからない扱いになる", func(t *testing.T) {
				pg.Truncate(t)
				now := time.Now().UTC().Truncate(time.Second)
				reviewedAt := now
				publishedAt := now.Add(-time.Hour)
				insertFixtureArticle(t, "detail-rejected-001", "aws", "https://example.com/detail-rejected-001", timePtr(reviewedAt), timePtr(publishedAt))
				insertFixtureTranslation(t, "detail-rejected-001", "ja", "title", "summary", "body")
				repo := postgres.NewNewsRepository(pg.Pool)

				_, err := repo.GetPublishedByID(context.Background(), "detail-rejected-001", "ja")

				assert.ErrorIs(t, err, port.ErrNotFound)
			})

			t.Run("指定したarticle_idの記事が存在しないとき、見つからない扱いになる", func(t *testing.T) {
				pg.Truncate(t)
				repo := postgres.NewNewsRepository(pg.Pool)

				_, err := repo.GetPublishedByID(context.Background(), "detail-does-not-exist", "ja")

				assert.ErrorIs(t, err, port.ErrNotFound)
			})
		})
	})
}
