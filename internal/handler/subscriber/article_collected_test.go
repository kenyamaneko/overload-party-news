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
	dbErr := errors.New("db connection lost")

	cases := []struct {
		name                 string
		payload              []byte
		articleInserted      bool
		articleErr           error
		transErr             error
		wantErr              error
		wantArticleCallCount int
		wantTransCallCount   int
	}{
		{
			name:                 "有効 payload で新規 INSERT",
			payload:              validEventJSON(t),
			articleInserted:      true,
			wantErr:              nil,
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name:                 "有効 payload で重複 (ACK)",
			payload:              validEventJSON(t),
			articleInserted:      false,
			wantErr:              nil,
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
		{
			name:                 "JSON 不正は ACK (repo を呼ばない)",
			payload:              []byte("{not-json"),
			wantErr:              nil,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "JSON が空でも ACK",
			payload:              []byte(""),
			wantErr:              nil,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		// 必須フィールド欠け: service でバリデーションされ ACK。
		{
			name: "translations 空は ACK",
			payload: func() []byte {
				b, _ := json.Marshal(apinews.ArticleCollectedEvent{
					ArticleID: "01", Source: "aws", SourceURL: "u",
				})
				return b
			}(),
			wantErr:              nil,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name: "lang が ja 以外は ACK",
			payload: func() []byte {
				b, _ := json.Marshal(apinews.ArticleCollectedEvent{
					ArticleID: "01", Source: "aws", SourceURL: "u",
					Translations: []apinews.EventTranslation{{Lang: domain.LangEn, Title: "t", Summary: "s", Body: "b"}},
				})
				return b
			}(),
			wantErr:              nil,
			wantArticleCallCount: 0,
			wantTransCallCount:   0,
		},
		{
			name:                 "記事 INSERT の DB 障害は NACK",
			payload:              validEventJSON(t),
			articleErr:           dbErr,
			wantErr:              dbErr,
			wantArticleCallCount: 1,
			wantTransCallCount:   0,
		},
		{
			name:                 "翻訳 INSERT の DB 障害は NACK",
			payload:              validEventJSON(t),
			articleInserted:      true,
			transErr:             dbErr,
			wantErr:              dbErr,
			wantArticleCallCount: 1,
			wantTransCallCount:   1,
		},
	}

	for _, tc := range cases {
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

			err := h.Handle(context.Background(), tc.payload)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantArticleCallCount, articleCalls)
			assert.Equal(t, tc.wantTransCallCount, transCalls)
		})
	}
}
