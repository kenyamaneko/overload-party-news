//go:build integration

package router_test

import (
	"context"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
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
	newsH := rest.NewNewsHandler(news.New(repo))
	subscriberH := subscriber.NewArticleCollectedHandler(ingest.New(repo))
	pushH := pubsubpush.NewHandler(subscriberH.Handle)
	verifier := internalauth.NewVerifier(internalauth.StaticHS256Resolver([]byte("test-internal-auth-secret"), internalauth.DefaultKeyID))

	return router.NewPublic(newsH, verifier, pushH), repo
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
	pub := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	data, err := json.Marshal(apinews.ArticleCollectedEvent{
		ArticleID:         articleID,
		Source:            "aws",
		SourceURL:         "https://aws.amazon.com/" + articleID,
		Tags:              []string{"compute"},
		SourcePublishedAt: &pub,
		Translations: []apinews.EventTranslation{
			{Lang: domain.LangJa, Title: "タイトル", Summary: "要約", Body: "本文"},
		},
	})
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

		t.Run("message.data が base64 として不正な push を投げると、400 を返し DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := httptest.NewRecorder()
			body := `{"message":{"data":"not-valid-base64!!"}}`
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(body)))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "base64 復号不能な push は DB に永続化されないべき")
		})

		t.Run("push envelope の形式でない本文を投げると、400 を返し DB に永続化されない", func(t *testing.T) {
			r, repo := newTestRouter(t)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, pushPath, strings.NewReader(`{not-json`)))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			items, err := repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "envelope 不正な push は DB に永続化されないべき")
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
