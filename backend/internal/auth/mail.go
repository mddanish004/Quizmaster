package auth

import (
	"context"
	"sync"
)

// Mailer sends account emails. Production uses SES; local/test uses mock.
type Mailer interface {
	SendPasswordReset(ctx context.Context, to, resetLink string) error
}

// MockMailer captures links for developer tooling/tests; never used in production.
type MockMailer struct {
	mu   sync.Mutex
	Sent []MailRecord
	Fail bool
}

// MailRecord is one captured send.
type MailRecord struct {
	To   string
	Link string
}

func (m *MockMailer) SendPasswordReset(_ context.Context, to, resetLink string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail {
		return errMailUnavailable
	}
	m.Sent = append(m.Sent, MailRecord{To: to, Link: resetLink})
	return nil
}

// LastLinkFor returns the most recent link for an address (test helper).
func (m *MockMailer) LastLinkFor(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.Sent) - 1; i >= 0; i-- {
		if m.Sent[i].To == to {
			return m.Sent[i].Link
		}
	}
	return ""
}

type mailError string

func (e mailError) Error() string { return string(e) }

const errMailUnavailable = mailError("mail unavailable")
