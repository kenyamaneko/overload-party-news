// Package apinews は news サービスが外部とやり取りする契約型を公開する。
//
// 公開対象:
//   - 公開 REST API (gateway → news) のレスポンス型 (NewsListItem, NewsDetail, NewsListResponse)
//   - Pub/Sub 購読イベントのペイロード型 (ArticleCollectedEvent, EventTranslation, TopicArticleCollected)
//
// ドメインモデル (Article / Translation / Status / DeriveStatus / lang 定数 等) は
// internal/domain に置く。本パッケージは「外部に公開する契約」だけを束ねる責務に限定し、
// repository / port / service の内部表現には立ち入らない。
//
// 将来 data/models.yaml による生成に移行するが、現時点では手書き。
package apinews
