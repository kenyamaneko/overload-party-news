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

// seedWithJa は ja 翻訳を持つ記事を 1 件 INSERT して status を指定値まで遷移させる。
// 呼び出しのたびに time.Now() を使うため、連続呼び出しで published_at / reviewed_at に差がつく。
func seedWithJa(t *testing.T, repo *postgres.NewsRepository, id string, status apinews.Status) apinews.Article {
	t.Helper()
	article := apinews.Article{
		ArticleID: id,
		Source:    "aws",
		SourceURL: "https://example.com/" + id,
		Tags:      []string{"compute"},
		Status:    apinews.StatusPending,
	}
	translations := []apinews.Translation{
		{ArticleID: id, Lang: apinews.LangJa, Title: "ja-" + id, Summary: "s-" + id, Body: "b-" + id},
	}
	inserted, err := repo.Insert(context.Background(), article, translations)
	require.NoError(t, err)
	require.True(t, inserted)

	switch status {
	case apinews.StatusPublished:
		require.NoError(t, repo.Publish(context.Background(), id, "seed@example.com", time.Now()))
	case apinews.StatusRejected:
		require.NoError(t, repo.Reject(context.Background(), id, "seed@example.com", time.Now()))
	}
	article.Status = status
	return article
}

// 仕様 (ARCHITECTURE §インジェスト): Insert は親記事 + 翻訳群を 1 tx で挿入。冪等性を担保する。
func TestInsert_仕様_冪等性(t *testing.T) {
	ctx := context.Background()
	baseArticle := apinews.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{"x"}, Status: apinews.StatusPending,
	}
	baseTrans := []apinews.Translation{
		{ArticleID: "01", Lang: apinews.LangJa, Title: "T", Summary: "S", Body: "B"},
	}

	cases := []struct {
		name         string
		setup        func(t *testing.T, repo *postgres.NewsRepository)
		mutate       func(*apinews.Article, *[]apinews.Translation)
		wantInserted bool
	}{
		{name: "新規挿入は true", setup: func(_ *testing.T, _ *postgres.NewsRepository) {}, mutate: func(_ *apinews.Article, _ *[]apinews.Translation) {}, wantInserted: true},
		{name: "同一 article_id は false", setup: func(t *testing.T, repo *postgres.NewsRepository) {
			_, err := repo.Insert(ctx, baseArticle, baseTrans)
			require.NoError(t, err)
		}, mutate: func(_ *apinews.Article, ts *[]apinews.Translation) {
			(*ts)[0].Title = "OVERWRITE-ATTEMPT"
		}, wantInserted: false},
		{name: "別 article_id でも同 source_url なら false", setup: func(t *testing.T, repo *postgres.NewsRepository) {
			_, err := repo.Insert(ctx, baseArticle, baseTrans)
			require.NoError(t, err)
		}, mutate: func(a *apinews.Article, ts *[]apinews.Translation) {
			a.ArticleID = "99"
			(*ts)[0].ArticleID = "99"
		}, wantInserted: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := postgres.NewNewsRepository(sharedPG.Pool)
			tc.setup(t, repo)

			a := baseArticle
			ts := make([]apinews.Translation, len(baseTrans))
			copy(ts, baseTrans)
			tc.mutate(&a, &ts)

			inserted, err := repo.Insert(ctx, a, ts)

			require.NoError(t, err)
			assert.Equal(t, tc.wantInserted, inserted)
		})
	}
}

// 仕様: Insert で既存行は上書きされない。ja 編集済みテキストを newsfeed 再送で壊さない契約。
func TestInsert_仕様_既存行を上書きしない(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	original := apinews.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{"x"}, Status: apinews.StatusPending,
	}
	originalTrans := []apinews.Translation{
		{ArticleID: "01", Lang: apinews.LangJa, Title: "ORIGINAL", Summary: "s", Body: "b"},
	}
	_, err := repo.Insert(ctx, original, originalTrans)
	require.NoError(t, err)

	// 別タイトルで再送
	overwrite := original
	overwriteTrans := []apinews.Translation{
		{ArticleID: "01", Lang: apinews.LangJa, Title: "OVERWRITE", Summary: "s2", Body: "b2"},
	}
	_, err = repo.Insert(ctx, overwrite, overwriteTrans)
	require.NoError(t, err)

	aw, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)
	require.Len(t, aw.Translations, 1)
	assert.Equal(t, "ORIGINAL", aw.Translations[0].Title, "既存 ja 翻訳は上書きされない")
}

