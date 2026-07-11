package apinewsclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-news/packages/api-news/apinewsclient"
	"github.com/kenyamaneko/overload-party-news/packages/api-news/apinewsserverfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// StatusMapping 群は、SDK の固有責務である「OpenAPI spec で宣言された 4xx/5xx status を
// sentinel error に変換する」契約を endpoint ごとに検証する。各テストは data/openapi.yaml が
// 宣言する error status を網羅する。

func TestClient_ListNews_StatusMapping(t *testing.T) {
	t.Run("ListNews のステータスマッピング", func(t *testing.T) {
		cases := []struct {
			name       string
			status     int
			wantTarget error
		}{
			{
				name:       "400 を受けたとき、ErrBadRequest になる",
				status:     http.StatusBadRequest,
				wantTarget: apinewsclient.ErrBadRequest,
			},
			{
				name:       "401 を受けたとき、ErrUnauthorized になる",
				status:     http.StatusUnauthorized,
				wantTarget: apinewsclient.ErrUnauthorized,
			},
			{
				name:       "500 を受けたとき、ErrInternalServer になる",
				status:     http.StatusInternalServerError,
				wantTarget: apinewsclient.ErrInternalServer,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := apinewsserverfake.NewServer()
				defer srv.Close()
				srv.ListNewsFn = func(_ string, _ int) (int, any) { return tc.status, nil }

				c := newTestClient(t, srv.URL())
				// status mapping 検証のため query param の値は無関係 (server fake は lang/limit を見ず tc.status を返す)。
				_, err := c.ListNews(context.Background(), "", 0)
				assertSentinel(t, err, tc.wantTarget)
			})
		}
	})
}

func TestClient_GetNewsDetail_StatusMapping(t *testing.T) {
	t.Run("GetNewsDetail のステータスマッピング", func(t *testing.T) {
		cases := []struct {
			name       string
			status     int
			wantTarget error
		}{
			{
				name:       "400 を受けたとき、ErrBadRequest になる",
				status:     http.StatusBadRequest,
				wantTarget: apinewsclient.ErrBadRequest,
			},
			{
				name:       "401 を受けたとき、ErrUnauthorized になる",
				status:     http.StatusUnauthorized,
				wantTarget: apinewsclient.ErrUnauthorized,
			},
			{
				name:       "404 を受けたとき、ErrNotFound になる",
				status:     http.StatusNotFound,
				wantTarget: apinewsclient.ErrNotFound,
			},
			{
				name:       "500 を受けたとき、ErrInternalServer になる",
				status:     http.StatusInternalServerError,
				wantTarget: apinewsclient.ErrInternalServer,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := apinewsserverfake.NewServer()
				defer srv.Close()
				srv.GetNewsDetailFn = func(_ string, _ string) (int, any) { return tc.status, nil }

				c := newTestClient(t, srv.URL())
				// status mapping 検証のため articleID / lang の値は無関係 (server fake は両者を見ず tc.status を返す)。
				_, err := c.GetNewsDetail(context.Background(), "x", "")
				assertSentinel(t, err, tc.wantTarget)
			})
		}
	})
}

func TestClient_RequestEditor(t *testing.T) {
	t.Run("リクエストエディタの適用", func(t *testing.T) {
		t.Run("WithRequestEditorFn で渡した editor が全リクエストに適用される", func(t *testing.T) {
			// X-Internal-Auth header 注入の接続点として SDK が機能することを担保する。
			var gotHeader string
			spy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get("X-Internal-Auth")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"articles":[]}`))
			}))
			defer spy.Close()

			c, err := apinewsclient.New(spy.URL,
				apinewsclient.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
					req.Header.Set("X-Internal-Auth", "test-token")
					return nil
				}),
			)
			require.NoError(t, err)

			_, err = c.ListNews(context.Background(), "ja", 10)
			require.NoError(t, err)
			assert.Equal(t, "test-token", gotHeader)
		})
	})
}

func newTestClient(t *testing.T, baseURL string) *apinewsclient.Client {
	t.Helper()
	c, err := apinewsclient.New(baseURL)
	require.NoError(t, err)
	return c
}

func assertSentinel(t *testing.T, gotErr, wantTarget error) {
	t.Helper()
	require.Error(t, gotErr)
	assert.ErrorIs(t, gotErr, wantTarget)
}
