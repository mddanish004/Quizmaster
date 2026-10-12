package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func testHTTP(t *testing.T, p Provider) (*Handler, *chi.Mux, uuid.UUID, string, string) {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "AI"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	uid := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'aihttp@test.local','aihttp','AI')`, uid); err != nil {
		t.Fatal(err)
	}
	// Real session for the user.
	raw, enc, _ := auth.NewOpaqueToken()
	sessID := uuid.New()
	now := time.Now()
	if _, err := pool.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at) VALUES($1,$2,$3,$4,$5)`,
		sessID, uid, auth.SHA256Hash(raw), now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "test", Origin: "http://localhost:5173", SessionCookieName: "__Host-qf_session",
		CSRFSecret:       "test-csrf-secret-0123456789abcdef",
		AIDailyUserLimit: 5, AIDailyIPLimit: 20, AIMaxInFlight: 4,
		AIRequestTimeout: 5 * time.Second, AIEndpointTimeout: 10 * time.Second,
		RateLimitEnabled: false,
	}
	authSvc := auth.NewService(cfg, pool, &auth.MockMailer{}, auth.FakeOIDCVerifier{})
	authH := auth.NewHandler(authSvc, cfg)
	svc := NewService(cfg, pool, p, nil)
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(authH.RequireAuth)
			r.Use(authH.RequireCSRF)
			h.Routes(r)
		})
	})
	var tokenHash []byte
	_ = tokenHash
	csrf := auth.DeriveCSRFToken(cfg.CSRFSecret, sessID.String(), auth.SHA256Hash(raw))
	return h, r, uid, enc, csrf
}

func TestHTTPIdempotentReplay(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return validGQ(2), Usage{}, nil
	}}
	_, mux, _, enc, csrf := testHTTP(t, p)
	body := `{"topic":"HTTP replay topic","difficulty":"easy","questionCount":2,"targetAudience":"","additionalInstructions":"","language":"en"}`
	call := func() (int, []byte) {
		req := httptest.NewRequest("POST", "/api/v1/ai/generations", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "replay-key-1")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(&http.Cookie{Name: "__Host-qf_session", Value: enc})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	c1, b1 := call()
	c2, b2 := call()
	if c1 != 201 || c2 != 201 {
		t.Fatalf("codes: %d %d body2=%s", c1, c2, b2)
	}
	var d1, d2 map[string]any
	_ = json.Unmarshal(b1, &d1)
	_ = json.Unmarshal(b2, &d2)
	g1 := d1["data"].(map[string]any)["generation"].(map[string]any)["id"]
	g2 := d2["data"].(map[string]any)["generation"].(map[string]any)["id"]
	if g1 != g2 {
		t.Fatalf("replay created new generation: %v vs %v (calls=%d)", g1, g2, p.calls)
	}
	if p.calls != 1 {
		t.Fatalf("provider called %d times", p.calls)
	}
	// Same key, different body → 409.
	req := httptest.NewRequest("POST", "/api/v1/ai/generations", bytes.NewReader([]byte(`{"topic":"Different topic entirely here","difficulty":"easy","questionCount":2,"targetAudience":"","additionalInstructions":"","language":"en"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "replay-key-1")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(&http.Cookie{Name: "__Host-qf_session", Value: enc})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}
