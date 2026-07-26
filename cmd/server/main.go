package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	internalauth "github.com/kenyamaneko/overload-party-gateway/packages/internalauth-go"
	"golang.org/x/sync/errgroup"

	"github.com/kenyamaneko/overload-party-news/internal/config"
	"github.com/kenyamaneko/overload-party-news/internal/handler/admin"
	"github.com/kenyamaneko/overload-party-news/internal/handler/pubsubpush"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/router"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/news"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/review"
)

func main() {
	if err := run(); err != nil {
		slog.Error("news fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := setupLogger(cfg.Env); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, closeDatabasePool, err := newDatabasePool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("new database pool: %w", err)
	}
	defer closeDatabasePool()
	defer pool.Close()

	repo := postgres.NewNewsRepository(pool)

	newsUC := news.New(repo)
	reviewUC := review.New(repo, repo, time.Now)
	ingestUC := ingest.New(repo)

	newsH := rest.NewNewsHandler(newsUC)
	adminH, err := admin.NewHandler(reviewUC)
	if err != nil {
		return fmt.Errorf("build admin handler: %w", err)
	}
	subscriberH := subscriber.NewArticleCollectedHandler(ingestUC)
	articleCollectedPushH := pubsubpush.NewHandler(subscriberH.Handle)

	authVerifier := internalauth.NewVerifier(
		internalauth.StaticHS256Resolver([]byte(cfg.InternalAuthSecret), internalauth.DefaultKeyID),
	)

	publicSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.InternalPort),
		Handler:           router.NewPublic(newsH, authVerifier, articleCollectedPushH),
		ReadHeaderTimeout: 10 * time.Second,
	}
	adminSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.AdminPort),
		Handler:           router.NewAdmin(cfg.Env, adminH),
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("listening",
		"public_addr", publicSrv.Addr,
		"admin_addr", adminSrv.Addr,
		"env", cfg.Env,
	)

	return runAll(ctx, publicSrv, adminSrv)
}

// setupLogger は env に応じて slog のハンドラを設定する。
// production は Cloud Logging 互換の JSON、それ以外は開発者向けのテキスト。
func setupLogger(env config.Env) error {
	switch env {
	case config.EnvProduction, config.EnvStaging:
		slog.SetDefault(slog.New(newCloudLoggingHandler(os.Stdout)).With("service", "news"))
	case config.EnvLocal:
		h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		slog.SetDefault(slog.New(h).With("service", "news"))
	default:
		return fmt.Errorf("unexpected ENV: %s", env)
	}
	return nil
}

// newCloudLoggingHandler は Cloud Logging が認識するフィールド名に slog の属性をリネームする。
func newCloudLoggingHandler(w io.Writer) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				a.Key = "severity"
				if lvl, ok := a.Value.Any().(slog.Level); ok {
					switch {
					case lvl >= slog.LevelError:
						a.Value = slog.StringValue("ERROR")
					case lvl >= slog.LevelWarn:
						a.Value = slog.StringValue("WARNING")
					case lvl >= slog.LevelInfo:
						a.Value = slog.StringValue("INFO")
					default:
						a.Value = slog.StringValue("DEBUG")
					}
				}
			}
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			return a
		},
	})
}

// runAll は 2 つの HTTP server を並行起動し、いずれかの失敗・シグナルで全員を停止させる。
func runAll(ctx context.Context, publicSrv, adminSrv *http.Server) error {
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if err := publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("public http server: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("admin http server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := publicSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("public http shutdown: %w", err)
		}
		if err := adminSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("admin http shutdown: %w", err)
		}
		return nil
	})

	return g.Wait()
}
