package ai

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// GroqProvider calls Groq's OpenAI-compatible chat completion endpoint
// (ADR-015). The free tier needs no card and supports strict json_schema on
// models such as openai/gpt-oss-120b. Transport only; the service owns
// quota, retry policy, validation, and persistence.
type GroqProvider struct {
	BaseURL   string
	Token     string
	ModelName string
	HTTP      *http.Client
	MaxBody   int64
}

// NewGroqProvider builds the adapter.
func NewGroqProvider(baseURL, token, model string, timeout time.Duration) *GroqProvider {
	return &GroqProvider{
		BaseURL: strings.TrimSuffix(baseURL, "/"), Token: token, ModelName: model,
		HTTP: &http.Client{Timeout: timeout}, MaxBody: 512 * 1024,
	}
}

// Name implements Provider.
func (p *GroqProvider) Name() string { return "groq" }

// Model implements Provider.
func (p *GroqProvider) Model() string { return p.ModelName }

// GenerateQuiz performs one non-streaming structured request (no retry here;
// service owns retry).
func (p *GroqProvider) GenerateQuiz(ctx context.Context, req GenerateRequest) (GeneratedQuiz, Usage, error) {
	return doChatCompletion(ctx, p.HTTP, p.BaseURL, p.Token, p.ModelName, p.ModelName, "groq", req, p.MaxBody)
}
