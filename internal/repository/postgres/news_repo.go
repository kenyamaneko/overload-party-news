// Package postgres は port で定義されたリポジトリインタフェースの PostgreSQL 実装。
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// PostgreSQL SQLSTATE。
const (
	pgCodeForeignKeyViolation = "23503"
	pgCodeCheckViolation      = "23514"
)

var (
	_ port.PublicNewsQuerier = (*NewsRepository)(nil)
	_ port.AdminNewsQuerier  = (*NewsRepository)(nil)
	_ port.NewsIngestWriter  = (*NewsRepository)(nil)
	_ port.NewsReviewWriter  = (*NewsRepository)(nil)
)

// NewsRepository は news スキーマ (news_articles + news_article_translations) への CRUD を提供する。
type NewsRepository struct {
	pool *pgxpool.Pool
}

// NewNewsRepository は pgxpool を受け取り NewsRepository を生成する。
func NewNewsRepository(pool *pgxpool.Pool) *NewsRepository {
	return &NewsRepository{pool: pool}
}

// InsertArticle は記事行を挿入する。既存なら inserted=false で no-op。
func (r *NewsRepository) InsertArticle(ctx context.Context, article domain.Article) (bool, error) {
	tag, err := r.pool.Exec(ctx,
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
	return tag.RowsAffected() == 1, nil
}

// InsertTranslation はインジェスト経路の翻訳挿入。既存翻訳は上書きしない (DO NOTHING)。
// 親記事不在の状態で呼ぶと FK 違反でエラー (呼び出し側で順序保証)。
// CHECK 制約違反 (未対応 lang) は ErrInvalidPersistedValue に変換する。
func (r *NewsRepository) InsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO news.news_article_translations
			(article_id, lang, title, summary, body)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (article_id, lang) DO NOTHING`,
		articleID, lang, title, summary, body,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgCodeCheckViolation {
			return fmt.Errorf("lang=%q: %w", lang, port.ErrInvalidPersistedValue)
		}
		return fmt.Errorf("insert translation (lang=%s): %w", lang, err)
	}
	return nil
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

// ListArticles は記事を ingested_at DESC で limit 件返す。フィルタは行わない。
func (r *NewsRepository) ListArticles(ctx context.Context, limit int) ([]domain.Article, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT article_id, source, source_url, tags,
		        source_published_at, published_at, ingested_at, reviewed_at, reviewer, updated_at
		   FROM news.news_articles
		  ORDER BY ingested_at DESC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query articles: %w", err)
	}
	defer rows.Close()

	var articles []domain.Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan article row: %w", err)
		}
		articles = append(articles, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles: %w", err)
	}
	return articles, nil
}

// GetArticleByID は status を問わず記事を返す。非存在なら ErrNotFound。
func (r *NewsRepository) GetArticleByID(ctx context.Context, articleID string) (*domain.Article, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT article_id, source, source_url, tags,
		        source_published_at, published_at, ingested_at, reviewed_at, reviewer, updated_at
		   FROM news.news_articles
		  WHERE article_id = $1`,
		articleID,
	)
	article, err := scanArticle(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("article %s: %w", articleID, port.ErrNotFound)
		}
		return nil, fmt.Errorf("query article: %w", err)
	}
	return article, nil
}

// ListTranslationsByArticleIDs は指定 article_id 群の翻訳行を 1 クエリで返す。グループ化は呼び出し側で行う。
func (r *NewsRepository) ListTranslationsByArticleIDs(ctx context.Context, articleIDs []string) ([]domain.Translation, error) {
	if len(articleIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT article_id, lang, title, summary, body, created_at, updated_at
		   FROM news.news_article_translations
		  WHERE article_id = ANY($1)
		  ORDER BY article_id, lang`,
		articleIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("query translations: %w", err)
	}
	defer rows.Close()

	var translations []domain.Translation
	for rows.Next() {
		var t domain.Translation
		if err := rows.Scan(
			&t.ArticleID, &t.Lang, &t.Title, &t.Summary, &t.Body, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan translation: %w", err)
		}
		translations = append(translations, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate translations: %w", err)
	}
	return translations, nil
}

// Publish は承認遷移 (published_at = reviewed_at = now、reviewer 更新)。非存在なら ErrNotFound。
func (r *NewsRepository) Publish(ctx context.Context, articleID string, reviewer string, now time.Time) error {
	return r.updateReviewed(ctx, articleID,
		`UPDATE news.news_articles
		    SET published_at = $2,
		        reviewed_at = $2,
		        reviewer = $3
		  WHERE article_id = $1`,
		articleID, now, reviewer)
}

// Reject は却下遷移 (reviewed_at と reviewer のみ更新、published_at は保持)。非存在なら ErrNotFound。
func (r *NewsRepository) Reject(ctx context.Context, articleID string, reviewer string, now time.Time) error {
	return r.updateReviewed(ctx, articleID,
		`UPDATE news.news_articles
		    SET reviewed_at = $2,
		        reviewer = $3
		  WHERE article_id = $1`,
		articleID, now, reviewer)
}

// UpsertTranslation は翻訳行を追加 / 更新する。news_articles には触れない。
// 親記事不在は FK 違反 → ErrNotFound、CHECK 違反 → ErrInvalidPersistedValue に変換。
func (r *NewsRepository) UpsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO news.news_article_translations
			(article_id, lang, title, summary, body)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (article_id, lang) DO UPDATE
		    SET title = EXCLUDED.title,
		        summary = EXCLUDED.summary,
		        body = EXCLUDED.body`,
		articleID, lang, title, summary, body,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case pgCodeForeignKeyViolation:
				return fmt.Errorf("article %s: %w", articleID, port.ErrNotFound)
			case pgCodeCheckViolation:
				return fmt.Errorf("lang=%q: %w", lang, port.ErrInvalidPersistedValue)
			}
		}
		return fmt.Errorf("upsert translation: %w", err)
	}
	return nil
}

// updateReviewed は Publish / Reject 共通の UPDATE 実行 + RowsAffected チェック。
func (r *NewsRepository) updateReviewed(ctx context.Context, articleID string, sql string, args ...any) error {
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update review state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("article %s: %w", articleID, port.ErrNotFound)
	}
	return nil
}

// scanArticle は Article 全カラム分の Scan を共通化する。
func scanArticle(row pgx.Row) (*domain.Article, error) {
	var a domain.Article
	if err := row.Scan(
		&a.ArticleID, &a.Source, &a.SourceURL, &a.Tags,
		&a.SourcePublishedAt, &a.PublishedAt, &a.IngestedAt,
		&a.ReviewedAt, &a.Reviewer, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}
