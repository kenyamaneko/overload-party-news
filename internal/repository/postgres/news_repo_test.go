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
// 呼び出しのたびに time.Now() を使うため、連続呼び出しで published_at / reviewed_at に差がつく。
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
	inserted, err := repo.InsertArticle(ctx, article)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, repo.InsertTranslation(ctx, id, domain.LangJa, "ja-"+id, "s-"+id, "b-"+id))

	switch status {
	case domain.StatusPublished:
		require.NoError(t, repo.Publish(ctx, id, "seed@example.com", time.Now()))
	case domain.StatusRejected:
		require.NoError(t, repo.Reject(ctx, id, "seed@example.com", time.Now()))
	}
	article.Status = status
	return article
}

func TestInsertArticle(t *testing.T) {
	ctx := context.Background()
	baseArticle := domain.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{"x"}, Status: domain.StatusPending,
	}

	t.Run("記事の INSERT", func(t *testing.T) {
		cases := []struct {
			name         string
			setup        func(t *testing.T, repo *postgres.NewsRepository)
			mutate       func(*domain.Article)
			wantInserted bool
		}{
			{
				name:         "新規記事のとき、true を返す",
				setup:        func(_ *testing.T, _ *postgres.NewsRepository) {},
				mutate:       func(_ *domain.Article) {},
				wantInserted: true,
			},
			{
				name: "同一 article_id が既存のとき、false を返す",
				setup: func(t *testing.T, repo *postgres.NewsRepository) {
					_, err := repo.InsertArticle(ctx, baseArticle)
					require.NoError(t, err)
				},
				mutate:       func(_ *domain.Article) {},
				wantInserted: false,
			},
			{
				name: "別 article_id でも source_url が既存のとき、false を返す",
				setup: func(t *testing.T, repo *postgres.NewsRepository) {
					_, err := repo.InsertArticle(ctx, baseArticle)
					require.NoError(t, err)
				},
				mutate:       func(a *domain.Article) { a.ArticleID = "99" },
				wantInserted: false,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				sharedPG.Truncate(t)
				repo := postgres.NewNewsRepository(sharedPG.Pool)
				tc.setup(t, repo)

				a := baseArticle
				tc.mutate(&a)

				inserted, err := repo.InsertArticle(ctx, a)

				require.NoError(t, err)
				assert.Equal(t, tc.wantInserted, inserted)
			})
		}
	})
}

func TestInsertTranslation(t *testing.T) {
	ctx := context.Background()

	t.Run("翻訳の INSERT", func(t *testing.T) {
		t.Run("同一 (article_id, lang) で再挿入すると、既存翻訳は上書きされない", func(t *testing.T) {
			repo := newRepo(t)

			_, err := repo.InsertArticle(ctx, domain.Article{
				ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
				Tags: []string{"x"}, Status: domain.StatusPending,
			})
			require.NoError(t, err)
			require.NoError(t, repo.InsertTranslation(ctx, "01", domain.LangJa, "ORIGINAL", "s", "b"))

			// 同一 (article_id, lang) で再挿入 → DO NOTHING
			require.NoError(t, repo.InsertTranslation(ctx, "01", domain.LangJa, "OVERWRITE", "s2", "b2"))

			aw, err := fetchArticleWithTranslations(t, repo, "01")
			require.NoError(t, err)
			require.Len(t, aw.Translations, 1)
			assert.Equal(t, "ORIGINAL", aw.Translations[0].Title, "既存 ja 翻訳は InsertTranslation で上書きされない")
		})

		t.Run("親記事が存在しないとき、FK 違反でエラーになる", func(t *testing.T) {
			repo := newRepo(t)

			err := repo.InsertTranslation(ctx, "ghost", domain.LangJa, "t", "s", "b")
			assert.Error(t, err, "親記事が無ければ FK 違反でエラーになるべき")
		})
	})
}

func TestTranslation_UnsupportedLang(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	_ = seedWithJa(t, repo, "01", domain.StatusPending)

	t.Run("未対応 lang の拒否", func(t *testing.T) {
		cases := []struct {
			name string
			op   func() error
		}{
			{
				name: "InsertTranslation に fr を渡すとき、ErrInvalidPersistedValue になる",
				op:   func() error { return repo.InsertTranslation(ctx, "01", "fr", "t", "s", "b") },
			},
			{
				name: "UpsertTranslation に fr を渡すとき、ErrInvalidPersistedValue になる",
				op:   func() error { return repo.UpsertTranslation(ctx, "01", "fr", "t", "s", "b") },
			},
			{
				name: "UpsertTranslation に空 lang を渡すとき、ErrInvalidPersistedValue になる",
				op:   func() error { return repo.UpsertTranslation(ctx, "01", "", "t", "s", "b") },
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.op()
				assert.ErrorIs(t, err, port.ErrInvalidPersistedValue)
			})
		}
	})
}

