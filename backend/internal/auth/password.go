package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters calibrated for t4g.small (see AUTH_SECURITY.md).
// Memory 64MiB x parallelism 2 bounds login bursts on 2GiB RAM.
const (
	argonMemory     uint32 = 64 * 1024
	argonIterations uint32 = 3
	argonParallel   uint8  = 2
	argonSaltLen           = 16
	argonKeyLen     uint32 = 32
)

// commonPasswords is a small local denylist; production may add a larger list.
var commonPasswords = map[string]bool{
	"password1234": true, "password12345": true, "letmeinletmein": true,
	"qwertyuiop12": true, "correct horse battery staple": true,
}

// ValidatePassword enforces 12-128 code points plus denylist (no composition rules).
func ValidatePassword(pw string) error {
	n := len([]rune(pw))
	if n < 12 || n > 128 {
		return errors.New("password must be 12-128 characters")
	}
	if commonPasswords[strings.ToLower(pw)] {
		return errors.New("password is too common; choose a less predictable passphrase")
	}
	return nil
}

// HashPassword hashes with Argon2id and a pepper; hashing happens outside DB tx.
func HashPassword(password, pepper string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(password+"\x00"+pepper), salt, argonIterations, argonMemory, argonParallel, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallel,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// dummyHash is a fixed Argon2id hash used for unknown users to keep timing shape.
var dummySalt = []byte("quizforge-dummy12")

// VerifyPassword checks a password against an encoded hash in constant-shape time.
func VerifyPassword(encoded, password, pepper string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var ver int
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) == 0 || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password+"\x00"+pepper), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// VerifyUnknownUser performs dummy verification to mitigate enumeration timing.
func VerifyUnknownUser(password, pepper string) {
	dummy := argon2.IDKey([]byte("dummy-unknown\x00"+pepper), dummySalt, 1, 8*1024, 1, 32)
	other := argon2.IDKey([]byte(password+"\x00"+pepper), dummySalt, 1, 8*1024, 1, 32)
	_ = subtle.ConstantTimeCompare(dummy, other)
}

// SHA256Hash hashes opaque tokens for storage (tokens carry 256-bit entropy).
func SHA256Hash(token []byte) []byte {
	h := sha256.Sum256(token)
	return h[:]
}
