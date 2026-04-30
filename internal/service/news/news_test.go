package news_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// 仕様: List は lang が空なら ErrLangRequired、対応外なら ErrUnsupportedLang を返す。
// lang 値の網羅は validateLang の専用テストに任せ、ここでは List が validateLang のエラーを正しく bubble するかだけ確認する。
func TestList_仕様_lang不正のエラー種別(t *testing.T) {
	cases := []struct {
		name    string
		lang    string
		wantErr error
	}{
		{
			name:    "未指定は ErrLangRequired",
			lang:    "",
			wantErr: news.ErrLangRequired,
		},
		{
			name:    "対応外は ErrUnsupportedLang",
			lang:    "fr",
			wantErr: news.ErrUnsupportedLang,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]apinews.NewsListItem, error) {
					return nil, nil
				},
			}
			_, err := news.New(repo).List(context.Background(), tc.lang, news.ListLimitMax)

			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// 仕様: List は limit が (0, ListLimitMax] の範囲内なら指定件数を返し、範囲外なら ErrInvalidLimit を返す。
func TestList_仕様_limitに応じた件数とエラー(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		wantErr   error
		wantCount int
	}{
		{
			name:      "1 (下限)",
			limit:     1,
			wantErr:   nil,
			wantCount: 1,
		},
		{
			name:      "上限",
			limit:     news.ListLimitMax,
			wantErr:   nil,
			wantCount: news.ListLimitMax,
		},
		{
			name:      "0 はエラー",
			limit:     0,
			wantErr:   news.ErrInvalidLimit,
			wantCount: 0,
		},
		{
			name:      "負値はエラー",
			limit:     -1,
			wantErr:   news.ErrInvalidLimit,
			wantCount: 0,
		},
		{
			name:      "上限超過はエラー",
			limit:     news.ListLimitMax + 1,
			wantErr:   news.ErrInvalidLimit,
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, limit int) ([]apinews.NewsListItem, error) {
					items := make([]apinews.NewsListItem, limit)
					return items, nil
				},
			}
			got, err := news.New(repo).List(context.Background(), apinews.LangJa, tc.limit)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Len(t, got, tc.wantCount)
		})
	}
}

// 仕様: GetDetail は lang を validateLang で検証し、不正なら repo を呼ばずに対応エラーを返す。
func TestGetDetail_仕様_lang不正は早期fail(t *testing.T) {
	cases := []struct {
		name    string
		lang    string
		wantErr error
	}{
		{
			name:    "未指定は ErrLangRequired",
			lang:    "",
			wantErr: news.ErrLangRequired,
		},
		{
			name:    "対応外は ErrUnsupportedLang",
			lang:    "fr",
			wantErr: news.ErrUnsupportedLang,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*apinews.NewsDetail, error) {
					called = true
					return nil, nil
				},
			}
			_, err := news.New(repo).GetDetail(context.Background(), "abc", tc.lang)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.False(t, called, "lang 不正なら repo は呼ばれない")
		})
	}
}

// 仕様: GetDetail は lang が妥当なら repo を呼び、その返り値とエラーをそのまま返す。
func TestGetDetail_仕様_repoの返り値がそのまま返る(t *testing.T) {
	dbErr := errors.New("db connection lost")
	successDetail := &apinews.NewsDetail{ArticleID: "abc"}

	cases := []struct {
		name       string
		repoReturn *apinews.NewsDetail
		repoErr    error
		wantDetail *apinews.NewsDetail
		wantErr    error
	}{
		{
			name:       "成功時は repo の値が返る",
			repoReturn: successDetail,
			wantDetail: successDetail,
			wantErr:    nil,
		},
		{
			name:    "repo の ErrNotFound がそのまま返る",
			repoErr: port.ErrNotFound,
			wantErr: port.ErrNotFound,
		},
		{
			name:    "repo の DB 障害がそのまま返る",
			repoErr: dbErr,
			wantErr: dbErr,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*apinews.NewsDetail, error) {
					return tc.repoReturn, tc.repoErr
				},
			}
			got, err := news.New(repo).GetDetail(context.Background(), "abc", apinews.LangJa)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantDetail, got)
		})
	}
}
