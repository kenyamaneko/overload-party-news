package presenter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/presenter"
)

func TestToNewsListItem(t *testing.T) {
	src := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	pub := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	in := domain.PublishedArticleSummary{
		ArticleID:         "abc",
		Source:            "aws",
		Title:             "T",
		Summary:           "S",
		Tags:              []string{"x", "y"},
		SourcePublishedAt: &src,
		PublishedAt:       pub,
	}

	got := presenter.ToNewsListItem(in)

	assert.Equal(t, "abc", got.ArticleID)
	assert.Equal(t, "aws", got.Source)
	assert.Equal(t, "T", got.Title)
	assert.Equal(t, "S", got.Summary)
	assert.Equal(t, []string{"x", "y"}, got.Tags)
	assert.Equal(t, &src, got.SourcePublishedAt)
	assert.Equal(t, pub, got.PublishedAt)
}

func TestToNewsListItems(t *testing.T) {
	pub := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	in := []domain.PublishedArticleSummary{
		{ArticleID: "a", PublishedAt: pub},
		{ArticleID: "b", PublishedAt: pub},
	}

	got := presenter.ToNewsListItems(in)

	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ArticleID)
	assert.Equal(t, "b", got[1].ArticleID)
}

func TestToNewsListItems_EmptyInputReturnsEmptySlice(t *testing.T) {
	got := presenter.ToNewsListItems(nil)
	assert.NotNil(t, got, "handler が nil ガードで埋め直す挙動と整合させ、空 slice を返す")
	assert.Empty(t, got)
}

func TestToNewsDetail(t *testing.T) {
	src := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	pub := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	in := &domain.PublishedArticleDetail{
		ArticleID:         "abc",
		Source:            "aws",
		Title:             "T",
		Summary:           "S",
		Body:              "B",
		Tags:              []string{"x"},
		SourceURL:         "https://example.com/abc",
		SourcePublishedAt: &src,
		PublishedAt:       pub,
	}

	got := presenter.ToNewsDetail(in)

	assert.Equal(t, "abc", got.ArticleID)
	assert.Equal(t, "aws", got.Source)
	assert.Equal(t, "T", got.Title)
	assert.Equal(t, "S", got.Summary)
	assert.Equal(t, "B", got.Body)
	assert.Equal(t, []string{"x"}, got.Tags)
	assert.Equal(t, "https://example.com/abc", got.SourceURL)
	assert.Equal(t, &src, got.SourcePublishedAt)
	assert.Equal(t, pub, got.PublishedAt)
}
