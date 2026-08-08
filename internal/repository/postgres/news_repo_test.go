package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
)

var sharedPG *postgrestest.Postgres

func TestMain(m *testing.M) {
	os.Exit(postgrestest.RunMain(m, &sharedPG))
}

var fixedNow = time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

func newRepo(t *testing.T) *postgres.NewsRepository {
	t.Helper()
	sharedPG.Truncate(t)
	return postgres.NewNewsRepository(sharedPG.Pool)
}

// articleExists は article_id の記事が永続化されているかを直接 SQL で確認するテストヘルパー。
func articleExists(t *testing.T, ctx context.Context, id string) bool {
	t.Helper()
	var exists bool
	err := sharedPG.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM news.news_articles WHERE article_id = $1)`,
		id,
	).Scan(&exists)
	require.NoError(t, err)
	return exists
}

// fetchTranslationsByArticleIDs は指定 article_id 群の翻訳行を直接 SQL で取得するテストヘルパー。
func fetchTranslationsByArticleIDs(t *testing.T, ctx context.Context, articleIDs []string) []domain.Translation {
	t.Helper()
	rows, err := sharedPG.Pool.Query(ctx,
		`SELECT article_id, lang, title, summary, body, created_at, updated_at
		   FROM news.news_article_translations
		  WHERE article_id = ANY($1)
		  ORDER BY article_id, lang`,
		articleIDs,
	)
	require.NoError(t, err)
	defer rows.Close()

	var translations []domain.Translation
	for rows.Next() {
		var tr domain.Translation
		require.NoError(t, rows.Scan(&tr.ArticleID, &tr.Lang, &tr.Title, &tr.Summary, &tr.Body, &tr.CreatedAt, &tr.UpdatedAt))
		translations = append(translations, tr)
	}
	require.NoError(t, rows.Err())
	return translations
}

// fetchArticleWithTranslations は記事 + 翻訳を直接 SQL で取得して合成し、Status を導出するテストヘルパー。
func fetchArticleWithTranslations(t *testing.T, id string) (*domain.ArticleWithTranslations, error) {
	t.Helper()
	ctx := context.Background()
	row := sharedPG.Pool.QueryRow(ctx,
		`SELECT article_id, source, source_url, tags,
		        source_published_at, published_at, ingested_at, reviewed_at, reviewer, updated_at
		   FROM news.news_articles
		  WHERE article_id = $1`,
		id,
	)
	var a domain.Article
	if err := row.Scan(
		&a.ArticleID, &a.Source, &a.SourceURL, &a.Tags,
		&a.SourcePublishedAt, &a.PublishedAt, &a.IngestedAt,
		&a.ReviewedAt, &a.Reviewer, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	a.Status = domain.DeriveStatus(a)
	translations := fetchTranslationsByArticleIDs(t, ctx, []string{id})
	return &domain.ArticleWithTranslations{Article: a, Translations: translations}, nil
}

// applyPublish は Publish 相当の承認遷移 (published_at = reviewed_at = at) を直接 SQL で行うテストヘルパー。
func applyPublish(t *testing.T, ctx context.Context, id string, reviewer string, at time.Time) {
	t.Helper()
	tag, err := sharedPG.Pool.Exec(ctx,
		`UPDATE news.news_articles
		    SET published_at = $2,
		        reviewed_at = $2,
		        reviewer = $3
		  WHERE article_id = $1`,
		id, at, reviewer,
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

// applyReject は Reject 相当の却下遷移 (published_at は保持) を直接 SQL で行うテストヘルパー。
func applyReject(t *testing.T, ctx context.Context, id string, reviewer string, at time.Time) {
	t.Helper()
	tag, err := sharedPG.Pool.Exec(ctx,
		`UPDATE news.news_articles
		    SET reviewed_at = $2,
		        reviewer = $3
		  WHERE article_id = $1`,
		id, at, reviewer,
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

// insertTranslation は翻訳行を直接 SQL で INSERT するテストヘルパー (UPSERT の ON CONFLICT 分岐は使わない)。
func insertTranslation(t *testing.T, ctx context.Context, articleID, lang, title, summary, body string) {
	t.Helper()
	_, err := sharedPG.Pool.Exec(ctx,
		`INSERT INTO news.news_article_translations (article_id, lang, title, summary, body)
		 VALUES ($1, $2, $3, $4, $5)`,
		articleID, lang, title, summary, body,
	)
	require.NoError(t, err)
}

// seedWithJa は ja 翻訳を持つ記事を 1 件 INSERT して status を指定値まで遷移させる。
// 呼び出しのたびに time.Now() を使うため、連続呼び出しで published_at / reviewed_at / ingested_at (DB 側 DEFAULT now()) に差がつく。
func seedWithJa(t *testing.T, repo *postgres.NewsRepository, id string, status domain.Status) domain.Article {
	t.Helper()
	ctx := context.Background()
	article := domain.Article{
		ArticleID: id,
		Source:    "aws",
		SourceURL: "https://example.com/" + id,
		Tags:      []string{"compute"},
		Status:    domain.StatusPending,
	}
	inserted, err := repo.InsertArticleWithTranslation(ctx, article, domain.LangJa, "ja-"+id, "s-"+id, "b-"+id)
	require.NoError(t, err)
	require.True(t, inserted)

	switch status {
	case domain.StatusPublished:
		applyPublish(t, ctx, id, "seed@example.com", time.Now())
	case domain.StatusRejected:
		applyReject(t, ctx, id, "seed@example.com", time.Now())
	}
	article.Status = status
	return article
}

// seedPublishedAt は ja 翻訳を持つ記事を 1 件 INSERT し、published_at を指定時刻に固定して公開する。
func seedPublishedAt(t *testing.T, ctx context.Context, repo *postgres.NewsRepository, id string, publishedAt time.Time) {
	t.Helper()
	_, err := repo.InsertArticleWithTranslation(ctx, domain.Article{
		ArticleID: id, Source: "aws", SourceURL: "https://example.com/" + id,
		Tags: []string{}, Status: domain.StatusPending,
	}, domain.LangJa, "ja-"+id, "s", "b")
	require.NoError(t, err)
	applyPublish(t, ctx, id, "seed@example.com", publishedAt)
}

func TestInsertArticleWithTranslation(t *testing.T) {
	ctx := context.Background()
	baseArticle := domain.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{"x"}, Status: domain.StatusPending,
	}

	t.Run("記事と翻訳のINSERT", func(t *testing.T) {
		t.Run("新規記事のとき、trueを返し記事と翻訳が保存される", func(t *testing.T) {
			repo := newRepo(t)

			inserted, err := repo.InsertArticleWithTranslation(ctx, baseArticle, domain.LangJa, "最初のタイトル", "最初の要約", "最初の本文")

			require.NoError(t, err)
			assert.True(t, inserted)
			aw, err := fetchArticleWithTranslations(t, "01")
			require.NoError(t, err)
			assert.Equal(t, "https://aws.amazon.com/a", aw.Article.SourceURL)
			require.Len(t, aw.Translations, 1)
			assert.Equal(t, "最初のタイトル", aw.Translations[0].Title)
			assert.Equal(t, "最初の要約", aw.Translations[0].Summary)
			assert.Equal(t, "最初の本文", aw.Translations[0].Body)
		})

		t.Run("同一article_idが既存のとき、falseを返し既存翻訳は上書きされない", func(t *testing.T) {
			repo := newRepo(t)
			_, err := repo.InsertArticleWithTranslation(ctx, baseArticle, domain.LangJa, "最初のタイトル", "最初の要約", "最初の本文")
			require.NoError(t, err)

			inserted, err := repo.InsertArticleWithTranslation(ctx, baseArticle, domain.LangJa, "再送のタイトル", "再送の要約", "再送の本文")

			require.NoError(t, err)
			assert.False(t, inserted)
			aw, err := fetchArticleWithTranslations(t, "01")
			require.NoError(t, err)
			require.Len(t, aw.Translations, 1)
			assert.Equal(t, "最初のタイトル", aw.Translations[0].Title)
		})

		t.Run("別article_idでもsource_urlが既存のとき、falseを返し記事も翻訳も追加されない", func(t *testing.T) {
			repo := newRepo(t)
			_, err := repo.InsertArticleWithTranslation(ctx, baseArticle, domain.LangJa, "最初のタイトル", "最初の要約", "最初の本文")
			require.NoError(t, err)
			renumbered := baseArticle
			renumbered.ArticleID = "99"

			inserted, err := repo.InsertArticleWithTranslation(ctx, renumbered, domain.LangJa, "取り直しのタイトル", "取り直しの要約", "取り直しの本文")

			require.NoError(t, err)
			assert.False(t, inserted)
			assert.False(t, articleExists(t, ctx, "99"))
			translations := fetchTranslationsByArticleIDs(t, ctx, []string{"01", "99"})
			require.Len(t, translations, 1)
			assert.Equal(t, "最初のタイトル", translations[0].Title)
		})
	})
}

func TestTranslation_UnsupportedLang(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	t.Run("未対応langの拒否", func(t *testing.T) {
		t.Run("取込でfrの翻訳を渡すとき、ErrInvalidPersistedValueになり記事も保存されない", func(t *testing.T) {
			article := domain.Article{
				ArticleID: "02", Source: "aws", SourceURL: "https://aws.amazon.com/b",
				Tags: []string{}, Status: domain.StatusPending,
			}

			_, err := repo.InsertArticleWithTranslation(ctx, article, "fr", "t", "s", "b")

			assert.ErrorIs(t, err, port.ErrInvalidPersistedValue)
			assert.False(t, articleExists(t, ctx, "02"))
		})
	})
}

func TestListPublished(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	// ja のみ翻訳あり
	_, err := repo.InsertArticleWithTranslation(ctx, domain.Article{
		ArticleID: "ja-only", Source: "aws", SourceURL: "https://example.com/ja-only",
		Tags: []string{}, Status: domain.StatusPending,
	}, domain.LangJa, "ja", "s", "b")
	require.NoError(t, err)
	applyPublish(t, ctx, "ja-only", "alice@example.com", time.Now())

	// ja + en 両方 (en は運用者が DB を直接更新して後追加される想定)
	_, err = repo.InsertArticleWithTranslation(ctx, domain.Article{
		ArticleID: "both", Source: "aws", SourceURL: "https://example.com/both",
		Tags: []string{}, Status: domain.StatusPending,
	}, domain.LangJa, "ja", "s", "b")
	require.NoError(t, err)
	insertTranslation(t, ctx, "both", domain.LangEn, "en", "s", "b")
	applyPublish(t, ctx, "both", "alice@example.com", time.Now())

	t.Run("公開記事一覧の取得", func(t *testing.T) {
		cases := []struct {
			name    string
			lang    string
			wantIDs []string
		}{
			{
				name:    "lang=jaのとき、公開済み2件を返す",
				lang:    domain.LangJa,
				wantIDs: []string{"both", "ja-only"}, // published_at 降順 (both が新しい)
			},
			{
				name:    "lang=enのとき、en翻訳を持つ1件を返す (ja-only除外)",
				lang:    domain.LangEn,
				wantIDs: []string{"both"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				items, err := repo.ListPublished(ctx, tc.lang, 10)
				require.NoError(t, err)
				gotIDs := make([]string, 0, len(items))
				for _, it := range items {
					gotIDs = append(gotIDs, it.ArticleID)
				}
				// published_at の順序までは厳密に検証しない (両方 time.Now() で微差) が、集合として等しいことを検証
				assert.ElementsMatch(t, tc.wantIDs, gotIDs)
			})
		}
	})

	t.Run("公開記事一覧のlimit打ち切りと並び順", func(t *testing.T) {
		t1 := fixedNow
		t2 := fixedNow.Add(1 * time.Hour)
		t3 := fixedNow.Add(2 * time.Hour)

		cases := []struct {
			name    string
			seed    func(t *testing.T, repo *postgres.NewsRepository)
			limit   int
			wantIDs []string
		}{
			{
				name: "published_atが異なる公開記事3件でlimit=2のとき、published_atの新しい2件だけが返る",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", t1)
					seedPublishedAt(t, ctx, repo, "TST-0002", t2)
					seedPublishedAt(t, ctx, repo, "TST-0003", t3)
				},
				limit:   2,
				wantIDs: []string{"TST-0003", "TST-0002"},
			},
			{
				name: "published_atが異なる公開記事3件でlimit=3のとき、3件全件が返る",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", t1)
					seedPublishedAt(t, ctx, repo, "TST-0002", t2)
					seedPublishedAt(t, ctx, repo, "TST-0003", t3)
				},
				limit:   3,
				wantIDs: []string{"TST-0003", "TST-0002", "TST-0001"},
			},
			{
				name: "published_atが異なる2件のとき、article_idの大小によらずpublished_atの新しい順に返る",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0009", t1)
					seedPublishedAt(t, ctx, repo, "TST-0001", t2)
				},
				limit:   10,
				wantIDs: []string{"TST-0001", "TST-0009"},
			},
			{
				name: "published_atが同時刻の2件のとき、article_idの降順で返る",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", t1)
					seedPublishedAt(t, ctx, repo, "TST-0002", t1)
				},
				limit:   10,
				wantIDs: []string{"TST-0002", "TST-0001"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				sharedPG.Truncate(t)
				repo := postgres.NewNewsRepository(sharedPG.Pool)
				tc.seed(t, repo)

				items, err := repo.ListPublished(ctx, domain.LangJa, tc.limit)
				require.NoError(t, err)
				gotIDs := make([]string, 0, len(items))
				for _, it := range items {
					gotIDs = append(gotIDs, it.ArticleID)
				}
				assert.Equal(t, tc.wantIDs, gotIDs)
			})
		}
	})

	t.Run("公開記事一覧の公開条件による除外", func(t *testing.T) {
		cases := []struct {
			name    string
			seed    func(t *testing.T, repo *postgres.NewsRepository)
			wantIDs []string
		}{
			{
				name: "承認直後 (published_at = reviewed_at)の記事は一覧に含まれる",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", fixedNow)
				},
				wantIDs: []string{"TST-0001"},
			},
			{
				name: "未校閲 (reviewed_at未設定)の記事があるとき、一覧に含まれない",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", fixedNow)
					_ = seedWithJa(t, repo, "TST-0002", domain.StatusPending)
				},
				wantIDs: []string{"TST-0001"},
			},
			{
				name: "校閲のみで公開時刻が無い (published_at未設定)記事があるとき、一覧に含まれない",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", fixedNow)
					_ = seedWithJa(t, repo, "TST-0002", domain.StatusRejected)
				},
				wantIDs: []string{"TST-0001"},
			},
			{
				name: "承認後に却下された (published_at < reviewed_at)記事があるとき、一覧に含まれない",
				seed: func(t *testing.T, repo *postgres.NewsRepository) {
					seedPublishedAt(t, ctx, repo, "TST-0001", fixedNow)
					seedPublishedAt(t, ctx, repo, "TST-0002", fixedNow)
					applyReject(t, ctx, "TST-0002", "seed@example.com", fixedNow.Add(1*time.Hour))
				},
				wantIDs: []string{"TST-0001"},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				sharedPG.Truncate(t)
				repo := postgres.NewNewsRepository(sharedPG.Pool)
				tc.seed(t, repo)

				items, err := repo.ListPublished(ctx, domain.LangJa, 10)
				require.NoError(t, err)
				gotIDs := make([]string, 0, len(items))
				for _, it := range items {
					gotIDs = append(gotIDs, it.ArticleID)
				}
				assert.ElementsMatch(t, tc.wantIDs, gotIDs)
			})
		}
	})
}

func TestGetPublishedByID(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	// pending + ja
	_ = seedWithJa(t, repo, "pending", domain.StatusPending)
	// published + ja
	_ = seedWithJa(t, repo, "published-ja", domain.StatusPublished)
	// published + ja + en
	_ = seedWithJa(t, repo, "published-both", domain.StatusPending)
	insertTranslation(t, ctx, "published-both", domain.LangEn, "en", "s", "b")
	applyPublish(t, ctx, "published-both", "alice@example.com", time.Now())
	// rejected + ja
	_ = seedWithJa(t, repo, "rejected", domain.StatusRejected)

	t.Run("公開記事詳細の取得", func(t *testing.T) {
		validCases := []struct {
			name string
			id   string
			lang string
		}{
			{
				name: "published + jaのとき、記事を返す",
				id:   "published-ja",
				lang: domain.LangJa,
			},
			{
				name: "published + en翻訳ありのとき、記事を返す",
				id:   "published-both",
				lang: domain.LangEn,
			},
		}
		for _, tc := range validCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := repo.GetPublishedByID(ctx, tc.id, tc.lang)
				require.NoError(t, err)
			})
		}

		notFoundCases := []struct {
			name string
			id   string
			lang string
		}{
			{
				name: "published + en翻訳なしのとき、ErrNotFoundになる",
				id:   "published-ja",
				lang: domain.LangEn,
			},
			{
				name: "pendingのとき、ErrNotFoundになる",
				id:   "pending",
				lang: domain.LangJa,
			},
			{
				name: "rejectedのとき、ErrNotFoundになる",
				id:   "rejected",
				lang: domain.LangJa,
			},
			{
				name: "非存在idのとき、ErrNotFoundになる",
				id:   "ghost",
				lang: domain.LangJa,
			},
		}
		for _, tc := range notFoundCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := repo.GetPublishedByID(ctx, tc.id, tc.lang)
				assert.ErrorIs(t, err, port.ErrNotFound)
			})
		}
	})
}

func TestColumnWidthDrift(t *testing.T) {
	t.Run("domainの最大文字数と適用済みスキーマの列幅の整合", func(t *testing.T) {
		cases := []struct {
			name   string
			table  string
			column string
			want   int
		}{
			{
				name:   "記事IDの最大文字数がnews_articles.article_idの列幅と一致する",
				table:  "news_articles",
				column: "article_id",
				want:   domain.MaxArticleIDLength,
			},
			{
				name:   "記事IDの最大文字数がnews_article_translations.article_idの列幅と一致する",
				table:  "news_article_translations",
				column: "article_id",
				want:   domain.MaxArticleIDLength,
			},
			{
				name:   "ソース種別の最大文字数がnews_articles.sourceの列幅と一致する",
				table:  "news_articles",
				column: "source",
				want:   domain.MaxSourceLength,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var width int
				err := sharedPG.Pool.QueryRow(context.Background(),
					`SELECT character_maximum_length
					   FROM information_schema.columns
					  WHERE table_schema = 'news' AND table_name = $1 AND column_name = $2`,
					tc.table, tc.column,
				).Scan(&width)

				require.NoError(t, err)
				assert.Equal(t, tc.want, width)
			})
		}
	})
}
