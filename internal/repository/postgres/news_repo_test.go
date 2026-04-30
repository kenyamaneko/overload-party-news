package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
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
func translationByLang(t *testing.T, aw *apinews.ArticleWithTranslations, lang string) apinews.Translation {
	t.Helper()
	for _, tr := range aw.Translations {
		if tr.Lang == lang {
			return tr
		}
	}
	t.Fatalf("translation not found: lang=%s", lang)
	return apinews.Translation{}
}

// fetchArticleWithTranslations は repo から記事 + 翻訳を取得して合成し、Status を導出するテストヘルパー。
// repo は分離された I/O を提供するだけで、合成と Status 導出は本来 service 層の責務だが、
// 本ファイルでは便宜的にテスト内で組み立てる。
func fetchArticleWithTranslations(t *testing.T, repo *postgres.NewsRepository, id string) (*apinews.ArticleWithTranslations, error) {
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
	article.Status = apinews.DeriveStatus(*article)
	return &apinews.ArticleWithTranslations{Article: *article, Translations: translations}, nil
}

// seedWithJa は ja 翻訳を持つ記事を 1 件 INSERT して status を指定値まで遷移させる。
// 呼び出しのたびに time.Now() を使うため、連続呼び出しで published_at / reviewed_at に差がつく。
func seedWithJa(t *testing.T, repo *postgres.NewsRepository, id string, status apinews.Status) apinews.Article {
	t.Helper()
	ctx := context.Background()
	article := apinews.Article{
		ArticleID: id,
		Source:    "aws",
		SourceURL: "https://example.com/" + id,
		Tags:      []string{"compute"},
		Status:    apinews.StatusPending,
	}
	inserted, err := repo.InsertArticle(ctx, article)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, repo.InsertTranslation(ctx, id, apinews.LangJa, "ja-"+id, "s-"+id, "b-"+id))

	switch status {
	case apinews.StatusPublished:
		require.NoError(t, repo.Publish(ctx, id, "seed@example.com", time.Now()))
	case apinews.StatusRejected:
		require.NoError(t, repo.Reject(ctx, id, "seed@example.com", time.Now()))
	}
	article.Status = status
	return article
}

// 仕様 (FEATURE_SPEC §3.2): InsertArticle は冪等。重複 PK / UNIQUE は inserted=false で no-op。
func TestInsertArticle_仕様_冪等性(t *testing.T) {
	ctx := context.Background()
	baseArticle := apinews.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{"x"}, Status: apinews.StatusPending,
	}

	cases := []struct {
		name         string
		setup        func(t *testing.T, repo *postgres.NewsRepository)
		mutate       func(*apinews.Article)
		wantInserted bool
	}{
		{
			name:         "新規挿入は true",
			setup:        func(_ *testing.T, _ *postgres.NewsRepository) {},
			mutate:       func(_ *apinews.Article) {},
			wantInserted: true,
		},
		{
			name: "同一 article_id は false",
			setup: func(t *testing.T, repo *postgres.NewsRepository) {
				_, err := repo.InsertArticle(ctx, baseArticle)
				require.NoError(t, err)
			},
			mutate:       func(_ *apinews.Article) {},
			wantInserted: false,
		},
		{
			name: "別 article_id でも同 source_url なら false",
			setup: func(t *testing.T, repo *postgres.NewsRepository) {
				_, err := repo.InsertArticle(ctx, baseArticle)
				require.NoError(t, err)
			},
			mutate:       func(a *apinews.Article) { a.ArticleID = "99" },
			wantInserted: false,
		},
	}

	for _, tc := range cases {
		tc := tc
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
}

// 仕様: InsertTranslation で既存翻訳は上書きされない。校閲済みテキストを newsfeed 再送で壊さない契約。
func TestInsertTranslation_仕様_既存行を上書きしない(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_, err := repo.InsertArticle(ctx, apinews.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{"x"}, Status: apinews.StatusPending,
	})
	require.NoError(t, err)
	require.NoError(t, repo.InsertTranslation(ctx, "01", apinews.LangJa, "ORIGINAL", "s", "b"))

	// 同一 (article_id, lang) で再挿入 → DO NOTHING
	require.NoError(t, repo.InsertTranslation(ctx, "01", apinews.LangJa, "OVERWRITE", "s2", "b2"))

	aw, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	require.Len(t, aw.Translations, 1)
	assert.Equal(t, "ORIGINAL", aw.Translations[0].Title, "既存 ja 翻訳は InsertTranslation で上書きされない")
}

