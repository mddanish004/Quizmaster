package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func testHandler(t *testing.T) (*Handler, *chi.Mux) {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURL(t))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	cfg := &config.Config{
		Env: "test", Origin: "http://localhost:5173",
		SessionCookieName: "__Host-qf_session",
		SessionIdleTTL:    24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
		CSRFSecret:       "test-csrf-secret-0123456789abcdef",
		RateLimitEnabled: false, MockOIDC: true,
		GoogleRedirectURL: "http://localhost:8080/api/v1/auth/google/callback",
	}
	h := NewHandler(NewService(cfg, pool, &MockMailer{}, FakeOIDCVerifier{}), cfg)
	r := chi.NewRouter()
	r.Route("/api/v1", h.Routes)
	return h, r
}

func doReq(t *testing.T, mux http.Handler, method, path, body string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHTTPRegisterLoginMeLogout(t *testing.T) {
	_, mux := testHandler(t)
	rec := doReq(t, mux, "POST", "/api/v1/auth/register",
		`{"email":"http@example.com","username":"http_u","displayName":"Http","password":"http-long-passphrase-1234","extra":"hol"}`, nil, "")
	// Unknown field must be rejected (DisallowUnknownFields).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, mux, "POST", "/api/v1/auth/register",
		`{"email":"http@example.com","username":"http_u","displayName":"Http","password":"http-long-passphrase-1234"}`, nil, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}
	c := cookies[0]
	if c.Name != "__Host-qf_session" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("weak cookie attributes: %+v", c)
	}
	var decoded struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			CSRFToken string `json:"csrfToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Data.CSRFToken == "" {
		t.Fatal("missing csrf")
	}
	// /me with cookie.
	rec = doReq(t, mux, "GET", "/api/v1/me", "", cookies, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	// Logout without CSRF → 403.
	rec = doReq(t, mux, "POST", "/api/v1/auth/logout", "", cookies, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected CSRF 403, got %d", rec.Code)
	}
	// Logout with CSRF → 204.
	rec = doReq(t, mux, "POST", "/api/v1/auth/logout", "", cookies, decoded.Data.CSRFToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body.String())
	}
	// /me now 401.
	rec = doReq(t, mux, "GET", "/api/v1/me", "", cookies, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", rec.Code)
	}
}

func TestHTTPLoginGenericErrorAndMeUnauthorized(t *testing.T) {
	_, mux := testHandler(t)
	rec := doReq(t, mux, "POST", "/api/v1/auth/login", `{"identifier":"nobody@example.com","password":"whatever-long-passphrase"}`, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.Error.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("code: %s", env.Error.Code)
	}
	rec = doReq(t, mux, "GET", "/api/v1/me", "", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
