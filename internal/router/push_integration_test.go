//go:build integration

package router_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
	"github.com/kenyamaneko/overload-party-news/internal/router"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

const pushPath = "/internal/v1/pubsub/news-article-collected"

var sharedPG *postgrestest.Postgres

// TestMain は Postgres testcontainer を package scope で 1 回だけ起動する。
// テスト間の分離は Postgres.Truncate で担保する。
func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	pg, err := postgrestest.Start(ctx)
	if err != nil {
		log.Fatalf("postgrestest.Start: %v", err)
	}
	defer func() {
		if err := pg.Close(ctx); err != nil {
			log.Printf("postgres close: %v", err)
		}
	}()
	sharedPG = pg

	return m.Run()
}

// newTestRouter は「HTTP push → router → subscriber → ingest → repo → DB」を実 DB で直結した
// 公開 router を構築する。push 受け口は認証を持たないため verifier は /api/v1/news 側の配線確認用。
func newTestRouter(t *testing.T) (*gin.Engine, *postgres.NewsRepository) {
	t.Helper()
	sharedPG.Truncate(t)

	repo := postgres.NewNewsRepository(sharedPG.Pool)
	return buildRouter(t, repo), repo
}

// buildRouter は指定 repo を配線した公開 router を構築する。
func buildRouter(t *testing.T, repo *postgres.NewsRepository) *gin.Engine {
	t.Helper()

	newsH := rest.NewNewsHandler(news.New(repo))
	subscriberH := subscriber.NewArticleCollectedHandler(ingest.New(repo))
	pushH := pubsubpush.NewHandler(subscriberH.Handle)
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	verifier := internalauth.NewVerifier(internalauth.StaticPublicKeyResolver(&signingKey.PublicKey, internalauth.DefaultKeyID))

	return router.NewPublic(newsH, verifier, pushH)
}

// newClosedPool は接続を閉じた pool を返す。DB へ到達できない状態を作るために用いる。
func newClosedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn, err := sharedPG.Container.ConnectionString(context.Background(), "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	pool.Close()
	return pool
}

