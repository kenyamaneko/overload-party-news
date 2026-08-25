//go:build integration

package router_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
	"github.com/kenyamaneko/overload-party-news/internal/router"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

var pg *postgrestest.Postgres

func TestMain(m *testing.M) {
	os.Exit(postgrestest.RunMain(m, &pg))
}

func newPushRouterEngine(repo *postgres.NewsRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	newsH := rest.NewNewsHandler(news.New(repo))
	subH := subscriber.NewArticleCollectedHandler(ingest.New(repo))
	pushH := pubsubpush.NewHandler(subH.Handle)
	verifier := &internalauth.MockVerifier{}
	return router.NewPublic(newsH, verifier, pushH)
}

func encodeEventBase64(t *testing.T, event apinews.ArticleCollectedEvent) string {
	t.Helper()
	data, err := json.Marshal(event)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(data)
}

func pushEnvelopeBody(dataB64 string) string {
	return `{"message":{"data":"` + dataB64 + `"}}`
}

func doPush(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/pubsub/news-article-collected", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func pushEvent(t *testing.T, r *gin.Engine, event apinews.ArticleCollectedEvent) *httptest.ResponseRecorder {
	t.Helper()
	return doPush(t, r, pushEnvelopeBody(encodeEventBase64(t, event)))
}

type persistedArticle struct {
	ArticleID         string
	Source            string
	SourceURL         string
	Tags              []string
	SourcePublishedAt *time.Time
	ReviewedAt        *time.Time
	PublishedAt       *time.Time
}

func queryArticle(t *testing.T, articleID string) (persistedArticle, bool) {
	t.Helper()
	var a persistedArticle
	err := pg.Pool.QueryRow(context.Background(), `
		SELECT article_id, source, source_url, tags, source_published_at, reviewed_at, published_at
		  FROM news.news_articles WHERE article_id = $1
	`, articleID).Scan(&a.ArticleID, &a.Source, &a.SourceURL, &a.Tags, &a.SourcePublishedAt, &a.ReviewedAt, &a.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedArticle{}, false
	}
	require.NoError(t, err)
	return a, true
}

type persistedTranslation struct {
	Title     string
	Summary   string
	Body      string
	CreatedAt time.Time
}

func queryTranslation(t *testing.T, articleID, lang string) (persistedTranslation, bool) {
	t.Helper()
	var tr persistedTranslation
	err := pg.Pool.QueryRow(context.Background(), `
		SELECT title, summary, body, created_at
		  FROM news.news_article_translations WHERE article_id = $1 AND lang = $2
	`, articleID, lang).Scan(&tr.Title, &tr.Summary, &tr.Body, &tr.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedTranslation{}, false
	}
	require.NoError(t, err)
	return tr, true
}

func countTranslationsFor(t *testing.T, articleID string) int {
	t.Helper()
	var count int
	err := pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM news.news_article_translations WHERE article_id = $1`, articleID).Scan(&count)
	require.NoError(t, err)
	return count
}

func countArticlesBySourceURL(t *testing.T, sourceURL string) int {
	t.Helper()
	var count int
	err := pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM news.news_articles WHERE source_url = $1`, sourceURL).Scan(&count)
	require.NoError(t, err)
	return count
}

