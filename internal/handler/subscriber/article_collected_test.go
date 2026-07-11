package subscriber_test

import (
	"context"
	"encoding/json"
	"errors"
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
	data, err := json.Marshal(apinews.ArticleCollectedEvent{
		ArticleID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Source:    "aws",
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
	t.Run("ArticleCollected イベントの処理", func(t *testing.T) {
		ackCases := []struct {
			name                 string
			payload              []byte
			articleInserted      bool
			wantArticleCallCount int
			wantTransCallCount   int
		}{
			{
				name:                 "有効 payload で新規のとき、記事と翻訳が 1 回ずつ INSERT され ACK される",
				payload:              validEventJSON(t),
				articleInserted:      true,
				wantArticleCallCount: 1,
				wantTransCallCount:   1,
			},
			{
				name:                 "有効 payload で重複のとき、ACK される",
				payload:              validEventJSON(t),
				articleInserted:      false,
				wantArticleCallCount: 1,
				wantTransCallCount:   1,
			},
			{
				name:                 "JSON 不正のとき、repo を呼ばず ACK される",
				payload:              []byte("{not-json"),
				wantArticleCallCount: 0,
				wantTransCallCount:   0,
			},
			{
				name:                 "JSON が空のとき、repo を呼ばず ACK される",
				payload:              []byte(""),
				wantArticleCallCount: 0,
				wantTransCallCount:   0,
			},
			// 必須フィールド欠けは usecase のバリデーションで ACK される。
			{
				name: "translations 空のとき、repo を呼ばず ACK される",
				payload: func() []byte {
					b, _ := json.Marshal(apinews.ArticleCollectedEvent{
						ArticleID: "01", Source: "aws", SourceURL: "u",
					})
					return b
				}(),
				wantArticleCallCount: 0,
				wantTransCallCount:   0,
			},
			{
				name: "lang が ja 以外のとき、repo を呼ばず ACK される",
				payload: func() []byte {
					b, _ := json.Marshal(apinews.ArticleCollectedEvent{
						ArticleID: "01", Source: "aws", SourceURL: "u",
						Translations: []apinews.EventTranslation{{Lang: domain.LangEn, Title: "t", Summary: "s", Body: "b"}},
					})
					return b
				}(),
				wantArticleCallCount: 0,
				wantTransCallCount:   0,
			},
		}
		for _, tc := range ackCases {
			t.Run(tc.name, func(t *testing.T) {
				var articleCalls, transCalls int
				repo := &port.MockNewsRepo{
					InsertArticleFn: func(_ context.Context, _ domain.Article) (bool, error) {
						articleCalls++
						return tc.articleInserted, nil
					},
					InsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
						transCalls++
						return nil
					},
				}
				h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

				err := h.Handle(context.Background(), tc.payload)

				assert.NoError(t, err)
				assert.Equal(t, tc.wantArticleCallCount, articleCalls)
				assert.Equal(t, tc.wantTransCallCount, transCalls)
			})
		}

		dbErr := errors.New("db connection lost")
		nackCases := []struct {
			name                 string
			articleInserted      bool
			articleErr           error
			transErr             error
			wantArticleCallCount int
			wantTransCallCount   int
		}{
			{
				name:                 "記事 INSERT で DB 障害のとき、NACK される (エラー伝播)",
				articleErr:           dbErr,
				wantArticleCallCount: 1,
				wantTransCallCount:   0,
			},
			{
				name:                 "翻訳 INSERT で DB 障害のとき、NACK される (エラー伝播)",
				articleInserted:      true,
				transErr:             dbErr,
				wantArticleCallCount: 1,
				wantTransCallCount:   1,
			},
		}
		for _, tc := range nackCases {
			t.Run(tc.name, func(t *testing.T) {
				var articleCalls, transCalls int
				repo := &port.MockNewsRepo{
					InsertArticleFn: func(_ context.Context, _ domain.Article) (bool, error) {
						articleCalls++
						return tc.articleInserted, tc.articleErr
					},
					InsertTranslationFn: func(_ context.Context, _, _, _, _, _ string) error {
						transCalls++
						return tc.transErr
					},
				}
				h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

				err := h.Handle(context.Background(), validEventJSON(t))

				assert.ErrorIs(t, err, dbErr)
				assert.Equal(t, tc.wantArticleCallCount, articleCalls)
				assert.Equal(t, tc.wantTransCallCount, transCalls)
			})
		}
	})
}
