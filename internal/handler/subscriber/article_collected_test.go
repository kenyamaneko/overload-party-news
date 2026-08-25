package subscriber_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func validCollectedEventForSubscriber() apinews.ArticleCollectedEvent {
	return apinews.ArticleCollectedEvent{
		ArticleID: "article-id-valid-002",
		Source:    "aws",
		SourceURL: "https://example.com/articles/2",
		Translations: []apinews.EventTranslation{
			{Lang: "ja", Title: "タイトル", Summary: "要約", Body: "本文"},
		},
	}
}

func newHandlerWithRecordingWriter(inserted bool, err error) (*subscriber.ArticleCollectedHandler, *[]domain.Article) {
	var calls []domain.Article
	writer := &port.MockNewsRepo{
		InsertArticleWithTranslationFn: func(_ context.Context, article domain.Article, _, _, _, _ string) (bool, error) {
			calls = append(calls, article)
			return inserted, err
		},
	}
	uc := ingest.New(writer)
	return subscriber.NewArticleCollectedHandler(uc), &calls
}

type capturingSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
	return nil
}

func (h *capturingSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingSlogHandler) WithGroup(_ string) slog.Handler      { return h }

func captureLogs(t *testing.T, do func()) *capturingSlogHandler {
	t.Helper()
	prev := slog.Default()
	handler := &capturingSlogHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() {
		slog.SetDefault(prev)
	})

	do()

	return handler
}

func recordAttr(t *testing.T, record slog.Record, key string) (string, bool) {
	t.Helper()
	var value string
	found := false
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			value = a.Value.String()
			found = true
			return false
		}
		return true
	})
	return value, found
}

func TestArticleCollectedHandlerHandle(t *testing.T) {
	t.Run("Pub/Sub購読ハンドラのack/nack判定", func(t *testing.T) {
		t.Run("取込可能な新規イベントを処理すると、記事と翻訳が1回だけ書き込み先に渡り、エラー無く終わる", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			data, err := json.Marshal(event)
			require.NoError(t, err)
			h, calls := newHandlerWithRecordingWriter(true, nil)

			handleErr := h.Handle(context.Background(), data)

			assert.NoError(t, handleErr)
			assert.Len(t, *calls, 1)
		})

		t.Run("取込可能だが既に取り込み済みのイベントを処理すると、エラー無く終わる", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			data, err := json.Marshal(event)
			require.NoError(t, err)
			h, _ := newHandlerWithRecordingWriter(false, nil)

			handleErr := h.Handle(context.Background(), data)

			assert.NoError(t, handleErr)
		})

		t.Run("本文がJSONとして解析できないデータを処理すると、書き込み先には何も渡らず、エラー無く終わる", func(t *testing.T) {
			h, calls := newHandlerWithRecordingWriter(true, nil)

			handleErr := h.Handle(context.Background(), []byte("not valid json"))

			assert.NoError(t, handleErr)
			assert.Empty(t, *calls)
		})

		t.Run("空のデータを処理すると、書き込み先には何も渡らず、エラー無く終わる", func(t *testing.T) {
			h, calls := newHandlerWithRecordingWriter(true, nil)

			handleErr := h.Handle(context.Background(), []byte(""))

			assert.NoError(t, handleErr)
			assert.Empty(t, *calls)
		})

		t.Run("4.1の規定により拒否されるイベント(必須フィールド欠落・列幅超過・翻訳の件数や言語が不正)を処理すると、書き込み先には何も渡らず、エラー無く終わる", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			event.ArticleID = ""
			data, err := json.Marshal(event)
			require.NoError(t, err)
			h, calls := newHandlerWithRecordingWriter(true, nil)

			handleErr := h.Handle(context.Background(), data)

			assert.NoError(t, handleErr)
			assert.Empty(t, *calls)
		})

		t.Run("4.1の規定により拒否されるイベントを処理すると、ログに記録される内容にそのイベントのarticle_idが含まれる", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			event.Translations = []apinews.EventTranslation{
				{Lang: "ja", Title: "タイトル", Summary: "", Body: "本文"},
			}
			data, err := json.Marshal(event)
			require.NoError(t, err)
			h, _ := newHandlerWithRecordingWriter(true, nil)

			handler := captureLogs(t, func() {
				handleErr := h.Handle(context.Background(), data)
				require.NoError(t, handleErr)
			})

			require.NotEmpty(t, handler.records)
			found := false
			for _, record := range handler.records {
				if value, ok := recordAttr(t, record, "article_id"); ok && value == event.ArticleID {
					found = true
					break
				}
			}
			assert.True(t, found, "ログにarticle_idが記録されていること")
		})

		t.Run("書き込み先がエラーを返すイベントを処理すると、そのエラーがそのまま呼び出し元に返る", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			data, err := json.Marshal(event)
			require.NoError(t, err)
			writerErr := errors.New("insert failed")
			h, _ := newHandlerWithRecordingWriter(false, writerErr)

			handleErr := h.Handle(context.Background(), data)

			assert.ErrorIs(t, handleErr, writerErr)
		})
	})
}
