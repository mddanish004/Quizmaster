package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestGroqContract verifies URL, auth, model, strict schema, usage, and error
// mapping against a fixture server (no credentials needed). The shared
// contractFixture from the Hugging Face test is reused: same wire shape.
func TestGroqContract(t *testing.T) {
	var got struct {
		path, auth, model string
		body              map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(contractFixture))
	}))
	defer srv.Close()

	p := NewGroqProvider(srv.URL, "groq_test_key", "openai/gpt-oss-120b", 10*time.Second)
	gq, usage, err := p.GenerateQuiz(context.Background(), GenerateRequest{
		Topic: "water", Difficulty: Easy, QuestionCount: 1, TargetAudience: "kids", Language: "en",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got.path != "/chat/completions" {
		t.Fatalf("path: %s", got.path)
	}
	if got.auth != "Bearer groq_test_key" {
		t.Fatalf("auth header wrong (token must travel as Bearer, never logged)")
	}
	if got.body["model"] != "openai/gpt-oss-120b" {
		t.Fatalf("model sent verbatim (no routing suffix on Groq): %v", got.body["model"])
	}
	msgs := got.body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages: %d", len(msgs))
	}
	sys := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "exactly 1 independent") || !strings.Contains(sys, "<user_brief>") {
		t.Fatalf("system prompt: %q", sys[:80])
	}
	rf := got.body["response_format"].(map[string]any)
	if rf["type"] != "json_schema" || rf["json_schema"].(map[string]any)["strict"] != true {
		t.Fatalf("response_format: %v", rf)
	}
	if len(gq.Questions) != 1 || gq.Questions[0].CorrectOptionID != "a" {
		t.Fatalf("parsed: %+v", gq)
	}
	if usage.InputTokens != 210 || usage.OutputTokens != 96 {
		t.Fatalf("usage tokens: %+v", usage)
	}
	if usage.Provider != "groq" || usage.Model != "openai/gpt-oss-120b" {
		t.Fatalf("usage labeling: %+v", usage)
	}
	if p.Name() != "groq" {
		t.Fatalf("name: %s", p.Name())
	}
}

func TestGroqErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   ErrorKind
	}{
		{429, `{"error":"quota"}`, KindRateLimited},
		{500, `{"error":"boom"}`, KindUnavailable},
		{401, `{"error":"bad key"}`, KindRejected},
		// Billing exhaustion surfaces as REJECTED (S5-24: HF 402 "no credits").
		{402, `{"error":"no remaining credits"}`, KindRejected},
		{400, `{"error":"schema unsupported"}`, KindRejected},
		{200, `{"choices":[]}`, KindInvalidResponse},
		{200, `not json`, KindInvalidResponse},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		p := NewGroqProvider(srv.URL, "t", "m", 5*time.Second)
		_, _, err := p.GenerateQuiz(context.Background(), GenerateRequest{Topic: "water", Difficulty: Easy, QuestionCount: 1, Language: "en"})
		srv.Close()
		perr, ok := err.(*ProviderError)
		if !ok || perr.Kind != c.kind {
			t.Fatalf("status %d: got %v, want %s", c.status, err, c.kind)
		}
	}
}
