package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimeoutExemptsWebSockets proves long-lived /ws handlers are not
// cancelled by the HTTP timeout (regression: soak connections died at 30s),
// while ordinary API handlers still are.
func TestTimeoutExemptsWebSockets(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			w.WriteHeader(499)
		case <-time.After(150 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	})
	h := Timeout(50 * time.Millisecond)(slow)

	req := httptest.NewRequest(http.MethodGet, "/ws/v1/sessions/abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ws path timed out: %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code == http.StatusOK {
		t.Fatal("api path was not timed out")
	}
}
