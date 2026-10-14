package auth

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
)

// SMTPMailer sends account mail through any SMTP relay (STARTTLS via the
// standard library). It replaces the old SES-specific plan: any provider
// that speaks SMTP (Gmail, Resend, Postmark, …) works through SMTP_* env.
type SMTPMailer struct {
	Addr     string
	Username string
	Password string
	From     string
}

// envelopeFrom returns a bare address suitable for the SMTP envelope,
// parsed out of the (possibly display-named) From header.
func (m *SMTPMailer) envelopeFrom() string {
	if a, err := mail.ParseAddress(m.From); err == nil {
		return a.Address
	}
	return strings.TrimSpace(m.From)
}

// buildResetMessage renders the plaintext reset mail (unit-tested).
func buildResetMessage(from, to, resetLink string) []byte {
	return []byte("From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: Reset your QuizForge password\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		"Someone requested a password reset for your QuizForge account.\r\n" +
		"\r\n" +
		resetLink + "\r\n" +
		"\r\n" +
		"The link expires in one hour. If that was not you, ignore this mail.\r\n")
}

// SendPasswordReset implements Mailer.
func (m *SMTPMailer) SendPasswordReset(ctx context.Context, to, resetLink string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(m.Addr)
	if err != nil {
		return fmt.Errorf("mail: invalid SMTP_ADDR: %w", err)
	}
	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, host)
	}
	return smtp.SendMail(m.Addr, auth, m.envelopeFrom(), []string{to}, buildResetMessage(m.From, to, resetLink))
}
