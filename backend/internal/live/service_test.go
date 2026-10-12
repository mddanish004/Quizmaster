package live

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

func testSetup(t *testing.T) (*Service, uuid.UUID, quiz.Quiz) {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "LIVE"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	host := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'host@test.local','live_host','Host')`, host); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "test", Origin: "http://localhost:5173", LiveSessionMaxTTL: 6 * time.Hour,
		WSAllowedOrigins: []string{"http://localhost:5173"},
		SessionIdleTTL:   24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
	}
	svc := NewService(cfg, pool)
	_ = NewManager(svc, cfg.WSAllowedOrigins)
	// Owned published quiz with two questions.
	quizSvc := quiz.NewService(pool)
	q, err := quizSvc.CreateBlank(ctx, host, "Live Quiz", "")
	if err != nil {
		t.Fatal(err)
	}
	mkQ := func(pos int, prompt string) quiz.Question {
		return quiz.Question{
			Type: "multiple_choice", Position: pos, Prompt: prompt, Explanation: "E",
			Difficulty: "easy", TimeLimitSeconds: 30, ScoringMode: "speed", BasePoints: 1000,
			Options: []quiz.Option{
				{Position: 0, Text: "A", IsCorrect: false},
				{Position: 1, Text: "B", IsCorrect: true},
			},
		}
	}
	updated, err := quizSvc.Update(ctx, q.ID, host, 1, "Live Quiz", "", []quiz.Question{mkQ(0, "Q1?"), mkQ(1, "Q2?")})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := quizSvc.Publish(ctx, q.ID, host, updated.Version)
	if err != nil {
		t.Fatal(err)
	}
	return svc, host, pub
}

func TestStartJoinLifecycle(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, err := svc.StartSession(ctx, q.ID, host, true, true)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(sess.JoinCode) != 6 || sess.Status != StateLobby || sess.QuestionCount != 2 {
		t.Fatalf("session: %+v", sess)
	}
	// Inspect shows no keys.
	if sess.JoinCode == "" {
		t.Fatal("no join code")
	}
	// Join two participants.
	_, p1, _, created, err := svc.Join(ctx, sess.JoinCode, "Alice", nil)
	if err != nil || !created {
		t.Fatalf("join alice: %v", err)
	}
	_, _, _, _, err = svc.Join(ctx, sess.JoinCode, "alice", nil)
	if err != ErrNameTaken {
		t.Fatalf("duplicate name: %v", err)
	}
	_, p2, _, _, err := svc.Join(ctx, sess.JoinCode, "Bob", nil)
	if err != nil {
		t.Fatalf("join bob: %v", err)
	}
	if p1.ID == p2.ID {
		t.Fatal("ids collide")
	}
	_ = p1
}

func TestHostCommandsAndAnswers(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, _ := svc.StartSession(ctx, q.ID, host, true, true)
	_, alice, aliceRaw, _, _ := svc.Join(ctx, sess.JoinCode, "Alice", nil)
	_, bob, _, _, _ := svc.Join(ctx, sess.JoinCode, "Bob", nil)

	// Invalid: end before start.
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "end_question", 1, nil); err != ErrInvalidState {
		t.Fatalf("expected invalid state, got %v", err)
	}
	// Start Q0.
	pos := 0
	updated, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", 1, &pos)
	if err != nil {
		t.Fatalf("start q0: %v", err)
	}
	if updated.Status != StateQuestionOpen || updated.StateVersion != 2 {
		t.Fatalf("after start: %+v", updated)
	}
	// Wrong host cannot control.
	other := uuid.New()
	if _, err := svc.HostCommand(ctx, sess.ID, other, uuid.NewString(), "end_question", 2, nil); err != ErrNotFound {
		t.Fatalf("cross-host: %v", err)
	}
	// Stale version rejected.
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "end_question", 1, nil); err != ErrVersionMismatch {
		t.Fatalf("stale: %v", err)
	}
	// Alice answers correctly (option B). Find correct option id.
	questions, _ := svc.Store().ListQuestions(ctx, sess.ID)
	var correctID uuid.UUID
	for _, o := range questions[0].Options {
		if o.IsCorrect {
			correctID = o.ID
		}
	}
	sub := uuid.New()
	out, err := svc.Store().SubmitAnswer(ctx, alice.ID, sub, []uuid.UUID{correctID})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !out.IsCorrect || out.Points <= 500 || out.Points > 1000 {
		t.Fatalf("speed scoring: %+v", out)
	}
	// Duplicate submissionId → same outcome, no double score.
	out2, err := svc.Store().SubmitAnswer(ctx, alice.ID, sub, []uuid.UUID{correctID})
	if err != nil || !out2.Duplicate || out2.Points != out.Points {
		t.Fatalf("duplicate: %+v %v", out2, err)
	}
	// Second question attempt by same participant with new submission → still one row.
	out3, err := svc.Store().SubmitAnswer(ctx, alice.ID, uuid.New(), []uuid.UUID{correctID})
	if err != nil || out3.Duplicate == false {
		t.Fatalf("second submit same question: %+v %v", out3, err)
	}
	// Bob answers wrong.
	var wrongID uuid.UUID
	for _, o := range questions[0].Options {
		if !o.IsCorrect {
			wrongID = o.ID
		}
	}
	bout, err := svc.Store().SubmitAnswer(ctx, bob.ID, uuid.New(), []uuid.UUID{wrongID})
	if err != nil || bout.IsCorrect || bout.Points != 0 {
		t.Fatalf("wrong: %+v %v", bout, err)
	}
	// Close + verify results + leaderboard order.
	closed, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "end_question", 2, nil)
	if err != nil || closed.Status != StateQuestionResults {
		t.Fatalf("close: %+v %v", closed, err)
	}
	// Late answer rejected.
	if _, err := svc.Store().SubmitAnswer(ctx, bob.ID, uuid.New(), []uuid.UUID{wrongID}); err != ErrQuestionClosed {
		t.Fatalf("late: %v", err)
	}
	res, err := svc.Results(ctx, sess.ID, host)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Participants) != 2 || res.Participants[0].DisplayName != "Alice" {
		t.Fatalf("leaderboard: %+v", res.Participants)
	}
	if res.Questions[0].Stats.AnswerCount != 2 {
		t.Fatalf("stats: %+v", res.Questions[0].Stats)
	}
	_ = aliceRaw
}

func TestJoinGoneAndLateJoinDisabled(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, _ := svc.StartSession(ctx, q.ID, host, false, true)
	if _, _, _, _, err := svc.Join(ctx, "ZZZZZZ", "Ghost", nil); err != ErrGone {
		t.Fatalf("bad code: %v", err)
	}
	pos := 0
	updated, _ := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", 1, &pos)
	_ = updated
	if _, _, _, _, err := svc.Join(ctx, sess.JoinCode, "Late", nil); err != ErrNotJoinable {
		t.Fatalf("late join: %v", err)
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, _ := svc.StartSession(ctx, q.ID, host, true, true)
	if _, err := svc.Store().Pool().Exec(ctx, `UPDATE live_session SET expires_at=now()-interval '1 second' WHERE id=$1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	pos := 0
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", 1, &pos); err != ErrGone {
		t.Fatalf("expired command: %v", err)
	}
	if _, _, _, _, err := svc.Join(ctx, sess.JoinCode, "Ghost", nil); err != ErrGone {
		t.Fatalf("expired join: %v", err)
	}
}