func TestListPublished(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	// ja のみ翻訳あり
	_, err := repo.InsertArticle(ctx, domain.Article{
		ArticleID: "ja-only", Source: "aws", SourceURL: "https://example.com/ja-only",
		Tags: []string{}, Status: domain.StatusPending,
	})
	require.NoError(t, err)
	require.NoError(t, repo.InsertTranslation(ctx, "ja-only", domain.LangJa, "ja", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "ja-only", "alice@example.com", time.Now()))

	// ja + en 両方 (en は管理 UI 経由で後追加される想定 → UpsertTranslation で入れる)
	_, err = repo.InsertArticle(ctx, domain.Article{
		ArticleID: "both", Source: "aws", SourceURL: "https://example.com/both",
		Tags: []string{}, Status: domain.StatusPending,
	})
	require.NoError(t, err)
	require.NoError(t, repo.InsertTranslation(ctx, "both", domain.LangJa, "ja", "s", "b"))
	require.NoError(t, repo.UpsertTranslation(ctx, "both", domain.LangEn, "en", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "both", "alice@example.com", time.Now()))

	t.Run("公開記事一覧の取得", func(t *testing.T) {
		cases := []struct {
			name    string
			lang    string
			wantIDs []string
		}{
			{
				name:    "lang=ja のとき、公開済み 2 件を返す",
				lang:    domain.LangJa,
				wantIDs: []string{"both", "ja-only"}, // published_at 降順 (both が新しい)
			},
			{
				name:    "lang=en のとき、en 翻訳を持つ 1 件を返す (ja-only 除外)",
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
				name: "published + ja のとき、記事を返す",
				id:   "published-ja",
				lang: domain.LangJa,
			},
			{
				name: "published + en 翻訳ありのとき、記事を返す",
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
				name: "published + en 翻訳なしのとき、ErrNotFound になる",
				id:   "published-ja",
				lang: domain.LangEn,
			},
			{
				name: "pending のとき、ErrNotFound になる",
				id:   "pending",
				lang: domain.LangJa,
			},
			{
				name: "rejected のとき、ErrNotFound になる",
				id:   "rejected",
				lang: domain.LangJa,
			},
			{
				name: "非存在 id のとき、ErrNotFound になる",
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
			wantLen int
		}{
			{name: "limit=100 のとき、全 4 件を返す", limit: 100, wantLen: 4},
			{name: "limit=2 のとき、2 件に絞られる", limit: 2, wantLen: 2},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				items, err := repo.ListArticles(ctx, tc.limit)
				require.NoError(t, err)
				assert.Len(t, items, tc.wantLen)
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

	t.Run("記事 ID 群からの翻訳取得", func(t *testing.T) {
		cases := []struct {
			name    string
			ids     []string
			wantLen int
		}{
			{name: "空入力のとき、0 件を返す", ids: nil, wantLen: 0},
			{name: "複数 ID のとき、全翻訳 (a1 の ja+en と a2 の ja で 3 件) を返す", ids: []string{"a1", "a2"}, wantLen: 3},
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
				name:    "pending 記事のとき、エラーにならない",
				id:      "pend",
				wantErr: nil,
			},
			{
				name:    "rejected 記事のとき、エラーにならない",
				id:      "rej",
				wantErr: nil,
			},
			{
				name:    "非存在 id のとき、ErrNotFound になる",
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
				name: "Publish で存在しない記事のとき、ErrNotFound になる",
				op:   func() error { return repo.Publish(ctx, "ghost", "x", fixedNow) },
			},
			{
				name: "Reject で存在しない記事のとき、ErrNotFound になる",
				op:   func() error { return repo.Reject(ctx, "ghost", "x", fixedNow) },
			},
			{
				name: "UpsertTranslation で存在しない記事のとき、ErrNotFound になる",
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
				name:            "初回承認のとき、published_at / reviewed_at / reviewer が now にセットされる",
				publishes:       []publishOp{{"alice@example.com", first}},
				wantPublishedAt: first,
				wantReviewedAt:  first,
				wantReviewer:    "alice@example.com",
			},
			{
				name:            "再承認のとき、published_at / reviewed_at / reviewer が最新値で上書きされる",
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
		t.Run("承認後に却下すると、published_at は据え置きで reviewed_at が却下時刻になる", func(t *testing.T) {
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

	t.Run("翻訳の UPSERT", func(t *testing.T) {
		cases := []struct {
			name        string
			preOp       func(t *testing.T, ctx context.Context, repo *postgres.NewsRepository)
			preSleep    time.Duration
			op          opArgs
			assertAfter func(t *testing.T, before, after *domain.ArticleWithTranslations, op opArgs)
		}{
			{
				name:  "未存在 lang のとき、INSERT で追加される (en)",
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
				name:  "既存 lang のとき、UPDATE で上書きされる (ja)",
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
				name:     "既存 lang を UPDATE するとき、trigger で translation.updated_at が進む",
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
				name: "翻訳を UPSERT しても、親記事の status / reviewed_at / reviewer / published_at / updated_at は不変",
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
