package live

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

// TestJoinRateLimited proves guest joins are throttled per IP (DB uniqueness
// stays authoritative; the limiter only sheds floods).
func TestJoinRateLimited(t *testing.T) {
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "LIVE"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	ctx := context.Background()
	host := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'rl@test.local','rl_host','RL')`, host); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "test", Origin: "http://localhost:5173",
		ParticipantCookieName: "__Host-qf_participant",
		LiveSessionMaxTTL:     6 * time.Hour, WSAllowedOrigins: []string{"http://localhost:5173"},
		RateLimitEnabled: true,
	}
	authSvc := auth.NewService(cfg, pool, &auth.MockMailer{}, auth.FakeOIDCVerifier{})
	authH := auth.NewHandler(authSvc, cfg)
	svc := NewService(cfg, pool)
	h := NewHandler(svc, authH, authSvc, cfg)

	quizSvc := quiz.NewService(pool)
	q, _ := quizSvc.CreateBlank(ctx, host, "RL Quiz", "")
	upd, _ := quizSvc.Update(ctx, q.ID, host, 1, "RL Quiz", "", []quiz.Question{{
		Type: "multiple_choice", Position: 0, Prompt: "Q?", Explanation: "E",
		Difficulty: "easy", TimeLimitSeconds: 60, ScoringMode: "fixed", BasePoints: 500,
		Options: []quiz.Option{{Position: 0, Text: "A", IsCorrect: false}, {Position: 1, Text: "B", IsCorrect: true}},
	}})
	pub, _ := quizSvc.Publish(ctx, q.ID, host, upd.Version)
	sess, err := svc.StartSession(ctx, pub.ID, host, true, true)
	if err != nil {
		t.Fatal(err)
	}
	limited := false
	for i := 0; i < 25; i++ {
		rec := httptestNew(t, h, sess.JoinCode, i)
		if rec == 429 {
			limited = true
			break
		}
		if rec != 201 {
			t.Fatalf("join %d: %d", i, rec)
		}
	}
	if !limited {
		t.Fatal("join flood never throttled")
	}
}

func httptestNew(t *testing.T, h *Handler, code string, i int) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/join/"+code, strings.NewReader(`{"displayName":"Flood`+itoa(i)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1234"
	// Direct handler invocation bypasses chi routing: inject the path param.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", code)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Join()(rec, req)
	return rec.Code
}

// TestSocketFloodThrottled proves per-socket message limits shed floods
// without closing healthy clients.
func TestSocketFloodThrottled(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, err := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := dialWS(t, f, sess.ID, f.hostCk)
	defer conn.Close(websocket.StatusNormalClosure, "done")
	_ = readEnv(t, conn) // snapshot
	// Burst 60 pings; some must be throttled with RATE_LIMITED, socket stays open.
	throttled := 0
	pongs := 0
	for i := 0; i < 60; i++ {
		raw, _ := json.Marshal(Envelope{V: 1, Type: "client.ping", SessionID: sess.ID.String(), SentAt: time.Now(), Payload: map[string]any{}})
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := conn.Write(wctx, websocket.MessageText, raw); err != nil {
			cancel()
			t.Fatalf("write %d: %v", i, err)
		}
		cancel()
	}
	// Exactly 60 responses are expected (pong or RATE_LIMITED each). Never let
	// a read context expire: coder/websocket closes the connection when a Read
	// context is cancelled, which would look like a dead socket.
	for i := 0; i < 60; i++ {
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, raw, err := conn.Read(rctx)
		cancel()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		var env Envelope
		_ = json.Unmarshal(raw, &env)
		switch env.Type {
		case "pong":
			pongs++
		case "session_error":
			if code, _ := env.Payload["code"].(string); code == "RATE_LIMITED" {
				throttled++
			} else {
				t.Fatalf("unexpected error: %v", env.Payload)
			}
		default:
			t.Fatalf("unexpected message: %s", env.Type)
		}
	}
	if throttled == 0 {
		t.Fatal("message flood never throttled")
	}
	t.Logf("pongs=%d throttled=%d", pongs, throttled)
	// Socket still usable: resync works.
	raw, _ := json.Marshal(Envelope{V: 1, Type: "client.resync", SessionID: sess.ID.String(), SentAt: time.Now(), Payload: map[string]any{"lastVersion": 1}})
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.Write(wctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("socket dead after flood: %v", err)
	}
	_ = strings.ToLower
}
