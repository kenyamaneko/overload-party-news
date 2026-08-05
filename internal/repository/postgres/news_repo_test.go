package postgres_test

import (
	"context"
	"errors"
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

// translationByLang は ArticleWithTranslations から指定 lang の翻訳を返す純粋なルックアップ。
// 非存在時は t.Fatalf で停止する。
func translationByLang(t *testing.T, aw *domain.ArticleWithTranslations, lang string) domain.Translation {
	t.Helper()
	for _, tr := range aw.Translations {
		if tr.Lang == lang {
			return tr
		}
	}
	t.Fatalf("translation not found: lang=%s", lang)
	return domain.Translation{}
}

// fetchArticleWithTranslations は repo から記事 + 翻訳を取得して合成し、Status を導出するテストヘルパー。
// repo は分離された I/O を提供するだけで、合成と Status 導出は本来 usecase 層の責務だが、
// 本ファイルでは便宜的にテスト内で組み立てる。
func fetchArticleWithTranslations(t *testing.T, repo *postgres.NewsRepository, id string) (*domain.ArticleWithTranslations, error) {
	t.Helper()
	ctx := context.Background()
	article, err := repo.GetArticleByID(ctx, id)
	if err != nil {
		return nil, err
	}
	translations, err := repo.ListTranslationsByArticleIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	article.Status = domain.DeriveStatus(*article)
	return &domain.ArticleWithTranslations{Article: *article, Translations: translations}, nil
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
		require.NoError(t, repo.Publish(ctx, id, "seed@example.com", time.Now()))
	case domain.StatusRejected:
		require.NoError(t, repo.Reject(ctx, id, "seed@example.com", time.Now()))
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
	require.NoError(t, repo.Publish(ctx, id, "seed@example.com", publishedAt))
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
			aw, err := fetchArticleWithTranslations(t, repo, "01")
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
			aw, err := fetchArticleWithTranslations(t, repo, "01")
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
			_, err = repo.GetArticleByID(ctx, "99")
			assert.ErrorIs(t, err, port.ErrNotFound)
			translations, err := repo.ListTranslationsByArticleIDs(ctx, []string{"01", "99"})
			require.NoError(t, err)
			require.Len(t, translations, 1)
			assert.Equal(t, "最初のタイトル", translations[0].Title)
		})
	})
}

func TestTranslation_UnsupportedLang(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	_ = seedWithJa(t, repo, "01", domain.StatusPending)

	t.Run("未対応langの拒否", func(t *testing.T) {
		t.Run("取込でfrの翻訳を渡すとき、ErrInvalidPersistedValueになり記事も保存されない", func(t *testing.T) {
			article := domain.Article{
				ArticleID: "02", Source: "aws", SourceURL: "https://aws.amazon.com/b",
				Tags: []string{}, Status: domain.StatusPending,
			}

			_, err := repo.InsertArticleWithTranslation(ctx, article, "fr", "t", "s", "b")

			assert.ErrorIs(t, err, port.ErrInvalidPersistedValue)
			_, err = repo.GetArticleByID(ctx, "02")
			assert.ErrorIs(t, err, port.ErrNotFound)
		})

		upsertCases := []struct {
			name string
			lang string
		}{
			{
				name: "UpsertTranslationにfrを渡すとき、ErrInvalidPersistedValueになる",
				lang: "fr",
			},
			{
				name: "UpsertTranslationに空langを渡すとき、ErrInvalidPersistedValueになる",
				lang: "",
			},
		}
		for _, tc := range upsertCases {
			t.Run(tc.name, func(t *testing.T) {
				err := repo.UpsertTranslation(ctx, "01", tc.lang, "t", "s", "b")
				assert.ErrorIs(t, err, port.ErrInvalidPersistedValue)
			})
		}
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
	require.NoError(t, repo.Publish(ctx, "ja-only", "alice@example.com", time.Now()))

	// ja + en 両方 (en は管理 UI 経由で後追加される想定 → UpsertTranslation で入れる)
	_, err = repo.InsertArticleWithTranslation(ctx, domain.Article{
		ArticleID: "both", Source: "aws", SourceURL: "https://example.com/both",
		Tags: []string{}, Status: domain.StatusPending,
	}, domain.LangJa, "ja", "s", "b")
	require.NoError(t, err)
	require.NoError(t, repo.UpsertTranslation(ctx, "both", domain.LangEn, "en", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "both", "alice@example.com", time.Now()))

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
					require.NoError(t, repo.Reject(ctx, "TST-0002", "seed@example.com", fixedNow.Add(1*time.Hour)))
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
	require.NoError(t, repo.UpsertTranslation(ctx, "published-both", domain.LangEn, "en", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "published-both", "alice@example.com", time.Now()))
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

func TestListArticles(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "p1", domain.StatusPending)
	_ = seedWithJa(t, repo, "p2", domain.StatusPending)
	_ = seedWithJa(t, repo, "pub", domain.StatusPublished)
	_ = seedWithJa(t, repo, "rej", domain.StatusRejected)

	t.Run("管理用記事一覧の取得", func(t *testing.T) {
		cases := []struct {
			name    string
			limit   int
			wantIDs []string
		}{
			{name: "limit=100のとき、ingested_at降順で全4件を返す", limit: 100, wantIDs: []string{"rej", "pub", "p2", "p1"}},
			{name: "limit=2のとき、ingested_atが新しい2件に絞られる", limit: 2, wantIDs: []string{"rej", "pub"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				items, err := repo.ListArticles(ctx, tc.limit)
				require.NoError(t, err)

				gotIDs := make([]string, len(items))
				for i, item := range items {
					gotIDs[i] = item.ArticleID
				}
				assert.Equal(t, tc.wantIDs, gotIDs)
			})
		}
	})
}

func TestListTranslationsByArticleIDs(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "a1", domain.StatusPending)
	require.NoError(t, repo.UpsertTranslation(ctx, "a1", domain.LangEn, "en", "s", "b"))
	_ = seedWithJa(t, repo, "a2", domain.StatusPending)

	t.Run("記事ID群からの翻訳取得", func(t *testing.T) {
		cases := []struct {
			name    string
			ids     []string
			wantLen int
		}{
			{name: "空入力のとき、0件を返す", ids: nil, wantLen: 0},
			{name: "複数IDのとき、全翻訳 (a1のja+enとa2のjaで3件)を返す", ids: []string{"a1", "a2"}, wantLen: 3},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ts, err := repo.ListTranslationsByArticleIDs(ctx, tc.ids)
				require.NoError(t, err)
				assert.Len(t, ts, tc.wantLen)
			})
		}
	})
}

