package ai

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestFailureLogsContainNoContent ensures metadata-only logging on failure.
func TestFailureLogsContainNoContent(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	p := &scriptedProvider{fn: func(int) (GeneratedQuiz, Usage, error) {
		return GeneratedQuiz{}, Usage{}, &ProviderError{Kind: KindUnavailable, Cause: errors.New("down")}
	}}
	svc, uid := testSvc(t, p)
	svc.log = log
	secretTopic := "Supercalifragilistic-study-topic-zzz"
	in := baseInput(uid)
	in.Topic = secretTopic
	in.AdditionalInstructions = "ultra-secret-instructions-zzz"
	if _, err := svc.Generate(context.Background(), in); err == nil {
		t.Fatal("expected failure")
	}
	out := buf.String()
	for _, secret := range []string{secretTopic, "ultra-secret-instructions", "hf-", "Bearer", "ai@test.local"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log leaks content %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "generationId") {
		t.Fatalf("expected metadata fields: %s", out)
	}
	_ = time.Now
	_ = uuid.Nil
}
