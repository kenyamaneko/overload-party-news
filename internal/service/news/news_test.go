package news_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// 仕様 (FEATURE_SPEC §4): lang は必須、対応外は ErrUnsupportedLang、未指定は ErrLangRequired。
// lang が妥当なときのみ repo が呼ばれる (早期 fail)。
func TestList_仕様_langバリデーション(t *testing.T) {
	cases := []struct {
		name          string
		lang          string
		wantErr       error
		wantCallCount int
	}{
		{name: "ja", lang: apinews.LangJa, wantErr: nil, wantCallCount: 1},
		{name: "en", lang: apinews.LangEn, wantErr: nil, wantCallCount: 1},
		{name: "未指定", lang: "", wantErr: news.ErrLangRequired, wantCallCount: 0},
		{name: "対応外 (fr)", lang: "fr", wantErr: news.ErrUnsupportedLang, wantCallCount: 0},
		{name: "対応外 (大文字)", lang: "JA", wantErr: news.ErrUnsupportedLang, wantCallCount: 0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]apinews.NewsListItem, error) {
					callCount++
					return []apinews.NewsListItem{}, nil
				},
			}
			_, err := news.New(repo).List(context.Background(), tc.lang, news.ListLimitDefault)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
		})
	}
}

// 仕様: List は limit が [ListLimitMin, ListLimitMax] の範囲外なら ErrInvalidLimit を返し、repo を呼ばない。
func TestList_仕様_limitの範囲内のみ_repoが呼ばれる(t *testing.T) {
	cases := []struct {
		name          string
		limit         int
		wantErr       error
		wantCallCount int
	}{
		{name: "下限", limit: news.ListLimitMin, wantErr: nil, wantCallCount: 1},
		{name: "デフォルト値", limit: news.ListLimitDefault, wantErr: nil, wantCallCount: 1},
		{name: "上限", limit: news.ListLimitMax, wantErr: nil, wantCallCount: 1},
		{name: "下限未満", limit: news.ListLimitMin - 1, wantErr: news.ErrInvalidLimit, wantCallCount: 0},
		{name: "負値", limit: -1, wantErr: news.ErrInvalidLimit, wantCallCount: 0},
		{name: "上限超過", limit: news.ListLimitMax + 1, wantErr: news.ErrInvalidLimit, wantCallCount: 0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			repo := &port.MockNewsRepo{
				ListPublishedFn: func(_ context.Context, _ string, _ int) ([]apinews.NewsListItem, error) {
					callCount++
					return []apinews.NewsListItem{}, nil
				},
			}
			_, err := news.New(repo).List(context.Background(), apinews.LangJa, tc.limit)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
		})
	}
}

// 仕様: List は repo の結果をそのまま返し、lang 引数を repo に透過する。
func TestList_仕様_repoに渡す値とレスポンス透過(t *testing.T) {
	want := []apinews.NewsListItem{{ArticleID: "01", Source: "aws", Title: "T"}}
	var gotLang string
	var gotLimit int
	repo := &port.MockNewsRepo{
		ListPublishedFn: func(_ context.Context, lang string, limit int) ([]apinews.NewsListItem, error) {
			gotLang = lang
			gotLimit = limit
			return want, nil
		},
	}
	got, err := news.New(repo).List(context.Background(), apinews.LangEn, 42)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, apinews.LangEn, gotLang)
	assert.Equal(t, 42, gotLimit)
}

// 仕様: GetDetail も lang 必須、妥当なら repo のエラーを透過する (ErrNotFound 含む)。
func TestGetDetail_仕様_langバリデーションとエラー透過(t *testing.T) {
	someOtherErr := errors.New("db connection lost")

	cases := []struct {
		name     string
		lang     string
		repoErr  error
		wantErr  error
		wantCall bool
	}{
		{name: "ja 成功", lang: apinews.LangJa, repoErr: nil, wantErr: nil, wantCall: true},
		{name: "en 成功", lang: apinews.LangEn, repoErr: nil, wantErr: nil, wantCall: true},
		{name: "repo の not found を透過", lang: apinews.LangJa, repoErr: port.ErrNotFound, wantErr: port.ErrNotFound, wantCall: true},
		{name: "repo の DB 障害を透過", lang: apinews.LangJa, repoErr: someOtherErr, wantErr: someOtherErr, wantCall: true},
		{name: "lang 未指定", lang: "", repoErr: nil, wantErr: news.ErrLangRequired, wantCall: false},
		{name: "lang 対応外", lang: "fr", repoErr: nil, wantErr: news.ErrUnsupportedLang, wantCall: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			repo := &port.MockNewsRepo{
				GetPublishedByIDFn: func(_ context.Context, _ string, _ string) (*apinews.NewsDetail, error) {
					called = true
					if tc.repoErr != nil {
						return nil, tc.repoErr
					}
					return &apinews.NewsDetail{ArticleID: "abc"}, nil
				},
			}
			_, err := news.New(repo).GetDetail(context.Background(), "abc", tc.lang)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCall, called)
		})
	}
}
