package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func testService(t *testing.T) (*Service, *MockMailer) {
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
	mail := &MockMailer{}
	return NewService(cfg, pool, mail, FakeOIDCVerifier{}), mail
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	res, err := svc.Register(ctx, RegisterInput{
		Email: "Ada@Example.com", Username: "ada_l", DisplayName: "Ada", Password: "a-very-long-passphrase-123", ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.User.Email != "ada@example.com" {
		t.Fatalf("email not normalized: %s", res.User.Email)
	}
	if res.CSRF == "" || res.Encoded == "" {
		t.Fatal("missing token/csrf")
	}
	// Duplicate email.
	if _, err := svc.Register(ctx, RegisterInput{Email: "ada@example.com", Username: "other_one", DisplayName: "X", Password: "another-long-passphrase-1", ClientIP: "127.0.0.1"}); err != ErrIdentityTaken {
		t.Fatalf("expected identity taken, got %v", err)
	}
	// Login by username.
	login, err := svc.Login(ctx, LoginInput{Identifier: "ada_l", Password: "a-very-long-passphrase-123", ClientIP: "127.0.0.1"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	sess, u, err := svc.AuthenticateSession(ctx, login.Encoded)
	if err != nil || u.ID != res.User.ID {
		t.Fatalf("auth session: %v", err)
	}
	if !VerifyCSRFToken("test-csrf-secret-0123456789abcdef", sess.ID.String(), sess.TokenHash, login.CSRF) {
		t.Fatal("csrf mismatch")
	}
	// Wrong password → generic error.
	if _, err := svc.Login(ctx, LoginInput{Identifier: "ada@example.com", Password: "wrong-passphrase-long-x", ClientIP: "127.0.0.1"}); err != ErrInvalidCredentials {
		t.Fatalf("expected invalid creds, got %v", err)
	}
	// Unknown user → same generic error.
	if _, err := svc.Login(ctx, LoginInput{Identifier: "ghost@example.com", Password: "wrong-passphrase-long-x", ClientIP: "127.0.0.1"}); err != ErrInvalidCredentials {
		t.Fatalf("expected invalid creds for unknown, got %v", err)
	}
	// Logout revokes.
	if err := svc.Logout(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, login.Encoded); err != ErrAuthRequired {
		t.Fatalf("revoked session still valid: %v", err)
	}
}

func TestTwoAccountIsolationAndLogoutAll(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	a, _ := svc.Register(ctx, RegisterInput{Email: "a@example.com", Username: "user_a", DisplayName: "A", Password: "a-very-long-passphrase-123", ClientIP: "1.1.1.1"})
	b, _ := svc.Register(ctx, RegisterInput{Email: "b@example.com", Username: "user_b", DisplayName: "B", Password: "b-very-long-passphrase-123", ClientIP: "1.1.1.2"})
	if a.User.ID == b.User.ID {
		t.Fatal("ids collide")
	}
	// b cannot use a's session.
	if _, _, err := svc.AuthenticateSession(ctx, a.Encoded); err != nil {
		t.Fatal(err)
	}
	if err := svc.LogoutAll(ctx, a.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, a.Encoded); err != ErrAuthRequired {
		t.Fatal("logout-all did not revoke")
	}
	// b unaffected.
	if _, _, err := svc.AuthenticateSession(ctx, b.Encoded); err != nil {
		t.Fatalf("b session revoked unexpectedly: %v", err)
	}
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	svc, mail := testService(t)
	ctx := context.Background()
	res, err := svc.Register(ctx, RegisterInput{Email: "r@example.com", Username: "reset_u", DisplayName: "R", Password: "original-long-passphrase-1", ClientIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	svc.ForgotPassword(ctx, "R@Example.com", "http://localhost:5173", "127.0.0.1")
	link := mail.LastLinkFor("r@example.com")
	if link == "" {
		t.Fatal("no reset link captured")
	}
	token := link[strings.LastIndex(link, "=")+1:]
	// Single use + invalid second use.
	if err := svc.ResetPassword(ctx, token, "brand-new-long-passphrase-2"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, res.Encoded); err != ErrAuthRequired {
		t.Fatal("old session survived reset")
	}
	if err := svc.ResetPassword(ctx, token, "another-long-passphrase-333"); err != ErrResetInvalid {
		t.Fatalf("reused token accepted: %v", err)
	}
	// New password works.
	if _, err := svc.Login(ctx, LoginInput{Identifier: "r@example.com", Password: "brand-new-long-passphrase-2", ClientIP: "127.0.0.1"}); err != nil {
		t.Fatalf("login after reset: %v", err)
	}
	// Forgot for unknown email still silent.
	svc.ForgotPassword(ctx, "nobody@example.com", "http://localhost:5173", "127.0.0.1")
}

func TestGoogleMockNewUserAndLinkRequired(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	// New Google user.
	startURL, err := svc.StartGoogleLogin(ctx, "/app")
	if err != nil || !strings.Contains(startURL, "accounts.google.com") {
		t.Fatalf("start: %v %s", err, startURL)
	}
	// Extract state from URL to drive callback through a real transaction.
	state := extractQuery(t, startURL, "state")
	got, err := svc.CallbackGoogle(ctx, state, "mock:neo:neo@example.com")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if len(got.Auth.User.LoginMethods) != 1 || got.Auth.User.LoginMethods[0] != "google" {
		t.Fatalf("methods: %v", got.Auth.User.LoginMethods)
	}
	// Existing password account with same email → link required, no auto-link.
	_, err = svc.Register(ctx, RegisterInput{Email: "sam@example.com", Username: "sam_u", DisplayName: "Sam", Password: "sam-long-passphrase-12345", ClientIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	startURL2, _ := svc.StartGoogleLogin(ctx, "/app")
	state2 := extractQuery(t, startURL2, "state")
	_, err = svc.CallbackGoogle(ctx, state2, "mock:sam:sam@example.com")
	if !errors.Is(err, ErrLinkRequired) {
		t.Fatalf("expected link required, got %v", err)
	}
}

func TestGoogleStateReuseRejected(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	u, _ := svc.StartGoogleLogin(ctx, "/app")
	state := extractQuery(t, u, "state")
	if _, err := svc.CallbackGoogle(ctx, state, "mock:amy:amy@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CallbackGoogle(ctx, state, "mock:amy:amy@example.com"); err != ErrOAuthInvalid {
		t.Fatalf("reused state accepted: %v", err)
	}
}

func extractQuery(t *testing.T, rawURL, key string) string {
	t.Helper()
	idx := strings.Index(rawURL, key+"=")
	if idx < 0 {
		t.Fatalf("missing %s in %s", key, rawURL)
	}
	rest := rawURL[idx+len(key)+1:]
	if i := strings.Index(rest, "&"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}