// 仕様: Insert は親記事が新規なら翻訳群も含めて全部挿入する (1 tx)。
func TestInsert_仕様_親新規なら翻訳も挿入される(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	article := apinews.Article{
		ArticleID: "01", Source: "aws", SourceURL: "https://aws.amazon.com/a",
		Tags: []string{}, Status: apinews.StatusPending,
	}
	trans := []apinews.Translation{
		{ArticleID: "01", Lang: apinews.LangJa, Title: "ja", Summary: "js", Body: "jb"},
		{ArticleID: "01", Lang: apinews.LangEn, Title: "en", Summary: "es", Body: "eb"},
	}

	inserted, err := repo.Insert(ctx, article, trans)
	require.NoError(t, err)
	require.True(t, inserted)

	aw, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)
	assert.Len(t, aw.Translations, 2)
}

// 仕様 (FEATURE_SPEC §4): ListPublished は status=published の記事のうち、指定 lang の翻訳があるもののみ返す。
func TestListPublished_仕様_lang別フィルタ(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	// ja のみ翻訳あり
	article1 := apinews.Article{
		ArticleID: "ja-only", Source: "aws", SourceURL: "https://example.com/ja-only",
		Tags: []string{}, Status: apinews.StatusPending,
	}
	_, err := repo.Insert(ctx, article1, []apinews.Translation{
		{ArticleID: "ja-only", Lang: apinews.LangJa, Title: "ja", Summary: "s", Body: "b"},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Publish(ctx, "ja-only", "alice@example.com", time.Now()))

	// ja + en 両方
	article2 := apinews.Article{
		ArticleID: "both", Source: "aws", SourceURL: "https://example.com/both",
		Tags: []string{}, Status: apinews.StatusPending,
	}
	_, err = repo.Insert(ctx, article2, []apinews.Translation{
		{ArticleID: "both", Lang: apinews.LangJa, Title: "ja", Summary: "s", Body: "b"},
		{ArticleID: "both", Lang: apinews.LangEn, Title: "en", Summary: "s", Body: "b"},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Publish(ctx, "both", "alice@example.com", time.Now()))

	cases := []struct {
		name     string
		lang     string
		wantIDs  []string
	}{
		{name: "ja は 2 件", lang: apinews.LangJa, wantIDs: []string{"both", "ja-only"}},     // published_at 降順 (both が新しい)
		{name: "en は 1 件 (ja-only 除外)", lang: apinews.LangEn, wantIDs: []string{"both"}},
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
		{name: "published + ja は返す", id: "published-ja", lang: apinews.LangJa, wantErr: nil},
		{name: "published + en (翻訳あり) は返す", id: "published-both", lang: apinews.LangEn, wantErr: nil},
		{name: "published + en (翻訳なし) は 404", id: "published-ja", lang: apinews.LangEn, wantErr: port.ErrNotFound},
		{name: "pending は 404", id: "pending", lang: apinews.LangJa, wantErr: port.ErrNotFound},
		{name: "rejected は 404", id: "rejected", lang: apinews.LangJa, wantErr: port.ErrNotFound},
		{name: "非存在 id は 404", id: "ghost", lang: apinews.LangJa, wantErr: port.ErrNotFound},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.GetPublishedByID(ctx, tc.id, tc.lang)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// 仕様: ListByStatus は status フィルタ + 各記事の全翻訳をまとめて返す。
func TestListByStatus_仕様_翻訳込みで返す(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "p1", apinews.StatusPending)
	// p1 に en 翻訳も追加
	require.NoError(t, repo.UpsertTranslation(ctx, "p1", apinews.LangEn, "en", "s", "b"))
	_ = seedWithJa(t, repo, "p2", apinews.StatusPending)
	_ = seedWithJa(t, repo, "pub", apinews.StatusPublished)

	pending := apinews.StatusPending

	cases := []struct {
		name      string
		filter    *apinews.Status
		wantCount int
	}{
		{name: "全件", filter: nil, wantCount: 3},
		{name: "pending のみ", filter: &pending, wantCount: 2},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			items, err := repo.ListByStatus(ctx, tc.filter, 100)
			require.NoError(t, err)
			assert.Len(t, items, tc.wantCount)
		})
	}

	// p1 には ja + en の 2 翻訳が付いてくる
	all, err := repo.ListByStatus(ctx, nil, 100)
	require.NoError(t, err)
	var p1 *apinews.ArticleWithTranslations
	for i := range all {
		if all[i].Article.ArticleID == "p1" {
			p1 = &all[i]
		}
	}
	require.NotNil(t, p1)
	assert.Len(t, p1.Translations, 2)
}

// 仕様: GetByID は status を問わず記事 + 翻訳を返す。非存在は ErrNotFound。
func TestGetByID_仕様(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "pend", apinews.StatusPending)
	_ = seedWithJa(t, repo, "rej", apinews.StatusRejected)

	cases := []struct {
		name    string
		id      string
		wantErr error
	}{
		{name: "pending を返す", id: "pend", wantErr: nil},
		{name: "rejected を返す", id: "rej", wantErr: nil},
		{name: "非存在は ErrNotFound", id: "ghost", wantErr: port.ErrNotFound},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.GetByID(ctx, tc.id)
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
		{name: "Publish", op: func() error { return repo.Publish(ctx, "ghost", "x", fixedNow) }},
		{name: "Reject", op: func() error { return repo.Reject(ctx, "ghost", "x", fixedNow) }},
		{name: "UpsertTranslation", op: func() error { return repo.UpsertTranslation(ctx, "ghost", apinews.LangEn, "t", "s", "b") }},
		{name: "ArticleExists", op: func() error { return repo.ArticleExists(ctx, "ghost") }},
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

	aw, err := repo.GetByID(ctx, "01")
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

	aw, err := repo.GetByID(ctx, "01")
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

	aw, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)
	assert.Equal(t, apinews.StatusRejected, aw.Article.Status)
	require.NotNil(t, aw.Article.PublishedAt)
	assert.True(t, aw.Article.PublishedAt.Equal(pub), "却下でも published_at は承認時刻のまま")
	require.NotNil(t, aw.Article.ReviewedAt)
	assert.True(t, aw.Article.ReviewedAt.Equal(rej))
}

