// Package news は公開 API (gateway → news) のユースケースを実装する。
// 指定言語で公開可能な記事 (status = 'published' かつ翻訳あり) の一覧・詳細を返す read-only サービス。
package news

import (
	"context"
	"errors"
	"fmt"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// FEATURE_SPEC.md §4 で定める一覧 limit の許容範囲。
const (
	ListLimitMin     = 1
	ListLimitMax     = 100
	ListLimitDefault = 20
)

// エラーセンチネル: handler が HTTP ステータスに変換する。
var (
	ErrInvalidLimit    = errors.New("invalid limit")
	ErrLangRequired    = errors.New("lang is required")
	ErrUnsupportedLang = errors.New("unsupported lang")
)

// Service は公開 API の use case 層。port.PublicNewsQuerier のみに依存する。
type Service struct {
	querier port.PublicNewsQuerier
}

// New は Service を生成する。
func New(querier port.PublicNewsQuerier) *Service {
	return &Service{querier: querier}
}

// List は指定言語で公開中記事の一覧を limit 件返す。
func (s *Service) List(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error) {
	if err := validateLang(lang); err != nil {
		return nil, err
	}
	if limit < ListLimitMin || limit > ListLimitMax {
		return nil, fmt.Errorf("%w: limit=%d must be in [%d, %d]",
			ErrInvalidLimit, limit, ListLimitMin, ListLimitMax)
	}
	return s.querier.ListPublished(ctx, lang, limit)
}

// GetDetail は指定言語で公開中記事の詳細を返す。
// 非存在 / 非公開 / 該当 lang 翻訳なしは port.ErrNotFound が bubble する。
func (s *Service) GetDetail(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error) {
	if err := validateLang(lang); err != nil {
		return nil, err
	}
	return s.querier.GetPublishedByID(ctx, articleID, lang)
}

// validateLang は lang が空でなく対応言語であることを確認する。
// 未指定 / 対応外は別エラー (handler で 400 にマップ) で識別する。
func validateLang(lang string) error {
	if lang == "" {
		return ErrLangRequired
	}
	if !apinews.IsSupportedLang(lang) {
		return fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	return nil
}
