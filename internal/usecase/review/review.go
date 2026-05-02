// Package review は管理 UI の校閲ユースケース (承認・却下・翻訳 upsert) と閲覧用 read 操作を実装する。
// 公開 API (usecase/news) とは独立した usecase で、依存する port も AdminNewsQuerier / NewsReviewWriter に限定する。
package review

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// FEATURE_SPEC.md で定める翻訳バリデーション。
const (
	TitleMaxLen   = 500
	SummaryMaxLen = 2000
)

// AdminListLimitMax は管理 UI 一覧 limit の上限 (FEATURE_SPEC.md)。
// 過大要求による I/O 圧迫を防ぐ安全弁。下限はゼロ以下を弾くだけで十分なため定数化していない。
const AdminListLimitMax = 200

// Interactor は校閲・管理閲覧の use case 層。
type Interactor struct {
	querier port.AdminNewsQuerier
	writer  port.NewsReviewWriter
	now     func() time.Time
}

// New は Interactor を生成する。now は time.Now を注入することでテスト容易性を保つ。
func New(querier port.AdminNewsQuerier, writer port.NewsReviewWriter, now func() time.Time) *Interactor {
	return &Interactor{querier: querier, writer: writer, now: now}
}

// List は管理 UI 向けに status 集合フィルタ可能な記事一覧 (各記事の全翻訳を含む) を返す。
// 「全件」を意図する場合は呼び出し側が domain.Statuses を渡す契約。
//
// status 導出は usecase 層の責務 (repo は status 概念を持たない)。
// 最新 limit 件を取ってから status で絞るため、結果は「最新 limit 件のうち指定 status のもの」になる。
func (uc *Interactor) List(ctx context.Context, statuses []domain.Status, limit int) ([]domain.ArticleWithTranslations, error) {
	if limit <= 0 || limit > AdminListLimitMax {
		return nil, fmt.Errorf("%w: limit=%d must be in (0, %d]",
			ErrInvalidField, limit, AdminListLimitMax)
	}
	articles, err := uc.querier.ListArticles(ctx, limit)
	if err != nil {
		return nil, err
	}

	wanted := make(map[domain.Status]struct{}, len(statuses))
	for _, st := range statuses {
		wanted[st] = struct{}{}
	}
	filtered := make([]domain.Article, 0, len(articles))
	for _, a := range articles {
		a.Status = domain.DeriveStatus(a)
		if _, ok := wanted[a.Status]; ok {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return []domain.ArticleWithTranslations{}, nil
	}

	ids := make([]string, len(filtered))
	for i, a := range filtered {
		ids[i] = a.ArticleID
	}
	translations, err := uc.querier.ListTranslationsByArticleIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	grouped := translationsByArticleID(translations)
	results := make([]domain.ArticleWithTranslations, len(filtered))
	for i, a := range filtered {
		results[i] = domain.ArticleWithTranslations{
			Article:      a,
			Translations: grouped[a.ArticleID],
		}
	}
	return results, nil
}

// Get は管理 UI 向けに任意 status の記事 + 全翻訳を返す。非存在は port.ErrNotFound が bubble。
func (uc *Interactor) Get(ctx context.Context, articleID string) (*domain.ArticleWithTranslations, error) {
	article, err := uc.querier.GetArticleByID(ctx, articleID)
	if err != nil {
		return nil, err
	}
	translations, err := uc.querier.ListTranslationsByArticleIDs(ctx, []string{articleID})
	if err != nil {
		return nil, err
	}
	article.Status = domain.DeriveStatus(*article)
	return &domain.ArticleWithTranslations{
		Article:      *article,
		Translations: translations,
	}, nil
}

func translationsByArticleID(translations []domain.Translation) map[string][]domain.Translation {
	grouped := make(map[string][]domain.Translation)
	for _, t := range translations {
		grouped[t.ArticleID] = append(grouped[t.ArticleID], t)
	}
	return grouped
}

// Publish は承認 (FEATURE_SPEC)。
// 既に published でも冪等 (published_at は毎回更新され、一覧で最新扱いになる)。
func (uc *Interactor) Publish(ctx context.Context, articleID string, reviewer string) error {
	if reviewer == "" {
		return fmt.Errorf("%w: reviewer is required", ErrInvalidField)
	}
	return uc.writer.Publish(ctx, articleID, reviewer, uc.now())
}

// Reject は却下 (FEATURE_SPEC)。
func (uc *Interactor) Reject(ctx context.Context, articleID string, reviewer string) error {
	if reviewer == "" {
		return fmt.Errorf("%w: reviewer is required", ErrInvalidField)
	}
	return uc.writer.Reject(ctx, articleID, reviewer, uc.now())
}

// UpsertTranslation は指定言語の翻訳を追加 / 更新する。
// 長さバリデーション (FEATURE_SPEC) をここで強制する。
// lang の許容値は DB 側の CHECK 制約が SSoT で、未対応値は port.ErrInvalidPersistedValue として bubble する。
// status / reviewer は変更しない (翻訳編集はレビュー判断と区別する)。
func (uc *Interactor) UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	if err := validateTitle(title); err != nil {
		return err
	}
	if err := validateSummary(summary); err != nil {
		return err
	}
	if err := validateBody(body); err != nil {
		return err
	}
	return uc.writer.UpsertTranslation(ctx, articleID, lang, title, summary, body)
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
