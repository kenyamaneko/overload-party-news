// Package postgres は port で定義されたリポジトリインタフェースの PostgreSQL 実装を提供する。
// pgxpool を共有し、pure data access 層として振る舞う (ビジネスロジックを持たない)。
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-news/internal/port"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// PostgreSQL SQLSTATE. `foreign_key_violation` は親行が無いまま子を INSERT した場合に発生する。
const pgCodeForeignKeyViolation = "23503"

// compile-time assertion: NewsRepository が port の全インタフェースを満たす。
var (
	_ port.PublicNewsQuerier = (*NewsRepository)(nil)
	_ port.AdminNewsQuerier  = (*NewsRepository)(nil)
	_ port.NewsIngester      = (*NewsRepository)(nil)
	_ port.NewsReviewer      = (*NewsRepository)(nil)
)

// NewsRepository は news スキーマ (news_articles + news_article_translations) への CRUD を提供する。
type NewsRepository struct {
	pool *pgxpool.Pool
}

// NewNewsRepository は pgxpool を受け取り NewsRepository を生成する。
func NewNewsRepository(pool *pgxpool.Pool) *NewsRepository {
	return &NewsRepository{pool: pool}
}

// ListPublished は公開可能 (status=published) かつ指定 lang の翻訳がある記事を
// published_at DESC NULLS LAST, article_id DESC で limit 件返す。
func (r *NewsRepository) ListPublished(ctx context.Context, lang string, limit int) ([]apinews.NewsListItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.article_id, a.source, t.title, t.summary, a.tags, a.source_published_at, a.published_at
		   FROM news.news_articles a
		   JOIN news.news_article_translations t
		     ON t.article_id = a.article_id AND t.lang = $1
		  WHERE a.status = 'published'
		  ORDER BY a.published_at DESC NULLS LAST, a.article_id DESC
		  LIMIT $2`,
		lang, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query published list: %w", err)
	}
	defer rows.Close()

	var items []apinews.NewsListItem
	for rows.Next() {
		var item apinews.NewsListItem
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

// GetPublishedByID は公開可能 + 指定 lang の翻訳がある単一記事を返す。非存在/非公開/翻訳なしは ErrNotFound。
func (r *NewsRepository) GetPublishedByID(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT a.article_id, a.source, t.title, t.summary, t.body, a.tags,
		        a.source_url, a.source_published_at, a.published_at
		   FROM news.news_articles a
		   JOIN news.news_article_translations t
		     ON t.article_id = a.article_id AND t.lang = $2
		  WHERE a.article_id = $1 AND a.status = 'published'`,
		articleID, lang,
	)
	var d apinews.NewsDetail
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

// ListByStatus は status フィルタ付きで記事 + 各記事の全翻訳を ingested_at DESC で limit 件返す。
// statusFilter が nil のとき全件。
// 翻訳は別クエリで一括取得する (1+1 クエリ、N+1 ではない)。
func (r *NewsRepository) ListByStatus(ctx context.Context, statusFilter *apinews.Status, limit int) ([]apinews.ArticleWithTranslations, error) {
	const selectArticles = `SELECT article_id, source, source_url, tags, status,
		source_published_at, published_at, ingested_at, reviewed_at, reviewer, updated_at
		   FROM news.news_articles`

	var (
		rows pgx.Rows
		err  error
	)
	if statusFilter == nil {
		rows, err = r.pool.Query(ctx,
			selectArticles+` ORDER BY ingested_at DESC LIMIT $1`, limit)
	} else {
		rows, err = r.pool.Query(ctx,
			selectArticles+` WHERE status = $1 ORDER BY ingested_at DESC LIMIT $2`,
			string(*statusFilter), limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query articles by status: %w", err)
	}
	defer rows.Close()

	var articles []apinews.Article
	articleIDs := make([]string, 0)
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan article row: %w", err)
		}
		articles = append(articles, *a)
		articleIDs = append(articleIDs, a.ArticleID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles: %w", err)
	}

	translationsByArticle, err := r.fetchTranslationsForIDs(ctx, articleIDs)
	if err != nil {
		return nil, err
	}

	results := make([]apinews.ArticleWithTranslations, 0, len(articles))
	for _, a := range articles {
		results = append(results, apinews.ArticleWithTranslations{
			Article:      a,
			Translations: translationsByArticle[a.ArticleID],
		})
	}
	return results, nil
}

