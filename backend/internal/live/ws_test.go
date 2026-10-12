package live

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

type wsFixture struct {
	srv     *httptest.Server
	url     string
	svc     *Service
	handler *Handler
	host    uuid.UUID
	hostCk  *http.Cookie
	quiz    quiz.Quiz
}

func setupWS(t *testing.T) *wsFixture {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "LIVE"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	ctx := context.Background()
	host := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'wshost@test.local','ws_host','Host')`, host); err != nil {
		t.Fatal(err)
	}
	// Host session cookie.
	raw, enc, err := auth.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	sessID := uuid.New()
	now := time.Now()
	if _, err := pool.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at) VALUES($1,$2,$3,$4,$5)`,
		sessID, host, auth.SHA256Hash(raw), now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "test", SessionCookieName: "__Host-qf_session", ParticipantCookieName: "__Host-qf_participant",
		CSRFSecret: "test-csrf-secret-0123456789abcdef",
		Origin:     "http://placeholder.invalid", LiveSessionMaxTTL: 6 * time.Hour,
		WSAllowedOrigins: []string{}, SessionIdleTTL: 24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
	}
	authSvc := auth.NewService(cfg, pool, &auth.MockMailer{}, auth.FakeOIDCVerifier{})
	authH := auth.NewHandler(authSvc, cfg)
	svc := NewService(cfg, pool)
	h := NewHandler(svc, authH, authSvc, cfg)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/join/{code}", h.InspectJoin())
		r.Post("/join/{code}", h.Join())
		r.Get("/play/{sessionId}/snapshot", h.ParticipantSnapshot())
	})
	h.WSRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	// Allow the httptest origin for upgrades.
	origin := srv.URL
	h.Manager().mu.Lock()
	h.Manager().cfg.origins[origin] = true
	h.Manager().mu.Unlock()
	cfg.Origin = origin

	quizSvc := quiz.NewService(pool)
	q, err := quizSvc.CreateBlank(ctx, host, "WS Quiz", "")
	if err != nil {
		t.Fatal(err)
	}
	upd, err := quizSvc.Update(ctx, q.ID, host, 1, "WS Quiz", "", []quiz.Question{{
		Type: "multiple_choice", Position: 0, Prompt: "Q?", Explanation: "E",
		Difficulty: "easy", TimeLimitSeconds: 30, ScoringMode: "speed", BasePoints: 1000,
		Options: []quiz.Option{{Position: 0, Text: "A", IsCorrect: false}, {Position: 1, Text: "B", IsCorrect: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := quizSvc.Publish(ctx, q.ID, host, upd.Version)
	if err != nil {
		t.Fatal(err)
	}
	return &wsFixture{srv: srv, url: srv.URL, svc: svc, handler: h, host: host,
		hostCk: &http.Cookie{Name: "__Host-qf_session", Value: enc}, quiz: pub}
}

func dialWS(t *testing.T, f *wsFixture, sessionID uuid.UUID, cookies ...*http.Cookie) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(f.url, "http") + "/ws/v1/sessions/" + sessionID.String()
	header := http.Header{}
	for _, c := range cookies {
		header.Add("Cookie", c.Name+"="+c.Value)
	}
	origin := f.url
	header.Set("Origin", origin)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func readEnv(t *testing.T, conn *websocket.Conn) Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

func sendCmd(t *testing.T, conn *websocket.Conn, env Envelope) {
	t.Helper()
	raw, _ := json.Marshal(env)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestWSHostAndParticipantFlow(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, err := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	if err != nil {
		t.Fatal(err)
	}
	// Participant joins via REST to get the resume cookie.
	joinReq, _ := http.NewRequest("POST", f.url+"/api/v1/join/"+sess.JoinCode, strings.NewReader(`{"displayName":"Zoe"}`))
	joinReq.Header.Set("Content-Type", "application/json")
	joinResp, err := http.DefaultClient.Do(joinReq)
	if err != nil {
		t.Fatal(err)
	}
	defer joinResp.Body.Close()
	if joinResp.StatusCode != 201 {
		t.Fatalf("join: %d", joinResp.StatusCode)
	}
	var partCk *http.Cookie
	for _, c := range joinResp.Cookies() {
		if c.Name == "__Host-qf_participant" {
			partCk = c
		}
	}
	if partCk == nil {
		t.Fatal("no participant cookie")
	}

	hostConn := dialWS(t, f, sess.ID, f.hostCk)
	defer hostConn.Close(websocket.StatusNormalClosure, "done")
	if env := readEnv(t, hostConn); env.Type != "state_snapshot" {
		t.Fatalf("host first: %s", env.Type)
	}
	partConn := dialWS(t, f, sess.ID, partCk)
	defer partConn.Close(websocket.StatusNormalClosure, "done")
	if env := readEnv(t, partConn); env.Type != "state_snapshot" {
		t.Fatalf("participant first: %s", env.Type)
	}

	// Host starts the question.
	reqID := uuid.NewString()
	ver := int64(1)
	sendCmd(t, hostConn, Envelope{V: 1, Type: "host.start_question", SessionID: sess.ID.String(),
		RequestID: &reqID, Expected: &ver, Payload: map[string]any{"questionPosition": 0}, SentAt: time.Now()})
	// Host receives ack + broadcast in either order: the ack is written directly
	// while broadcasts fan out through the pump, so no ordering is promised
	// (clients correlate via requestId/stateVersion). Participant receives
	// question_started WITHOUT keys.
	gotAck, gotQ := false, false
	for i := 0; i < 4 && !(gotAck && gotQ); i++ {
		env := readEnv(t, hostConn)
		switch env.Type {
		case "command_ack":
			if env.RequestID == nil || *env.RequestID != reqID {
				t.Fatalf("ack requestId mismatch: %+v", env)
			}
			gotAck = true
		case "question_started":
			gotQ = true
		}
	}
	if !gotAck {
		t.Fatal("host never got command_ack")
	}
	if !gotQ {
		t.Fatal("host never got question_started")
	}
	pev := readEnv(t, partConn)
	if pev.Type != "question_started" {
		t.Fatalf("participant: %s", pev.Type)
	}
	qpayload := pev.Payload["question"].(map[string]any)
	opts := qpayload["options"].([]any)
	for _, o := range opts {
		if _, has := o.(map[string]any)["isCorrect"]; has {
			t.Fatal("participant payload leaks isCorrect before reveal")
		}
	}
	if _, has := pev.Payload["correctOptionIds"]; has {
		t.Fatal("participant payload leaks correct ids")
	}

	// Participant submits an answer.
	questions, _ := f.svc.Store().ListQuestions(ctx, sess.ID)
	optID := questions[0].Options[0].ID.String()
	subID := uuid.NewString()
	sendCmd(t, partConn, Envelope{V: 1, Type: "participant.submit_answer", SessionID: sess.ID.String(),
		RequestID: &[]string{uuid.NewString()}[0],
		Payload:   map[string]any{"submissionId": subID, "questionId": questions[0].ID.String(), "optionIds": []string{optID}},
		SentAt:    time.Now()})
	aenv := readEnv(t, partConn)
	if aenv.Type != "answer_acknowledged" {
		t.Fatalf("ack: %s", aenv.Type)
	}
	if aenv.Payload["outcome"] != "accepted" {
		t.Fatalf("outcome: %v", aenv.Payload)
	}
	if _, has := aenv.Payload["isCorrect"]; has {
		t.Fatal("ack reveals correctness while open")
	}

	// Host ends the question; participant gets results WITH keys + own result.
	req2 := uuid.NewString()
	ver2 := int64(2)
	sendCmd(t, hostConn, Envelope{V: 1, Type: "host.end_question", SessionID: sess.ID.String(),
		RequestID: &req2, Expected: &ver2, Payload: map[string]any{}, SentAt: time.Now()})
	// Drain host events until results.
	foundHost, foundPart := false, false
	for i := 0; i < 6 && !foundHost; i++ {
		if env := readEnv(t, hostConn); env.Type == "question_results" {
			foundHost = true
		}
	}
	// Participant may first get answer_submitted? No — that's host-only. Read until results.
	for i := 0; i < 6 && !foundPart; i++ {
		if env := readEnv(t, partConn); env.Type == "question_results" {
			foundPart = true
			if _, has := env.Payload["correctOptionIds"]; !has {
				t.Fatal("results missing keys after close")
			}
			own, has := env.Payload["ownResult"]
			if !has {
				t.Fatal("results missing ownResult")
			}
			_ = own
		}
	}
	if !foundHost || !foundPart {
		t.Fatalf("results: host=%v part=%v", foundHost, foundPart)
	}
}

func TestWSUpgradeRejectedWithoutIdentity(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, _ := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	wsURL := "ws" + strings.TrimPrefix(f.url, "http") + "/ws/v1/sessions/" + sess.ID.String()
	header := http.Header{}
	header.Set("Origin", f.url)
	dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err == nil {
		t.Fatal("anonymous dial accepted")
	}
	if resp != nil && resp.StatusCode != 401 && resp.StatusCode != 403 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestWSRoleViolationAndBadShape(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, _ := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	joinReq, _ := http.NewRequest("POST", f.url+"/api/v1/join/"+sess.JoinCode, strings.NewReader(`{"displayName":"Mallory"}`))
	joinReq.Header.Set("Content-Type", "application/json")
	joinResp, err := http.DefaultClient.Do(joinReq)
	if err != nil {
		t.Fatal(err)
	}
	defer joinResp.Body.Close()
	var partCk *http.Cookie
	for _, c := range joinResp.Cookies() {
		if c.Name == "__Host-qf_participant" {
			partCk = c
		}
	}
	conn := dialWS(t, f, sess.ID, partCk)
	defer conn.Close(websocket.StatusNormalClosure, "done")
	_ = readEnv(t, conn) // snapshot
	// Participant attempts host command → FORBIDDEN session_error.
	reqID := uuid.NewString()
	ver := int64(1)
	sendCmd(t, conn, Envelope{V: 1, Type: "host.end_session", SessionID: sess.ID.String(),
		RequestID: &reqID, Expected: &ver, Payload: map[string]any{}, SentAt: time.Now()})
	env := readEnv(t, conn)
	if env.Type != "session_error" {
		t.Fatalf("expected session_error, got %s", env.Type)
	}
	// Malformed JSON → INVALID_MESSAGE session_error.
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(wctx, websocket.MessageText, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	env = readEnv(t, conn)
	if env.Type != "session_error" {
		t.Fatalf("expected session_error, got %s", env.Type)
	}
}

func TestWSReconnectGetsSnapshot(t *testing.T) {
	f := setupWS(t)
	ctx := context.Background()
	sess, _ := f.svc.StartSession(ctx, f.quiz.ID, f.host, true, true)
	c1 := dialWS(t, f, sess.ID, f.hostCk)
	_ = readEnv(t, c1)
	_ = c1.Close(websocket.StatusNormalClosure, "refresh")
	// Reconnect like a host refresh: snapshot again with current version.
	c2 := dialWS(t, f, sess.ID, f.hostCk)
	defer c2.Close(websocket.StatusNormalClosure, "done")
	env := readEnv(t, c2)
	if env.Type != "state_snapshot" || env.StateVersion != 1 {
		t.Fatalf("reconnect snapshot: %+v", env)
	}
}

func b64enc(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }
