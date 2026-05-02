package apinews

import "time"

// NewsListItem は公開 API (gateway → news) の一覧レスポンス要素。
// title / summary は指定 lang の翻訳由来。body / source_url は含まない。
type NewsListItem struct {
	ArticleID         string     `json:"article_id"`
	Source            string     `json:"source"`
	Title             string     `json:"title"`
	Summary           string     `json:"summary"`
	Tags              []string   `json:"tags"`
	SourcePublishedAt *time.Time `json:"source_published_at,omitempty"`
	PublishedAt       time.Time  `json:"published_at"`
}

// NewsListResponse は GET /internal/v1/news のレスポンス形状。
type NewsListResponse struct {
	Articles []NewsListItem `json:"articles"`
}

// NewsDetail は公開 API の詳細レスポンス。title / summary / body は指定 lang の翻訳由来。
type NewsDetail struct {
	ArticleID         string     `json:"article_id"`
	Source            string     `json:"source"`
	Title             string     `json:"title"`
	Summary           string     `json:"summary"`
	Body              string     `json:"body"`
	Tags              []string   `json:"tags"`
	SourceURL         string     `json:"source_url"`
	SourcePublishedAt *time.Time `json:"source_published_at,omitempty"`
	PublishedAt       time.Time  `json:"published_at"`
}

// EventTranslation は news-article-collected イベントペイロード内の 1 翻訳エントリ。
type EventTranslation struct {
	Lang    string `json:"lang"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Body    string `json:"body"`
}

// ArticleCollectedEvent は news-article-collected トピックのペイロード。
// MVP では Translations は ja 1 件のみ (将来 newsfeed が多言語生成する場合は要素が増える)。
type ArticleCollectedEvent struct {
	ArticleID         string             `json:"article_id"`
	Source            string             `json:"source"`
	SourceURL         string             `json:"source_url"`
	Tags              []string           `json:"tags"`
	SourcePublishedAt *time.Time         `json:"source_published_at,omitempty"`
	Translations      []EventTranslation `json:"translations"`
}

// TopicArticleCollected は Pub/Sub トピック名。
// 暫定的に news が所有する (将来 overload-party-common/pubsub-events に移管)。
const TopicArticleCollected = "news-article-collected"
