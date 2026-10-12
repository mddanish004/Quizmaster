package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// NewOpaqueToken generates 32 random bytes + base64url encoding for cookies/reset tokens.
func NewOpaqueToken() (raw []byte, encoded string, err error) {
	raw = make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("rand: %w", err)
	}
	return raw, base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeOpaqueToken decodes a base64url token; rejects malformed input.
func DecodeOpaqueToken(enc string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("invalid token")
	}
	return raw, nil
}

// DeriveCSRFToken computes HMAC-SHA256(secret, sessionID || tokenHashHex).
// The token rotates when the session rotates; it is returned by /me.
func DeriveCSRFToken(secret, sessionID string, tokenHash []byte) string {
	if secret == "" {
		secret = "dev-only-csrf-key-change-in-production"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(sessionID))
	mac.Write([]byte(hex.EncodeToString(tokenHash)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCSRFToken compares in constant time.
func VerifyCSRFToken(secret, sessionID string, tokenHash []byte, got string) bool {
	want := DeriveCSRFToken(secret, sessionID, tokenHash)
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// SetSessionCookie writes the opaque session cookie. Secure only in production/HTTPS.
func SetSessionCookie(w http.ResponseWriter, name, encoded string, secure bool, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge.Seconds()),
	})
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(0, 0),
	})
}
