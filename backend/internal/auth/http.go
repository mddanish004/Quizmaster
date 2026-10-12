package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/httpserver"
)

type ctxKey struct{ name string }

var (
	ctxUser    = ctxKey{"user"}
	ctxSession = ctxKey{"session"}
)

// UserFromContext returns the authenticated user, if present.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxUser).(User)
	return u, ok
}

// SessionFromContext returns the session, if present.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(ctxSession).(Session)
	return s, ok
}

// Handler wires auth HTTP endpoints.
type Handler struct {
	svc *Service
	cfg *config.Config
}

// NewHandler builds the handler.
func NewHandler(svc *Service, cfg *config.Config) *Handler {
	return &Handler{svc: svc, cfg: cfg}
}

func (h *Handler) secureCookie() bool {
	// __Host- cookies require Secure; browsers treat http://localhost as
	// trustworthy so Secure cookies still work in local development.
	return true
}

func (h *Handler) setAuthCookie(w http.ResponseWriter, enc string) {
	SetSessionCookie(w, h.cfg.SessionCookieName, enc, h.secureCookie(), h.cfg.SessionIdleTTL)
}

// RequireAuth loads the session from the cookie and rejects anonymous callers.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(h.cfg.SessionCookieName)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
			return
		}
		sess, u, err := h.svc.AuthenticateSession(r.Context(), c.Value)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxSession, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireCSRF enforces per-session CSRF header + exact Origin on mutating requests.
func (h *Handler) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		sess, ok := SessionFromContext(r.Context())
		if !ok {
			// Participant/public endpoints handle their own auth; skip here.
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-CSRF-Token")
		if got == "" || !VerifyCSRFToken(h.cfg.CSRFSecret, sess.ID.String(), sess.TokenHash, got) {
			_ = subtle.ConstantTimeCompare([]byte{1}, []byte{1})
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeCSRF, "Security check failed. Refresh and try again.")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != h.cfg.Origin {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeCSRF, "Security check failed. Refresh and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func userDTO(u User) map[string]any {
	return map[string]any{
		"id": u.ID.String(), "email": u.Email, "username": u.Username,
		"displayName": u.DisplayName, "emailVerified": u.EmailVerified != nil,
		"loginMethods": u.LoginMethods, "createdAt": u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func mapAuthError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrValidation):
		httpserver.WriteErrorFields(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Some fields need attention.", nil, false, nil, nil)
	case errors.Is(err, ErrIdentityTaken):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "That email or username is already taken.")
	case errors.Is(err, ErrInvalidCredentials):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeInvalidCreds, "Invalid credentials.")
	case errors.Is(err, ErrRateLimited):
		secs := 60
		httpserver.WriteErrorFields(w, r, http.StatusTooManyRequests, httpserver.CodeRateLimited, "Too many attempts. Try again shortly.", nil, true, &secs, nil)
	case errors.Is(err, ErrResetInvalid):
		httpserver.WriteError(w, r, http.StatusGone, httpserver.CodeGone, "That reset link is invalid or expired.")
	case errors.Is(err, ErrOAuthInvalid):
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Sign-in did not complete. Try again.")
	case errors.Is(err, ErrLinkRequired):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "An account with that email already exists. Sign in with your password, then link Google from settings.")
	case errors.Is(err, ErrLinkConflict):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "That Google account is linked to a different user.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternal, "Something went wrong.")
	}
	return true
}

