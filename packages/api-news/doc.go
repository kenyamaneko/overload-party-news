// Package apinews は news サービスの API 契約型を公開する。
//
// 公開対象:
//   - 公開 REST API (gateway → news) のレスポンス型 (NewsListItem, NewsDetail, NewsListResponse)
//   - Pub/Sub 購読イベントのペイロード型 (ArticleCollectedEvent)
//   - ドメインモデル (Article, Status)
//
// internal/port と service 層はここで定義された型を共通言語とする。
// 将来 data/models.yaml による生成に移行するが、現時点では手書き。
package apinews
