package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/httpserver"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

// TestSharedLiveManager proves REST-triggered broadcasts (join presence)
// reach WebSocket clients: both route trees must share one live Manager.
// Regression test for the double-wire bug (two Managers: presence lost,
// recovery/timers doubled).
func TestSharedLiveManager(t *testing.T) {
	pool := testutil.OpenPool(t, testutil.TestDatabaseURL(t))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	ctx := context.Background()
	host := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'wire@test.local','wire_host','Wire')`, host); err != nil {
		t.Fatal(err)
	}
	raw, enc, _ := auth.NewOpaqueToken()
	if _, err := pool.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at) VALUES($1,$2,$3,now()+interval '1 hour',now()+interval '1 day')`,
		uuid.New(), host, auth.SHA256Hash(raw)); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "test", Origin: "http://placeholder.invalid",
		SessionCookieName: "__Host-qf_session", ParticipantCookieName: "__Host-qf_participant",
		CSRFSecret:     "test-csrf-secret-0123456789abcdef",
		SessionIdleTTL: 24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
		LiveSessionMaxTTL: 6 * time.Hour, WSAllowedOrigins: []string{},
		RateLimitEnabled: false, MockOIDC: true,
	}
	w := wire(pool, cfg)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := httpserver.NewRouter(httpserver.Deps{Version: "test", Commit: "test", ExtraRoutes: RegisterRoutes(w, log), RootRoutes: RegisterWSRoutes(w, log)})
	srv := httptest.NewServer(h)
	defer srv.Close()
	origin := srv.URL
	w.live.Manager().AllowOrigin(origin)

	quizSvc := quiz.NewService(pool)
	q, _ := quizSvc.CreateBlank(ctx, host, "Wire Quiz", "")
	upd, _ := quizSvc.Update(ctx, q.ID, host, 1, "Wire Quiz", "", []quiz.Question{{
		Type: "multiple_choice", Position: 0, Prompt: "Q?", Explanation: "E",
		Difficulty: "easy", TimeLimitSeconds: 60, ScoringMode: "fixed", BasePoints: 500,
		Options: []quiz.Option{{Position: 0, Text: "A", IsCorrect: false}, {Position: 1, Text: "B", IsCorrect: true}},
	}})
	pub, _ := quizSvc.Publish(ctx, q.ID, host, upd.Version)
	sess, err := w.live.Service().StartSession(ctx, pub.ID, host, true, true)
	if err != nil {
		t.Fatal(err)
	}

	// Host dials WS first.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/sessions/" + sess.ID.String()
	header := http.Header{"Cookie": {"__Host-qf_session=" + enc}, "Origin": {origin}}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer hostConn.Close(websocket.StatusNormalClosure, "done")
	rctx, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	if _, _, err := hostConn.Read(rctx); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Participant joins via REST (different route tree, same process).
	jb, _ := json.Marshal(map[string]string{"displayName": "Wirep"})
	jreq, _ := http.NewRequest("POST", srv.URL+"/api/v1/join/"+sess.JoinCode, strings.NewReader(string(jb)))
	jreq.Header.Set("Content-Type", "application/json")
	jresp, err := http.DefaultClient.Do(jreq)
	if err != nil {
		t.Fatal(err)
	}
	jresp.Body.Close()
	if jresp.StatusCode != 201 {
		t.Fatalf("join: %d", jresp.StatusCode)
	}

	// Host must observe the presence broadcast on its socket.
	found := false
	for i := 0; i < 5 && !found; i++ {
		rctx, cancel3 := context.WithTimeout(ctx, 5*time.Second)
		_, raw, err := hostConn.Read(rctx)
		cancel3()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var env map[string]any
		_ = json.Unmarshal(raw, &env)
		if env["type"] == "participant_count_changed" {
			found = true
		}
	}
	if !found {
		t.Fatal("host never received participant_count_changed: REST and WS do not share a live Manager")
	}
}
