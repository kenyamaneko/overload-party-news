// Package news は公開 API (gateway → news) の usecase を実装する。
// status = 'published' かつ翻訳ありの記事のみを返す read-only。
package news

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/presenter"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ListLimitMax は一覧 limit の上限。
const ListLimitMax = 100

var (
	ErrInvalidLimit    = errors.New("invalid limit")
	ErrLangRequired    = errors.New("lang is required")
	ErrUnsupportedLang = errors.New("unsupported lang")
)

// Interactor は公開 API の usecase。port.PublicNewsQuerier のみに依存する。
type Interactor struct {
	querier port.PublicNewsQuerier
}

// New は Interactor を生成する。
func New(querier port.PublicNewsQuerier) *Interactor {
	return &Interactor{querier: querier}
}

// List は指定言語で公開中記事の一覧を limit 件返す。
func (uc *Interactor) List(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error) {
	if err := validateLang(lang); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > ListLimitMax {
		return nil, fmt.Errorf("%w: limit=%d must be in (0, %d]",
			ErrInvalidLimit, limit, ListLimitMax)
	}
	rows, err := uc.querier.ListPublished(ctx, lang, limit)
	if err != nil {
		return nil, err
	}
	return presenter.ToNewsListItems(rows), nil
}

// GetDetail は指定言語で公開中記事の詳細を返す。非存在 / 非公開 / 該当 lang 翻訳なしは port.ErrNotFound が bubble する。
func (uc *Interactor) GetDetail(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error) {
	if err := validateLang(lang); err != nil {
		return nil, err
	}
	d, err := uc.querier.GetPublishedByID(ctx, articleID, lang)
	if err != nil {
		return nil, err
	}
	return presenter.ToNewsDetail(d), nil
}

// validateLang は lang を repo に渡す前に弾き「不正入力 (400)」と「該当データなし (404)」を区別する。
// 未指定と対応外で別エラーにし、handler / Gateway 側のメッセージ書き分けを許す。
func validateLang(lang string) error {
	if lang == "" {
		return ErrLangRequired
	}
	if !slices.Contains(domain.SupportedLangs, lang) {
		return fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	return nil
}
