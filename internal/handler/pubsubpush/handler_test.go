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

	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestRouter は handle を委譲先とする Handler を単一ルートに載せた gin.Engine を返す。
func newTestRouter(handle func(ctx context.Context, data []byte) error) *gin.Engine {
	r := gin.New()
	r.POST("/push", pubsubpush.NewHandler(handle).Handle)
	return r
}

func pushBody(t *testing.T, rawData string) *strings.Reader {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(rawData))
	return strings.NewReader(`{"message":{"data":"` + encoded + `","messageId":"m1"},"subscription":"projects/p/subscriptions/s"}`)
}

func TestHandle(t *testing.T) {
	t.Run("push envelope の処理", func(t *testing.T) {
		t.Run("有効な envelope で後続処理が成功したとき、200 を返し後続処理に復号済みの本文が渡る", func(t *testing.T) {
			var gotData []byte
			r := newTestRouter(func(_ context.Context, data []byte) error {
				gotData = data
				return nil
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/push", pushBody(t, `{"article_id":"TST-0001"}`))
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, `{"article_id":"TST-0001"}`, string(gotData))
		})

		t.Run("有効な envelope で後続処理がエラーを返すとき、500 を返しエラー内容が応答に含まれる", func(t *testing.T) {
			r := newTestRouter(func(context.Context, []byte) error {
				return errors.New("db connection lost")
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/push", pushBody(t, `{}`))
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "db connection lost")
		})

		t.Run("本文が push envelope の JSON 形式でないとき、400 を返し後続処理を呼ばず応答に envelope 不正の内容が含まれる", func(t *testing.T) {
			var called bool
			r := newTestRouter(func(context.Context, []byte) error {
				called = true
				return nil
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(`{not-json`))
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.False(t, called)
			assert.Contains(t, w.Body.String(), "malformed envelope")
		})

		t.Run("message フィールドが無いとき、400 を返し後続処理を呼ばず応答に envelope 不正の内容が含まれる", func(t *testing.T) {
			var called bool
			r := newTestRouter(func(context.Context, []byte) error {
				called = true
				return nil
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(`{}`))
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.False(t, called)
			assert.Contains(t, w.Body.String(), "malformed envelope")
		})

		t.Run("message.data が空文字のとき、400 を返し後続処理を呼ばず応答に envelope 不正の内容が含まれる", func(t *testing.T) {
			var called bool
			r := newTestRouter(func(context.Context, []byte) error {
				called = true
				return nil
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(`{"message":{"data":""}}`))
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.False(t, called)
			assert.Contains(t, w.Body.String(), "malformed envelope")
		})

		t.Run("message.data が base64 として不正なとき、400 を返し後続処理を呼ばず応答に復号不能の内容が含まれる", func(t *testing.T) {
			var called bool
			r := newTestRouter(func(context.Context, []byte) error {
				called = true
				return nil
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(`{"message":{"data":"not-valid-base64!!"}}`))
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.False(t, called)
			assert.Contains(t, w.Body.String(), "undecodable data")
		})
	})
}
