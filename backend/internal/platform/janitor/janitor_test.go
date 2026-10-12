package janitor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRetentionBoundaries seeds expired + fresh rows across every table and
// asserts the janitor removes exactly the expired set, preserves live data,
// and is idempotent on repeat.
func TestRetentionBoundaries(t *testing.T) {
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "JANITOR"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	ctx := context.Background()

	uid := uuid.New()
	old := time.Now().Add(-40 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'jan@test.local','janitor_u','Jan')`, uid); err != nil {
		t.Fatal(err)
	}
	// OAuth: one expired, one fresh.
	if _, err := pool.Exec(ctx, `INSERT INTO oauth_transaction(id,state_hash,nonce_hash,pkce_verifier_ciphertext,expires_at) VALUES($1,$2,$3,'v',now()-interval '1 hour'),($4,$5,$6,'v',now()+interval '1 hour')`,
		uuid.New(), bytes32(1), bytes32(4), uuid.New(), bytes32(7), bytes32(10)); err != nil {
		t.Fatal(err)
	}
	// Reset tokens: consumed-old (purge), fresh-active (keep).
	if _, err := pool.Exec(ctx, `INSERT INTO password_reset_token(id,user_id,token_hash,expires_at,consumed_at) VALUES($1,$2,$3,now()+interval '1 hour',NULL),($4,$2,$5,now()+interval '1 hour',now()-interval '2 days')`,
		uuid.New(), uid, bytes32(1), uuid.New(), bytes32(2)); err != nil {
		t.Fatal(err)
	}
	// Sessions: revoked 40d ago (purge), active (keep).
	if _, err := pool.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at,revoked_at) VALUES($1,$2,$3,now()+interval '1 hour',now()+interval '1 day',$4),($5,$2,$6,now()+interval '1 hour',now()+interval '1 day',NULL)`,
		uuid.New(), uid, bytes32(1), old, uuid.New(), bytes32(2)); err != nil {
		t.Fatal(err)
	}
	// Quiz trash: deleted 40d ago (purge) with a question, fresh draft (keep).
	oldQ, freshQ := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quiz(id,owner_user_id,title,deleted_at) VALUES($1,$2,'old',$3),($4,$2,'fresh',NULL)`, oldQ, uid, old, freshQ); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO question(id,quiz_id,position,type,prompt,difficulty) VALUES($1,$2,0,'multiple_choice','Q?','easy')`, uuid.New(), oldQ); err != nil {
		t.Fatal(err)
	}
	// Ended session 40d ago with two events (purge events only, keep session+snapshot).
	sess := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO live_session(id,host_user_id,quiz_version,join_code,status,state_version,expires_at,ended_at) VALUES($1,$2,1,'AAAAAA','ended',2,now()+interval '1 hour',$3)`, sess, uid, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO session_event(live_session_id,version,event_type) VALUES($1,1,'session_started'),($1,2,'session_ended')`, sess); err != nil {
		t.Fatal(err)
	}
	// AI generation 100d ago (purge) — quiz_id nullable so no quiz needed.
	if _, err := pool.Exec(ctx, `INSERT INTO ai_generation(id,user_id,provider,model,prompt_template,status,request_fingerprint,created_at) VALUES($1,$2,'mock','m','quiz-v1.0.0','failed','fp',now()-interval '100 days')`, uuid.New(), uid); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(ctx, pool, DefaultPolicy(), testLogger())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.OAuthTransactions != 1 || rep.ResetTokens != 1 || rep.SessionsAudited != 1 || rep.QuizzesPurged != 1 || rep.SessionEvents != 2 || rep.AIGenerations != 1 {
		t.Fatalf("report: %+v", rep)
	}
	assertCount(t, pool, ctx, "oauth_transaction", 1)
	assertCount(t, pool, ctx, "password_reset_token", 1)
	assertCount(t, pool, ctx, "auth_session", 1)
	assertCount(t, pool, ctx, "quiz", 1)
	assertCount(t, pool, ctx, "question", 0)
	assertCount(t, pool, ctx, "live_session", 1) // history preserved
	assertCount(t, pool, ctx, "session_event", 0)
	assertCount(t, pool, ctx, "ai_generation", 0)
	assertCount(t, pool, ctx, "app_user", 1) // owner preserved

	// Repeat: idempotent, removes nothing more.
	rep2, err := Run(ctx, pool, DefaultPolicy(), testLogger())
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if rep2.OAuthTransactions+rep2.ResetTokens+rep2.SessionsAudited+rep2.QuizzesPurged+rep2.SessionEvents+rep2.AIGenerations != 0 {
		t.Fatalf("not idempotent: %+v", rep2)
	}
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	out[0] = b
	return out
}

func assertCount(t *testing.T, pool *pgxpool.Pool, ctx context.Context, table string, want int) {
	t.Helper()
	var n int
	// Table name is a fixed test constant, never user input.
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("%s: got %d, want %d", table, n, want)
	}
}
