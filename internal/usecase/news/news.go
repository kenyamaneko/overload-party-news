// Package news は公開 API (gateway → news) のユースケースを実装する。
// 指定言語で公開可能な記事 (status = 'published' かつ翻訳あり) の一覧・詳細を返す read-only サービス。
package news

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// ListLimitMax は一覧 limit の上限 (FEATURE_SPEC.md)。
// 過大要求による I/O 圧迫を防ぐ安全弁。下限はゼロ以下を弾くだけで十分なため定数化していない。
const ListLimitMax = 100

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
// repo はドメイン DTO を返すため、ここで API 契約 (apinews.NewsListItem) に射影する。
func (s *Service) List(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error) {
	if err := validateLang(lang); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > ListLimitMax {
		return nil, fmt.Errorf("%w: limit=%d must be in (0, %d]",
			ErrInvalidLimit, limit, ListLimitMax)
	}
	rows, err := s.querier.ListPublished(ctx, lang, limit)
	if err != nil {
		return nil, err
	}
	items := make([]apinews.NewsListItem, len(rows))
	for i, r := range rows {
		items[i] = apinews.NewsListItem{
			ArticleID:         r.ArticleID,
			Source:            r.Source,
			Title:             r.Title,
			Summary:           r.Summary,
			Tags:              r.Tags,
			SourcePublishedAt: r.SourcePublishedAt,
			PublishedAt:       r.PublishedAt,
		}
	}
	return items, nil
}

// GetDetail は指定言語で公開中記事の詳細を返す。
// 非存在 / 非公開 / 該当 lang 翻訳なしは port.ErrNotFound が bubble する。
// 取得した domain DTO は API 契約 (apinews.NewsDetail) に射影してから返す。
func (s *Service) GetDetail(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error) {
	if err := validateLang(lang); err != nil {
		return nil, err
	}
	d, err := s.querier.GetPublishedByID(ctx, articleID, lang)
	if err != nil {
		return nil, err
	}
	return &apinews.NewsDetail{
		ArticleID:         d.ArticleID,
		Source:            d.Source,
		Title:             d.Title,
		Summary:           d.Summary,
		Body:              d.Body,
		Tags:              d.Tags,
		SourceURL:         d.SourceURL,
		SourcePublishedAt: d.SourcePublishedAt,
		PublishedAt:       d.PublishedAt,
	}, nil
}

// validateLang は lang を repo に渡す前に弾くことで「不正入力 (400)」と「該当データなし (404)」を区別するためにある。
// lang は repo の SQL の JOIN 条件 (t.lang = $1) にそのまま渡るため、未対応値が来てもクエリ自体は成功し
// 「結果が空」という形になる。それを repo まで通すと ErrNotFound と区別できなくなるため、
// service 層で先に専用エラーを返す。
// 未指定と対応外でエラーを分けてあるのは、handler / Gateway 側でメッセージやログを書き分けられる粒度を残すため。
func validateLang(lang string) error {
	if lang == "" {
		return ErrLangRequired
	}
	if !slices.Contains(domain.SupportedLangs, lang) {
		return fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	return nil
}
