package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/router"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
)

func newTestPublicServer(t *testing.T, listFn func(context.Context, string, int) ([]domain.PublishedArticleSummary, error)) (*http.Server, net.Listener) {
	t.Helper()
	repo := &port.MockNewsRepo{ListPublishedFn: listFn}
	newsH := rest.NewNewsHandler(news.New(repo))
	verifier := &internalauth.MockVerifier{VerifyFn: func(string) (string, error) { return "player-001", nil }}
	pushH := pubsubpush.NewHandler(func(context.Context, []byte) error { return nil })
	engine := router.NewPublic(newsH, verifier, pushH)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{Handler: engine}
	return srv, ln
}

func TestServe(t *testing.T) {
	t.Run("HTTPサーバの起動・停止", func(t *testing.T) {
		t.Run("サーバ起動中は、公開APIへのリクエスト(認証成功・記事取得成功のケース)が200を返す", func(t *testing.T) {
			srv, ln := newTestPublicServer(t, func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
				return []domain.PublishedArticleSummary{}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())

			serveErr := make(chan error, 1)
			go func() {
				serveErr <- serve(ctx, srv, ln)
			}()

			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/v1/news?lang=ja&limit=10", ln.Addr().String()), nil)
			require.NoError(t, err)
			req.Header.Set(internalauth.HeaderName, "valid-token")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			cancel()
			select {
			case err := <-serveErr:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("serve did not return after cancel")
			}
		})

		t.Run("停止要求(コンテキストのキャンセル)を送ると、serveの呼び出しはエラー無く終了する", func(t *testing.T) {
			srv, ln := newTestPublicServer(t, func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
				return []domain.PublishedArticleSummary{}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())

			serveErr := make(chan error, 1)
			go func() {
				serveErr <- serve(ctx, srv, ln)
			}()

			cancel()
			select {
			case err := <-serveErr:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("serve did not return after cancel")
			}
		})

		t.Run("停止要求を送った時点で応答に時間のかかる処理中のリクエストがあるとき、そのリクエストは打ち切られずに200の応答を最後まで受け取り、その後にserveの呼び出しがエラー無く終了する", func(t *testing.T) {
			started := make(chan struct{})
			srv, ln := newTestPublicServer(t, func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
				close(started)
				time.Sleep(300 * time.Millisecond)
				return []domain.PublishedArticleSummary{}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())

			serveErr := make(chan error, 1)
			go func() {
				serveErr <- serve(ctx, srv, ln)
			}()

			type result struct {
				status int
				err    error
			}
			respCh := make(chan result, 1)
			go func() {
				req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/v1/news?lang=ja&limit=10", ln.Addr().String()), nil)
				if err != nil {
					respCh <- result{err: err}
					return
				}
				req.Header.Set(internalauth.HeaderName, "valid-token")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					respCh <- result{err: err}
					return
				}
				defer func() { _ = resp.Body.Close() }()
				respCh <- result{status: resp.StatusCode}
			}()

			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("in-flight request did not start")
			}
			cancel()

			select {
			case r := <-respCh:
				require.NoError(t, r.err)
				assert.Equal(t, http.StatusOK, r.status)
			case <-time.After(5 * time.Second):
				t.Fatal("in-flight request did not complete")
			}

			select {
			case err := <-serveErr:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("serve did not return after in-flight request completed")
			}
		})
	})
}

func newCloudLoggingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(newCloudLoggingHandler(buf))
}

func decodeLastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &parsed))
	return parsed
}

func TestNewCloudLoggingHandler(t *testing.T) {
	t.Run("Cloud Logging向けログ属性変換", func(t *testing.T) {
		t.Run("Errorレベルで出力すると、severityがERRORになる", func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCloudLoggingLogger(&buf)

			logger.Error("error level message")

			parsed := decodeLastLogLine(t, &buf)
			assert.Equal(t, "ERROR", parsed["severity"])
		})

		t.Run("Warnレベルで出力すると、severityがWARNINGになる", func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCloudLoggingLogger(&buf)

			logger.Warn("warn level message")

			parsed := decodeLastLogLine(t, &buf)
			assert.Equal(t, "WARNING", parsed["severity"])
		})

		t.Run("Infoレベルで出力すると、severityがINFOになる", func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCloudLoggingLogger(&buf)

			logger.Info("info level message")

			parsed := decodeLastLogLine(t, &buf)
			assert.Equal(t, "INFO", parsed["severity"])
		})

		t.Run("出力したメッセージの内容が、messageフィールドにそのまま入る", func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCloudLoggingLogger(&buf)

			logger.Info("distinctive test message content")

			parsed := decodeLastLogLine(t, &buf)
			assert.Equal(t, "distinctive test message content", parsed["message"])
		})
	})
}
