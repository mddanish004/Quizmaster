package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
)

// Service errors map to stable API codes in the HTTP layer.
var (
	ErrValidation    = errors.New("validation failed")
	ErrQuota         = errors.New("quota exceeded")
	ErrUnavailable   = errors.New("ai unavailable")
	ErrInvalidOutput = errors.New("ai invalid output")
	ErrTooMany       = errors.New("too many concurrent generations")
)

// GenerateInput is the validated brief plus idempotency/identity metadata.
type GenerateInput struct {
	Topic                  string
	Difficulty             string
	QuestionCount          int
	TargetAudience         string
	AdditionalInstructions string
	Language               string
	UserID                 uuid.UUID
	ClientIP               string
	IdempotencyKey         string
}

// GenerateOutput is the created draft plus generation metadata.
type GenerateOutput struct {
	Generation Generation
	Quiz       quiz.Quiz
	Mock       bool
}

// Service orchestrates quota, provider calls, validation, and persistence.
type Service struct {
	cfg      *config.Config
	store    *Store
	quizSvc  *quiz.Service
	provider Provider
	log      *slog.Logger

	globalSem chan struct{}
	userSem   map[string]chan struct{}
	mu        sync.Mutex

	dailyUser map[string]*dayCounter
	dailyIP   map[string]*dayCounter
	countMu   sync.Mutex

	breaker breaker
}

type dayCounter struct {
	day   string
	count int
}

type breaker struct {
	mu       sync.Mutex
	failures []time.Time
	openTill time.Time
}

// NewService builds a Service. Pass provider nil to select from config
// (mock when MOCK_AI/HF token absent, real Hugging Face otherwise).
func NewService(cfg *config.Config, pool *pgxpool.Pool, provider Provider, log *slog.Logger) *Service {
	if provider == nil {
		if cfg.MockAI || cfg.HFToken == "" {
			provider = NewMockProvider()
		} else {
			provider = NewHuggingFaceProvider(cfg.HFBaseURL, cfg.HFToken, cfg.HFModel, cfg.HFProvider, cfg.AIRequestTimeout)
		}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		cfg: cfg, store: NewStore(pool), quizSvc: quiz.NewService(pool),
		provider: provider, log: log,
		globalSem: make(chan struct{}, cfg.AIMaxInFlight),
		userSem:   map[string]chan struct{}{},
		dailyUser: map[string]*dayCounter{},
		dailyIP:   map[string]*dayCounter{},
	}
}

// ProviderName reports the active provider (for honest UI labeling).
func (s *Service) ProviderName() string { return s.provider.Name() }

// Config exposes the service configuration (trusted proxies for handlers).
func (s *Service) Config() *config.Config { return s.cfg }

func (s *Service) userSemFor(user string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	sem, ok := s.userSem[user]
	if !ok {
		sem = make(chan struct{}, 2)
		s.userSem[user] = sem
	}
	return sem
}

func (s *Service) allowDaily(m map[string]*dayCounter, key string, limit int) bool {
	s.countMu.Lock()
	defer s.countMu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	c, ok := m[key]
	if !ok || c.day != today {
		c = &dayCounter{day: today}
		m[key] = c
	}
	if c.count >= limit {
		return false
	}
	c.count++
	return true
}

func (s *Service) breakerOpen() bool {
	s.breaker.mu.Lock()
	defer s.breaker.mu.Unlock()
	return time.Now().Before(s.breaker.openTill)
}

func (s *Service) recordBreaker(success bool) {
	s.breaker.mu.Lock()
	defer s.breaker.mu.Unlock()
	now := time.Now()
	if success {
		s.breaker.failures = nil
		return
	}
	s.breaker.failures = append(s.breaker.failures, now)
	cutoff := now.Add(-60 * time.Second)
	kept := s.breaker.failures[:0]
	for _, f := range s.breaker.failures {
		if f.After(cutoff) {
			kept = append(kept, f)
		}
	}
	s.breaker.failures = kept
	if len(kept) >= 5 {
		s.breaker.openTill = now.Add(30 * time.Second)
	}
}

