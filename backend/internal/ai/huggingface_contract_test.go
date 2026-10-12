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

// contractFixture is a realistic OpenAI-compatible chat completion payload.
const contractFixture = `{
  "id": "chatcmpl-test",
  "object": "chat.completion",
  "created": 1760000000,
  "model": "Qwen/Qwen3-32B",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "{\"title\":\"Water\",\"description\":\"Basics\",\"questions\":[{\"type\":\"multiple_choice\",\"prompt\":\"What is H2O?\",\"options\":[{\"id\":\"a\",\"text\":\"Water\"},{\"id\":\"b\",\"text\":\"Fire\"}],\"correctOptionIds\":[\"a\"],\"explanation\":\"H2O is water.\",\"difficulty\":\"easy\"}]}"},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 210, "completion_tokens": 96, "total_tokens": 306}
}`

// TestHuggingFaceContract verifies URL, auth, model, strict schema, usage,
// and error mapping against a fixture server (no credentials needed).
func TestHuggingFaceContract(t *testing.T) {
	var got struct {
		path, auth, model, hfProvider string
		body                          map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.hfProvider = r.Header.Get("X-HF-Provider")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(contractFixture))
	}))
	defer srv.Close()

	p := NewHuggingFaceProvider(srv.URL, "hf_test_token", "Qwen/Qwen3-32B", "auto", 10*time.Second)
	gq, usage, err := p.GenerateQuiz(context.Background(), GenerateRequest{
		Topic: "water", Difficulty: Easy, QuestionCount: 1, TargetAudience: "kids", Language: "en",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got.path != "/chat/completions" {
		t.Fatalf("path: %s", got.path)
	}
	if got.auth != "Bearer hf_test_token" {
		t.Fatalf("auth header wrong (token must travel as Bearer, never logged)")
	}
	if got.hfProvider != "" {
		t.Fatalf("unexpected provider header %q: routing uses the documented model suffix", got.hfProvider)
	}
	if got.body["model"] != "Qwen/Qwen3-32B" {
		t.Fatalf("model with auto routing: %v", got.body["model"])
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
	if got.body["temperature"] != 0.2 || got.body["stream"] != false {
		t.Fatalf("sampling: %v", got.body)
	}
	if len(gq.Questions) != 1 || gq.Questions[0].CorrectOptionID != "a" {
		t.Fatalf("parsed: %+v", gq)
	}
	// Explicit provider selection uses the documented model suffix.
	p2 := NewHuggingFaceProvider(srv.URL, "t", "Qwen/Qwen3-32B", "fireworks-ai", 10*time.Second)
	if _, _, err := p2.GenerateQuiz(context.Background(), GenerateRequest{
		Topic: "water", Difficulty: Easy, QuestionCount: 1, Language: "en",
	}); err != nil {
		t.Fatalf("suffixed: %v", err)
	}
	if got.body["model"] != "Qwen/Qwen3-32B:fireworks-ai" {
		t.Fatalf("provider suffix: %v", got.body["model"])
	}
	if usage.InputTokens != 210 || usage.OutputTokens != 96 || usage.Model != "Qwen/Qwen3-32B" {
		t.Fatalf("usage: %+v", usage)
	}
}

func TestHuggingFaceErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   ErrorKind
	}{
		{429, `{"error":"quota"}`, KindRateLimited},
		{500, `{"error":"boom"}`, KindUnavailable},
		{503, `{"error":"overload"}`, KindUnavailable},
		{401, `{"error":"bad token"}`, KindRejected},
		{200, `{"choices":[]}`, KindInvalidResponse},
		{200, `not json`, KindInvalidResponse},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		p := NewHuggingFaceProvider(srv.URL, "t", "m", "auto", 5*time.Second)
		_, _, err := p.GenerateQuiz(context.Background(), GenerateRequest{Topic: "water", Difficulty: Easy, QuestionCount: 1, Language: "en"})
		srv.Close()
		perr, ok := err.(*ProviderError)
		if !ok || perr.Kind != c.kind {
			t.Fatalf("status %d: got %v, want %s", c.status, err, c.kind)
		}
	}
}

func TestHuggingFaceTimeoutCancels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	p := NewHuggingFaceProvider(srv.URL, "t", "m", "auto", 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := p.GenerateQuiz(ctx, GenerateRequest{Topic: "water", Difficulty: Easy, QuestionCount: 1, Language: "en"})
	perr, ok := err.(*ProviderError)
	if !ok || perr.Kind != KindTimeout {
		t.Fatalf("got %v, want timeout", err)
	}
}
