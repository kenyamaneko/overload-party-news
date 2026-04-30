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

// AdminListLimitMax は管理 UI 一覧 limit の上限 (FEATURE_SPEC.md §7.1)。
// 過大要求による I/O 圧迫を防ぐ安全弁。下限はゼロ以下を弾くだけで十分なため定数化していない。
const AdminListLimitMax = 200

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

// List は管理 UI 向けに status 集合フィルタ可能な記事一覧 (各記事の全翻訳を含む) を返す。
// 「全件」を意図する場合は呼び出し側が apinews.Statuses を渡す契約。
//
// 設計方針: status 解決は service 層の責務とし、repo には status 概念を持ち込まない。
// repo から ingested_at DESC で limit 件取得 → apinews.DeriveStatus で導出 → 要求 status に絞り込む。
// この semantics は「最新 limit 件のうち、指定 status のもの」を返す (admin UI の用途として許容)。
// 翻訳は status 絞り込み後の article_id に対してのみ取得し、article_id で結合する。
func (s *Service) List(ctx context.Context, statuses []apinews.Status, limit int) ([]apinews.ArticleWithTranslations, error) {
	if limit <= 0 || limit > AdminListLimitMax {
		return nil, fmt.Errorf("%w: limit=%d must be in (0, %d]",
			ErrInvalidField, limit, AdminListLimitMax)
	}
	articles, err := s.querier.ListArticles(ctx, limit)
	if err != nil {
		return nil, err
	}

	wanted := make(map[apinews.Status]struct{}, len(statuses))
	for _, st := range statuses {
		wanted[st] = struct{}{}
	}
	filtered := make([]apinews.Article, 0, len(articles))
	for _, a := range articles {
		a.Status = apinews.DeriveStatus(a)
		if _, ok := wanted[a.Status]; ok {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return []apinews.ArticleWithTranslations{}, nil
	}

	ids := make([]string, len(filtered))
	for i, a := range filtered {
		ids[i] = a.ArticleID
	}
	translations, err := s.querier.ListTranslationsByArticleIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	grouped := groupTranslationsByArticleID(translations)
	results := make([]apinews.ArticleWithTranslations, len(filtered))
	for i, a := range filtered {
		results[i] = apinews.ArticleWithTranslations{
			Article:      a,
			Translations: grouped[a.ArticleID],
		}
	}
	return results, nil
}

// Get は管理 UI 向けに任意 status の記事 + 全翻訳を返す。非存在は port.ErrNotFound が bubble。
// Article.Status は apinews.DeriveStatus で導出してから返す。
func (s *Service) Get(ctx context.Context, articleID string) (*apinews.ArticleWithTranslations, error) {
	article, err := s.querier.GetArticleByID(ctx, articleID)
	if err != nil {
		return nil, err
	}
	translations, err := s.querier.ListTranslationsByArticleIDs(ctx, []string{articleID})
	if err != nil {
		return nil, err
	}
	article.Status = apinews.DeriveStatus(*article)
	return &apinews.ArticleWithTranslations{
		Article:      *article,
		Translations: translations,
	}, nil
}

func groupTranslationsByArticleID(translations []apinews.Translation) map[string][]apinews.Translation {
	grouped := make(map[string][]apinews.Translation)
	for _, t := range translations {
		grouped[t.ArticleID] = append(grouped[t.ArticleID], t)
	}
	return grouped
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
// lang の許容値は DB 側の CHECK 制約が SSoT で、未対応値は port.ErrInvalidPersistedValue として bubble する。
// status / reviewer は変更しない (翻訳編集はレビュー判断と区別する)。
func (s *Service) UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
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
