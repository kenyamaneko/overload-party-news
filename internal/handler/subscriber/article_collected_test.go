package subscriber_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func validEventJSON(t *testing.T) []byte {
	t.Helper()
	return eventJSON(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "aws")
}

func eventJSON(t *testing.T, articleID, source string) []byte {
	t.Helper()
	data, err := json.Marshal(apinews.ArticleCollectedEvent{
		ArticleID: articleID,
		Source:    source,
		SourceURL: "https://aws.amazon.com/foo",
		Tags:      []string{"compute"},
		Translations: []apinews.EventTranslation{
			{Lang: domain.LangJa, Title: "T", Summary: "S", Body: "B"},
		},
	})
	require.NoError(t, err)
	return data
}

func TestHandle(t *testing.T) {
	t.Run("記事収集イベントの処理", func(t *testing.T) {
		ackCases := []struct {
			name          string
			payload       []byte
			writeResult   bool
			wantWriteCall int
		}{
			{
				name:          "有効payloadで新規のとき、記事と翻訳が1回INSERTされACKされる",
				payload:       validEventJSON(t),
				writeResult:   true,
				wantWriteCall: 1,
			},
			{
				name:          "有効payloadで既に取り込み済みのとき、ACKされる",
				payload:       validEventJSON(t),
				writeResult:   false,
				wantWriteCall: 1,
			},
			{
				name:          "JSON不正のとき、repoを呼ばずACKされる",
				payload:       []byte("{not-json"),
				wantWriteCall: 0,
			},
			{
				name:          "JSONが空のとき、repoを呼ばずACKされる",
				payload:       []byte(""),
				wantWriteCall: 0,
			},
			// 必須フィールド欠けは usecase のバリデーションで ACK される。
			{
				name: "translations空のとき、repoを呼ばずACKされる",
				payload: func() []byte {
					b, _ := json.Marshal(apinews.ArticleCollectedEvent{
						ArticleID: "01", Source: "aws", SourceURL: "u",
					})
					return b
				}(),
				wantWriteCall: 0,
			},
			{
				name:          "article_idが27文字のとき、repoを呼ばずACKされる",
				payload:       eventJSON(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ7", "aws"),
				wantWriteCall: 0,
			},
			{
				name:          "sourceが21文字のとき、repoを呼ばずACKされる",
				payload:       eventJSON(t, "01", "123456789012345678901"),
				wantWriteCall: 0,
			},
			{
				name: "langがja以外のとき、repoを呼ばずACKされる",
				payload: func() []byte {
					b, _ := json.Marshal(apinews.ArticleCollectedEvent{
						ArticleID: "01", Source: "aws", SourceURL: "u",
						Translations: []apinews.EventTranslation{{Lang: domain.LangEn, Title: "t", Summary: "s", Body: "b"}},
					})
					return b
				}(),
				wantWriteCall: 0,
			},
		}
		for _, tc := range ackCases {
			t.Run(tc.name, func(t *testing.T) {
				var writeCalls int
				repo := &port.MockNewsRepo{
					InsertArticleWithTranslationFn: func(_ context.Context, _ domain.Article, _, _, _, _ string) (bool, error) {
						writeCalls++
						return tc.writeResult, nil
					},
				}
				h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

				err := h.Handle(context.Background(), tc.payload)

				assert.NoError(t, err)
				assert.Equal(t, tc.wantWriteCall, writeCalls)
			})
		}

		t.Run("INSERTでDB障害のとき、NACKされる (エラー伝播)", func(t *testing.T) {
			dbErr := errors.New("db connection lost")
			var writeCalls int
			repo := &port.MockNewsRepo{
				InsertArticleWithTranslationFn: func(_ context.Context, _ domain.Article, _, _, _, _ string) (bool, error) {
					writeCalls++
					return false, dbErr
				},
			}
			h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

			err := h.Handle(context.Background(), validEventJSON(t))

			assert.ErrorIs(t, err, dbErr)
			assert.Equal(t, 1, writeCalls)
		})

		logCases := []struct {
			name       string
			payload    []byte
			wantReason string
		}{
			{
				name:       "article_idが27文字のとき、article_idが上限を超えた旨がログに出る",
				payload:    eventJSON(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ7", "aws"),
				wantReason: "article_id exceeds 26 characters (got 27)",
			},
			{
				name:       "sourceが21文字のとき、sourceが上限を超えた旨がログに出る",
				payload:    eventJSON(t, "01", "123456789012345678901"),
				wantReason: "source exceeds 20 characters (got 21)",
			},
		}
		for _, tc := range logCases {
			t.Run(tc.name, func(t *testing.T) {
				var logged bytes.Buffer
				prev := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
				t.Cleanup(func() { slog.SetDefault(prev) })
				repo := &port.MockNewsRepo{
					InsertArticleWithTranslationFn: func(_ context.Context, _ domain.Article, _, _, _, _ string) (bool, error) {
						return true, nil
					},
				}
				h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

				err := h.Handle(context.Background(), tc.payload)

				assert.NoError(t, err)
				assert.Contains(t, logged.String(), tc.wantReason)
			})
		}
	})
}