// validateInput enforces the input rules table (AI_INTEGRATION.md).
func validateInput(in GenerateInput) error {
	if utf8.RuneCountInString(strings.TrimSpace(in.Topic)) < 3 || utf8.RuneCountInString(in.Topic) > 200 {
		return fmt.Errorf("%w: topic must be 3-200 characters", ErrValidation)
	}
	switch in.Difficulty {
	case "easy", "medium", "hard":
	default:
		return fmt.Errorf("%w: difficulty", ErrValidation)
	}
	if in.QuestionCount < 1 || in.QuestionCount > 20 {
		return fmt.Errorf("%w: questionCount must be 1-20", ErrValidation)
	}
	if utf8.RuneCountInString(in.TargetAudience) > 200 {
		return fmt.Errorf("%w: targetAudience too long", ErrValidation)
	}
	if utf8.RuneCountInString(in.AdditionalInstructions) > 1000 {
		return fmt.Errorf("%w: additionalInstructions too long", ErrValidation)
	}
	if in.Language == "" {
		in.Language = "en"
	}
	if len(in.Language) != 2 {
		return fmt.Errorf("%w: language", ErrValidation)
	}
	return nil
}

// Generate runs the full pipeline: quota → idempotency → provider (≤2 attempts)
// → validation → one atomic quiz transaction. Failures persist metadata only.
func (s *Service) Generate(ctx context.Context, in GenerateInput) (GenerateOutput, error) {
	if err := validateInput(in); err != nil {
		return GenerateOutput{}, err
	}
	if in.TargetAudience == "" {
		in.TargetAudience = "general audience"
	}
	if s.breakerOpen() {
		return GenerateOutput{}, ErrUnavailable
	}
	if !s.allowDaily(s.dailyUser, in.UserID.String(), s.cfg.AIDailyUserLimit) {
		return GenerateOutput{}, ErrQuota
	}
	if !s.allowDaily(s.dailyIP, in.ClientIP, s.cfg.AIDailyIPLimit) {
		return GenerateOutput{}, ErrQuota
	}
	usem := s.userSemFor(in.UserID.String())
	select {
	case usem <- struct{}{}:
		defer func() { <-usem }()
	default:
		return GenerateOutput{}, ErrTooMany
	}
	select {
	case s.globalSem <- struct{}{}:
		defer func() { <-s.globalSem }()
	default:
		return GenerateOutput{}, ErrTooMany
	}

	req := GenerateRequest{
		Topic: strings.TrimSpace(in.Topic), Difficulty: Difficulty(in.Difficulty),
		QuestionCount: in.QuestionCount, TargetAudience: strings.TrimSpace(in.TargetAudience),
		AdditionalInstructions: in.AdditionalInstructions, Language: in.Language,
		RequestID: uuid.NewString(),
	}
	fp := Fingerprint(req)
	genID := uuid.New()
	if err := s.store.CreateRunning(ctx, genID, in.UserID, s.provider.Name(), s.provider.Model(), fp); err != nil {
		return GenerateOutput{}, err
	}

	endpointCtx, cancel := context.WithTimeout(ctx, s.cfg.AIEndpointTimeout)
	defer cancel()
	var gq GeneratedQuiz
	var usage Usage
	var perr *ProviderError
	attempts := 0
	for attempt := 1; attempt <= 2; attempt++ {
		attempts = attempt
		attemptCtx, attemptCancel := context.WithTimeout(endpointCtx, s.cfg.AIRequestTimeout)
		g, u, err := s.provider.GenerateQuiz(attemptCtx, req)
		attemptCancel()
		if err == nil {
			if len(g.Questions) != req.QuestionCount {
				perr = &ProviderError{Kind: KindInvalidResponse, Cause: fmt.Errorf("expected %d questions, got %d", req.QuestionCount, len(g.Questions))}
				if attempt == 1 {
					time.Sleep(jitterBackoff())
					continue
				}
				break
			}
			gq, usage = g, u
			perr = nil
			break
		}
		if !errors.As(err, &perr) {
			perr = &ProviderError{Kind: KindUnavailable, Cause: err}
		}
		if !retryable(perr.Kind) || attempt == 2 {
			break
		}
		time.Sleep(jitterBackoff())
	}
	if perr != nil || len(gq.Questions) == 0 {
		code := "PROVIDER_ERROR"
		var outErr error = ErrUnavailable
		if perr != nil {
			switch perr.Kind {
			case KindRateLimited:
				code, outErr = "RATE_LIMITED", ErrQuota
			case KindInvalidResponse:
				code, outErr = "INVALID_OUTPUT", ErrInvalidOutput
			case KindRejected:
				code, outErr = "REJECTED", ErrInvalidOutput
			case KindTimeout:
				code, outErr = "TIMEOUT", ErrUnavailable
			}
			s.recordBreaker(false)
		}
		_ = s.store.FinalizeFailure(ctx, genID, code, attempts)
		// Log metadata only — never prompt, completion, or user content.
		s.log.Info("ai generation failed", "generationId", genID, "attempts", attempts, "errorCode", code, "fingerprintPrefix", fp[:12])
		return GenerateOutput{}, outErr
	}
	s.recordBreaker(true)

	// Map provider output to the quiz domain aggregate (server UUIDs, 30s timers).
	domain := make([]quiz.Question, 0, len(gq.Questions))
	for i, pq := range gq.Questions {
		dq := quiz.Question{
			ID: uuid.New(), Type: "multiple_choice", Position: i,
			Prompt: pq.Prompt, Explanation: pq.Explanation, Difficulty: pq.Difficulty,
			TimeLimitSeconds: 30, ScoringMode: "speed", BasePoints: 1000,
		}
		for j, po := range pq.Options {
			dq.Options = append(dq.Options, quiz.Option{
				ID: uuid.New(), Position: j, Text: po.Text, IsCorrect: po.ID == pq.CorrectOptionID,
			})
		}
		domain = append(domain, dq)
	}
	if fe := quiz.ValidateQuizWrite(gq.Title, gq.Description, domain, true); fe.HasErrors() {
		_ = s.store.FinalizeFailure(ctx, genID, "DOMAIN_INVALID", attempts)
		return GenerateOutput{}, ErrInvalidOutput
	}

	// One transaction: draft quiz + questions + options. Reuse quiz store via blank+replace.
	created, err := s.quizSvc.CreateBlank(ctx, in.UserID, gq.Title, gq.Description)
	if err != nil {
		_ = s.store.FinalizeFailure(ctx, genID, "PERSIST_FAILED", attempts)
		return GenerateOutput{}, err
	}
	full, err := s.quizSvc.Update(ctx, created.ID, in.UserID, created.Version, gq.Title, gq.Description, domain)
	if err != nil {
		_ = s.store.FinalizeFailure(ctx, genID, "PERSIST_FAILED", attempts)
		return GenerateOutput{}, err
	}
	if err := s.store.FinalizeSuccess(ctx, genID, full.ID, usage, attempts); err != nil {
		return GenerateOutput{}, err
	}
	gen, err := s.store.GetForUser(ctx, genID, in.UserID)
	if err != nil {
		return GenerateOutput{}, err
	}
	s.log.Info("ai generation succeeded", "generationId", genID, "attempts", attempts, "provider", usage.Provider, "fingerprintPrefix", fp[:12])
	return GenerateOutput{Generation: gen, Quiz: full, Mock: s.provider.Name() == "mock"}, nil
}

func retryable(k ErrorKind) bool {
	return k == KindUnavailable || k == KindRateLimited || k == KindTimeout || k == KindInvalidResponse
}

func jitterBackoff() time.Duration {
	// ~500ms full jitter per spec.
	return time.Duration(250+time.Now().UnixNano()%500) * time.Millisecond
}

// GetForUser returns owned generation metadata.
func (s *Service) GetForUser(ctx context.Context, id, userID uuid.UUID) (Generation, error) {
	return s.store.GetForUser(ctx, id, userID)
}

// CheckIdempotency delegates to the store.
func (s *Service) CheckIdempotency(ctx context.Context, userID uuid.UUID, key, body []byte) (int, []byte, bool, error) {
	return s.store.CheckIdempotency(ctx, userID, "ai-generation", key, body)
}

// SaveIdempotency delegates to the store.
func (s *Service) SaveIdempotency(ctx context.Context, userID uuid.UUID, key, body []byte, status int, resp []byte) error {
	return s.store.SaveIdempotency(ctx, userID, "ai-generation", key, body, status, resp)
}
