// Package domain は news サービスのドメインモデルと業務不変条件を定義する。
package domain

import "time"

// Status は校閲状態の enum 値。永続化せず DeriveStatus で導出する。
type Status string

const (
	StatusPending   Status = "pending"
	StatusPublished Status = "published"
	StatusRejected  Status = "rejected"
)

// Statuses は全 Status の列挙。「全件」呼び出しを nil 分岐なく表現するために slice で公開。
var Statuses = []Status{StatusPending, StatusPublished, StatusRejected}

// DeriveStatus は reviewed_at / published_at から status を導出する。校閲状態の SSoT。
func DeriveStatus(a Article) Status {
	if a.ReviewedAt == nil {
		return StatusPending
	}
	// publish 後に reject された記事を考慮して時刻関係まで見る
	// (両者非 nil の状態は Reject が published_at を保持する設計に由来)。
	if a.PublishedAt != nil && !a.PublishedAt.Before(*a.ReviewedAt) {
		return StatusPublished
	}
	return StatusRejected
}

// 対応言語コード (MVP は ja / en のみ)。
const (
	LangJa = "ja"
	LangEn = "en"
)

// SupportedLangs は対応言語コード集合。DB 側 CHECK 制約と同期して保つ。
var SupportedLangs = []string{LangJa, LangEn}

// Article は news_articles 行の言語非依存部分を表すドメインエンティティ。
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
// title / summary は指定 lang の翻訳由来。published_at は published 条件で事前絞り込み済みのため非 nullable。
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
// title / summary / body は指定 lang の翻訳由来。published_at は published 条件で事前絞り込み済みのため非 nullable。
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
