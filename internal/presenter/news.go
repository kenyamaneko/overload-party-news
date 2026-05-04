package presenter

import (
	"github.com/kenyamaneko/overload-party-news/internal/domain"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ToNewsListItem は domain.PublishedArticleSummary を wire の NewsListItem に詰め替えます。
func ToNewsListItem(s domain.PublishedArticleSummary) apinews.NewsListItem {
	return apinews.NewsListItem{
		ArticleID:         s.ArticleID,
		Source:            s.Source,
		Title:             s.Title,
		Summary:           s.Summary,
		Tags:              s.Tags,
		SourcePublishedAt: s.SourcePublishedAt,
		PublishedAt:       s.PublishedAt,
	}
}

// ToNewsListItems は domain.PublishedArticleSummary slice を NewsListItem slice に詰め替えます。
func ToNewsListItems(rows []domain.PublishedArticleSummary) []apinews.NewsListItem {
	items := make([]apinews.NewsListItem, len(rows))
	for i, r := range rows {
		items[i] = ToNewsListItem(r)
	}
	return items
}

// ToNewsDetail は domain.PublishedArticleDetail を wire の NewsDetail に詰め替えます。
func ToNewsDetail(d *domain.PublishedArticleDetail) *apinews.NewsDetail {
	return &apinews.NewsDetail{
		ArticleID:         d.ArticleID,
		Source:            d.Source,
		Title:             d.Title,
		Summary:           d.Summary,
		Body:              d.Body,
		Tags:              d.Tags,
		SourceURL:         d.SourceURL,
		SourcePublishedAt: d.SourcePublishedAt,
		PublishedAt:       d.PublishedAt,
	}
}
