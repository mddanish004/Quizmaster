package auth

import (
	"context"
	"crypto"
	"crypto/sha256"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func hashSHA256(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

const cryptoHashSHA256 = crypto.SHA256

func oidcTestService(t *testing.T) *Service {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURL(t))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	cfg := &config.Config{
		Env: "test", Origin: "http://localhost:5173",
		SessionIdleTTL: 24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
		CSRFSecret:       "test-csrf-secret-0123456789abcdef",
		RateLimitEnabled: false, MockOIDC: true,
		GoogleRedirectURL: "http://localhost:8080/api/v1/auth/google/callback",
	}
	return NewService(cfg, pool, &MockMailer{}, FakeOIDCVerifier{})
}

func oidcTestServiceWithVerifier(t *testing.T, pool *pgxpool.Pool, v OIDCVerifier) *Service {
	t.Helper()
	cfg := &config.Config{
		Env: "test", Origin: "http://localhost:5173",
		SessionIdleTTL: 24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
		CSRFSecret:       "test-csrf-secret-0123456789abcdef",
		RateLimitEnabled: false, MockOIDC: false,
		GoogleClientID:    "test-client",
		GoogleRedirectURL: "http://localhost/cb",
	}
	return NewService(cfg, pool, &MockMailer{}, v)
}

func testutil_OpenPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURL(t))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	return pool
}

func registerPasswordUser(t *testing.T, svc *Service, ctx context.Context, email string) (User, error) {
	t.Helper()
	base := strings.ReplaceAll(strings.Split(email, "@")[0], ".", "_")
	res, err := svc.Register(ctx, RegisterInput{
		Email: email, Username: base + "_u", DisplayName: "Link User",
		Password: "a-very-long-passphrase-123", ClientIP: "127.0.0.1",
	})
	if err != nil {
		return User{}, err
	}
	return res.User, nil
}

func queryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	v := u.Query().Get(key)
	if v == "" {
		t.Fatalf("missing %s in %s", key, rawURL)
	}
	_ = uuid.Nil
	return v
}

// txNonceForState reads the stored transaction nonce (what the real Google
// flow echoes inside the signed ID token).
func txNonceForState(t *testing.T, svc *Service, ctx context.Context, state string) string {
	t.Helper()
	raw, err := decodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	sh := hashSHA256(raw)
	var nonce string
	if err := svc.store.pool.QueryRow(ctx, `SELECT COALESCE(nonce_text,'') FROM oauth_transaction WHERE state_hash=$1`, sh).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	if nonce == "" {
		t.Fatal("no stored nonce")
	}
	return nonce
}