// GetByID は status を問わず記事 + 全翻訳を返す。非存在なら ErrNotFound。
func (r *NewsRepository) GetByID(ctx context.Context, articleID string) (*apinews.ArticleWithTranslations, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT article_id, source, source_url, tags, status,
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

	translationsByArticle, err := r.fetchTranslationsForIDs(ctx, []string{articleID})
	if err != nil {
		return nil, err
	}
	return &apinews.ArticleWithTranslations{
		Article:      *article,
		Translations: translationsByArticle[articleID],
	}, nil
}

// InsertArticle は記事行を挿入する。既存なら inserted=false で no-op。
func (r *NewsRepository) InsertArticle(ctx context.Context, article apinews.Article) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO news.news_articles
			(article_id, source, source_url, tags, status, source_published_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT DO NOTHING`,
		article.ArticleID, article.Source, article.SourceURL, article.Tags,
		string(apinews.StatusPending), article.SourcePublishedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert article: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// InsertTranslation はインジェスト経路の翻訳挿入。既存翻訳は上書きしない (DO NOTHING)。
// 親記事が存在しない状態で呼ぶと FK 違反でエラーになる (呼び出し側で順序保証)。
func (r *NewsRepository) InsertTranslation(ctx context.Context, articleID string, lang, title, summary, body string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO news.news_article_translations
			(article_id, lang, title, summary, body)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (article_id, lang) DO NOTHING`,
		articleID, lang, title, summary, body,
	)
	if err != nil {
		return fmt.Errorf("insert translation (lang=%s): %w", lang, err)
	}
	return nil
}

// Publish は承認遷移。非存在なら ErrNotFound。
func (r *NewsRepository) Publish(ctx context.Context, articleID string, reviewer string, now time.Time) error {
	return r.updateReviewed(ctx, articleID,
		`UPDATE news.news_articles
		    SET status = 'published',
		        published_at = $2,
		        reviewed_at = $2,
		        reviewer = $3
		  WHERE article_id = $1`,
		articleID, now, reviewer)
}

// Reject は却下遷移。published_at は変更しない (再承認時の履歴参照用)。
func (r *NewsRepository) Reject(ctx context.Context, articleID string, reviewer string, now time.Time) error {
	return r.updateReviewed(ctx, articleID,
		`UPDATE news.news_articles
		    SET status = 'rejected',
		        reviewed_at = $2,
		        reviewer = $3
		  WHERE article_id = $1`,
		articleID, now, reviewer)
}

// UpsertTranslation は翻訳行を追加 / 更新する。親記事が存在しない場合は FK 違反で ErrNotFound を返す。
// news_articles には一切触れない (翻訳編集はレビュー判断と区別するため)。
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
		if errors.As(err, &pgErr) && pgErr.Code == pgCodeForeignKeyViolation {
			return fmt.Errorf("article %s: %w", articleID, port.ErrNotFound)
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

// fetchTranslationsForIDs は指定 article_id 集合の翻訳を 1 クエリで取得し、
// article_id キーでグループ化して返す (N+1 回避)。
func (r *NewsRepository) fetchTranslationsForIDs(ctx context.Context, ids []string) (map[string][]apinews.Translation, error) {
	if len(ids) == 0 {
		return map[string][]apinews.Translation{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT article_id, lang, title, summary, body, created_at, updated_at
		   FROM news.news_article_translations
		  WHERE article_id = ANY($1)
		  ORDER BY article_id, lang`,
		ids,
	)
	if err != nil {
		return nil, fmt.Errorf("query translations: %w", err)
	}
	defer rows.Close()

	grouped := make(map[string][]apinews.Translation)
	for rows.Next() {
		var t apinews.Translation
		if err := rows.Scan(
			&t.ArticleID, &t.Lang, &t.Title, &t.Summary, &t.Body, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan translation: %w", err)
		}
		grouped[t.ArticleID] = append(grouped[t.ArticleID], t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate translations: %w", err)
	}
	return grouped, nil
}

// scanArticle は Article 全カラム分の Scan を共通化する。
func scanArticle(row pgx.Row) (*apinews.Article, error) {
	var a apinews.Article
	var status string
	if err := row.Scan(
		&a.ArticleID, &a.Source, &a.SourceURL, &a.Tags,
		&status,
		&a.SourcePublishedAt, &a.PublishedAt, &a.IngestedAt,
		&a.ReviewedAt, &a.Reviewer, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	a.Status = apinews.Status(status)
	return &a, nil
}
