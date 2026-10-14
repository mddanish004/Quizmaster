package httpserver

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ServeSPA serves a Vite production build with index.html fallback, so one
// Render web service hosts the UI and the API on a single origin. API, WS,
// health, and version routes must be registered before calling this — the
// catch-all only answers paths nothing else claimed.
func ServeSPA(r chi.Router, dir string) {
	fs := http.Dir(dir)
	serve := func(w http.ResponseWriter, req *http.Request) {
		p := strings.TrimPrefix(path.Clean("/"+req.URL.Path), "/")
		if p != "" {
			if f, err := fs.Open(p); err == nil {
				if st, serr := f.Stat(); serr == nil && !st.IsDir() {
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					http.ServeContent(w, req, st.Name(), st.ModTime(), f)
					_ = f.Close()
					return
				}
				_ = f.Close()
			}
		}
		// Client-side route (or "/"): never cache the shell.
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, req, filepath.Join(dir, "index.html"))
	}
	r.Get("/*", serve)
	r.Head("/*", serve)
}
