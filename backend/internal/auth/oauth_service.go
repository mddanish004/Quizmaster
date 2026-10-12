package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/ids"
)

// StartGoogleLogin creates an OAuth transaction and returns the redirect URL.
func (s *Service) StartGoogleLogin(ctx context.Context, returnPath string) (string, error) {
	if s.cfg.GoogleClientID == "" && !s.cfg.MockOIDC {
		return "", fmt.Errorf("%w: google not configured", ErrOAuthInvalid)
	}
	tx, state, nonce, _, challenge, err := NewOAuthTransaction(safeReturnPath(returnPath), "login", nil)
	if err != nil {
		return "", err
	}
	if err := s.store.SaveOAuthTransaction(ctx, tx); err != nil {
		return "", err
	}
	// The plaintext nonce travels to Google in the auth request and returns
	// inside the signed ID token; we compare it against the stored row.
	clientID := s.cfg.GoogleClientID
	if clientID == "" {
		clientID = "mock-client"
	}
	return GoogleAuthURL(clientID, s.cfg.GoogleRedirectURL, state, nonce, challenge), nil
}

// GoogleCallbackResult is the outcome of the OAuth callback.
type GoogleCallbackResult struct {
	Auth        AuthResult
	ReturnPath  string
	LinkNeeded  bool
	MaskedEmail string
}

// CallbackGoogle consumes the transaction, verifies the code, and logs in/creates/links.
func (s *Service) CallbackGoogle(ctx context.Context, state, code string) (GoogleCallbackResult, error) {
	raw, err := decodeState(state)
	if err != nil {
		return GoogleCallbackResult{}, ErrOAuthInvalid
	}
	sh := sha256.Sum256(raw)
	tx, err := s.store.ConsumeOAuthTransaction(ctx, sh[:])
	if err != nil {
		return GoogleCallbackResult{}, ErrOAuthInvalid
	}
	claims, err := s.verifier.VerifyCode(ctx, code, string(tx.Verifier), tx.Nonce)
	if err != nil {
		return GoogleCallbackResult{}, ErrOAuthInvalid
	}
	if !claims.EmailVerified {
		return GoogleCallbackResult{}, ErrOAuthInvalid
	}
	email, err := NormalizeEmail(claims.Email)
	if err != nil {
		return GoogleCallbackResult{}, ErrOAuthInvalid
	}
	// Existing link → login.
	if it, err := s.store.FindIdentity(ctx, "google", claims.Subject); err == nil {
		u, err := s.store.GetUser(ctx, it.UserID)
		if err != nil {
			return GoogleCallbackResult{}, err
		}
		sess, rawTok, enc, err := s.newSession(ctx, u.ID, nil)
		if err != nil {
			return GoogleCallbackResult{}, err
		}
		if err := s.store.CreateSession(ctx, sess); err != nil {
			return GoogleCallbackResult{}, err
		}
		_ = s.store.MarkEmailVerified(ctx, u.ID)
		return GoogleCallbackResult{Auth: AuthResult{User: u, SessionID: sess.ID, RawToken: rawTok, Encoded: enc, CSRF: s.csrfFor(sess)}, ReturnPath: tx.ReturnPath}, nil
	}
	// Password account with same email → require explicit link (never auto-link).
	if _, err := s.store.FindUserByEmail(ctx, email); err == nil {
		return GoogleCallbackResult{LinkNeeded: true, ReturnPath: "/login?link=required", MaskedEmail: maskEmail(email)}, fmt.Errorf("%w", ErrLinkRequired)
	}
	// New user → create.
	base := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return -1
	}, strings.Split(email, "@")[0]))
	if len(base) < 3 {
		base = "player"
	}
	if len(base) > 20 {
		base = base[:20]
	}
	username, err := s.store.UniqueUsername(ctx, base)
	if err != nil {
		return GoogleCallbackResult{}, err
	}
	display := claims.Name
	if display == "" {
		display = username
	}
	if display, err = ValidateDisplayName(display); err != nil {
		display = username
	}
	uid := ids.NewUUIDv7()
	sess, rawTok, enc, err := s.newSession(ctx, uid, nil)
	if err != nil {
		return GoogleCallbackResult{}, err
	}
	u := User{ID: uid, Email: email, Username: username, DisplayName: display}
	now := time.Now()
	u.EmailVerified = &now
	it := Identity{ID: ids.NewUUIDv7(), UserID: uid, Provider: "google", Subject: claims.Subject, Email: &email}
	if err := s.store.CreateUserWithIdentity(ctx, u, it, sess); err != nil {
		return GoogleCallbackResult{}, err
	}
	u.LoginMethods = []string{"google"}
	return GoogleCallbackResult{Auth: AuthResult{User: u, SessionID: sess.ID, RawToken: rawTok, Encoded: enc, CSRF: s.csrfFor(sess)}, ReturnPath: tx.ReturnPath}, nil
}

// LinkGoogleToUser attaches the verified Google subject to the authenticated user.
func (s *Service) LinkGoogleToUser(ctx context.Context, userID uuid.UUID, state, code string) error {
	raw, err := decodeState(state)
	if err != nil {
		return ErrOAuthInvalid
	}
	sh := sha256.Sum256(raw)
	tx, err := s.store.ConsumeOAuthTransaction(ctx, sh[:])
	if err != nil {
		return ErrOAuthInvalid
	}
	if tx.Kind != "link" || tx.LinkUserID == nil || *tx.LinkUserID != userID {
		return ErrOAuthInvalid
	}
	claims, err := s.verifier.VerifyCode(ctx, code, string(tx.Verifier), tx.Nonce)
	if err != nil {
		return ErrOAuthInvalid
	}
	if it, err := s.store.FindIdentity(ctx, "google", claims.Subject); err == nil && it.UserID != userID {
		return ErrLinkConflict
	}
	email := claims.Email
	return s.store.LinkIdentity(ctx, Identity{ID: ids.NewUUIDv7(), UserID: userID, Provider: "google", Subject: claims.Subject, Email: &email})
}

// StartGoogleLink creates a link transaction for an authenticated user.
func (s *Service) StartGoogleLink(ctx context.Context, userID uuid.UUID, returnPath string) (string, error) {
	tx, state, nonce, _, challenge, err := NewOAuthTransaction(safeReturnPath(returnPath), "link", &userID)
	if err != nil {
		return "", err
	}
	if err := s.store.SaveOAuthTransaction(ctx, tx); err != nil {
		return "", err
	}
	clientID := s.cfg.GoogleClientID
	if clientID == "" {
		clientID = "mock-client"
	}
	return GoogleAuthURL(clientID, s.cfg.GoogleRedirectURL, state, nonce, challenge), nil
}

func decodeState(state string) ([]byte, error) {
	return decodeB64URL(state)
}

func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}
	name := parts[0]
	if len(name) <= 2 {
		name = "***"
	} else {
		name = string(name[0]) + "***" + string(name[len(name)-1])
	}
	_ = uuid.Nil
	return name + "@" + parts[1]
}