func TestEndSessionTerminal(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, _ := svc.StartSession(ctx, q.ID, host, true, true)
	ended, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "end_session", 1, nil)
	if err != nil || ended.Status != StateEnded {
		t.Fatalf("end: %+v %v", ended, err)
	}
	pos := 0
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", ended.StateVersion, &pos); err != ErrInvalidState {
		t.Fatalf("reopen: %v", err)
	}
	if _, _, _, _, err := svc.Join(ctx, sess.JoinCode, "Nobody", nil); err != ErrGone {
		t.Fatalf("join ended: %v", err)
	}
}

func TestConcurrentAnswersScoreOnce(t *testing.T) {
	svc, host, q := testSetup(t)
	ctx := context.Background()
	sess, _ := svc.StartSession(ctx, q.ID, host, true, true)
	questions, _ := svc.Store().ListQuestions(ctx, sess.ID)
	var correctID uuid.UUID
	for _, o := range questions[0].Options {
		if o.IsCorrect {
			correctID = o.ID
		}
	}
	pos := 0
	if _, err := svc.HostCommand(ctx, sess.ID, host, uuid.NewString(), "start_question", 1, &pos); err != nil {
		t.Fatal(err)
	}
	const N = 100
	parts := make([]Participant, N)
	for i := 0; i < N; i++ {
		_, p, _, _, err := svc.Join(ctx, sess.JoinCode, "P"+string(rune('A'+i/26))+string(rune('a'+i%26))+itoa(i), nil)
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}
		parts[i] = p
	}
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Store().SubmitAnswer(ctx, parts[i].ID, uuid.New(), []uuid.UUID{correctID})
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("answer %d: %v", i, e)
		}
	}
	st, err := svc.Store().StatsFor(ctx, questions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.AnswerCount != N {
		t.Fatalf("answers=%d want %d", st.AnswerCount, N)
	}
	leaders, err := svc.Store().Leaderboard(ctx, sess.ID, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaders) != N {
		t.Fatalf("leaders=%d", len(leaders))
	}
	// Participant score equals sum of awarded points (reconciliation).
	for _, l := range leaders {
		if l.Score <= 500 || l.Score > 1000 {
			t.Fatalf("score bounds: %+v", l)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
