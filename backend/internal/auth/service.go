package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/ids"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/ratelimit"
)

// Service enforces authentication business rules. Handlers stay thin.
type Service struct {
	cfg      *config.Config
	store    *Store
	mail     Mailer
	verifier OIDCVerifier

	registerLimit   *ratelimit.Limiter // 5/hour/IP
	loginIPLimiter  *ratelimit.Limiter
	loginIDLimiter  *ratelimit.Limiter
	forgotIPLimiter *ratelimit.Limiter
}

// NewService wires dependencies.
func NewService(cfg *config.Config, pool *pgxpool.Pool, mail Mailer, verifier OIDCVerifier) *Service {
	return &Service{
		cfg: cfg, store: NewStore(pool), mail: mail, verifier: verifier,
		registerLimit:   ratelimit.New(5.0/3600, 5, cfg.RateLimitEnabled),
		loginIPLimiter:  ratelimit.New(10.0/600, 10, cfg.RateLimitEnabled),
		loginIDLimiter:  ratelimit.New(5.0/600, 5, cfg.RateLimitEnabled),
		forgotIPLimiter: ratelimit.New(5.0/3600, 5, cfg.RateLimitEnabled),
	}
}

// AuthResult is a login/registration outcome with raw token for the cookie.
type AuthResult struct {
	User      User
	SessionID uuid.UUID
	RawToken  []byte
	Encoded   string
	CSRF      string
}

func (s *Service) newSession(ctx context.Context, userID uuid.UUID, ip net.IP) (Session, []byte, string, error) {
	raw, enc, err := NewOpaqueToken()
	if err != nil {
		return Session{}, nil, "", err
	}
	now := time.Now()
	sess := Session{
		ID: uuid.New(), UserID: userID, TokenHash: SHA256Hash(raw),
		IdleExpires: now.Add(s.cfg.SessionIdleTTL), AbsExpires: now.Add(s.cfg.SessionAbsoluteTTL),
	}
	_ = ip
	return sess, raw, enc, nil
}

func (s *Service) csrfFor(sess Session) string {
	return DeriveCSRFToken(s.cfg.CSRFSecret, sess.ID.String(), sess.TokenHash)
}

// RegisterInput is validated registration data.
type RegisterInput struct {
	Email       string
	Username    string
	DisplayName string
	Password    string
	ClientIP    string
}

