package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/kenyamaneko/overload-party-news/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-news/internal/config"
	"github.com/kenyamaneko/overload-party-news/internal/handler/admin"
	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/handler/subscriber"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/repository/postgres"
	"github.com/kenyamaneko/overload-party-news/internal/router"
	"github.com/kenyamaneko/overload-party-news/internal/service/ingest"
	"github.com/kenyamaneko/overload-party-news/internal/service/news"
	"github.com/kenyamaneko/overload-party-news/internal/service/review"
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

	pool, err := pgxpool.New(ctx, cfg.DatabaseConn)
	if err != nil {
		return fmt.Errorf("pgxpool new: %w", err)
	}
	defer pool.Close()

	repo := postgres.NewNewsRepository(pool)

	newsSvc := news.New(repo)
	reviewSvc := review.New(repo, repo, time.Now)
	ingestSvc := ingest.New(repo)

	newsH := rest.NewNewsHandler(newsSvc)
	adminH, err := admin.NewHandler(reviewSvc)
	if err != nil {
		return fmt.Errorf("build admin handler: %w", err)
	}
	subscriberH := subscriber.NewArticleCollectedHandler(ingestSvc)

	stream, err := pubsub.NewStream(ctx, cfg.GoogleCloudProject, cfg.NewsArticleCollectedSubscription)
	if err != nil {
		return fmt.Errorf("build stream: %w", err)
	}
	defer func() {
		if cerr := stream.Close(); cerr != nil {
			slog.Error("stream close failed", "error", cerr)
		}
	}()

	internalSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.InternalPort),
		Handler:           router.NewInternal(newsH),
		ReadHeaderTimeout: 10 * time.Second,
	}
	adminSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.AdminPort),
		Handler:           router.NewAdmin(cfg.Env, adminH),
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("listening",
		"internal_addr", internalSrv.Addr,
		"admin_addr", adminSrv.Addr,
		"env", cfg.Env,
		"cloud_project", cfg.GoogleCloudProject,
		"subscription", cfg.NewsArticleCollectedSubscription,
	)

	return runAll(ctx, internalSrv, adminSrv, stream, subscriberH.Handle)
}

// setupLogger は env に応じて slog のハンドラを設定する。
// production は Cloud Logging 互換の JSON、それ以外は開発者向けのテキスト。
func setupLogger(env config.Env) error {
	switch env {
	case config.EnvProduction, config.EnvStaging:
		slog.SetDefault(slog.New(newCloudLoggingHandler()).With("service", "news"))
	case config.EnvLocal:
		h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		slog.SetDefault(slog.New(h).With("service", "news"))
	default:
		return fmt.Errorf("unexpected ENV: %s", env)
	}
	return nil
}

// newCloudLoggingHandler は Cloud Logging が認識するフィールド名に slog の属性をリネームする。
func newCloudLoggingHandler() slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
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

// runAll は 2 つの HTTP server と Pub/Sub stream を並行起動し、
// いずれかの失敗・シグナルで全員を停止させる。
func runAll(ctx context.Context, internalSrv, adminSrv *http.Server, stream *pubsub.Stream, handle port.MessageHandler) error {
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if err := internalSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("internal http server: %w", err)
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
		return stream.Consume(gCtx, handle)
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := internalSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("internal http shutdown: %w", err)
		}
		if err := adminSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("admin http shutdown: %w", err)
		}
		return nil
	})

	return g.Wait()
}
