package auth

import (
	"strings"
	"testing"
)

func TestBuildResetMessage(t *testing.T) {
	msg := string(buildResetMessage("QuizForge AI <no-reply@example.com>", "user@example.com", "https://app.example/reset?token=abc"))
	for _, want := range []string{
		"Subject: Reset your QuizForge password",
		"To: user@example.com",
		"https://app.example/reset?token=abc",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestSMTPEnvelopeFrom(t *testing.T) {
	m := &SMTPMailer{From: "QuizForge AI <no-reply@example.com>"}
	if got := m.envelopeFrom(); got != "no-reply@example.com" {
		t.Fatalf("envelopeFrom = %q", got)
	}
	m = &SMTPMailer{From: "plain@example.com"}
	if got := m.envelopeFrom(); got != "plain@example.com" {
		t.Fatalf("envelopeFrom = %q", got)
	}
}
