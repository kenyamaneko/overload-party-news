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
		Tags:      []string{"cloud", "release"},
		Translations: []apinews.EventTranslation{
			{Lang: "ja", Title: "タイトル", Summary: "要約", Body: "本文"},
		},
	}
}

type recordedInsert struct {
	article                    domain.Article
	lang, title, summary, body string
}

func newHandlerWithRecordingWriter(inserted bool, err error) (*subscriber.ArticleCollectedHandler, *[]recordedInsert) {
	var calls []recordedInsert
	writer := &port.MockNewsRepo{
		InsertArticleWithTranslationFn: func(_ context.Context, article domain.Article, lang, title, summary, body string) (bool, error) {
			calls = append(calls, recordedInsert{article: article, lang: lang, title: title, summary: summary, body: body})
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
	t.Run("[記事収集購読ハンドラ]Pub/Sub購読ハンドラのack/nack判定", func(t *testing.T) {
		t.Run("取込可能な新規イベントを受け取ると、記事と翻訳が1回だけ書き込み先に渡り、エラーを返さない", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			data, err := json.Marshal(event)
			require.NoError(t, err)
			h, calls := newHandlerWithRecordingWriter(true, nil)

			handleErr := h.Handle(context.Background(), data)

			assert.NoError(t, handleErr)
			require.Len(t, *calls, 1)
			got := (*calls)[0]
			assert.Equal(t, event.ArticleID, got.article.ArticleID)
			assert.Equal(t, event.Translations[0].Lang, got.lang)
			assert.Equal(t, event.Translations[0].Title, got.title)
			assert.Equal(t, event.Translations[0].Summary, got.summary)
			assert.Equal(t, event.Translations[0].Body, got.body)
		})

		t.Run("取込可能だが既に取り込み済みのイベントを受け取ると、エラーを返さない", func(t *testing.T) {
			event := validCollectedEventForSubscriber()
			data, err := json.Marshal(event)
			require.NoError(t, err)
			h, _ := newHandlerWithRecordingWriter(false, nil)

			handleErr := h.Handle(context.Background(), data)

			assert.NoError(t, handleErr)
		})

		rejectedTests := []struct {
			name      string
			buildData func(t *testing.T) []byte
		}{
			{
				name:      "本文がJSONとして解析できないデータを受け取ると",
				buildData: func(*testing.T) []byte { return []byte("not valid json") },
			},
			{
				name:      "空のデータを受け取ると",
				buildData: func(*testing.T) []byte { return []byte("") },
			},
			{
				name: "article_idが空のイベントを受け取ると",
				buildData: func(t *testing.T) []byte {
					event := validCollectedEventForSubscriber()
					event.ArticleID = ""
					data, err := json.Marshal(event)
					require.NoError(t, err)
					return data
				},
			},
		}
		for _, tt := range rejectedTests {
			t.Run(tt.name+"、書き込み先には何も渡らず、エラーを返さない", func(t *testing.T) {
				h, calls := newHandlerWithRecordingWriter(true, nil)

				handleErr := h.Handle(context.Background(), tt.buildData(t))

				assert.NoError(t, handleErr)
				assert.Empty(t, *calls)
			})
		}

		t.Run("翻訳の要約が空のペイロードを拒否したとき、ログの記録内容にそのイベントのarticle_idが含まれる", func(t *testing.T) {
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

			require.Len(t, handler.records, 1)
			articleID, ok := recordAttr(t, handler.records[0], "article_id")
			require.True(t, ok)
			assert.Equal(t, event.ArticleID, articleID)
		})

		t.Run("有効なイベントを受け取り、書き込み先がエラーを返すとき、そのエラーがそのまま呼び出し元に返る", func(t *testing.T) {
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
