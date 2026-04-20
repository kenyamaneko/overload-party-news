package apinews

import "time"

// Status は news_articles.status に対応する enum 値。
// 未知の値は news サービスが非公開扱いするフェイルセーフを備える。
type Status string

const (
	StatusPending   Status = "pending"
	StatusPublished Status = "published"
	StatusRejected  Status = "rejected"
)

// 対応言語コード (MVP は ja / en のみ)。
// 未知値はリクエスト時に ErrUnsupportedLang として弾く。
const (
	LangJa = "ja"
	LangEn = "en"
)

// SupportedLangs はサポート対象の言語コード集合 (定義順を保つため slice)。
var SupportedLangs = []string{LangJa, LangEn}

// IsSupportedLang は lang が SupportedLangs に含まれるかを判定する。
func IsSupportedLang(lang string) bool {
	for _, l := range SupportedLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// Article は news_articles 行の言語非依存部分。
// 公開 API のレスポンスは NewsListItem / NewsDetail に射影した上で返す。
type Article struct {
	ArticleID         string     `json:"article_id"`
	Source            string     `json:"source"`
	SourceURL         string     `json:"source_url"`
	Tags              []string   `json:"tags"`
	Status            Status     `json:"status"`
	SourcePublishedAt *time.Time `json:"source_published_at,omitempty"`
	PublishedAt       *time.Time `json:"published_at,omitempty"`
	IngestedAt        time.Time  `json:"ingested_at"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	Reviewer          *string    `json:"reviewer,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Translation は news_article_translations の 1 行。
type Translation struct {
	ArticleID string    `json:"article_id"`
	Lang      string    `json:"lang"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ArticleWithTranslations は管理 UI 向けに記事 + 存在する翻訳群をまとめた view。
type ArticleWithTranslations struct {
	Article      Article       `json:"article"`
	Translations []Translation `json:"translations"`
}

// NewsListItem は公開 API の一覧レスポンス要素。
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
// newsfeed Cloud Run Job が publish、news サービスが subscribe する。
// MVP では Translations は ja 1 件のみだが、将来 newsfeed が多言語を生成すれば要素が増えるだけで済む設計。
type ArticleCollectedEvent struct {
	ArticleID         string             `json:"article_id"`
	Source            string             `json:"source"`
	SourceURL         string             `json:"source_url"`
	Tags              []string           `json:"tags"`
	SourcePublishedAt *time.Time         `json:"source_published_at,omitempty"`
	Translations      []EventTranslation `json:"translations"`
}

// TopicArticleCollected は Pub/Sub トピック名。
// 本来は overload-party-common/pubsub-events で管理するべきだが、現時点で未定義のため
// news リポジトリ内で暫定的に所有する。common への移管は後続タスク。
const TopicArticleCollected = "news-article-collected"
