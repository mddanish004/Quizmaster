package auth

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,30}$`)

// NormalizeEmail lowercases and trims; citext handles DB-side case-insensitivity.
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if len(e) == 0 || len(e) > 254 {
		return "", errors.New("email must be 1-254 characters")
	}
	if _, err := mail.ParseAddress(e); err != nil {
		return "", errors.New("email is invalid")
	}
	return e, nil
}

// ValidateUsername enforces [A-Za-z0-9_]{3,30}.
func ValidateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return errors.New("username must be 3-30 letters, numbers, or underscores")
	}
	return nil
}

// ValidateDisplayName enforces 1-80 chars after trim/collapse.
func ValidateDisplayName(d string) (string, error) {
	clean := strings.Join(strings.Fields(d), " ")
	if utf8.RuneCountInString(clean) < 1 || utf8.RuneCountInString(clean) > 80 {
		return "", errors.New("display name must be 1-80 characters")
	}
	return clean, nil
}
