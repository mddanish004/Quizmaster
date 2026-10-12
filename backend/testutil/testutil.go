// Package testutil provisions real PostgreSQL state for integration tests.
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDatabaseURL returns the integration database URL or skips the test.
func TestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://quizforge:quizforge@localhost:5432/quizforge_test?sslmode=disable"
	}
	return url
}

// TestDatabaseURLFor returns a per-package database URL so parallel test
// packages never truncate each other's tables. Falls back to TestDatabaseURL.
func TestDatabaseURLFor(t *testing.T, name string) string {
	t.Helper()
	if url := os.Getenv("TEST_DATABASE_URL_" + name); url != "" {
		return url
	}
	return TestDatabaseURL(t)
}

// OpenPool opens a pool to the test database.
func OpenPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse test db url: %v", err)
	}
	cfg.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// MigrateUp applies all *.up.sql migrations in order.
func MigrateUp(t *testing.T, pool *pgxpool.Pool, migrationsDir string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no migrations in %s", migrationsDir)
	}
	sort.Strings(entries)
	ctx := context.Background()
	for _, f := range entries {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil && !isIdempotentError(err) {
			t.Fatalf("apply %s: %v", f, err)
		}
	}
}

// TruncateAll wipes domain tables between tests (auth phase tables only).
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `TRUNCATE app_user CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func isIdempotentError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already exists")
}

// MigrationsDir locates database/migrations from backend module tests.
func MigrationsDir(t *testing.T) string {
	t.Helper()
	for _, c := range []string{"../../database/migrations", "../../../database/migrations", "/home/mddanish/quiz/database/migrations"} {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	t.Fatal("migrations dir not found")
	return ""
}