// 仕様: InsertTranslation は親記事が存在しないと FK 違反でエラーを返す。
func TestInsertTranslation_仕様_親記事なしはエラー(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	err := repo.InsertTranslation(ctx, "ghost", apinews.LangJa, "t", "s", "b")
	assert.Error(t, err, "親記事が無ければ FK 違反でエラーになるべき")
}

// 仕様: lang 列の CHECK 制約により、未対応 lang は ErrInvalidPersistedValue として弾かれる。
// service 層から validateLang を撤去し、許容値の SSoT を DB 側に寄せた契約。
func TestTranslation_仕様_未対応langはCHECK違反(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	_ = seedWithJa(t, repo, "01", apinews.StatusPending)

	cases := []struct {
		name string
		op   func() error
	}{
		{
			name: "InsertTranslation で fr",
			op:   func() error { return repo.InsertTranslation(ctx, "01", "fr", "t", "s", "b") },
		},
		{
			name: "UpsertTranslation で fr",
			op:   func() error { return repo.UpsertTranslation(ctx, "01", "fr", "t", "s", "b") },
		},
		{
			name: "UpsertTranslation で 空 lang",
			op:   func() error { return repo.UpsertTranslation(ctx, "01", "", "t", "s", "b") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.op()
			assert.ErrorIs(t, err, port.ErrInvalidPersistedValue)
		})
	}
}

