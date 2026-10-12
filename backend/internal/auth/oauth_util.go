package auth

import (
	"crypto/sha256"
	"regexp"
	"strings"
)

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

var safePathRe = regexp.MustCompile(`^/[A-Za-z0-9/_\-.?=&%]*$`)

func safeReturnPath(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || !safePathRe.MatchString(p) {
		return "/app"
	}
	return p
}
