// Package ingest は news-article-collected Pub/Sub イベントを自スキーマに永続化する use case。
// 冪等性は port.NewsIngester (ON CONFLICT DO NOTHING) に委譲する。
package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ErrInvalidEventPayload は必須フィールドが欠けたイベントに対して返すセンチネル。
// subscriber handler はこれを deterministic error として ACK する (再送しても結果が変わらない)。
var ErrInvalidEventPayload = errors.New("invalid event payload")

// Service は Pub/Sub event 1 件を受け取り DB 行 (記事 + 翻訳群) に変換・永続化する。
type Service struct {
	ingester port.NewsIngester
}

// New は Service を生成する。
func New(ingester port.NewsIngester) *Service {
	return &Service{ingester: ingester}
}

// Insert はイベントを変換して INSERT する。
// 既存記事とのコンフリクトは inserted=false で no-op 扱い。
// 必須フィールド欠落・対応外 lang・translations 空は ErrInvalidEventPayload を返す。
func (s *Service) Insert(ctx context.Context, event apinews.ArticleCollectedEvent) (inserted bool, err error) {
	if err := validateEvent(event); err != nil {
		return false, err
	}

	article := apinews.Article{
		ArticleID:         event.ArticleID,
		Source:            event.Source,
		SourceURL:         event.SourceURL,
		Tags:              event.Tags,
		Status:            apinews.StatusPending,
		SourcePublishedAt: event.SourcePublishedAt,
	}
	translations := make([]apinews.Translation, 0, len(event.Translations))
	for _, t := range event.Translations {
		translations = append(translations, apinews.Translation{
			ArticleID: event.ArticleID,
			Lang:      t.Lang,
			Title:     t.Title,
			Summary:   t.Summary,
			Body:      t.Body,
		})
	}
	return s.ingester.Insert(ctx, article, translations)
}

// validateEvent は必須フィールドが揃い、translations が 1 件以上かつ全て対応言語 + 非空であることを確認する。
func validateEvent(e apinews.ArticleCollectedEvent) error {
	if e.ArticleID == "" {
		return fmt.Errorf("%w: article_id is empty", ErrInvalidEventPayload)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: source is empty", ErrInvalidEventPayload)
	}
	if e.SourceURL == "" {
		return fmt.Errorf("%w: source_url is empty", ErrInvalidEventPayload)
	}
	if len(e.Translations) == 0 {
		return fmt.Errorf("%w: translations is empty", ErrInvalidEventPayload)
	}
	for i, t := range e.Translations {
		if err := validateEventTranslation(t); err != nil {
			return fmt.Errorf("translations[%d]: %w", i, err)
		}
	}
	return nil
}

func validateEventTranslation(t apinews.EventTranslation) error {
	if t.Lang == "" {
		return fmt.Errorf("%w: lang is empty", ErrInvalidEventPayload)
	}
	if !apinews.IsSupportedLang(t.Lang) {
		return fmt.Errorf("%w: unsupported lang %q", ErrInvalidEventPayload, t.Lang)
	}
	if t.Title == "" {
		return fmt.Errorf("%w: title is empty", ErrInvalidEventPayload)
	}
	if t.Summary == "" {
		return fmt.Errorf("%w: summary is empty", ErrInvalidEventPayload)
	}
	if t.Body == "" {
		return fmt.Errorf("%w: body is empty", ErrInvalidEventPayload)
	}
	return nil
}
