package apinewsclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
	"github.com/kenyamaneko/overload-party-news/packages/api-news/apinewsclient"
	"github.com/kenyamaneko/overload-party-news/packages/api-news/apinewsserverfake"
)

func newTestClient(t *testing.T, srv *apinewsserverfake.Server, opts ...apinewsclient.Option) *apinewsclient.Client {
	t.Helper()
	c, err := apinewsclient.New(srv.URL(), opts...)
	require.NoError(t, err)
	return c
}

func TestClientListNews(t *testing.T) {
	t.Run("ニュース一覧取得のステータス変換", func(t *testing.T) {
		t.Run("サーバが400を返したとき、bad requestを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.ListNewsFn = func(string, int) (int, any) { return http.StatusBadRequest, nil }
			c := newTestClient(t, srv)

			_, err := c.ListNews(context.Background(), "ja", 10)

			assert.ErrorIs(t, err, apinewsclient.ErrBadRequest)
		})

		t.Run("サーバが401を返したとき、unauthorizedを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.ListNewsFn = func(string, int) (int, any) { return http.StatusUnauthorized, nil }
			c := newTestClient(t, srv)

			_, err := c.ListNews(context.Background(), "ja", 10)

			assert.ErrorIs(t, err, apinewsclient.ErrUnauthorized)
		})

		t.Run("サーバが500を返したとき、internal server errorを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.ListNewsFn = func(string, int) (int, any) { return http.StatusInternalServerError, nil }
			c := newTestClient(t, srv)

			_, err := c.ListNews(context.Background(), "ja", 10)

			assert.ErrorIs(t, err, apinewsclient.ErrInternalServer)
		})

		t.Run("契約に無い403をサーバが返したとき、unexpected status 403を含む想定外のエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.ListNewsFn = func(string, int) (int, any) { return http.StatusForbidden, nil }
			c := newTestClient(t, srv)

			_, err := c.ListNews(context.Background(), "ja", 10)

			require.Error(t, err)
			assert.ErrorContains(t, err, "unexpected status 403")
		})
	})
}

func TestClientGetNewsDetail(t *testing.T) {
	t.Run("ニュース詳細取得のステータス変換", func(t *testing.T) {
		t.Run("サーバが400を返したとき、bad requestを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.GetNewsDetailFn = func(string, string) (int, any) { return http.StatusBadRequest, nil }
			c := newTestClient(t, srv)

			_, err := c.GetNewsDetail(context.Background(), "article-001", "ja")

			assert.ErrorIs(t, err, apinewsclient.ErrBadRequest)
		})

		t.Run("サーバが401を返したとき、unauthorizedを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.GetNewsDetailFn = func(string, string) (int, any) { return http.StatusUnauthorized, nil }
			c := newTestClient(t, srv)

			_, err := c.GetNewsDetail(context.Background(), "article-001", "ja")

			assert.ErrorIs(t, err, apinewsclient.ErrUnauthorized)
		})

		t.Run("サーバが404を返したとき、not foundを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.GetNewsDetailFn = func(string, string) (int, any) { return http.StatusNotFound, nil }
			c := newTestClient(t, srv)

			_, err := c.GetNewsDetail(context.Background(), "article-001", "ja")

			assert.ErrorIs(t, err, apinewsclient.ErrNotFound)
		})

		t.Run("サーバが500を返したとき、internal server errorを表すエラーになる", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.GetNewsDetailFn = func(string, string) (int, any) { return http.StatusInternalServerError, nil }
			c := newTestClient(t, srv)

			_, err := c.GetNewsDetail(context.Background(), "article-001", "ja")

			assert.ErrorIs(t, err, apinewsclient.ErrInternalServer)
		})
	})
}

func TestClientGetHealth(t *testing.T) {
	t.Run("ヘルスチェック取得", func(t *testing.T) {
		t.Run("サーバが200とヘルスチェック応答を返したとき、その内容をそのまま返す", func(t *testing.T) {
			srv := apinewsserverfake.NewServer()
			defer srv.Close()
			srv.GetHealthFn = func() (int, any) {
				return http.StatusOK, apinews.HealthResponse{Status: "ok"}
			}
			c := newTestClient(t, srv)

			got, err := c.GetHealth(context.Background())

			require.NoError(t, err)
			assert.Equal(t, "ok", got.Status)
		})
	})
}

func TestClientRequestEditor(t *testing.T) {
	t.Run("リクエストへの加工の適用", func(t *testing.T) {
		t.Run("クライアント構築時にリクエストへの加工(追加ヘッダの付与など)を指定すると、その加工が実行したリクエストに適用され、サーバー側に届くリクエストに反映される", func(t *testing.T) {
			var gotHeader string
			mux := http.NewServeMux()
			mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get("X-Custom-Header")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			rawSrv := httptest.NewServer(mux)
			defer rawSrv.Close()

			c, err := apinewsclient.New(rawSrv.URL, apinewsclient.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
				req.Header.Set("X-Custom-Header", "editor-applied")
				return nil
			}))
			require.NoError(t, err)

			_, err = c.GetHealth(context.Background())

			require.NoError(t, err)
			assert.Equal(t, "editor-applied", gotHeader)
		})
	})
}
