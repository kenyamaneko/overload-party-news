package pubsubpush_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
)

func newPushEngine(handle func(ctx context.Context, data []byte) error) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := pubsubpush.NewHandler(handle)
	r := gin.New()
	r.POST("/push", h.Handle)
	return r
}

func doPush(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func envelopeBody(data string) string {
	return `{"message":{"data":"` + data + `"}}`
}

func TestPubsubPushHandlerHandle(t *testing.T) {
	t.Run("[Pub/Sub pushハンドラ]Pub/Sub push envelopeの解析", func(t *testing.T) {
		t.Run("envelopeが正しい形式で、後続処理が成功するとき、200を返し、後続処理にはbase64復号後の本文がそのまま渡る", func(t *testing.T) {
			var got []byte
			r := newPushEngine(func(_ context.Context, data []byte) error {
				got = data
				return nil
			})

			w := doPush(t, r, envelopeBody(base64.StdEncoding.EncodeToString([]byte("payload-content"))))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "payload-content", string(got))
		})

		t.Run("envelopeが正しい形式で、後続処理がエラーを返すとき、500と、そのエラーの内容を含む応答を返す", func(t *testing.T) {
			handlerErr := errors.New("downstream failed")
			r := newPushEngine(func(_ context.Context, _ []byte) error {
				return handlerErr
			})

			w := doPush(t, r, envelopeBody(base64.StdEncoding.EncodeToString([]byte("payload-content"))))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), handlerErr.Error())
		})

		malformedTests := []struct {
			name           string
			body           string
			wantErrContain string
		}{
			{
				name:           "本文がJSONとして解析できないとき",
				body:           "not valid json",
				wantErrContain: "malformed envelope",
			},
			{
				name:           "messageフィールドが無いとき",
				body:           `{}`,
				wantErrContain: "malformed envelope",
			},
			{
				name:           "message.dataが空文字のとき",
				body:           envelopeBody(""),
				wantErrContain: "malformed envelope",
			},
			{
				name:           "message.dataがbase64として復号できない値のとき",
				body:           envelopeBody("not-valid-base64!!"),
				wantErrContain: "undecodable data",
			},
		}
		for _, tt := range malformedTests {
			t.Run(tt.name+"、400と、応答本文に「"+tt.wantErrContain+"」を含む内容を返し、後続処理は実行されない", func(t *testing.T) {
				called := false
				r := newPushEngine(func(_ context.Context, _ []byte) error {
					called = true
					return nil
				})

				w := doPush(t, r, tt.body)

				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Contains(t, w.Body.String(), tt.wantErrContain)
				assert.False(t, called)
			})
		}
	})
}
