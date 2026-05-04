package presenter

import (
	"github.com/kenyamaneko/overload-party-news/internal/domain"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ArticleFromCollectedEvent は news-article-collected イベントの言語非依存部分を
// domain.Article に詰め替えます。Status は SSoT 上で導出されるためここでは設定しません。
// イベントの妥当性 (必須欠け・言語制約等) は呼び出し側 usecase で検証済みである前提です。
func ArticleFromCollectedEvent(e apinews.ArticleCollectedEvent) domain.Article {
	return domain.Article{
		ArticleID:         e.ArticleID,
		Source:            e.Source,
		SourceURL:         e.SourceURL,
		Tags:              e.Tags,
		SourcePublishedAt: e.SourcePublishedAt,
	}
}
