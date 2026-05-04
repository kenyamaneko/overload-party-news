package presenter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/presenter"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func TestArticleFromCollectedEvent(t *testing.T) {
	src := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	in := apinews.ArticleCollectedEvent{
		ArticleID:         "abc",
		Source:            "aws",
		SourceURL:         "https://example.com/abc",
		Tags:              []string{"compute"},
		SourcePublishedAt: &src,
		Translations:      []apinews.EventTranslation{{Lang: domain.LangJa}},
	}

	got := presenter.ArticleFromCollectedEvent(in)

	assert.Equal(t, "abc", got.ArticleID)
	assert.Equal(t, "aws", got.Source)
	assert.Equal(t, "https://example.com/abc", got.SourceURL)
	assert.Equal(t, []string{"compute"}, got.Tags)
	assert.Equal(t, &src, got.SourcePublishedAt)
	assert.Empty(t, got.Status, "Status は SSoT 上で導出するため presenter は値を入れない")
}
