package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/testutil"
)

// scriptedProvider returns queued responses per call.
type scriptedProvider struct {
	mu    sync.Mutex
	calls int
	fn    func(call int) (GeneratedQuiz, Usage, error)
}

func (s *scriptedProvider) Name() string  { return "scripted" }
func (s *scriptedProvider) Model() string { return "scripted-v1" }
func (s *scriptedProvider) GenerateQuiz(ctx context.Context, req GenerateRequest) (GeneratedQuiz, Usage, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	return s.fn(call)
}

func validGQ(n int) GeneratedQuiz {
	gq := GeneratedQuiz{Title: "Go Quiz", Description: "d"}
	for i := 0; i < n; i++ {
		gq.Questions = append(gq.Questions, GeneratedQuestion{
			Type: "multiple_choice", Prompt: "Prompt number",
			Options:         []GeneratedOption{{ID: "a", Text: "Alpha"}, {ID: "b", Text: "Beta"}},
			CorrectOptionID: "b", Explanation: "Because.", Difficulty: "medium",
		})
	}
	// Unique prompts (quality rule is in provider-output validation; domain mapping
	// keeps them; ensure distinctness here).
	for i := range gq.Questions {
		gq.Questions[i].Prompt = "Prompt number " + string(rune('A'+i))
	}
	return gq
}

func testSvc(t *testing.T, p Provider) (*Service, uuid.UUID) {
	t.Helper()
	pool := testutil.OpenPool(t, testutil.TestDatabaseURLFor(t, "AI"))
	testutil.MigrateUp(t, pool, testutil.MigrationsDir(t))
	testutil.TruncateAll(t, pool)
	uid := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name) VALUES($1,'ai@test.local','ai_owner','AI')`, uid); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "test", AIDailyUserLimit: 5, AIDailyIPLimit: 20, AIMaxInFlight: 4,
		AIRequestTimeout: 5 * time.Second, AIEndpointTimeout: 10 * time.Second,
		SessionIdleTTL: 24 * time.Hour, SessionAbsoluteTTL: 720 * time.Hour,
	}
	return NewService(cfg, pool, p, nil), uid
}

func baseInput(uid uuid.UUID) GenerateInput {
	return GenerateInput{
		Topic: "Go concurrency", Difficulty: "medium", QuestionCount: 3,
		TargetAudience: "devs", Language: "en", UserID: uid, ClientIP: "127.0.0.1", IdempotencyKey: uuid.NewString(),
	}
}

func TestGoldenValidCreatesOneQuiz(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return validGQ(3), Usage{Provider: "scripted", Model: "v1"}, nil
	}}
	svc, uid := testSvc(t, p)
	out, err := svc.Generate(context.Background(), baseInput(uid))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if out.Mock {
		t.Fatal("scripted provider must not be labeled mock")
	}
	if len(out.Quiz.Questions) != 3 || out.Quiz.Status != "draft" {
		t.Fatalf("quiz: %+v", out.Quiz)
	}
	if out.Generation.Status != "succeeded" || out.Generation.Attempts != 1 {
		t.Fatalf("generation: %+v", out.Generation)
	}
	if p.calls != 1 {
		t.Fatalf("calls=%d", p.calls)
	}
}

func TestInvalidThenValidRetryCreatesOneQuiz(t *testing.T) {
	p := &scriptedProvider{fn: func(call int) (GeneratedQuiz, Usage, error) {
		if call == 1 {
			return GeneratedQuiz{}, Usage{}, &ProviderError{Kind: KindInvalidResponse, Cause: errors.New("bad json")}
		}
		return validGQ(2), Usage{}, nil
	}}
	svc, uid := testSvc(t, p)
	in := baseInput(uid)
	in.QuestionCount = 2
	out, err := svc.Generate(context.Background(), in)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if out.Generation.Attempts != 2 || len(out.Quiz.Questions) != 2 {
		t.Fatalf("retry: %+v", out.Generation)
	}
}

func TestDoubleInvalidYieldsNoQuiz(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return GeneratedQuiz{}, Usage{}, &ProviderError{Kind: KindInvalidResponse, Cause: errors.New("bad")}
	}}
	svc, uid := testSvc(t, p)
	if _, err := svc.Generate(context.Background(), baseInput(uid)); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("expected invalid output, got %v", err)
	}
	// No quiz rows persisted.
	if n := countQuizzes(t, svc, uid); n != 0 {
		t.Fatalf("partial quiz persisted: %d", n)
	}
}

func TestProviderDownYieldsUnavailableNoQuiz(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return GeneratedQuiz{}, Usage{}, &ProviderError{Kind: KindUnavailable, Cause: errors.New("down")}
	}}
	svc, uid := testSvc(t, p)
	if _, err := svc.Generate(context.Background(), baseInput(uid)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
	if n := countQuizzes(t, svc, uid); n != 0 {
		t.Fatalf("partial quiz persisted: %d", n)
	}
}

func TestAuthFailureDoesNotRetry(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return GeneratedQuiz{}, Usage{}, &ProviderError{Kind: KindRejected, Cause: errors.New("401")}
	}}
	svc, uid := testSvc(t, p)
	if _, err := svc.Generate(context.Background(), baseInput(uid)); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("expected invalid output for 401, got %v", err)
	}
	if p.calls != 1 {
		t.Fatalf("401 must not retry: calls=%d", p.calls)
	}
}

func TestQuotaEnforced(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return validGQ(1), Usage{}, nil
	}}
	svc, uid := testSvc(t, p)
	in := baseInput(uid)
	in.QuestionCount = 1
	for i := 0; i < 5; i++ {
		if _, err := svc.Generate(context.Background(), in); err != nil {
			t.Fatalf("gen %d: %v", i, err)
		}
	}
	if _, err := svc.Generate(context.Background(), in); !errors.Is(err, ErrQuota) {
		t.Fatalf("expected quota, got %v", err)
	}
}

func TestWrongCountFailsWithoutWrites(t *testing.T) {
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return validGQ(2), Usage{}, nil // requested 3
	}}
	svc, uid := testSvc(t, p)
	if _, err := svc.Generate(context.Background(), baseInput(uid)); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("expected invalid output, got %v", err)
	}
	if n := countQuizzes(t, svc, uid); n != 0 {
		t.Fatalf("partial quiz persisted: %d", n)
	}
}

func countQuizzes(t *testing.T, svc *Service, uid uuid.UUID) int {
	t.Helper()
	items, _, _, err := svc.quizSvc.ListSummaries(context.Background(), uid, "", "", "-updatedAt", 50, "")
	if err != nil {
		t.Fatal(err)
	}
	return len(items)
}
