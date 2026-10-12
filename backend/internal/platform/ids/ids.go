package ids

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
)

// NewUUIDv7 generates a time-ordered UUIDv7, falling back to v4 on failure.
func NewUUIDv7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// NewToken generates 32 random bytes and returns the raw token plus base64url encoding.
func NewToken() (raw []byte, encoded string, err error) {
	raw = make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate token: %w", err)
	}
	return raw, base64.RawURLEncoding.EncodeToString(raw), nil
}

// MustParseUUID parses a UUID or returns an error.
func MustParseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