// 仕様 (FEATURE_SPEC §4): ListPublished は status=published の記事のうち、指定 lang の翻訳があるもののみ返す。
func TestListPublished_仕様_lang別フィルタ(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	// ja のみ翻訳あり
	_, err := repo.InsertArticle(ctx, apinews.Article{
		ArticleID: "ja-only", Source: "aws", SourceURL: "https://example.com/ja-only",
		Tags: []string{}, Status: apinews.StatusPending,
	})
	require.NoError(t, err)
	require.NoError(t, repo.InsertTranslation(ctx, "ja-only", apinews.LangJa, "ja", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "ja-only", "alice@example.com", time.Now()))

	// ja + en 両方 (en は管理 UI 経由で後追加される想定 → UpsertTranslation で入れる)
	_, err = repo.InsertArticle(ctx, apinews.Article{
		ArticleID: "both", Source: "aws", SourceURL: "https://example.com/both",
		Tags: []string{}, Status: apinews.StatusPending,
	})
	require.NoError(t, err)
	require.NoError(t, repo.InsertTranslation(ctx, "both", apinews.LangJa, "ja", "s", "b"))
	require.NoError(t, repo.UpsertTranslation(ctx, "both", apinews.LangEn, "en", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "both", "alice@example.com", time.Now()))

	cases := []struct {
		name    string
		lang    string
		wantIDs []string
	}{
		{
			name:    "ja は 2 件",
			lang:    apinews.LangJa,
			wantIDs: []string{"both", "ja-only"}, // published_at 降順 (both が新しい)
		},
		{
			name:    "en は 1 件 (ja-only 除外)",
			lang:    apinews.LangEn,
			wantIDs: []string{"both"},
		},
	}

	for _, tc := range cases {
		tc := tc
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
}

// 仕様 (FEATURE_SPEC §5): GetPublishedByID は status=published + 指定 lang 翻訳がある記事のみ返す。
// status / 翻訳条件が合わないと ErrNotFound。
func TestGetPublishedByID_仕様_ステータスとlangの両条件(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	// pending + ja
	_ = seedWithJa(t, repo, "pending", apinews.StatusPending)
	// published + ja
	_ = seedWithJa(t, repo, "published-ja", apinews.StatusPublished)
	// published + ja + en
	_ = seedWithJa(t, repo, "published-both", apinews.StatusPending)
	require.NoError(t, repo.UpsertTranslation(ctx, "published-both", apinews.LangEn, "en", "s", "b"))
	require.NoError(t, repo.Publish(ctx, "published-both", "alice@example.com", time.Now()))
	// rejected + ja
	_ = seedWithJa(t, repo, "rejected", apinews.StatusRejected)

	cases := []struct {
		name    string
		id      string
		lang    string
		wantErr error
	}{
		{
			name:    "published + ja は返す",
			id:      "published-ja",
			lang:    apinews.LangJa,
			wantErr: nil,
		},
		{
			name:    "published + en (翻訳あり) は返す",
			id:      "published-both",
			lang:    apinews.LangEn,
			wantErr: nil,
		},
		{
			name:    "published + en (翻訳なし) は 404",
			id:      "published-ja",
			lang:    apinews.LangEn,
			wantErr: port.ErrNotFound,
		},
		{
			name:    "pending は 404",
			id:      "pending",
			lang:    apinews.LangJa,
			wantErr: port.ErrNotFound,
		},
		{
			name:    "rejected は 404",
			id:      "rejected",
			lang:    apinews.LangJa,
			wantErr: port.ErrNotFound,
		},
		{
			name:    "非存在 id は 404",
			id:      "ghost",
			lang:    apinews.LangJa,
			wantErr: port.ErrNotFound,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.GetPublishedByID(ctx, tc.id, tc.lang)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// 仕様: ListArticles は記事を ingested_at DESC で limit 件返す (フィルタなし)。
// status による絞り込みは service 層の責務であり、repo は status 概念を持たない。
func TestListArticles_仕様(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "p1", apinews.StatusPending)
	_ = seedWithJa(t, repo, "p2", apinews.StatusPending)
	_ = seedWithJa(t, repo, "pub", apinews.StatusPublished)
	_ = seedWithJa(t, repo, "rej", apinews.StatusRejected)

	t.Run("limit 100 で全件取得", func(t *testing.T) {
		items, err := repo.ListArticles(ctx, 100)
		require.NoError(t, err)
		assert.Len(t, items, 4)
	})

	t.Run("limit で件数を絞れる", func(t *testing.T) {
		items, err := repo.ListArticles(ctx, 2)
		require.NoError(t, err)
		assert.Len(t, items, 2)
	})
}

// 仕様: ListTranslationsByArticleIDs は指定 article_id 群の翻訳行を全件返す。
// 並びは article_id, lang。空入力では 0 件。
func TestListTranslationsByArticleIDs_仕様(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "a1", apinews.StatusPending)
	require.NoError(t, repo.UpsertTranslation(ctx, "a1", apinews.LangEn, "en", "s", "b"))
	_ = seedWithJa(t, repo, "a2", apinews.StatusPending)

	t.Run("空入力は 0 件", func(t *testing.T) {
		ts, err := repo.ListTranslationsByArticleIDs(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, ts)
	})

	t.Run("複数 ID で全翻訳を返す", func(t *testing.T) {
		ts, err := repo.ListTranslationsByArticleIDs(ctx, []string{"a1", "a2"})
		require.NoError(t, err)
		// a1: ja+en, a2: ja → 計 3 件
		assert.Len(t, ts, 3)
	})
}

// 仕様: GetArticleByID は status を問わず記事を返す。非存在は ErrNotFound。
func TestGetArticleByID_仕様(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "pend", apinews.StatusPending)
	_ = seedWithJa(t, repo, "rej", apinews.StatusRejected)

	cases := []struct {
		name    string
		id      string
		wantErr error
	}{
		{
			name:    "pending を返す",
			id:      "pend",
			wantErr: nil,
		},
		{
			name:    "rejected を返す",
			id:      "rej",
			wantErr: nil,
		},
		{
			name:    "非存在は ErrNotFound",
			id:      "ghost",
			wantErr: port.ErrNotFound,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.GetArticleByID(ctx, tc.id)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// 仕様: Publish / Reject / UpsertTranslation は非存在記事で ErrNotFound。
func TestUpdateNotFound_仕様(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	cases := []struct {
		name string
		op   func() error
	}{
		{
			name: "Publish",
			op:   func() error { return repo.Publish(ctx, "ghost", "x", fixedNow) },
		},
		{
			name: "Reject",
			op:   func() error { return repo.Reject(ctx, "ghost", "x", fixedNow) },
		},
		{
			name: "UpsertTranslation",
			op:   func() error { return repo.UpsertTranslation(ctx, "ghost", apinews.LangEn, "t", "s", "b") },
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.op()
			assert.True(t, errors.Is(err, port.ErrNotFound))
		})
	}
}

// 仕様 (FEATURE_SPEC §6): Publish は published_at / reviewed_at / reviewer を now にセット。
func TestPublish_仕様_承認時のカラム更新(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)
	require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", fixedNow))

	aw, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	assert.Equal(t, apinews.StatusPublished, aw.Article.Status)
	require.NotNil(t, aw.Article.PublishedAt)
	assert.True(t, aw.Article.PublishedAt.Equal(fixedNow))
	require.NotNil(t, aw.Article.ReviewedAt)
	assert.True(t, aw.Article.ReviewedAt.Equal(fixedNow))
	require.NotNil(t, aw.Article.Reviewer)
	assert.Equal(t, "alice@example.com", *aw.Article.Reviewer)
}

// 仕様: 再承認時も published_at を毎回更新する。
func TestPublish_仕様_再承認でpublished_atが更新される(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)

	first := fixedNow
	second := fixedNow.Add(1 * time.Hour)
	require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", first))
	require.NoError(t, repo.Publish(ctx, "01", "bob@example.com", second))

	aw, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	require.NotNil(t, aw.Article.PublishedAt)
	assert.True(t, aw.Article.PublishedAt.Equal(second))
	assert.Equal(t, "bob@example.com", *aw.Article.Reviewer)
}

// 仕様: Reject は published_at を保持する (再承認時の履歴参照用)。
func TestReject_仕様_却下はpublished_atを保持(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)

	pub := fixedNow
	rej := fixedNow.Add(1 * time.Hour)
	require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", pub))
	require.NoError(t, repo.Reject(ctx, "01", "bob@example.com", rej))

	aw, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	assert.Equal(t, apinews.StatusRejected, aw.Article.Status)
	require.NotNil(t, aw.Article.PublishedAt)
	assert.True(t, aw.Article.PublishedAt.Equal(pub), "却下でも published_at は承認時刻のまま")
	require.NotNil(t, aw.Article.ReviewedAt)
	assert.True(t, aw.Article.ReviewedAt.Equal(rej))
}

