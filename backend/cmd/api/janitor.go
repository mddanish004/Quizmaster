package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/database"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/janitor"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/logging"
)

// janitorMain runs one bounded retention-cleanup pass and exits.
// Schedule daily as a Render cron job with command `/quizforge janitor`
// and the same env group as the web service.
func janitorMain() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "janitor config:", err)
		os.Exit(1)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "janitor: DATABASE_URL is empty")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.DatabaseURL, 2, 0, 5*time.Minute, time.Minute)
	if err != nil {
		fmt.Fprintln(os.Stderr, "janitor database:", err)
		os.Exit(1)
	}
	defer pool.Close()
	rep, err := janitor.Run(ctx, pool, janitor.DefaultPolicy(), log)
	if err != nil {
		log.Error("janitor partial failure", "error", err)
		os.Exit(1)
	}
	log.Info("janitor complete",
		slog.Int64("oauth_transactions", rep.OAuthTransactions),
		slog.Int64("reset_tokens", rep.ResetTokens),
		slog.Int64("revoked_sessions", rep.SessionsAudited),
		slog.Int64("idempotency_keys", rep.IdempotencyKeys),
		slog.Int64("session_events", rep.SessionEvents),
		slog.Int64("ai_generations", rep.AIGenerations),
		slog.Int64("quizzes_purged", rep.QuizzesPurged))
}
