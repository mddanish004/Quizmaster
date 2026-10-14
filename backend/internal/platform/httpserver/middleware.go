package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type ctxKeyRequestID struct{}

// RequestID assigns X-Request-ID if absent and propagates it.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, rid)
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Timeout exempts hijacked/streaming paths (WebSocket) from the Chi timeout:
// cancelling a socket handler mid-session would kill long-lived connections.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	inner := middleware.Timeout(d)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/ws/") {
				next.ServeHTTP(w, r)
				return
			}
			inner(next).ServeHTTP(w, r)
		})
	}
}

// Recover converts panics into 500 envelopes (never for expected errors).
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				WriteError(w, r, http.StatusInternalServerError, CodeInternal, "Something went wrong.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets baseline headers. The API serves them directly (there
// is no reverse proxy in front of Render), and the same process also serves
// the SPA in production — so this is the single place enforcing the policy
// the old Caddyfile used to own.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self' https://accounts.google.com")
		next.ServeHTTP(w, r)
	})
}

// NoStore disables caching for authenticated/participant responses.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
