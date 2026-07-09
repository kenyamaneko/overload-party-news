//go:build integration

package pubsub_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	newspubsub "github.com/kenyamaneko/overload-party-news/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-news/internal/adapter/pubsub/pubsubtest"
	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres/postgrestest"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

var (
	sharedPG       *postgrestest.Postgres
	sharedEmulator *pubsubtest.Emulator
)

// TestMain は emulator + postgres を package scope で 1 回ずつ起動する。
// container 起動コストが高いので per-test ではなく package scope で償却する。
// テスト間の分離は topic/subscription UUID suffix と Postgres.Truncate で担保する。
func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
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

	em, err := pubsubtest.StartEmulator(ctx, "news-test")
	if err != nil {
		log.Fatalf("pubsubtest.StartEmulator: %v", err)
	}
	defer func() {
		if err := em.Close(ctx); err != nil {
			log.Printf("emulator close: %v", err)
		}
	}()
	sharedEmulator = em

	return m.Run()
}

// pipeline はテスト内で publish を駆動する最小パイプラインのハンドル。
type pipeline struct {
	topicID string
	repo    *postgres.NewsRepository
	cancel  context.CancelFunc
	done    <-chan struct{}
}

// setupPipeline は「topic → news subscriber adapter → handler → ingest → repo → DB」を
// 実 emulator + 実 DB で直結する。ingest のユースケース全体を本番 wire で走らせる。
func setupPipeline(t *testing.T) *pipeline {
	t.Helper()
	sharedPG.Truncate(t)

	topicID := sharedEmulator.CreateTopic(t, "news-article-collected")
	subID := sharedEmulator.CreateSubscription(t, topicID, "news-sub")

	repo := postgres.NewNewsRepository(sharedPG.Pool)
	handler := subscriber.NewArticleCollectedHandler(ingest.New(repo))

	stream, err := newspubsub.NewStream(context.Background(), sharedEmulator.ProjectID(), subID)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = stream.Consume(ctx, handler.Handle)
		_ = stream.Close()
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return &pipeline{
		topicID: topicID,
		repo:    repo,
		cancel:  cancel,
		done:    done,
	}
}

// waitForArticle は DB 書き込みを polling で待つ。Pub/Sub 配送は非同期なので、
// publish 後の「いつ DB に現れるか」をテスト時間内で収束させるための helper。
func waitForArticle(t *testing.T, repo *postgres.NewsRepository, articleID string, timeout time.Duration) *domain.ArticleWithTranslations {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		aw, err := loadArticleWithTranslations(repo, articleID)
		if err == nil {
			return aw
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("article %s not found within %s", articleID, timeout)
	return nil
}

// loadArticleWithTranslations は repo の分離された I/O を結合し Status を導出する統合テスト用ヘルパー。
// usecase 層を経由せず DB 状態を直接観測したいときに使う。
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

func TestIngestE2E(t *testing.T) {
	t.Run("記事取込パイプラインの E2E", func(t *testing.T) {
		t.Run("有効イベントを publish すると、記事と ja 翻訳が pending で永続化される", func(t *testing.T) {
			p := setupPipeline(t)

			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
			sharedEmulator.Publish(t, p.topicID, validEventPayload(t, articleID))

			aw := waitForArticle(t, p.repo, articleID, 5*time.Second)
			assert.Equal(t, articleID, aw.Article.ArticleID)
			assert.Equal(t, domain.StatusPending, aw.Article.Status)
			require.Len(t, aw.Translations, 1)
			assert.Equal(t, domain.LangJa, aw.Translations[0].Lang)
			assert.Equal(t, "タイトル", aw.Translations[0].Title)
		})

		t.Run("同一イベントを 2 回 publish しても、翻訳は増えず created_at も変わらない", func(t *testing.T) {
			p := setupPipeline(t)

			articleID := "01ARZ3NDEKTSV4RRFFQ69G5FA2"
			payload := validEventPayload(t, articleID)

			sharedEmulator.Publish(t, p.topicID, payload)
			aw := waitForArticle(t, p.repo, articleID, 5*time.Second)
			require.Len(t, aw.Translations, 1)

			// 2 回目の publish: 記事・翻訳とも ON CONFLICT DO NOTHING で no-op になる
			sharedEmulator.Publish(t, p.topicID, payload)
			// 2 回目の永続化を待つ決定的な手段がないので、短時間経過で結果を観測する
			time.Sleep(500 * time.Millisecond)

			aw2, err := loadArticleWithTranslations(p.repo, articleID)
			require.NoError(t, err)
			require.Len(t, aw2.Translations, 1, "重複 publish で翻訳は増えない")
			assert.Equal(t, aw.Translations[0].CreatedAt.UnixNano(), aw2.Translations[0].CreatedAt.UnixNano(),
				"重複 publish で既存翻訳の created_at は変わらない")
		})

		t.Run("ja 以外の翻訳を含むイベントは、ACK され DB に永続化されない", func(t *testing.T) {
			p := setupPipeline(t)

			// translations が ja 以外 → service のバリデーションで ErrInvalidEventPayload → ACK
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

			sharedEmulator.Publish(t, p.topicID, data)
			time.Sleep(500 * time.Millisecond)

			_, err = p.repo.GetArticleByID(context.Background(), articleID)
			assert.Error(t, err, "invalid payload は DB に永続化されないべき")
		})

		t.Run("壊れた JSON を publish しても、DB に永続化されない", func(t *testing.T) {
			p := setupPipeline(t)

			sharedEmulator.Publish(t, p.topicID, []byte("{not-json"))
			time.Sleep(500 * time.Millisecond)

			// 壊れた payload では article_id が得られないので件数で観測する
			items, err := p.repo.ListArticles(context.Background(), 10)
			require.NoError(t, err)
			assert.Empty(t, items, "壊れた JSON は DB に永続化されないべき")
		})
	})
}
