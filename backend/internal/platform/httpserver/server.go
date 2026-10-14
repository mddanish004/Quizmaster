package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps are router dependencies.
type Deps struct {
	Pool        *pgxpool.Pool
	Version     string
	Commit      string
	ReadyCheck  func(ctx context.Context) error
	ExtraRoutes func(r chi.Router)
	RootRoutes  func(r chi.Router)
	// StaticDir serves the built SPA (Vite dist) with index.html fallback
	// when set. Empty in local development, where `vite dev` serves the UI.
	StaticDir string
}

// NewRouter builds the base router with middleware and health/version endpoints.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestID)
	r.Use(middleware.RealIP)
	r.Use(Recover)
	r.Use(SecurityHeaders)
	r.Use(Timeout(30 * time.Second))

	r.Get("/health/live", func(w http.ResponseWriter, req *http.Request) {
		WriteJSON(w, req, http.StatusOK, map[string]any{"status": "ok"})
	})
	r.Get("/health/ready", func(w http.ResponseWriter, req *http.Request) {
		if d.ReadyCheck != nil {
			ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
			defer cancel()
			if err := d.ReadyCheck(ctx); err != nil {
				WriteError(w, req, http.StatusServiceUnavailable, CodeAIUnavailable, "Service not ready.")
				return
			}
		}
		WriteJSON(w, req, http.StatusOK, map[string]any{"status": "ok"})
	})
	r.Get("/version", func(w http.ResponseWriter, req *http.Request) {
		WriteJSON(w, req, http.StatusOK, map[string]any{"version": d.Version, "commit": d.Commit})
	})

	if d.ExtraRoutes != nil {
		r.Route("/api/v1", d.ExtraRoutes)
	}
	if d.RootRoutes != nil {
		d.RootRoutes(r)
	}
	if d.StaticDir != "" {
		ServeSPA(r, d.StaticDir)
	}
	return r
}
