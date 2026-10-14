package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, h http.Handler, target string) (int, http.Header, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

func TestServeSPA(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html>app-shell</html>")
	writeFile(t, filepath.Join(dir, "assets", "app-abc123.js"), "console.log(1)")

	h := NewRouter(Deps{StaticDir: dir})

	if code, _, body := get(t, h, "/"); code != 200 || !strings.Contains(body, "app-shell") {
		t.Fatalf("root: got %d %q", code, body)
	}
	if code, hdr, body := get(t, h, "/app/quizzes"); code != 200 || !strings.Contains(body, "app-shell") {
		t.Fatalf("client route fallback: got %d %q", code, body)
	} else if cc := hdr.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("shell cache header = %q, want no-store", cc)
	}
	if code, hdr, body := get(t, h, "/assets/app-abc123.js"); code != 200 || body != "console.log(1)" {
		t.Fatalf("asset: got %d %q", code, body)
	} else if cc := hdr.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("asset cache header = %q, want immutable", cc)
	}
	req := httptest.NewRequest(http.MethodHead, "/assets/app-abc123.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if res := rec.Result(); res.StatusCode != 200 {
		t.Fatalf("asset HEAD: got %d, want 200", res.StatusCode)
	}
	if code, _, body := get(t, h, "/missing.png"); code != 200 || !strings.Contains(body, "app-shell") {
		t.Fatalf("missing file fallback: got %d %q", code, body)
	}
}

func TestNoStaticDirServesAPIOnly(t *testing.T) {
	h := NewRouter(Deps{})
	if code, _, _ := get(t, h, "/"); code != 404 {
		t.Fatalf("root without STATIC_DIR: got %d, want 404", code)
	}
	if code, _, _ := get(t, h, "/health/live"); code != 200 {
		t.Fatalf("health without STATIC_DIR: got %d, want 200", code)
	}
}
