package main

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/ai"
	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/live"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
)

// wired holds module handlers for both /api/v1 and root WS mounting.
type wired struct {
	auth *auth.Handler
	quiz *quiz.Handler
	ai   *ai.Handler
	live *live.Handler
}

func wire(pool *pgxpool.Pool, cfg *config.Config) *wired {
	if pool == nil {
		return &wired{}
	}
	var mail auth.Mailer = &auth.MockMailer{}
	if !cfg.MockMail && cfg.MailProvider == "smtp" {
		mail = &auth.SMTPMailer{
			Addr: cfg.SMTPAddr, Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword, From: cfg.MailFrom,
		}
	}
	var verifier auth.OIDCVerifier = auth.FakeOIDCVerifier{}
	if !cfg.MockOIDC {
		verifier = &auth.RealOIDCVerifier{
			ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret,
			RedirectURL: cfg.GoogleRedirectURL,
		}
	}
	authSvc := auth.NewService(cfg, pool, mail, verifier)
	authH := auth.NewHandler(authSvc, cfg)
	quizH := quiz.NewHandler(quiz.NewService(pool), authH)
	aiH := ai.NewHandler(ai.NewService(cfg, pool, nil, nil))
	liveSvc := live.NewService(cfg, pool)
	liveH := live.NewHandler(liveSvc, authH, authSvc, cfg)
	return &wired{auth: authH, quiz: quizH, ai: aiH, live: liveH}
}

// RegisterRoutes wires module HTTP handlers under /api/v1.
func RegisterRoutes(w *wired, log *slog.Logger) func(r chi.Router) {
	return func(r chi.Router) {
		if w.auth != nil {
			w.auth.Routes(r)
		}
		if w.quiz != nil && w.auth != nil {
			r.Group(func(r chi.Router) {
				r.Use(w.auth.RequireAuth)
				r.Use(w.auth.RequireCSRF)
				w.quiz.Routes(r)
			})
		}
		if w.ai != nil && w.auth != nil {
			r.Group(func(r chi.Router) {
				r.Use(w.auth.RequireAuth)
				r.Use(w.auth.RequireCSRF)
				w.ai.Routes(r)
			})
		}
		if w.live != nil && w.auth != nil {
			// Public guest endpoints first (no session yet).
			r.Get("/join/{code}", w.live.InspectJoin())
			r.Post("/join/{code}", w.live.Join())
			r.Get("/play/{sessionId}/snapshot", w.live.ParticipantSnapshot())
			// Everything else needs the creator session (+CSRF for writes).
			r.Group(func(r chi.Router) {
				r.Use(w.auth.RequireAuth)
				r.Use(w.auth.RequireCSRF)
				w.live.Routes(r)
			})
		}
		_ = log
	}
}

// RegisterWSRoutes mounts the WebSocket upgrade endpoint at the server root.
func RegisterWSRoutes(w *wired, log *slog.Logger) func(r chi.Router) {
	return func(r chi.Router) {
		if w.live != nil {
			w.live.WSRoutes(r)
		}
		_ = log
	}
}
