package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoints(t *testing.T) {
	h := NewRouter(Deps{Version: "dev", Commit: "test"})
	for _, p := range []string{"/health/live", "/version"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d", p, rec.Code)
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s: missing X-Request-ID", p)
		}
	}
}

func TestReadinessFailsWithoutDB(t *testing.T) {
	h := NewRouter(Deps{
		Version:    "dev",
		ReadyCheck: func(ctx context.Context) error { return errors.New("no db") },
	})
	_ = h
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