// doPush は指定 payload を Pub/Sub push envelope に包んで push 受け口へ POST する。
func doPush(t *testing.T, r *gin.Engine, payload []byte) *httptest.ResponseRecorder {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(payload)
	body := `{"message":{"data":"` + encoded + `","messageId":"m1","attributes":{}},"subscription":"projects/p/subscriptions/news-article-collected-news-sub"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(body)))
	return w
}

func validEventPayload(t *testing.T, articleID string) []byte {
	t.Helper()
	return eventPayload(t, articleID, "aws")
}

func eventPayload(t *testing.T, articleID, source string) []byte {
	t.Helper()
	event := collectedEvent(articleID, "https://aws.amazon.com/"+articleID, "タイトル", "要約", "本文")
	event.Source = source
	return marshalEvent(t, event)
}

// collectedEvent は source_url と翻訳文面を article_id と独立に指定した記事収集イベントを組み立てる。
func collectedEvent(articleID, sourceURL, title, summary, body string) apinews.ArticleCollectedEvent {
	pub := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	return apinews.ArticleCollectedEvent{
		ArticleID:         articleID,
		Source:            "aws",
		SourceURL:         sourceURL,
		Tags:              []string{"compute"},
		SourcePublishedAt: &pub,
		Translations: []apinews.EventTranslation{
			{Lang: domain.LangJa, Title: title, Summary: summary, Body: body},
		},
	}
}

func marshalEvent(t *testing.T, event apinews.ArticleCollectedEvent) []byte {
	t.Helper()
	data, err := json.Marshal(event)
	require.NoError(t, err)
	return data
}

// loadArticleWithTranslations は repo の分離された I/O を結合し Status を導出する統合テスト用ヘルパー。
func loadArticleWithTranslations(repo *postgres.NewsRepository, articleID string) (*domain.ArticleWithTranslations, error) {
	ctx := context.Background()
	article, err := repo.GetArticleByID(ctx, articleID)
	if err != nil {
		return nil, err
	}
	translations, err := repo.ListTranslationsByArticleIDs(ctx, []string{articleID})
	if err != nil {
		return nil, err
	}
	article.Status = domain.DeriveStatus(*article)
	return &domain.ArticleWithTranslations{Article: *article, Translations: translations}, nil
}

func TestPushIngestE2E(t *testing.T) {
	t.Run("push 受け口経由の記事取込パイプライン", func(t *testing.T) {
		t.Run("有効な push を投げると、200 を返し記事と ja 翻訳が全フィールドで pending 永続化される", func(t *testing.T) {
			r, repo := newTestRouter(t)
			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"

			w := doPush(t, r, validEventPayload(t, articleID))
			assert.Equal(t, http.StatusOK, w.Code)

			aw, err := loadArticleWithTranslations(repo, articleID)
			require.NoError(t, err)
			assert.Equal(t, articleID, aw.Article.ArticleID)
			assert.Equal(t, domain.StatusPending, aw.Article.Status)
			assert.Equal(t, "aws", aw.Article.Source)
			assert.Equal(t, "https://aws.amazon.com/"+articleID, aw.Article.SourceURL)
			assert.Equal(t, []string{"compute"}, aw.Article.Tags)
			require.NotNil(t, aw.Article.SourcePublishedAt)
			assert.True(t, aw.Article.SourcePublishedAt.Equal(time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)))
			require.Len(t, aw.Translations, 1)
			assert.Equal(t, domain.LangJa, aw.Translations[0].Lang)
			assert.Equal(t, "タイトル", aw.Translations[0].Title)
			assert.Equal(t, "要約", aw.Translations[0].Summary)
			assert.Equal(t, "本文", aw.Translations[0].Body)
		})

		t.Run("同一イベントを push で2回投げても、200を2回返し翻訳は増えず created_at も変わらない", func(t *testing.T) {
			r, repo := newTestRouter(t)
			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FA2"
			payload := validEventPayload(t, articleID)

			w1 := doPush(t, r, payload)
			assert.Equal(t, http.StatusOK, w1.Code)
			aw1, err := loadArticleWithTranslations(repo, articleID)
			require.NoError(t, err)
			require.Len(t, aw1.Translations, 1)

			w2 := doPush(t, r, payload)
			assert.Equal(t, http.StatusOK, w2.Code)

			aw2, err := loadArticleWithTranslations(repo, articleID)
			require.NoError(t, err)
			require.Len(t, aw2.Translations, 1, "重複 push で翻訳は増えない")
			assert.Equal(t, aw1.Translations[0].CreatedAt.UnixNano(), aw2.Translations[0].CreatedAt.UnixNano(),
				"重複 push で既存翻訳の created_at は変わらない")
		})

		t.Run("既存記事と同じ source_url を別の article_id で push すると、200 を返し記事も翻訳も増えない", func(t *testing.T) {
			r, repo := newTestRouter(t)
			ctx := context.Background()
			const sourceURL = "https://aws.amazon.com/blogs/aws/same-url"
			firstID := "01ARZ3NDEKTSV4RRFFQ69G5FB1"
			secondID := "01ARZ3NDEKTSV4RRFFQ69G5FB2"

			w1 := doPush(t, r, marshalEvent(t, collectedEvent(firstID, sourceURL, "最初のタイトル", "最初の要約", "最初の本文")))
			require.Equal(t, http.StatusOK, w1.Code)

			w2 := doPush(t, r, marshalEvent(t, collectedEvent(secondID, sourceURL, "取り直しのタイトル", "取り直しの要約", "取り直しの本文")))
			assert.Equal(t, http.StatusOK, w2.Code)

			articles, err := repo.ListArticles(ctx, 10)
			require.NoError(t, err)
			assert.Len(t, articles, 1)
			_, err = repo.GetArticleByID(ctx, secondID)
			assert.ErrorIs(t, err, port.ErrNotFound)

			translations, err := repo.ListTranslationsByArticleIDs(ctx, []string{firstID, secondID})
			require.NoError(t, err)
			assert.Len(t, translations, 1)
		})

		t.Run("既存記事と同じ source_url を別の article_id で push しても、先に入った記事の翻訳は上書きされない", func(t *testing.T) {
			r, repo := newTestRouter(t)
			const sourceURL = "https://aws.amazon.com/blogs/aws/keep-first"
			firstID := "01ARZ3NDEKTSV4RRFFQ69G5FB3"
			secondID := "01ARZ3NDEKTSV4RRFFQ69G5FB4"

			w1 := doPush(t, r, marshalEvent(t, collectedEvent(firstID, sourceURL, "最初のタイトル", "最初の要約", "最初の本文")))
			require.Equal(t, http.StatusOK, w1.Code)
			aw1, err := loadArticleWithTranslations(repo, firstID)
			require.NoError(t, err)
			require.Len(t, aw1.Translations, 1)

			w2 := doPush(t, r, marshalEvent(t, collectedEvent(secondID, sourceURL, "取り直しのタイトル", "取り直しの要約", "取り直しの本文")))
			require.Equal(t, http.StatusOK, w2.Code)

			aw2, err := loadArticleWithTranslations(repo, firstID)
			require.NoError(t, err)
			require.Len(t, aw2.Translations, 1)
			assert.Equal(t, "最初のタイトル", aw2.Translations[0].Title)
			assert.Equal(t, "最初の要約", aw2.Translations[0].Summary)
			assert.Equal(t, "最初の本文", aw2.Translations[0].Body)
			assert.Equal(t, aw1.Translations[0].CreatedAt.UnixNano(), aw2.Translations[0].CreatedAt.UnixNano())
		})

		t.Run("DB へ接続できない状態で push すると 500 を返し、接続が戻ってから同じ記事を push し直すと 200 を返し永続化される", func(t *testing.T) {
			sharedPG.Truncate(t)
			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FB5"
			payload := validEventPayload(t, articleID)

			brokenRouter := buildRouter(t, postgres.NewNewsRepository(newClosedPool(t)))
			wDown := doPush(t, brokenRouter, payload)
			require.Equal(t, http.StatusInternalServerError, wDown.Code)

			repo := postgres.NewNewsRepository(sharedPG.Pool)
			wUp := doPush(t, buildRouter(t, repo), payload)
			assert.Equal(t, http.StatusOK, wUp.Code)

			aw, err := loadArticleWithTranslations(repo, articleID)
			require.NoError(t, err)
			assert.Equal(t, articleID, aw.Article.ArticleID)
			require.Len(t, aw.Translations, 1)
		})

		t.Run("ja 以外の翻訳を含むイベントを push すると、200 を返し DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)
			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FA3"
			data, err := json.Marshal(apinews.ArticleCollectedEvent{
				ArticleID: articleID,
				Source:    "aws",
				SourceURL: "https://aws.amazon.com/" + articleID,
				Translations: []apinews.EventTranslation{
					{Lang: domain.LangEn, Title: "T", Summary: "S", Body: "B"},
				},
			})
			require.NoError(t, err)

			w := doPush(t, r, data)
			assert.Equal(t, http.StatusOK, w.Code)

			_, err = repo.GetArticleByID(context.Background(), articleID)
			assert.Error(t, err, "invalid payload は DB に永続化されないべき")
		})

		t.Run("壊れた JSON イベントを push すると、200 を返し DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := doPush(t, r, []byte("{not-json"))
			assert.Equal(t, http.StatusOK, w.Code)

			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "壊れた JSON は DB に永続化されないべき")
		})

		t.Run("message.data が base64 として不正な push を投げると、400 を返し応答に復号不能の内容が含まれ DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := httptest.NewRecorder()
			body := `{"message":{"data":"not-valid-base64!!"}}`
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(body)))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "undecodable data")
			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "base64 復号不能な push は DB に永続化されないべき")
		})

		t.Run("push envelope の形式でない本文を投げると、400 を返し応答に envelope 不正の内容が含まれ DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(`{not-json`)))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "malformed envelope")
			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "envelope 不正な push は DB に永続化されないべき")
		})

		t.Run("message フィールドが無い push を投げると、400 を返し応答に envelope 不正の内容が含まれ DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(`{}`)))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "malformed envelope")
			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "message フィールドが無い push は DB に永続化されないべき")
		})

		t.Run("message.data が空文字の push を投げると、400 を返し応答に envelope 不正の内容が含まれ DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(`{"message":{"data":""}}`)))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "malformed envelope")
			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "message.data が空文字の push は DB に永続化されないべき")
		})

		storedCases := []struct {
			name      string
			articleID string
			source    string
		}{
			{
				name:      "article_id が 26 文字のとき、200 を返し記事が永続化される",
				articleID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
				source:    "aws",
			},
			{
				name:      "source が 20 文字のとき、200 を返し記事が永続化される",
				articleID: "01ARZ3NDEKTSV4RRFFQ69G5FA5",
				source:    "12345678901234567890",
			},
			{
				name:      "article_id が全角 26 文字のとき、200 を返し記事が永続化される",
				articleID: strings.Repeat("あ", 26),
				source:    "aws",
			},
		}
		for _, tc := range storedCases {
			t.Run(tc.name, func(t *testing.T) {
				r, repo := newTestRouter(t)

				w := doPush(t, r, eventPayload(t, tc.articleID, tc.source))
				assert.Equal(t, http.StatusOK, w.Code)

				aw, err := loadArticleWithTranslations(repo, tc.articleID)
				require.NoError(t, err)
				assert.Equal(t, tc.articleID, aw.Article.ArticleID)
				assert.Equal(t, tc.source, aw.Article.Source)
				require.Len(t, aw.Translations, 1)
			})
		}

		discardedCases := []struct {
			name      string
			articleID string
			source    string
		}{
			{
				name:      "article_id が 27 文字のとき、200 を返し DB に永続化されない",
				articleID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ7",
				source:    "aws",
			},
			{
				name:      "source が 21 文字のとき、200 を返し DB に永続化されない",
				articleID: "01ARZ3NDEKTSV4RRFFQ69G5FA6",
				source:    "123456789012345678901",
			},
		}
		for _, tc := range discardedCases {
			t.Run(tc.name, func(t *testing.T) {
				r, repo := newTestRouter(t)

				w := doPush(t, r, eventPayload(t, tc.articleID, tc.source))
				assert.Equal(t, http.StatusOK, w.Code)

				items, err := repo.ListArticles(context.Background(), 10)
				require.NoError(t, err)
				assert.Empty(t, items)
			})
		}

		t.Run("article_id が 27 文字の push で記事が捨てられた後、26 文字に直して push すると 200 を返し記事が永続化される", func(t *testing.T) {
			r, repo := newTestRouter(t)

			wTooLong := doPush(t, r, validEventPayload(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ7"))
			require.Equal(t, http.StatusOK, wTooLong.Code)

			wRetry := doPush(t, r, validEventPayload(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
			assert.Equal(t, http.StatusOK, wRetry.Code)

			aw, err := loadArticleWithTranslations(repo, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
			require.NoError(t, err)
			assert.Equal(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ", aw.Article.ArticleID)
			require.Len(t, aw.Translations, 1)
		})

		t.Run("envelope 不正な push で 400 になった後、同じ記事の有効な push を投げ直すと 200 を返し記事が永続化される", func(t *testing.T) {
			r, repo := newTestRouter(t)
			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FA4"

			wBad := httptest.NewRecorder()
			r.ServeHTTP(wBad, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(`{not-json`)))
			require.Equal(t, http.StatusBadRequest, wBad.Code)

			wRetry := doPush(t, r, validEventPayload(t, articleID))
			assert.Equal(t, http.StatusOK, wRetry.Code)

			aw, err := loadArticleWithTranslations(repo, articleID)
			require.NoError(t, err)
			assert.Equal(t, articleID, aw.Article.ArticleID)
			require.Len(t, aw.Translations, 1)
		})
	})
}
