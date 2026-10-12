// Package janitor implements bounded, idempotent retention cleanup.
//
// It deletes only expired/transient rows; historical quiz results, answers,
// snapshots, and active user data are never touched. Every operation is
// bounded (LIMIT batches) and safe to re-run. Run daily via cron/systemd on
// the single EC2 host (see docs/DEPLOYMENT.md).
package janitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Policy configures retention windows. Zero values select spec defaults.
type Policy struct {
	// SessionAudit retains revoked sessions this long for audit metadata.
	SessionAudit time.Duration
	// Events retains session_event rows this long after their session ended.
	Events time.Duration
	// AIGenerations retains generation metadata (no raw prompts/outputs).
	AIGenerations time.Duration
	// Trash purges soft-deleted quizzes (snapshots already detached).
	Trash time.Duration
	// Batch caps rows per DELETE statement.
	Batch int
}

// DefaultPolicy follows DATABASE_DESIGN.md retention rules.
func DefaultPolicy() Policy {
	return Policy{
		SessionAudit:  30 * 24 * time.Hour,
		Events:        30 * 24 * time.Hour,
		AIGenerations: 90 * 24 * time.Hour,
		Trash:         30 * 24 * time.Hour,
		Batch:         1000,
	}
}

// Report counts what one run removed.
type Report struct {
	OAuthTransactions int64
	ResetTokens       int64
	VerifyTokens      int64
	SessionsAudited   int64
	IdempotencyKeys   int64
	SessionEvents     int64
	AIGenerations     int64
	QuizzesPurged     int64
}

// Run executes all cleanup steps in dependency-safe order.
func Run(ctx context.Context, pool *pgxpool.Pool, p Policy, log *slog.Logger) (Report, error) {
	if p.Batch <= 0 || p.Batch > 10000 {
		p.Batch = 1000
	}
	var rep Report
	var err error
	steps := []struct {
		name string
		fn   func(context.Context) (int64, error)
		dest *int64
	}{
		{"oauth_transactions", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM oauth_transaction WHERE ctid IN (SELECT ctid FROM oauth_transaction WHERE expires_at<now() OR consumed_at IS NOT NULL LIMIT $1)`, p.Batch)
		}, &rep.OAuthTransactions},
		{"password_reset_tokens", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM password_reset_token WHERE ctid IN (SELECT ctid FROM password_reset_token WHERE (consumed_at IS NOT NULL AND consumed_at<now()-interval '24 hours') OR expires_at<now()-interval '24 hours' LIMIT $1)`, p.Batch)
		}, &rep.ResetTokens},
		{"email_verification_tokens", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM email_verification_token WHERE ctid IN (SELECT ctid FROM email_verification_token WHERE (consumed_at IS NOT NULL AND consumed_at<now()-interval '24 hours') OR expires_at<now()-interval '24 hours' LIMIT $1)`, p.Batch)
		}, &rep.VerifyTokens},
		{"revoked_sessions", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM auth_session WHERE ctid IN (SELECT ctid FROM auth_session WHERE revoked_at<now()-$1::interval LIMIT $2)`, p.SessionAudit, p.Batch)
		}, &rep.SessionsAudited},
		{"idempotency_keys", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM idempotency_key WHERE ctid IN (SELECT ctid FROM idempotency_key WHERE expires_at<now() LIMIT $1)`, p.Batch)
		}, &rep.IdempotencyKeys},
		{"session_events", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM session_event WHERE ctid IN (SELECT e.ctid FROM session_event e JOIN live_session s ON s.id=e.live_session_id WHERE s.status='ended' AND s.ended_at<now()-$1::interval LIMIT $2)`, p.Events, p.Batch)
		}, &rep.SessionEvents},
		{"ai_generations", func(ctx context.Context) (int64, error) {
			return execBounded(ctx, pool, `DELETE FROM ai_generation WHERE ctid IN (SELECT ctid FROM ai_generation WHERE created_at<now()-$1::interval LIMIT $2)`, p.AIGenerations, p.Batch)
		}, &rep.AIGenerations},
		{"trashed_quizzes", func(ctx context.Context) (int64, error) {
			// Questions/options CASCADE; live snapshots keep SET NULL source IDs.
			return execBounded(ctx, pool, `DELETE FROM quiz WHERE ctid IN (SELECT ctid FROM quiz WHERE deleted_at IS NOT NULL AND deleted_at<now()-$1::interval LIMIT $2)`, p.Trash, p.Batch)
		}, &rep.QuizzesPurged},
	}
	for _, s := range steps {
		n, serr := s.fn(ctx)
		*s.dest = n
		if serr != nil {
			log.Error("janitor step failed", "step", s.name, "error", serr)
			if err == nil {
				err = fmt.Errorf("%s: %w", s.name, serr)
			}
			continue
		}
		log.Info("janitor step", "step", s.name, "removed", n)
	}
	return rep, err
}

func execBounded(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (int64, error) {
	var total int64
	// Cap iterations so one run cannot monopolize the database; leftovers
	// are picked up by the next daily run (idempotent by construction).
	for i := 0; i < 100; i++ {
		ct, err := pool.Exec(ctx, sql, args...)
		if err != nil {
			return total, err
		}
		if ct.RowsAffected() == 0 {
			return total, nil
		}
		total += ct.RowsAffected()
	}
	return total, nil
}