// Routes mounts auth endpoints under /api/v1.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)
	r.With(h.RequireAuth, h.RequireCSRF).Post("/auth/logout", h.logout)
	r.With(h.RequireAuth, h.RequireCSRF).Post("/auth/logout-all", h.logoutAll)
	r.Get("/auth/google/start", h.googleStart)
	r.Get("/auth/google/callback", h.googleCallback)
	r.With(h.RequireAuth, h.RequireCSRF).Post("/auth/google/link/start", h.googleLinkStart)
	r.Get("/auth/google/link/callback", h.googleLinkCallback)
	r.Post("/auth/password/forgot", h.forgot)
	r.Post("/auth/password/reset", h.reset)
	r.With(h.RequireAuth).Get("/me", h.me)
	r.With(h.RequireAuth, h.RequireCSRF).Patch("/me", h.updateMe)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email"`
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	res, err := h.svc.Register(r.Context(), RegisterInput{
		Email: body.Email, Username: body.Username, DisplayName: body.DisplayName,
		Password: body.Password, ClientIP: httpserver.ClientIP(r, h.cfg.TrustedProxies),
	})
	if mapAuthError(w, r, err) {
		return
	}
	h.setAuthCookie(w, res.Encoded)
	httpserver.WriteJSON(w, r, http.StatusCreated, map[string]any{"data": map[string]any{"user": userDTO(res.User), "csrfToken": res.CSRF}})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	res, err := h.svc.Login(r.Context(), LoginInput{Identifier: body.Identifier, Password: body.Password, ClientIP: httpserver.ClientIP(r, h.cfg.TrustedProxies)})
	if mapAuthError(w, r, err) {
		return
	}
	h.setAuthCookie(w, res.Encoded)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{"user": userDTO(res.User), "csrfToken": res.CSRF}})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if sess, ok := SessionFromContext(r.Context()); ok {
		_ = h.svc.Logout(r.Context(), sess.ID)
	}
	ClearSessionCookie(w, h.cfg.SessionCookieName, h.secureCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	if u, ok := UserFromContext(r.Context()); ok {
		_ = h.svc.LogoutAll(r.Context(), u.ID)
	}
	ClearSessionCookie(w, h.cfg.SessionCookieName, h.secureCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	sess, _ := SessionFromContext(r.Context())
	csrf := DeriveCSRFToken(h.cfg.CSRFSecret, sess.ID.String(), sess.TokenHash)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{"user": userDTO(u), "csrfToken": csrf}})
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var body struct {
		Username    *string `json:"username"`
		DisplayName *string `json:"displayName"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	username := u.Username
	display := u.DisplayName
	if body.Username != nil {
		username = *body.Username
	}
	if body.DisplayName != nil {
		display = *body.DisplayName
	}
	updated, err := h.svc.UpdateProfile(r.Context(), u.ID, username, display)
	if mapAuthError(w, r, err) {
		return
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": updatedToDTO(updated)})
}

func updatedToDTO(u User) map[string]any { return map[string]any{"user": userDTO(u)} }

func (h *Handler) forgot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	h.svc.ForgotPassword(r.Context(), body.Email, h.cfg.Origin, httpserver.ClientIP(r, h.cfg.TrustedProxies))
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	if mapAuthError(w, r, h.svc.ResetPassword(r.Context(), body.Token, body.Password)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) googleStart(w http.ResponseWriter, r *http.Request) {
	returnTo := r.URL.Query().Get("returnTo")
	url, err := h.svc.StartGoogleLogin(r.Context(), returnTo)
	if mapAuthError(w, r, err) {
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handler) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("error") != "" {
		http.Redirect(w, r, "/login?oauth=cancelled", http.StatusSeeOther)
		return
	}
	res, err := h.svc.CallbackGoogle(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		if errors.Is(err, ErrLinkRequired) {
			http.Redirect(w, r, "/login?link=required", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login?oauth=failed", http.StatusSeeOther)
		return
	}
	h.setAuthCookie(w, res.Auth.Encoded)
	http.Redirect(w, r, safeReturnPath(res.ReturnPath), http.StatusSeeOther)
}

func (h *Handler) googleLinkStart(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	url, err := h.svc.StartGoogleLink(r.Context(), u.ID, "/settings")
	if mapAuthError(w, r, err) {
		return
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{"authorizationUrl": url}})
}

func (h *Handler) googleLinkCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(h.cfg.SessionCookieName)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	sess, u, err := h.svc.AuthenticateSession(r.Context(), c.Value)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = sess
	q := r.URL.Query()
	if err := h.svc.LinkGoogleToUser(r.Context(), u.ID, q.Get("state"), q.Get("code")); err != nil {
		if errors.Is(err, ErrLinkConflict) {
			http.Redirect(w, r, "/settings?link=conflict", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/settings?link=failed", http.StatusSeeOther)
		return
	}
	_ = uuid.Nil
	http.Redirect(w, r, "/settings?link=ok", http.StatusSeeOther)
}
