// Package review は管理 UI の校閲ユースケース (承認・却下・翻訳 upsert) と閲覧用 read 操作を実装する。
// 公開 API (service/news) とは別サービスとし、依存する port も AdminNewsQuerier / NewsReviewer に限定する。
package review

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// FEATURE_SPEC.md §6.4 で定める翻訳バリデーション。
const (
	TitleMaxLen   = 500
	SummaryMaxLen = 2000
)

// 管理 UI 一覧表示のデフォルト件数。FEATURE_SPEC.md §7.1。
const (
	AdminListLimitMin     = 1
	AdminListLimitMax     = 200
	AdminListLimitDefault = 50
)

// Service は校閲・管理閲覧の use case 層。
type Service struct {
	querier  port.AdminNewsQuerier
	reviewer port.NewsReviewer
	now      func() time.Time
}

// New は Service を生成する。now は time.Now を注入することでテスト容易性を保つ。
func New(querier port.AdminNewsQuerier, reviewer port.NewsReviewer, now func() time.Time) *Service {
	return &Service{querier: querier, reviewer: reviewer, now: now}
}

// List は管理 UI 向けに status フィルタ可能な記事一覧 (各記事の全翻訳を含む) を返す。
// statusFilter が nil のとき全件。
func (s *Service) List(ctx context.Context, statusFilter *apinews.Status, limit int) ([]apinews.ArticleWithTranslations, error) {
	if limit < AdminListLimitMin || limit > AdminListLimitMax {
		return nil, fmt.Errorf("%w: limit=%d must be in [%d, %d]",
			ErrInvalidField, limit, AdminListLimitMin, AdminListLimitMax)
	}
	return s.querier.ListByStatus(ctx, statusFilter, limit)
}

// Get は管理 UI 向けに任意 status の記事 + 全翻訳を返す。非存在は port.ErrNotFound が bubble。
func (s *Service) Get(ctx context.Context, articleID string) (*apinews.ArticleWithTranslations, error) {
	return s.querier.GetByID(ctx, articleID)
}

// Publish は承認。published_at / reviewed_at / reviewer を now にセットする。
// 既に published でも冪等 (published_at は毎回更新され、一覧で最新扱いになる)。
func (s *Service) Publish(ctx context.Context, articleID string, reviewer string) error {
	if reviewer == "" {
		return fmt.Errorf("%w: reviewer is required", ErrInvalidField)
	}
	return s.reviewer.Publish(ctx, articleID, reviewer, s.now())
}

// Reject は却下。reviewed_at / reviewer を now にセットする。published_at は変更しない。
func (s *Service) Reject(ctx context.Context, articleID string, reviewer string) error {
	if reviewer == "" {
		return fmt.Errorf("%w: reviewer is required", ErrInvalidField)
	}
	return s.reviewer.Reject(ctx, articleID, reviewer, s.now())
}

// UpsertTranslation は指定言語の翻訳を追加 / 更新する。
// 長さバリデーション (FEATURE_SPEC §6.4) をここで強制する。
// status / reviewer は変更しない (翻訳編集はレビュー判断と区別する)。
func (s *Service) UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	if err := validateLang(lang); err != nil {
		return err
	}
	if err := validateTitle(title); err != nil {
		return err
	}
	if err := validateSummary(summary); err != nil {
		return err
	}
	if err := validateBody(body); err != nil {
		return err
	}
	return s.reviewer.UpsertTranslation(ctx, articleID, lang, title, summary, body)
}

func validateLang(lang string) error {
	if lang == "" {
		return fmt.Errorf("%w: lang is required", ErrInvalidField)
	}
	if !apinews.IsSupportedLang(lang) {
		return fmt.Errorf("%w: unsupported lang %q", ErrInvalidField, lang)
	}
	return nil
}

func validateTitle(title string) error {
	n := utf8.RuneCountInString(title)
	if n == 0 {
		return fmt.Errorf("%w: title is empty", ErrInvalidField)
	}
	if n > TitleMaxLen {
		return fmt.Errorf("%w: title length %d exceeds max %d", ErrInvalidField, n, TitleMaxLen)
	}
	return nil
}

func validateSummary(summary string) error {
	n := utf8.RuneCountInString(summary)
	if n == 0 {
		return fmt.Errorf("%w: summary is empty", ErrInvalidField)
	}
	if n > SummaryMaxLen {
		return fmt.Errorf("%w: summary length %d exceeds max %d", ErrInvalidField, n, SummaryMaxLen)
	}
	return nil
}

func validateBody(body string) error {
	if utf8.RuneCountInString(body) == 0 {
		return fmt.Errorf("%w: body is empty", ErrInvalidField)
	}
	return nil
}
