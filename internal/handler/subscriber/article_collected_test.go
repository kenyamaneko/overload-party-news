package subscriber_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/service/ingest"
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
			{Lang: apinews.LangJa, Title: "T", Summary: "S", Body: "B"},
		},
	})
	require.NoError(t, err)
	return data
}

// 仕様 (FEATURE_SPEC §3.1, ARCHITECTURE §ACK 戦略):
//   - JSON デコード失敗 → ACK (nil 返り、repo を呼ばない)
//   - ErrInvalidEventPayload → ACK (nil 返り、repo を呼ばない or 呼んだ後でも ACK)
//   - DB 障害など deterministic でないエラー → NACK (err を返す)
//   - 正常 INSERT / 重複 → ACK
func TestHandle_仕様_ACKとNACK(t *testing.T) {
	dbErr := errors.New("db connection lost")

	cases := []struct {
		name           string
		payload        []byte
		repoInserted   bool
		repoErr        error
		repoReturnsErr bool
		wantErr        error
		wantCallCount  int
	}{
		{name: "有効 payload で新規 INSERT", payload: validEventJSON(t), repoInserted: true, wantErr: nil, wantCallCount: 1},
		{name: "有効 payload で重複 (ACK)", payload: validEventJSON(t), repoInserted: false, wantErr: nil, wantCallCount: 1},

		{name: "JSON 不正は ACK (repo を呼ばない)", payload: []byte("{not-json"), wantErr: nil, wantCallCount: 0},
		{name: "JSON が空でも ACK", payload: []byte(""), wantErr: nil, wantCallCount: 0},

		// 必須フィールド欠け: service でバリデーションされ ACK。
		{name: "translations 空は ACK", payload: func() []byte {
			b, _ := json.Marshal(apinews.ArticleCollectedEvent{
				ArticleID: "01", Source: "aws", SourceURL: "u",
			})
			return b
		}(), wantErr: nil, wantCallCount: 0},

		{name: "lang 対応外は ACK", payload: func() []byte {
			b, _ := json.Marshal(apinews.ArticleCollectedEvent{
				ArticleID: "01", Source: "aws", SourceURL: "u",
				Translations: []apinews.EventTranslation{{Lang: "fr", Title: "t", Summary: "s", Body: "b"}},
			})
			return b
		}(), wantErr: nil, wantCallCount: 0},

		{name: "DB 障害は NACK", payload: validEventJSON(t), repoReturnsErr: true, repoErr: dbErr, wantErr: dbErr, wantCallCount: 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var callCount int
			repo := &port.MockNewsRepo{
				InsertFn: func(_ context.Context, _ apinews.Article, _ []apinews.Translation) (bool, error) {
					callCount++
					if tc.repoReturnsErr {
						return false, tc.repoErr
					}
					return tc.repoInserted, nil
				},
			}
			h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

			err := h.Handle(context.Background(), tc.payload)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCallCount, callCount)
		})
	}
}

// 仕様: ingest.ErrInvalidEventPayload は ACK する (deterministic)。
func TestHandle_仕様_ErrInvalidEventPayloadはACK(t *testing.T) {
	repo := &port.MockNewsRepo{
		InsertFn: func(_ context.Context, _ apinews.Article, _ []apinews.Translation) (bool, error) {
			return false, ingest.ErrInvalidEventPayload
		},
	}
	h := subscriber.NewArticleCollectedHandler(ingest.New(repo))

	err := h.Handle(context.Background(), validEventJSON(t))

	assert.NoError(t, err, "ErrInvalidEventPayload は nil (ACK) で吸収すべき")
}