func TestGetArticleByID(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "pend", domain.StatusPending)
	_ = seedWithJa(t, repo, "rej", domain.StatusRejected)

	t.Run("記事の取得", func(t *testing.T) {
		cases := []struct {
			name    string
			id      string
			wantErr error
		}{
			{
				name:    "pending記事のとき、エラーにならない",
				id:      "pend",
				wantErr: nil,
			},
			{
				name:    "rejected記事のとき、エラーにならない",
				id:      "rej",
				wantErr: nil,
			},
			{
				name:    "非存在idのとき、ErrNotFoundになる",
				id:      "ghost",
				wantErr: port.ErrNotFound,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := repo.GetArticleByID(ctx, tc.id)
				assert.ErrorIs(t, err, tc.wantErr)
			})
		}
	})
}

func TestUpdateNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	t.Run("存在しない記事への更新", func(t *testing.T) {
		cases := []struct {
			name string
			op   func() error
		}{
			{
				name: "Publishで存在しない記事のとき、ErrNotFoundになる",
				op:   func() error { return repo.Publish(ctx, "ghost", "x", fixedNow) },
			},
			{
				name: "Rejectで存在しない記事のとき、ErrNotFoundになる",
				op:   func() error { return repo.Reject(ctx, "ghost", "x", fixedNow) },
			},
			{
				name: "UpsertTranslationで存在しない記事のとき、ErrNotFoundになる",
				op:   func() error { return repo.UpsertTranslation(ctx, "ghost", domain.LangEn, "t", "s", "b") },
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.op()
				assert.True(t, errors.Is(err, port.ErrNotFound))
			})
		}
	})
}

func TestPublish(t *testing.T) {
	type publishOp struct {
		reviewer string
		at       time.Time
	}
	first := fixedNow
	second := fixedNow.Add(1 * time.Hour)

	t.Run("記事の承認", func(t *testing.T) {
		cases := []struct {
			name            string
			publishes       []publishOp
			wantPublishedAt time.Time
			wantReviewedAt  time.Time
			wantReviewer    string
		}{
			{
				name:            "初回承認のとき、published_at / reviewed_at / reviewerがnowにセットされる",
				publishes:       []publishOp{{"alice@example.com", first}},
				wantPublishedAt: first,
				wantReviewedAt:  first,
				wantReviewer:    "alice@example.com",
			},
			{
				name:            "再承認のとき、published_at / reviewed_at / reviewerが最新値で上書きされる",
				publishes:       []publishOp{{"alice@example.com", first}, {"bob@example.com", second}},
				wantPublishedAt: second,
				wantReviewedAt:  second,
				wantReviewer:    "bob@example.com",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				repo := newRepo(t)
				_ = seedWithJa(t, repo, "01", domain.StatusPending)
				for _, op := range tc.publishes {
					require.NoError(t, repo.Publish(ctx, "01", op.reviewer, op.at))
				}

				aw, err := fetchArticleWithTranslations(t, repo, "01")
				require.NoError(t, err)
				assert.Equal(t, domain.StatusPublished, aw.Article.Status)
				require.NotNil(t, aw.Article.PublishedAt)
				assert.True(t, aw.Article.PublishedAt.Equal(tc.wantPublishedAt))
				require.NotNil(t, aw.Article.ReviewedAt)
				assert.True(t, aw.Article.ReviewedAt.Equal(tc.wantReviewedAt))
				require.NotNil(t, aw.Article.Reviewer)
				assert.Equal(t, tc.wantReviewer, *aw.Article.Reviewer)
			})
		}
	})
}