// 仕様 (FEATURE_SPEC §6.2): UpsertTranslation は翻訳の追加 / 更新を行い、
// news_articles の timestamp / status には触れない。
func TestUpsertTranslation_仕様_新規追加と更新(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)
	// 事前に publish しておいて、翻訳編集で news_articles の reviewed_at が触られないことを確認する
	require.NoError(t, repo.Publish(ctx, "01", "alice@example.com", fixedNow))
	before, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)

	// en を新規追加
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangEn, "en-title", "en-summary", "en-body"))

	after, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)

	// en 翻訳が追加されている
	var en *apinews.Translation
	for i := range after.Translations {
		if after.Translations[i].Lang == apinews.LangEn {
			en = &after.Translations[i]
		}
	}
	require.NotNil(t, en)
	assert.Equal(t, "en-title", en.Title)

	// news_articles の reviewed_at / reviewer / published_at / status は不変
	assert.Equal(t, before.Article.ReviewedAt.Unix(), after.Article.ReviewedAt.Unix())
	assert.Equal(t, before.Article.PublishedAt.Unix(), after.Article.PublishedAt.Unix())
	assert.Equal(t, before.Article.Reviewer, after.Article.Reviewer)
	assert.Equal(t, before.Article.Status, after.Article.Status)

	// 更新 (同じ lang を upsert)
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangEn, "en-updated", "en-summary-2", "en-body-2"))

	afterUpdate, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)
	var en2 *apinews.Translation
	for i := range afterUpdate.Translations {
		if afterUpdate.Translations[i].Lang == apinews.LangEn {
			en2 = &afterUpdate.Translations[i]
		}
	}
	require.NotNil(t, en2)
	assert.Equal(t, "en-updated", en2.Title)
	// 翻訳の updated_at が trigger で進む
	assert.True(t, en2.UpdatedAt.After(en.UpdatedAt))
}

// 仕様: UpsertTranslation は親記事の updated_at を触らない (翻訳編集 ≠ 記事レベル監査)。
func TestUpsertTranslation_仕様_親のupdated_atを触らない(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	_ = seedWithJa(t, repo, "01", apinews.StatusPending)
	before, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, repo.UpsertTranslation(ctx, "01", apinews.LangJa, "new-ja", "new-s", "new-b"))

	after, err := repo.GetByID(ctx, "01")
	require.NoError(t, err)
	assert.Equal(t, before.Article.UpdatedAt.UnixNano(), after.Article.UpdatedAt.UnixNano(),
		"翻訳編集では news_articles.updated_at は不変であるべき")
}
