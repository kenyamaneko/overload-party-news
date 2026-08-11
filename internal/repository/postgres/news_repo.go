// Package postgres は port で定義されたリポジトリインタフェースの PostgreSQL 実装。
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// PostgreSQL SQLSTATE。
const (
	pgCodeCheckViolation = "23514"
)

var (
	_ port.PublicNewsQuerier = (*NewsRepository)(nil)
	_ port.NewsIngestWriter  = (*NewsRepository)(nil)
)

// NewsRepository は news スキーマ (news_articles + news_article_translations) への CRUD を提供する。
type NewsRepository struct {
	pool *pgxpool.Pool
}

// NewNewsRepository は pgxpool を受け取り NewsRepository を生成する。
func NewNewsRepository(pool *pgxpool.Pool) *NewsRepository {
	return &NewsRepository{pool: pool}
}

// InsertArticleWithTranslation は記事行と翻訳行を 1 トランザクションで挿入する。
// 記事が既存 (同一 article_id、または同一 source_url の別 article_id) なら inserted=false で何も挿入しない。
// CHECK 制約違反 (未対応 lang) は ErrInvalidPersistedValue に変換し、記事側も挿入しない。
func (r *NewsRepository) InsertArticleWithTranslation(ctx context.Context, article domain.Article, lang, title, summary, body string) (bool, error) {
	// 記事行と翻訳行の間に中間状態を作らないため、1 トランザクションで書く。
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin ingest transaction: %w", err)
	}
	// Commit 済みなら Rollback は no-op になるため、失敗時の解放だけを目的に defer する
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`INSERT INTO news.news_articles
			(article_id, source, source_url, tags, source_published_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT DO NOTHING`,
		article.ArticleID, article.Source, article.SourceURL, article.Tags,
		article.SourcePublishedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert article: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit ingest transaction: %w", err)
		}
		return false, nil
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO news.news_article_translations
			(article_id, lang, title, summary, body)
		 VALUES ($1, $2, $3, $4, $5)`,
		article.ArticleID, lang, title, summary, body,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgCodeCheckViolation {
			return false, fmt.Errorf("lang=%q: %w", lang, port.ErrInvalidPersistedValue)
		}
		return false, fmt.Errorf("insert translation (lang=%s): %w", lang, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit ingest transaction: %w", err)
	}
	return true, nil
}

// ListPublished は公開可能かつ指定 lang の翻訳がある記事を published_at DESC, article_id DESC で limit 件返す。
func (r *NewsRepository) ListPublished(ctx context.Context, lang string, limit int) ([]domain.PublishedArticleSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.article_id, a.source, t.title, t.summary, a.tags, a.source_published_at, a.published_at
		   FROM news.news_articles a
		   JOIN news.news_article_translations t
		     ON t.article_id = a.article_id AND t.lang = $1
		  WHERE a.published_at IS NOT NULL AND a.published_at >= a.reviewed_at
		  ORDER BY a.published_at DESC NULLS LAST, a.article_id DESC
		  LIMIT $2`,
		lang, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query published list: %w", err)
	}
	defer rows.Close()

	var items []domain.PublishedArticleSummary
	for rows.Next() {
		var item domain.PublishedArticleSummary
		if err := rows.Scan(
			&item.ArticleID, &item.Source, &item.Title, &item.Summary, &item.Tags,
			&item.SourcePublishedAt, &item.PublishedAt,
		); err != nil {
			return nil, fmt.Errorf("scan published list row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published list: %w", err)
	}
	return items, nil
}

// GetPublishedByID は公開可能 + 指定 lang の翻訳がある単一記事の詳細 (body / source_url 含む) を返す。
// 非存在 / 非公開 / 翻訳なしは ErrNotFound。
func (r *NewsRepository) GetPublishedByID(ctx context.Context, articleID string, lang string) (*domain.PublishedArticleDetail, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT a.article_id, a.source, t.title, t.summary, t.body, a.tags,
		        a.source_url, a.source_published_at, a.published_at
		   FROM news.news_articles a
		   JOIN news.news_article_translations t
		     ON t.article_id = a.article_id AND t.lang = $2
		  WHERE a.article_id = $1
		    AND a.published_at IS NOT NULL AND a.published_at >= a.reviewed_at`,
		articleID, lang,
	)
	var d domain.PublishedArticleDetail
	if err := row.Scan(
		&d.ArticleID, &d.Source, &d.Title, &d.Summary, &d.Body, &d.Tags,
		&d.SourceURL, &d.SourcePublishedAt, &d.PublishedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("published article %s (lang=%s): %w", articleID, lang, port.ErrNotFound)
		}
		return nil, fmt.Errorf("query published detail: %w", err)
	}
	return &d, nil
}
