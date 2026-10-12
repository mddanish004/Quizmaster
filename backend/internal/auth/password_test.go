package auth

import (
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	pw := "correct horse battery staple extended!"
	if ValidatePassword("short") == nil {
		t.Fatal("short password accepted")
	}
	if ValidatePassword("password1234") == nil {
		t.Fatal("denylist password accepted")
	}
	h, err := HashPassword(pw, "pepper")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("unexpected format: %s", h[:20])
	}
	if !VerifyPassword(h, pw, "pepper") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(h, pw+"x", "pepper") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword(h, pw, "other-pepper") {
		t.Fatal("wrong pepper accepted")
	}
	if VerifyPassword("garbage", pw, "pepper") {
		t.Fatal("garbage hash accepted")
	}
}

func TestValidation(t *testing.T) {
	if _, err := NormalizeEmail("  ADA@Example.COM "); err != nil || mustEmail() != "ada@example.com" {
		t.Fatalf("normalize: %v", err)
	}
	if ValidateUsername("ab") == nil || ValidateUsername("has space") == nil {
		t.Fatal("bad username accepted")
	}
	if _, err := ValidateDisplayName("   "); err == nil {
		t.Fatal("blank display accepted")
	}
	if d, err := ValidateDisplayName("  Ada   Lovelace  "); err != nil || d != "Ada Lovelace" {
		t.Fatalf("display collapse: %q %v", d, err)
	}
}

func mustEmail() string {
	e, _ := NormalizeEmail("  ADA@Example.COM ")
	return e
}

func TestCSRFDeriveVerify(t *testing.T) {
	hash := SHA256Hash([]byte("0123456789abcdef0123456789abcdef"))
	tok := DeriveCSRFToken("secret-0123456789abcdef0123456789", "session-id", hash)
	if !VerifyCSRFToken("secret-0123456789abcdef0123456789", "session-id", hash, tok) {
		t.Fatal("valid csrf rejected")
	}
	if VerifyCSRFToken("secret-0123456789abcdef0123456789", "session-id", hash, tok+"x") {
		t.Fatal("forged csrf accepted")
	}
	if VerifyCSRFToken("other-secret", "session-id", hash, tok) {
		t.Fatal("wrong secret accepted")
	}
}

func TestOpaqueTokenRoundTrip(t *testing.T) {
	raw, enc, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeOpaqueToken(enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(raw) {
		t.Fatal("token mismatch")
	}
	if _, err := DecodeOpaqueToken("short"); err == nil {
		t.Fatal("bad token accepted")
	}
}

// BenchmarkArgon2id reports verify latency for instance calibration.
// Run on the target hardware: go test ./internal/auth/ -run=NONE -bench=BenchmarkArgon2id -benchtime=5x
func BenchmarkArgon2id(b *testing.B) {
	h, err := HashPassword("benchmark-passphrase-twelve!", "")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !VerifyPassword(h, "benchmark-passphrase-twelve!", "") {
			b.Fatal("verify failed")
		}
	}
}