// Register creates a user + session. Duplicate email/username → ErrIdentityTaken.
func (s *Service) Register(ctx context.Context, in RegisterInput) (AuthResult, error) {
	if !s.registerLimit.Allow("register:" + in.ClientIP) {
		return AuthResult{}, ErrRateLimited
	}
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return AuthResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := ValidateUsername(in.Username); err != nil {
		return AuthResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	display, err := ValidateDisplayName(in.DisplayName)
	if err != nil {
		return AuthResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	// Hash outside the transaction (bounded work, may take ~200ms).
	hash, err := HashPassword(in.Password, s.cfg.PasswordPepper)
	if err != nil {
		return AuthResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	uid := ids.NewUUIDv7()
	sess, raw, enc, err := s.newSession(ctx, uid, nil)
	if err != nil {
		return AuthResult{}, err
	}
	now := time.Now()
	u := User{ID: uid, Email: email, Username: in.Username, DisplayName: display, PasswordHash: &hash, CreatedAt: now}
	if err := s.store.CreateUserWithSession(ctx, u, sess); err != nil {
		if isUniqueViolation(err) {
			return AuthResult{}, ErrIdentityTaken
		}
		return AuthResult{}, err
	}
	u.LoginMethods = []string{"password"}
	return AuthResult{User: u, SessionID: sess.ID, RawToken: raw, Encoded: enc, CSRF: s.csrfFor(sess)}, nil
}

// LoginInput authenticates by email-or-username.
type LoginInput struct {
	Identifier string
	Password   string
	ClientIP   string
}

// Login verifies credentials with constant-shape behavior and rotates a session.
func (s *Service) Login(ctx context.Context, in LoginInput) (AuthResult, error) {
	if !s.loginIPLimiter.Allow("login-ip:"+in.ClientIP) || !s.loginIDLimiter.Allow("login-id:"+strings.ToLower(in.Identifier)) {
		return AuthResult{}, ErrRateLimited
	}
	id := strings.TrimSpace(in.Identifier)
	u, err := s.store.FindUserByIdentifier(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			VerifyUnknownUser(in.Password, s.cfg.PasswordPepper)
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, err
	}
	if u.Status != "active" || u.PasswordHash == nil {
		VerifyUnknownUser(in.Password, s.cfg.PasswordPepper)
		return AuthResult{}, ErrInvalidCredentials
	}
	if !VerifyPassword(*u.PasswordHash, in.Password, s.cfg.PasswordPepper) {
		return AuthResult{}, ErrInvalidCredentials
	}
	sess, raw, enc, err := s.newSession(ctx, u.ID, nil)
	if err != nil {
		return AuthResult{}, err
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: u, SessionID: sess.ID, RawToken: raw, Encoded: enc, CSRF: s.csrfFor(sess)}, nil
}

// Logout revokes the current session.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.store.RevokeSession(ctx, sessionID)
}

// LogoutAll revokes every session for the user.
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return s.store.RevokeAllUserSessions(ctx, userID)
}

// UpdateProfile changes username/display name.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, username, display string) (User, error) {
	if err := ValidateUsername(username); err != nil {
		return User{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	display, err := ValidateDisplayName(display)
	if err != nil {
		return User{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := s.store.UpdateProfile(ctx, userID, username, display); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrIdentityTaken
		}
		return User{}, err
	}
	return s.store.GetUser(ctx, userID)
}

// ForgotPassword always succeeds from the caller's view (enumeration-safe).
func (s *Service) ForgotPassword(ctx context.Context, email, origin, clientIP string) {
	if !s.forgotIPLimiter.Allow("forgot:" + clientIP) {
		return
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return
	}
	u, err := s.store.FindUserByEmail(ctx, normalized)
	if err != nil {
		return
	}
	raw, enc, err := NewOpaqueToken()
	if err != nil {
		return
	}
	_ = s.store.CreatePasswordReset(ctx, ids.NewUUIDv7(), u.ID, SHA256Hash(raw), time.Now().Add(30*time.Minute))
	link := strings.TrimSuffix(origin, "/") + "/reset-password?token=" + enc
	_ = s.mail.SendPasswordReset(ctx, u.Email, link)
}

// ResetPassword consumes a token, sets the hash, revokes all sessions.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if err := ValidatePassword(password); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	decoded, err := DecodeOpaqueToken(strings.TrimSpace(token))
	if err != nil {
		return ErrResetInvalid
	}
	userID, err := s.store.ConsumePasswordReset(ctx, SHA256Hash(decoded))
	if err != nil {
		return ErrResetInvalid
	}
	hash, err := HashPassword(password, s.cfg.PasswordPepper)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return s.store.SetPassword(ctx, userID, hash)
}

// AuthenticateSession validates a cookie token and returns session+user.
func (s *Service) AuthenticateSession(ctx context.Context, encoded string) (Session, User, error) {
	raw, err := DecodeOpaqueToken(encoded)
	if err != nil {
		return Session{}, User{}, ErrAuthRequired
	}
	sess, u, err := s.store.GetSessionByHash(ctx, SHA256Hash(raw))
	if err != nil {
		return Session{}, User{}, ErrAuthRequired
	}
	now := time.Now()
	if sess.Revoked != nil || now.After(sess.IdleExpires) || now.After(sess.AbsExpires) || u.Status != "active" {
		return Session{}, User{}, ErrAuthRequired
	}
	if now.Sub(sess.LastSeen) > 5*time.Minute {
		_ = s.store.TouchSession(ctx, sess.ID)
	}
	return sess, u, nil
}
