package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"time"
)

// MockProvider is the deterministic local provider for tests and demos
// without external credentials. Output is always labeled provider "mock".
type MockProvider struct {
	ModelName string
}

// NewMockProvider builds the mock.
func NewMockProvider() *MockProvider { return &MockProvider{ModelName: "quizforge-mock-v1"} }

// Name implements Provider.
func (m *MockProvider) Name() string { return "mock" }

// Model implements Provider.
func (m *MockProvider) Model() string { return m.ModelName }

var mockPrompts = []string{
	"Which statement best captures the core idea?",
	"Which option is most consistent with the brief?",
	"What would an experienced practitioner choose first?",
	"Which detail is the strongest signal of understanding?",
	"Which approach avoids the most common mistake?",
}

var mockOptions = [][]string{
	{"Recall the definition", "Apply the concept", "Compare alternatives", "Evaluate trade-offs"},
	{"A surface feature", "A causal mechanism", "An unrelated fact", "A vague generality"},
	{"The fastest hack", "The principled default", "The rarest edge", "The oldest myth"},
}

// GenerateQuiz deterministically derives questions from the topic hash.
func (m *MockProvider) GenerateQuiz(_ context.Context, req GenerateRequest) (GeneratedQuiz, Usage, error) {
	start := time.Now()
	h := fnv.New64a()
	h.Write([]byte(req.Topic + "|" + string(req.Difficulty) + "|" + req.TargetAudience))
	seed := h.Sum64()
	gq := GeneratedQuiz{Title: truncate(req.Topic, 60) + " Quiz", Description: "Mock-generated draft — review before presenting."}
	for i := 0; i < req.QuestionCount; i++ {
		prompt := mockPrompts[int((seed>>uint(i*3))%uint64(len(mockPrompts)))]
		opts := mockOptions[int((seed>>uint(i*5))%uint64(len(mockOptions)))]
		correct := int((seed >> uint(i*7)) % 4)
		g := GeneratedQuestion{
			Type: "multiple_choice", Prompt: fmt.Sprintf("%s (%d)", prompt, i+1),
			CorrectOptionID: string(rune('a' + correct)),
			Explanation:     "Mock explanation — verify correctness before publishing.",
			Difficulty:      string(req.Difficulty),
		}
		for j, text := range opts {
			id := string(rune('a' + j))
			g.Options = append(g.Options, GeneratedOption{ID: id, Text: fmt.Sprintf("%s %d", text, i+1)})
		}
		gq.Questions = append(gq.Questions, g)
	}
	_ = sha256.Sum256([]byte(req.Topic))
	_ = hex.EncodeToString
	return gq, Usage{Provider: "mock", Model: m.ModelName, Latency: time.Since(start)}, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
