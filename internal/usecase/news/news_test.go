package news_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// validLimit は境界を狙わない「任意の有効な limit」を表す。
const validLimit = 10

// TestList_ProjectsPublishedRowsAndForwardsParams は List が querier へ lang/limit を渡し、
// 返った行を wire の一覧項目へ射影することを検証する。
func TestList_ProjectsPublishedRowsAndForwardsParams(t *testing.T) {
	pub := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	rows := []domain.PublishedArticleSummary{
		{ArticleID: "a1", Source: string(apinews.SourceAws), Title: "T1", Summary: "S1", Tags: []string{"x"}, PublishedAt: pub},
		{ArticleID: "a2", Source: string(apinews.SourceGoogleCloud), Title: "T2", Summary: "S2", Tags: []string{"y"}, PublishedAt: pub},
	}

	cases := []struct {
		name  string
		lang  string
		limit int
	}{
		{name: "lang=ja + limit=1 (下限)", lang: domain.LangJa, limit: 1},
		{name: "lang=ja + limit=上限", lang: domain.LangJa, limit: news.ListLimitMax},
		{name: "lang=en", lang: domain.LangEn, limit: validLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLang string
			var gotLimit int
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error) {
					gotLang, gotLimit = lang, limit
					return rows, nil
				},
			}

			got, err := news.New(repo).List(context.Background(), tc.lang, tc.limit)

			assert.NoError(t, err)
			assert.Equal(t, tc.lang, gotLang)
			assert.Equal(t, tc.limit, gotLimit)
			assert.Len(t, got, len(rows))
			assert.Equal(t, "a1", got[0].ArticleID)
			assert.Equal(t, "T1", got[0].Title)
			assert.Equal(t, apinews.SourceAws, got[0].Source)
			assert.Equal(t, "a2", got[1].ArticleID)
			assert.Equal(t, apinews.SourceGoogleCloud, got[1].Source)
		})
	}
}

// TestList_RejectsInvalidInputWithoutQuerying は不正な lang/limit を querier に到達させず、
// 対応する sentinel を返すことを検証する。
func TestList_RejectsInvalidInputWithoutQuerying(t *testing.T) {
	cases := []struct {
		name    string
		lang    string
		limit   int
		wantErr error
	}{
		{name: "lang 未指定は ErrLangRequired", lang: "", limit: news.ListLimitMax, wantErr: news.ErrLangRequired},
		{name: "lang 対応外は ErrUnsupportedLang", lang: "fr", limit: news.ListLimitMax, wantErr: news.ErrUnsupportedLang},
		{name: "lang は大小文字を区別する (JA)", lang: "JA", limit: news.ListLimitMax, wantErr: news.ErrUnsupportedLang},
		{name: "limit=0 は ErrInvalidLimit", lang: domain.LangJa, limit: 0, wantErr: news.ErrInvalidLimit},
		{name: "limit 負値は ErrInvalidLimit", lang: domain.LangJa, limit: -1, wantErr: news.ErrInvalidLimit},
		{name: "limit 上限超過は ErrInvalidLimit", lang: domain.LangJa, limit: news.ListLimitMax + 1, wantErr: news.ErrInvalidLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// ListPublishedFn を未設定にすることで、入力検証を抜けて querier に到達したら
			// MockNewsRepo が panic する = repo 未到達を強制する。
			repo := &port.MockNewsRepo{}

			got, err := news.New(repo).List(context.Background(), tc.lang, tc.limit)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, got)
		})
	}
}

// TestList_PropagatesQuerierError は querier の失敗が List からそのまま伝播することを検証する。
func TestList_PropagatesQuerierError(t *testing.T) {
	wantErr := errors.New("querier: db connection lost")
	repo := &port.MockNewsRepo{
		ListPublishedFn: func(_ context.Context, _ string, _ int) ([]domain.PublishedArticleSummary, error) {
			return nil, wantErr
		},
	}

	got, err := news.New(repo).List(context.Background(), domain.LangJa, validLimit)

	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, got)
}

func TestGetDetail(t *testing.T) {
	dbErr := errors.New("db connection lost")
	pub := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	repoSuccess := &domain.PublishedArticleDetail{
		ArticleID: "abc", Source: "aws", Title: "T", Summary: "S",
		Body: "B", Tags: []string{"x"}, SourceURL: "https://example.com/abc",
		PublishedAt: pub,
	}
	wantSuccess := &apinews.NewsDetail{
		ArticleID: "abc", Source: apinews.SourceAws, Title: "T", Summary: "S",
		Body: "B", Tags: []string{"x"}, SourceURL: "https://example.com/abc",
		PublishedAt: pub,
	}

	cases := []struct {
		name        string
		lang        string
		repoReturn  *domain.PublishedArticleDetail
		repoErr     error
		wantDetail  *apinews.NewsDetail
		wantErr     error
		wantRepoHit bool
	}{
		{
			name:        "成功時は domain DTO を apinews.NewsDetail に射影して返す",
			lang:        domain.LangJa,
			repoReturn:  repoSuccess,
			wantDetail:  wantSuccess,
			wantRepoHit: true,
		},
		{
			name:        "repo の ErrNotFound がそのまま返る",
			lang:        domain.LangJa,
			repoErr:     port.ErrNotFound,
			wantErr:     port.ErrNotFound,
			wantRepoHit: true,
		},
		{
			name:        "repo の DB 障害がそのまま返る",
			lang:        domain.LangJa,
			repoErr:     dbErr,
			wantErr:     dbErr,
			wantRepoHit: true,
		},
		{
			name:        "lang 未指定は repo を呼ばず ErrLangRequired",
			lang:        "",
			wantErr:     news.ErrLangRequired,
			wantRepoHit: false,
		},
		{
			name:        "lang 対応外は repo を呼ばず ErrUnsupportedLang",
			lang:        "fr",
			wantErr:     news.ErrUnsupportedLang,
			wantRepoHit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var repoCalled bool
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*domain.PublishedArticleDetail, error) {
					repoCalled = true
					return tc.repoReturn, tc.repoErr
				},
			}
			got, err := news.New(repo).GetDetail(context.Background(), "abc", tc.lang)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantDetail, got)
			assert.Equal(t, tc.wantRepoHit, repoCalled)
		})
	}
}