// 仕様 (FEATURE_SPEC §6.2): 未存在 lang に対する UpsertTranslation は INSERT として動作する。
func TestUpsertTranslation_仕様_新規追加(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)

	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangEn, "en-title", "en-summary", "en-body"))

	aw, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	en := translationByLang(t, aw, apinews.LangEn)
	assert.Equal(t, "en-title", en.Title)
	assert.Equal(t, "en-summary", en.Summary)
	assert.Equal(t, "en-body", en.Body)
}

// 仕様: 既存 lang に対する UpsertTranslation は UPDATE として動作する。
func TestUpsertTranslation_仕様_再upsertで更新(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)
	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangJa, "updated", "updated-s", "updated-b"))

	aw, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	ja := translationByLang(t, aw, apinews.LangJa)
	assert.Equal(t, "updated", ja.Title)
	assert.Equal(t, "updated-s", ja.Summary)
	assert.Equal(t, "updated-b", ja.Body)
}

// 仕様: 翻訳の UPDATE で translation.updated_at が trigger により進む。
func TestUpsertTranslation_仕様_translationのupdated_atが進む(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)
	before, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	beforeJa := translationByLang(t, before, apinews.LangJa)

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangJa, "new", "new-s", "new-b"))

	after, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	afterJa := translationByLang(t, after, apinews.LangJa)
	assert.True(t, afterJa.UpdatedAt.After(beforeJa.UpdatedAt))
}

// 仕様: UpsertTranslation は news_articles を一切触らない (翻訳編集 ≠ 記事レベル監査)。
// status / reviewed_at / reviewer / published_at / updated_at が全て不変であることを確認する。
func TestUpsertTranslation_仕様_親記事を触らない(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)
	// publish しておき reviewed_at / reviewer / published_at に値を入れた状態で観測する
	require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", fixedNow))
	before, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangEn, "en-title", "en-summary", "en-body"))

	after, err := fetchArticleWithTranslations(t, repo, "01")
	require.NoError(t, err)
	assert.Equal(t, before.Article.Status, after.Article.Status)
	assert.Equal(t, before.Article.Reviewer, after.Article.Reviewer)
	assert.Equal(t, before.Article.ReviewedAt.UnixNano(), after.Article.ReviewedAt.UnixNano())
	assert.Equal(t, before.Article.PublishedAt.UnixNano(), after.Article.PublishedAt.UnixNano())
	assert.Equal(t, before.Article.UpdatedAt.UnixNano(), after.Article.UpdatedAt.UnixNano())
}
