package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
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

func TestServe(t *testing.T) {
	t.Run("gateway向け内部APIサーバの起動と停止", func(t *testing.T) {
		t.Run("起動中はニュース一覧の取得が200を返し、停止要求で終了する", func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)

			srv := &http.Server{
				Handler:           newPublicRouterWithStubs(),
				ReadHeaderTimeout: 10 * time.Second,
			}
			ctx, cancel := context.WithCancel(context.Background())
			served := make(chan error, 1)
			go func() { served <- serve(ctx, srv, ln) }()

			req, err := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/api/v1/news?lang=ja&limit=10", nil)
			require.NoError(t, err)
			req.Header.Set(internalauth.HeaderName, "any.token")
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			cancel()
			require.NoError(t, <-served)
		})
	})
}

// newPublicRouterWithStubs は記事なしを返す repo と常に成功する検証器で公開ルータを構築する。
func newPublicRouterWithStubs() http.Handler {
	querier := &port.MockNewsRepo{
		ListPublishedFn: func(context.Context, string, int) ([]domain.PublishedArticleSummary, error) {
			return []domain.PublishedArticleSummary{}, nil
		},
	}
	verifier := &internalauth.MockVerifier{
		VerifyFn: func(string) (string, error) { return "TST-PLAYER-1", nil },
	}
	pushH := pubsubpush.NewHandler(func(context.Context, []byte) error { return nil })
	return router.NewPublic(rest.NewNewsHandler(news.New(querier)), verifier, pushH)
}

func TestCloudLoggingHandler(t *testing.T) {
	t.Run("Cloud Logging向けログ属性変換", func(t *testing.T) {
		cases := []struct {
			name         string
			log          func(l *slog.Logger)
			wantSeverity string
		}{
			{
				name:         "Errorレベルで出力すると、severityがERRORになる",
				log:          func(l *slog.Logger) { l.Error("boom") },
				wantSeverity: "ERROR",
			},
			{
				name:         "Warnレベルで出力すると、severityがWARNINGになる",
				log:          func(l *slog.Logger) { l.Warn("boom") },
				wantSeverity: "WARNING",
			},
			{
				name:         "Infoレベルで出力すると、severityがINFOになる",
				log:          func(l *slog.Logger) { l.Info("boom") },
				wantSeverity: "INFO",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				logger := slog.New(newCloudLoggingHandler(&buf))

				tc.log(logger)

				var record map[string]any
				require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
				assert.Equal(t, tc.wantSeverity, record["severity"])
				assert.Equal(t, "boom", record["message"])
			})
		}
	})
}
