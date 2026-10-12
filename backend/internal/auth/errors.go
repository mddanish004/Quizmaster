package auth

import (
	"errors"
	"strings"
)

// Sentinel errors mapped to stable API codes by the HTTP layer.
var (
	ErrValidation         = errors.New("validation failed")
	ErrIdentityTaken      = errors.New("identity unavailable")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAuthRequired       = errors.New("authentication required")
	ErrRateLimited        = errors.New("rate limited")
	ErrResetInvalid       = errors.New("reset token invalid or expired")
	ErrOAuthInvalid       = errors.New("oauth transaction invalid")
	ErrLinkRequired       = errors.New("account link required")
	ErrLinkConflict       = errors.New("account link conflict")
)

// isUniqueViolation detects Postgres unique violations without importing pgconn here.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505") || strings.Contains(err.Error(), "duplicate key")
}
