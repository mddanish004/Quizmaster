// Command api runs the QuizForge modular monolith.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/database"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/httpserver"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/logging"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		healthcheckMain()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "janitor" {
		janitorMain()
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		pool, err = database.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns, cfg.DBMaxConnLifetime, cfg.DBMaxConnIdleTime)
		if err != nil {
			log.Warn("database unavailable at startup; readiness will fail", "error", err)
		} else {
			defer pool.Close()
		}
	} else {
		log.Warn("DATABASE_URL empty; running without database (health only)")
	}

	// Wire modules once: REST and WebSocket handlers must share one live
	// Manager, otherwise presence broadcasts and recovery run doubled/lost.
	w := wire(pool, cfg)
	handler := httpserver.NewRouter(httpserver.Deps{
		Pool:    pool,
		Version: firstNonEmpty(version, cfg.BuildVersion),
		Commit:  firstNonEmpty(commit, cfg.BuildCommit), ReadyCheck: func(ctx context.Context) error {
			if pool == nil {
				return errors.New("no database pool")
			}
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				return err
			}
			var v int
			if err := pool.QueryRow(ctx, `select count(*) from app_user`).Scan(&v); err != nil {
				return fmt.Errorf("migration check: %w", err)
			}
			return nil
		},
		ExtraRoutes: RegisterRoutes(w, log),
		RootRoutes:  RegisterWSRoutes(w, log),
		StaticDir:   os.Getenv("STATIC_DIR"),
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