func countAllArticles(t *testing.T) int {
	t.Helper()
	var count int
	err := pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM news.news_articles`).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestPushArticleCollectedPersistence(t *testing.T) {
	t.Run("取り込みパイプライン全体の永続化", func(t *testing.T) {
		t.Run("有効なイベントをpushすると、200を返し、記事とja翻訳が全フィールドを保った状態で永続化され、校閲前の状態になる", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			sourcePublishedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			event := apinews.ArticleCollectedEvent{
				ArticleID:         "push-full-fields-00001",
				Source:            "aws",
				SourceURL:         "https://example.com/push/full-fields",
				Tags:              []string{"tag-a", "tag-b"},
				SourcePublishedAt: &sourcePublishedAt,
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "full title", Summary: "full summary", Body: "full body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			article, found := queryArticle(t, event.ArticleID)
			require.True(t, found)
			assert.Equal(t, event.Source, article.Source)
			assert.Equal(t, event.SourceURL, article.SourceURL)
			assert.ElementsMatch(t, event.Tags, article.Tags)
			require.NotNil(t, article.SourcePublishedAt)
			assert.True(t, sourcePublishedAt.Equal(*article.SourcePublishedAt))
			assert.Nil(t, article.ReviewedAt)
			assert.Nil(t, article.PublishedAt)

			translation, found := queryTranslation(t, event.ArticleID, "ja")
			require.True(t, found)
			assert.Equal(t, "full title", translation.Title)
			assert.Equal(t, "full summary", translation.Summary)
			assert.Equal(t, "full body", translation.Body)
		})

		t.Run("同一のイベントを2回pushしても、2回とも200を返し、翻訳は1件のまま増えず、既存翻訳の作成日時も変わらない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			event := apinews.ArticleCollectedEvent{
				ArticleID: "push-idempotent-00001",
				Source:    "aws",
				SourceURL: "https://example.com/push/idempotent",
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "idempotent title", Summary: "idempotent summary", Body: "idempotent body"},
				},
			}

			w1 := pushEvent(t, engine, event)
			require.Equal(t, http.StatusOK, w1.Code)
			first, found := queryTranslation(t, event.ArticleID, "ja")
			require.True(t, found)

			w2 := pushEvent(t, engine, event)
			require.Equal(t, http.StatusOK, w2.Code)

			assert.Equal(t, 1, countTranslationsFor(t, event.ArticleID))
			second, found := queryTranslation(t, event.ArticleID, "ja")
			require.True(t, found)
			assert.True(t, first.CreatedAt.Equal(second.CreatedAt))
		})

		t.Run("既存記事と同じsource_urlを別のarticle_idでpushすると、200を返すが、記事も翻訳も増えない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			sourceURL := "https://example.com/push/retake"
			first := apinews.ArticleCollectedEvent{
				ArticleID: "push-retake-first-0001",
				Source:    "aws",
				SourceURL: sourceURL,
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "first title", Summary: "first summary", Body: "first body"},
				},
			}
			second := apinews.ArticleCollectedEvent{
				ArticleID: "push-retake-second-001",
				Source:    "aws",
				SourceURL: sourceURL,
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "second title", Summary: "second summary", Body: "second body"},
				},
			}

			w1 := pushEvent(t, engine, first)
			require.Equal(t, http.StatusOK, w1.Code)

			w2 := pushEvent(t, engine, second)
			require.Equal(t, http.StatusOK, w2.Code)

			assert.Equal(t, 1, countArticlesBySourceURL(t, sourceURL))
			_, foundSecond := queryArticle(t, second.ArticleID)
			assert.False(t, foundSecond)
		})

		t.Run("既存記事と同じsource_urlを別のarticle_idでpushしても、先に取り込まれた記事の翻訳内容は上書きされない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			sourceURL := "https://example.com/push/retake-content"
			first := apinews.ArticleCollectedEvent{
				ArticleID: "push-retake-c-first-01",
				Source:    "aws",
				SourceURL: sourceURL,
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "original title", Summary: "original summary", Body: "original body"},
				},
			}
			second := apinews.ArticleCollectedEvent{
				ArticleID: "push-retake-c-second-1",
				Source:    "aws",
				SourceURL: sourceURL,
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "overwrite title", Summary: "overwrite summary", Body: "overwrite body"},
				},
			}

			require.Equal(t, http.StatusOK, pushEvent(t, engine, first).Code)
			require.Equal(t, http.StatusOK, pushEvent(t, engine, second).Code)

			translation, found := queryTranslation(t, first.ArticleID, "ja")
			require.True(t, found)
			assert.Equal(t, "original title", translation.Title)
			assert.Equal(t, "original summary", translation.Summary)
			assert.Equal(t, "original body", translation.Body)
		})

		t.Run("DBに接続できない状態でpushすると500を返し、DB接続が復旧してから同じイベントをpushし直すと200を返し正しく永続化される", func(t *testing.T) {
			pg.Truncate(t)
			ctx := context.Background()
			dsn, err := pg.Container.ConnectionString(ctx, "sslmode=disable")
			require.NoError(t, err)

			event := apinews.ArticleCollectedEvent{
				ArticleID: "push-disconnect-00001",
				Source:    "aws",
				SourceURL: "https://example.com/push/disconnect",
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "disconnect title", Summary: "disconnect summary", Body: "disconnect body"},
				},
			}

			dedicatedPool, err := pgxpool.New(ctx, dsn)
			require.NoError(t, err)
			dedicatedEngine := newPushRouterEngine(postgres.NewNewsRepository(dedicatedPool))
			dedicatedPool.Close()

			downW := pushEvent(t, dedicatedEngine, event)
			require.Equal(t, http.StatusInternalServerError, downW.Code)

			freshPool, err := pgxpool.New(ctx, dsn)
			require.NoError(t, err)
			t.Cleanup(freshPool.Close)
			freshEngine := newPushRouterEngine(postgres.NewNewsRepository(freshPool))

			recoveredW := pushEvent(t, freshEngine, event)
			require.Equal(t, http.StatusOK, recoveredW.Code)

			_, found := queryArticle(t, event.ArticleID)
			assert.True(t, found)
		})

		t.Run("ja以外の翻訳を含むイベントをpushすると、200を返すがDBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			event := apinews.ArticleCollectedEvent{
				ArticleID: "push-non-ja-lang-00001",
				Source:    "aws",
				SourceURL: "https://example.com/push/non-ja-lang",
				Translations: []apinews.EventTranslation{
					{Lang: "en", Title: "title", Summary: "summary", Body: "body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, event.ArticleID)
			assert.False(t, found)
		})

		t.Run("壊れたJSONをイベント本文としてpushすると、200を返すがDBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			brokenData := base64.StdEncoding.EncodeToString([]byte("not a valid json payload"))

			w := doPush(t, engine, pushEnvelopeBody(brokenData))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, 0, countAllArticles(t))
		})

		t.Run("message.dataがbase64として不正なpushをすると、400とundecodable dataを含む応答を返し、DBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))

			w := doPush(t, engine, pushEnvelopeBody("not-valid-base64!!"))

			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "undecodable data")
			assert.Equal(t, 0, countAllArticles(t))
		})

		t.Run("push envelopeの形式でない本文を投げると、400とmalformed envelopeを含む応答を返し、DBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))

			w := doPush(t, engine, "not valid json at all")

			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "malformed envelope")
			assert.Equal(t, 0, countAllArticles(t))
		})

		t.Run("messageフィールドが無い本文を投げると、400とmalformed envelopeを含む応答を返し、DBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))

			w := doPush(t, engine, `{}`)

			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "malformed envelope")
			assert.Equal(t, 0, countAllArticles(t))
		})

		t.Run("message.dataが空文字の本文を投げると、400とmalformed envelopeを含む応答を返し、DBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))

			w := doPush(t, engine, pushEnvelopeBody(""))

			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "malformed envelope")
			assert.Equal(t, 0, countAllArticles(t))
		})

		t.Run("article_idがちょうど26文字のとき、200を返し記事が永続化される", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			articleID := strings.Repeat("a", 26)
			event := apinews.ArticleCollectedEvent{
				ArticleID: articleID,
				Source:    "aws",
				SourceURL: "https://example.com/push/article-id-26",
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, articleID)
			assert.True(t, found)
		})

		t.Run("article_idが全角文字26文字のとき、200を返し記事が永続化される", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			articleID := strings.Repeat("あ", 26)
			event := apinews.ArticleCollectedEvent{
				ArticleID: articleID,
				Source:    "aws",
				SourceURL: "https://example.com/push/article-id-fullwidth",
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, articleID)
			assert.True(t, found)
		})

		t.Run("sourceがちょうど20文字のとき、200を返し記事が永続化される", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			event := apinews.ArticleCollectedEvent{
				ArticleID: "push-source-20-chars-01",
				Source:    strings.Repeat("s", 20),
				SourceURL: "https://example.com/push/source-20",
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, event.ArticleID)
			assert.True(t, found)
		})

		t.Run("article_idが27文字(上限超過)のとき、200を返すがDBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			articleID := strings.Repeat("a", 27)
			event := apinews.ArticleCollectedEvent{
				ArticleID: articleID,
				Source:    "aws",
				SourceURL: "https://example.com/push/article-id-27",
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, articleID)
			assert.False(t, found)
		})

		t.Run("sourceが21文字(上限超過)のとき、200を返すがDBに永続化されない", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			event := apinews.ArticleCollectedEvent{
				ArticleID: "push-source-21-chars-01",
				Source:    strings.Repeat("s", 21),
				SourceURL: "https://example.com/push/source-21",
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}

			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, event.ArticleID)
			assert.False(t, found)
		})

		t.Run("article_idの上限超過で記事が捨てられた後、26文字に直して同じ記事をpushすると、200を返し記事が永続化される", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))
			sourceURL := "https://example.com/push/article-id-retry"
			oversizedID := strings.Repeat("b", 27)
			fixedID := strings.Repeat("b", 26)

			oversized := apinews.ArticleCollectedEvent{
				ArticleID: oversizedID,
				Source:    "aws",
				SourceURL: sourceURL,
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}
			require.Equal(t, http.StatusOK, pushEvent(t, engine, oversized).Code)
			_, foundOversized := queryArticle(t, oversizedID)
			require.False(t, foundOversized)

			fixed := oversized
			fixed.ArticleID = fixedID
			w := pushEvent(t, engine, fixed)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, fixedID)
			assert.True(t, found)
		})

		t.Run("envelope不正で400になった後、同じ記事の有効なpushをすると、200を返し記事が永続化される", func(t *testing.T) {
			pg.Truncate(t)
			engine := newPushRouterEngine(postgres.NewNewsRepository(pg.Pool))

			badW := doPush(t, engine, "not valid json at all")
			require.Equal(t, http.StatusBadRequest, badW.Code)

			event := apinews.ArticleCollectedEvent{
				ArticleID: "push-after-bad-envelope",
				Source:    "aws",
				SourceURL: "https://example.com/push/after-bad-envelope",
				Tags:      []string{},
				Translations: []apinews.EventTranslation{
					{Lang: "ja", Title: "title", Summary: "summary", Body: "body"},
				},
			}
			w := pushEvent(t, engine, event)

			require.Equal(t, http.StatusOK, w.Code)
			_, found := queryArticle(t, event.ArticleID)
			assert.True(t, found)
		})
	})
}
