// Package domain は news サービスのドメインモデルと業務不変条件を定義する。
// 外部 (DB / API / Pub/Sub) に依存せず、純粋な値型と派生規則のみを持つ。
// repository / port / service が共通言語として参照する。
package domain

import "time"

// Status は校閲状態の enum 値。テーブルには永続化せず、reviewed_at と published_at から導出する。
// 導出規則は DeriveStatus が SSoT。
type Status string

const (
	StatusPending   Status = "pending"
	StatusPublished Status = "published"
	StatusRejected  Status = "rejected"
)

// Statuses は全 Status の列挙 (定義順を保つため slice)。
// 「全件」を表現したいときに丸ごと渡すことで、リポジトリ層に「nil なら全件」の分岐を持たせない。
var Statuses = []Status{StatusPending, StatusPublished, StatusRejected}

// DeriveStatus は記事の reviewed_at / published_at から status を導出する。
// 校閲状態の SSoT。repo の WHERE 述語と 1:1 で対応する。
// 状態遷移と各カラム更新の仕様は FEATURE_SPEC を参照。
func DeriveStatus(a Article) Status {
	if a.ReviewedAt == nil {
		return StatusPending
	}
	// 通常動線では発生しないが、publish 後に reject された記事を考慮して時刻関係まで見る
	// (両者非 nil の状態は Reject が published_at を保持する設計に由来)。
	if a.PublishedAt != nil && !a.PublishedAt.Before(*a.ReviewedAt) {
		return StatusPublished
	}
	return StatusRejected
}

// 対応言語コード (MVP は ja / en のみ)。
// 未知値はリクエスト時に usecase 層で弾く。
const (
	LangJa = "ja"
	LangEn = "en"
)

// SupportedLangs はサポート対象の言語コード集合 (定義順を保つため slice)。
// 許容値の SSoT は DB の news_article_translations.lang CHECK 制約で、本リストはそれと同期して保つ。
var SupportedLangs = []string{LangJa, LangEn}

// Article は news_articles 行の言語非依存部分を表すドメインエンティティ。
// API レスポンスに直接シリアライズせず、必要に応じて usecase 層が apinews.X に射影する。
type Article struct {
	ArticleID         string
	Source            string
	SourceURL         string
	Tags              []string
	Status            Status
	SourcePublishedAt *time.Time
	PublishedAt       *time.Time
	IngestedAt        time.Time
	ReviewedAt        *time.Time
	Reviewer          *string
	UpdatedAt         time.Time
}

// Translation は news_article_translations の 1 行。
type Translation struct {
	ArticleID string
	Lang      string
	Title     string
	Summary   string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ArticleWithTranslations は管理 UI が扱う「記事 + 存在する翻訳群」の合成 view。
type ArticleWithTranslations struct {
	Article      Article
	Translations []Translation
}

// PublishedArticleSummary は ListPublished が返す一覧 1 行分の DTO。
// title / summary は指定 lang の翻訳由来。published_at は事前に published 条件で絞り込んでいるため非 nullable。
// 公開 API レスポンス (apinews.NewsListItem) への射影は usecase 層が行う。
type PublishedArticleSummary struct {
	ArticleID         string
	Source            string
	Title             string
	Summary           string
	Tags              []string
	SourcePublishedAt *time.Time
	PublishedAt       time.Time
}

// PublishedArticleDetail は GetPublishedByID が返す詳細 1 件分の DTO。
// title / summary / body は指定 lang の翻訳由来。published_at は事前に published 条件で絞り込んでいるため非 nullable。
// 公開 API レスポンス (apinews.NewsDetail) への射影は usecase 層が行う。
type PublishedArticleDetail struct {
	ArticleID         string
	Source            string
	Title             string
	Summary           string
	Body              string
	Tags              []string
	SourceURL         string
	SourcePublishedAt *time.Time
	PublishedAt       time.Time
}