func TestReject(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	_ = seedWithJa(t, repo, "01", domain.StatusPending)

	t.Run("記事の却下", func(t *testing.T) {
		t.Run("承認後に却下すると、published_atは据え置きでreviewed_atが却下時刻になる", func(t *testing.T) {
			pub := fixedNow
			rej := fixedNow.Add(1 * time.Hour)
			require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", pub))
			require.NoError(t, repo.Reject(ctx, "01", "bob@example.com", rej))

			aw, err := fetchArticleWithTranslations(t, repo, "01")
			require.NoError(t, err)
			assert.Equal(t, domain.StatusRejected, aw.Article.Status)
			require.NotNil(t, aw.Article.PublishedAt)
			assert.True(t, aw.Article.PublishedAt.Equal(pub), "却下でも published_at は承認時刻のまま")
			require.NotNil(t, aw.Article.ReviewedAt)
			assert.True(t, aw.Article.ReviewedAt.Equal(rej))
		})
	})
}

func TestUpsertTranslation(t *testing.T) {
	type opArgs struct {
		lang, title, summary, body string
	}
	noPreOp := func(_ *testing.T, _ context.Context, _ *postgres.NewsRepository) {}

	t.Run("翻訳のUPSERT", func(t *testing.T) {
		cases := []struct {
			name        string
			preOp       func(t *testing.T, ctx context.Context, repo *postgres.NewsRepository)
			preSleep    time.Duration
			op          opArgs
			assertAfter func(t *testing.T, before, after *domain.ArticleWithTranslations, op opArgs)
		}{
			{
				name:  "未存在langのとき、INSERTで追加される (en)",
				preOp: noPreOp,
				op:    opArgs{domain.LangEn, "en-title", "en-summary", "en-body"},
				assertAfter: func(t *testing.T, _, after *domain.ArticleWithTranslations, op opArgs) {
					tr := translationByLang(t, after, op.lang)
					assert.Equal(t, op.title, tr.Title)
					assert.Equal(t, op.summary, tr.Summary)
					assert.Equal(t, op.body, tr.Body)
				},
			},
			{
				name:  "既存langのとき、UPDATEで上書きされる (ja)",
				preOp: noPreOp,
				op:    opArgs{domain.LangJa, "updated", "updated-s", "updated-b"},
				assertAfter: func(t *testing.T, _, after *domain.ArticleWithTranslations, op opArgs) {
					tr := translationByLang(t, after, op.lang)
					assert.Equal(t, op.title, tr.Title)
					assert.Equal(t, op.summary, tr.Summary)
					assert.Equal(t, op.body, tr.Body)
				},
			},
			{
				name:     "既存langをUPDATEするとき、triggerでtranslation.updated_atが進む",
				preOp:    noPreOp,
				preSleep: 5 * time.Millisecond,
				op:       opArgs{domain.LangJa, "new", "new-s", "new-b"},
				assertAfter: func(t *testing.T, before, after *domain.ArticleWithTranslations, _ opArgs) {
					beforeJa := translationByLang(t, before, domain.LangJa)
					afterJa := translationByLang(t, after, domain.LangJa)
					assert.True(t, afterJa.UpdatedAt.After(beforeJa.UpdatedAt))
				},
			},
			{
				name: "翻訳をUPSERTしても、親記事のstatus / reviewed_at / reviewer / published_at / updated_atは不変",
				// publish しておき reviewed_at / reviewer / published_at に値を入れた状態で観測する
				preOp: func(t *testing.T, ctx context.Context, repo *postgres.NewsRepository) {
					require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", fixedNow))
				},
				preSleep: 10 * time.Millisecond,
				op:       opArgs{domain.LangEn, "en-title", "en-summary", "en-body"},
				assertAfter: func(t *testing.T, before, after *domain.ArticleWithTranslations, _ opArgs) {
					assert.Equal(t, before.Article.Status, after.Article.Status)
					assert.Equal(t, before.Article.Reviewer, after.Article.Reviewer)
					assert.Equal(t, before.Article.ReviewedAt.UnixNano(), after.Article.ReviewedAt.UnixNano())
					assert.Equal(t, before.Article.PublishedAt.UnixNano(), after.Article.PublishedAt.UnixNano())
					assert.Equal(t, before.Article.UpdatedAt.UnixNano(), after.Article.UpdatedAt.UnixNano())
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				repo := newRepo(t)
				_ = seedWithJa(t, repo, "01", domain.StatusPending)
				tc.preOp(t, ctx, repo)

				before, err := fetchArticleWithTranslations(t, repo, "01")
				require.NoError(t, err)

				time.Sleep(tc.preSleep)
				require.NoError(t, repo.UpsertTranslation(ctx, "01", tc.op.lang, tc.op.title, tc.op.summary, tc.op.body))

				after, err := fetchArticleWithTranslations(t, repo, "01")
				require.NoError(t, err)
				tc.assertAfter(t, before, after, tc.op)
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
