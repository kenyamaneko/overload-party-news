// Package apinews は news サービスが外部とやり取りする契約型を公開する。
//
// 公開対象:
//   - 公開 REST API (gateway → news) のレスポンス型 (NewsListItem, NewsDetail, NewsListResponse)
//   - Pub/Sub 購読イベントのペイロード型 (ArticleCollectedEvent, EventTranslation, TopicArticleCollected)
//
// ドメインモデルは internal/domain に置く。本パッケージは外部に公開する契約だけを束ねる。
package apinews
