package ai

import (
	"context"
	"time"
)

// Difficulty mirrors quiz difficulty values.
type Difficulty string

const (
	Easy   Difficulty = "easy"
	Medium Difficulty = "medium"
	Hard   Difficulty = "hard"
)

// GenerateRequest is the validated generation brief.
type GenerateRequest struct {
	Topic                  string
	Difficulty             Difficulty
	QuestionCount          int
	TargetAudience         string
	AdditionalInstructions string
	Language               string
	RequestID              string
}

// Usage records provider consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Provider     string
	Model        string
	Latency      time.Duration
}

// GeneratedOption is one provider-side option (provider IDs a-f).
type GeneratedOption struct {
	ID   string
	Text string
}

// GeneratedQuestion is one provider-side question.
type GeneratedQuestion struct {
	Type            string
	Prompt          string
	Options         []GeneratedOption
	CorrectOptionID string
	Explanation     string
	Difficulty      string
}

// GeneratedQuiz is validated provider output before domain mapping.
type GeneratedQuiz struct {
	Title       string
	Description string
	Questions   []GeneratedQuestion
}

// ErrorKind classifies provider failures for retry policy.
type ErrorKind string

const (
	KindUnavailable     ErrorKind = "unavailable"
	KindRateLimited     ErrorKind = "rate_limited"
	KindTimeout         ErrorKind = "timeout"
	KindInvalidResponse ErrorKind = "invalid_response"
	KindRejected        ErrorKind = "rejected"
)

// ProviderError carries retry metadata.
type ProviderError struct {
	Kind       ErrorKind
	RetryAfter time.Duration
	Cause      error
}

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return string(e.Kind) + ": " + e.Cause.Error()
	}
	return string(e.Kind)
}

// Provider owns HTTP transport only; the service owns quota/validation/persistence.
type Provider interface {
	GenerateQuiz(ctx context.Context, req GenerateRequest) (GeneratedQuiz, Usage, error)
	Name() string
	Model() string
}
