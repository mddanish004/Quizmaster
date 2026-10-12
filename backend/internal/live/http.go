package live

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/httpserver"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/ratelimit"
)

// Handler exposes live REST + WebSocket endpoints.
type Handler struct {
	svc     *Service
	manager *Manager
	auth    *auth.Handler
	authSvc *auth.Service
	cfg     *config.Config

	joinLimit    *ratelimit.Limiter
	upgradeLimit *ratelimit.Limiter
}

// NewHandler builds a Handler (manager created once here).
func NewHandler(svc *Service, authH *auth.Handler, authSvc *auth.Service, cfg *config.Config) *Handler {
	return &Handler{
		svc: svc, manager: NewManager(svc, cfg.WSAllowedOrigins), auth: authH, authSvc: authSvc, cfg: cfg,
		// Single-instance token buckets per spec (DB constraints stay authoritative).
		joinLimit:    ratelimit.New(20.0/60, 20, cfg.RateLimitEnabled), // 20/min/IP
		upgradeLimit: ratelimit.New(10.0/60, 10, cfg.RateLimitEnabled), // 10/min/IP
	}
}

// Manager exposes the WS manager.
func (h *Handler) Manager() *Manager { return h.manager }

// Service exposes the live service (wiring/tests).
func (h *Handler) Service() *Service { return h.svc }

// Routes mounts live REST endpoints (under /api/v1).
// Join/inspect/participant-snapshot are public and registered separately;
// everything here requires the creator session (writes also need CSRF).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/quizzes/{quizId}/live-sessions", h.createSession)
	r.Get("/live-sessions/{sessionId}", h.hostSnapshot)
	r.Post("/live-sessions/{sessionId}/commands", h.hostCommand)
	r.Get("/live-sessions/{sessionId}/results", h.results)
	r.Get("/sessions", h.history)
	r.Get("/sessions/{sessionId}", h.historyDetail)
}

// InspectJoin exposes the public join-metadata handler.
func (h *Handler) InspectJoin() http.HandlerFunc { return h.inspectJoin }

// Join exposes the public guest-join handler.
func (h *Handler) Join() http.HandlerFunc { return h.join }

// ParticipantSnapshot exposes the participant recovery snapshot handler.
func (h *Handler) ParticipantSnapshot() http.HandlerFunc { return h.participantSnapshot }

// WSRoutes mounts the upgrade endpoint at the server root (/ws/...).
func (h *Handler) WSRoutes(r chi.Router) {
	r.Get("/ws/v1/sessions/{sessionId}", h.serveWS)
}

func hostOf(r *http.Request) (uuid.UUID, bool) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		return uuid.Nil, false
	}
	return u.ID, true
}

func mapLiveError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Session not found.")
	case errors.Is(err, ErrGone):
		httpserver.WriteError(w, r, http.StatusGone, httpserver.CodeGone, "That join code has expired.")
	case errors.Is(err, ErrNotJoinable):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeNotJoinable, "Late join is disabled for this session.")
	case errors.Is(err, ErrNameTaken):
		httpserver.WriteErrorFields(w, r, http.StatusConflict, httpserver.CodeConflict, "That name is taken. Try a suggestion.", nil, false, nil,
			map[string]any{"suggestions": []string{"Player-1", "Player-2"}})
	case errors.Is(err, ErrInvalidState):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeInvalidState, "That action is not available in the current state.")
	case errors.Is(err, ErrVersionMismatch):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeVersionConflict, "Stale view — resync and retry.")
	case errors.Is(err, ErrQuestionClosed):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeQuestionClosed, "That round is closed; the answer was not counted.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternal, "Something went wrong.")
	}
	return true
}

func sessionDTO(s Session, origin string) map[string]any {
	joinURL := strings.TrimSuffix(origin, "/") + "/join/" + s.JoinCode
	m := map[string]any{
		"id": s.ID.String(), "joinCode": s.JoinCode, "joinUrl": joinURL,
		"status": s.Status, "stateVersion": s.StateVersion,
		"participantCount": s.ParticipantCount, "questionCount": s.QuestionCount,
	}
	if s.CurrentQuestion != nil {
		m["currentQuestionPosition"] = s.CurrentQuestion.Position
	} else {
		m["currentQuestionPosition"] = nil
	}
	if s.QuestionEndsAt != nil {
		m["questionEndsAt"] = s.QuestionEndsAt.UTC().Format(time.RFC3339)
	} else {
		m["questionEndsAt"] = nil
	}
	return m
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	host, ok := hostOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	quizID, err := uuid.Parse(chi.URLParam(r, "quizId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	var body struct {
		ShowLeaderboardAfterEachQuestion *bool `json:"showLeaderboardAfterEachQuestion"`
		AllowLateJoin                    *bool `json:"allowLateJoin"`
	}
	if r.ContentLength != 0 {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Request body is invalid.")
			return
		}
	}
	allowLate, showBoard := true, true
	if body.AllowLateJoin != nil {
		allowLate = *body.AllowLateJoin
	}
	if body.ShowLeaderboardAfterEachQuestion != nil {
		showBoard = *body.ShowLeaderboardAfterEachQuestion
	}
	sess, err := h.svc.StartSession(r.Context(), quizID, host, allowLate, showBoard)
	if mapLiveError(w, r, err) {
		return
	}
	httpserver.WriteJSON(w, r, http.StatusCreated, map[string]any{"data": sessionDTO(sess, h.cfg.Origin)})
}

func (h *Handler) hostSnapshot(w http.ResponseWriter, r *http.Request) {
	host, ok := hostOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Session not found.")
		return
	}
	sess, err := h.svc.Store().GetSession(r.Context(), id)
	if err != nil || sess.HostUserID != host {
		mapLiveError(w, r, ErrNotFound)
		return
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": sessionDTO(sess, h.cfg.Origin)})
}

func (h *Handler) hostCommand(w http.ResponseWriter, r *http.Request) {
	host, ok := hostOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Session not found.")
		return
	}
	var body struct {
		CommandID       string         `json:"commandId"`
		Type            string         `json:"type"`
		ExpectedVersion int64          `json:"expectedVersion"`
		Payload         map[string]any `json:"payload"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	if _, err := uuid.Parse(body.CommandID); err != nil {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "commandId must be a UUID.")
		return
	}
	var pos *int
	if body.Type == "start_question" {
		p, ok := body.Payload["questionPosition"].(float64)
		if !ok || p < 0 {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "questionPosition is required.")
			return
		}
		pi := int(p)
		pos = &pi
	}
	sess, err := h.svc.HostCommand(r.Context(), id, host, body.CommandID, body.Type, body.ExpectedVersion, pos)
	if mapLiveError(w, r, err) {
		return
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{
		"commandId": body.CommandID, "stateVersion": sess.StateVersion, "state": sess.Status,
	}})
}

func (h *Handler) inspectJoin(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "code")))
	sess, err := h.svc.Store().GetSessionByCode(r.Context(), code)
	if err != nil {
		mapLiveError(w, r, err)
		return
	}
	title := "Live quiz"
	if sess.QuizID != nil {
		if q, qerr := h.svc.quizSvc.Store().GetAny(r.Context(), *sess.QuizID); qerr == nil {
			title = q.Title
		}
	}
	joinable := sess.Status != StateEnded && time.Now().Before(sess.ExpiresAt) && (sess.Status == StateLobby || sess.AllowLateJoin)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{
		"sessionId": sess.ID.String(), "title": title, "status": sess.Status,
		"joinable": joinable, "participantCount": sess.ParticipantCount,
	}})
}

func participantCookieSecure() bool { return true }

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "code")))
	// Guest join is origin- and rate-limit-protected (no session exists yet for CSRF).
	if origin := r.Header.Get("Origin"); origin != "" && origin != h.cfg.Origin {
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeCSRF, "Join from the quiz page.")
		return
	}
	if !h.joinLimit.Allow("join:" + httpserver.ClientIP(r, h.cfg.TrustedProxies)) {
		secs := 60
		httpserver.WriteErrorFields(w, r, http.StatusTooManyRequests, httpserver.CodeRateLimited, "Too many join attempts. Try again shortly.", nil, true, &secs, nil)
		return
	}
	var body struct {
		DisplayName string `json:"displayName"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	var resume []byte
	if c, err := r.Cookie(h.cfg.ParticipantCookieName); err == nil {
		if raw, derr := base64.RawURLEncoding.DecodeString(c.Value); derr == nil && len(raw) == 32 {
			resume = raw
		}
	}
	sess, p, raw, created, err := h.svc.Join(r.Context(), code, body.DisplayName, resume)
	if mapLiveError(w, r, err) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: h.cfg.ParticipantCookieName, Value: base64.RawURLEncoding.EncodeToString(raw),
		Path: "/", HttpOnly: true, Secure: participantCookieSecure(), SameSite: http.SameSiteLaxMode,
		MaxAge: int(h.cfg.LiveSessionMaxTTL.Seconds()),
	})
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpserver.WriteJSON(w, r, status, map[string]any{"data": map[string]any{
		"participant":  map[string]any{"id": p.ID.String(), "displayName": p.DisplayName, "score": p.Score, "joinedAt": p.JoinedAt.UTC().Format(time.RFC3339)},
		"sessionId":    sess.ID.String(),
		"webSocketUrl": "wss://" + r.Host + "/ws/v1/sessions/" + sess.ID.String(),
	}})
}

func (h *Handler) participantSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Session not found.")
		return
	}
	c, err := r.Cookie(h.cfg.ParticipantCookieName)
	if err != nil {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Join the session first.")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Join the session first.")
		return
	}
	p, err := h.svc.Store().GetParticipantByToken(r.Context(), auth.SHA256Hash(raw))
	if err != nil || p.SessionID != id {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Join the session first.")
		return
	}
	sess, err := h.svc.Store().GetSession(r.Context(), id)
	if err != nil {
		mapLiveError(w, r, err)
		return
	}
	questions, _ := h.svc.Store().ListQuestions(r.Context(), id)
	payload := snapshotPayload("participant", sess, questions, &p, h.svc.Store())
	// Attach own ack/result context for the current question.
	if sess.CurrentQuestion != nil {
		if own, ok := h.svc.Store().OwnAnswer(r.Context(), p.ID, sess.CurrentQuestion.ID); ok {
			payload["ownAnswerAcknowledged"] = true
			if sess.Status == StateQuestionResults || sess.Status == StateLeaderboard {
				payload["releasedResult"] = map[string]any{"isCorrect": own.IsCorrect, "points": own.Points}
			} else {
				payload["releasedResult"] = nil
			}
		} else {
			payload["ownAnswerAcknowledged"] = false
			payload["releasedResult"] = nil
		}
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": payload})
}

func (h *Handler) results(w http.ResponseWriter, r *http.Request) {
	host, ok := hostOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Session not found.")
		return
	}
	res, err := h.svc.Results(r.Context(), id, host)
	if mapLiveError(w, r, err) {
		return
	}
	qs := make([]any, 0, len(res.Questions))
	for _, qr := range res.Questions {
		opts := make([]any, 0, len(qr.Question.Options))
		total := 0
		for _, o := range qr.Question.Options {
			total += qr.Stats.OptionCounts[o.ID.String()]
		}
		correct := []string{}
		for _, o := range qr.Question.Options {
			if o.IsCorrect {
				correct = append(correct, o.ID.String())
			}
		}
		for _, o := range qr.Question.Options {
			c := qr.Stats.OptionCounts[o.ID.String()]
			pct := 0.0
			if total > 0 {
				pct = float64(c) * 100 / float64(total)
			}
			opts = append(opts, map[string]any{"id": o.ID.String(), "text": o.Text, "count": c, "percentage": pct})
		}
		acc := 0.0
		if qr.Stats.AnswerCount > 0 {
			acc = float64(qr.Stats.CorrectCount) * 100 / float64(qr.Stats.AnswerCount)
		}
		qs = append(qs, map[string]any{
			"id": qr.Question.ID.String(), "position": qr.Question.Position, "prompt": qr.Question.Prompt,
			"correctOptionIds": correct, "options": opts,
			"answerCount": qr.Stats.AnswerCount, "accuracy": acc,
		})
	}
	parts := make([]any, 0, len(res.Participants))
	for _, l := range res.Participants {
		parts = append(parts, map[string]any{"id": l.ParticipantID.String(), "displayName": l.DisplayName, "score": l.Score, "joinedAt": l.JoinedAt.UTC().Format(time.RFC3339)})
	}
	title := "Live quiz"
	if res.Session.QuizID != nil {
		if q, qerr := h.svc.quizSvc.Store().GetAny(r.Context(), *res.Session.QuizID); qerr == nil {
			title = q.Title
		}
	}
	var ended any
	if res.Session.EndedAt != nil {
		ended = res.Session.EndedAt.UTC().Format(time.RFC3339)
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{
		"session": map[string]any{"id": res.Session.ID.String(), "quizTitle": title, "status": res.Session.Status,
			"participantCount": res.Session.ParticipantCount, "questionCount": res.Session.QuestionCount,
			"createdAt": res.Session.CreatedAt.UTC().Format(time.RFC3339), "endedAt": ended},
		"participants": parts, "questions": qs,
	}})
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	host, ok := hostOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	rows, err := h.svc.Store().HistoryForOwner(r.Context(), host, 20)
	if mapLiveError(w, r, err) {
		return
	}
	data := make([]any, 0, len(rows))
	for _, s := range rows {
		var ended any
		if s.EndedAt != nil {
			ended = s.EndedAt.UTC().Format(time.RFC3339)
		}
		data = append(data, map[string]any{"id": s.ID.String(), "quizTitle": s.QuizTitle, "status": s.Status,
			"participantCount": s.ParticipantCount, "questionCount": s.QuestionCount,
			"createdAt": s.CreatedAt.UTC().Format(time.RFC3339), "endedAt": ended})
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": data, "page": map[string]any{"nextCursor": nil, "hasMore": false}})
}

func (h *Handler) historyDetail(w http.ResponseWriter, r *http.Request) {
	// Same shape as results; owner-scoped.
	h.results(w, r)
}

// serveWS upgrades an authenticated host or participant socket.
func (h *Handler) serveWS(w http.ResponseWriter, r *http.Request) {
	if !h.manager.originAllowed(r) {
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeCSRF, "Origin not allowed.")
		return
	}
	if !h.upgradeLimit.Allow("ws-upgrade:" + httpserver.ClientIP(r, h.cfg.TrustedProxies)) {
		httpserver.WriteError(w, r, http.StatusTooManyRequests, httpserver.CodeRateLimited, "Too many connection attempts.")
		return
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Session not found.")
		return
	}
	// Identify: host via creator cookie, else participant via resume cookie.
	var role string
	var userID *uuid.UUID
	var participant *Participant
	if c, cerr := r.Cookie(h.cfg.SessionCookieName); cerr == nil {
		if h.authSvc != nil {
			if sess, u, aerr := h.authSvc.AuthenticateSession(r.Context(), c.Value); aerr == nil {
				if live, lerr := h.svc.Store().GetSession(r.Context(), sessionID); lerr == nil && live.HostUserID == u.ID {
					role = "host"
					uid := u.ID
					userID = &uid
					_ = sess
				}
			}
		}
	}
	if role == "" {
		c, cerr := r.Cookie(h.cfg.ParticipantCookieName)
		if cerr != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Join the session first.")
			return
		}
		raw, derr := base64.RawURLEncoding.DecodeString(c.Value)
		if derr != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Join the session first.")
			return
		}
		p, perr := h.svc.Store().GetParticipantByToken(r.Context(), auth.SHA256Hash(raw))
		if perr != nil || p.SessionID != sessionID {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "That identity cannot join this session.")
			return
		}
		role = "participant"
		participant = &p
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns(),
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(MaxMessageBytes)
	ctx, cancel := context.WithCancel(r.Context())
	cl := &client{conn: conn, role: role, sessionID: sessionID, userID: userID, participant: participant,
		send: make(chan []byte, OutboundQueue), closed: make(chan struct{})}
	h.manager.mu.Lock()
	if h.manager.clients[sessionID] == nil {
		h.manager.clients[sessionID] = map[*client]bool{}
	}
	h.manager.clients[sessionID][cl] = true
	h.manager.mu.Unlock()
	if participant != nil {
		h.manager.trackConnect(*participant)
	}
	h.manager.EnsureActor(sessionID)
	// Authoritative role-filtered snapshot first.
	h.sendSnapshot(ctx, cl)
	go h.writePump(ctx, cancel, cl)
	h.readPump(ctx, cancel, cl)
}

// originPatterns derives coder/websocket host patterns from the configured
// exact origins. The explicit originAllowed check above stays authoritative.
func (h *Handler) originPatterns() []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range h.cfg.WSAllowedOrigins {
		u, err := url.Parse(strings.TrimSpace(o))
		if err != nil || u.Host == "" {
			continue
		}
		if !seen[u.Host] {
			seen[u.Host] = true
			out = append(out, u.Host)
		}
	}
	if len(out) == 0 {
		return []string{"localhost"}
	}
	return out
}

func (h *Handler) sendSnapshot(ctx context.Context, cl *client) {
	sess, err := h.svc.Store().GetSession(ctx, cl.sessionID)
	if err != nil {
		return
	}
	questions, _ := h.svc.Store().ListQuestions(ctx, cl.sessionID)
	payload := snapshotPayload(cl.role, sess, questions, cl.participant, h.svc.Store())
	env := Envelope{V: 1, Type: "state_snapshot", EventID: eventID(sess.ID, sess.StateVersion),
		SessionID: sess.ID.String(), StateVersion: sess.StateVersion, SentAt: time.Now(), Payload: payload}
	raw, _ := json.Marshal(env)
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = cl.conn.Write(ctx2, websocket.MessageText, raw)
}

func (h *Handler) writePump(ctx context.Context, cancel context.CancelFunc, cl *client) {
	defer cancel()
	// If the pump exits (failed ping/write), retire the whole client: leaving
	// readPump and the map entry behind would leak a goroutine and stale
	// presence for a peer we already consider dead.
	defer h.removeClient(cl)
	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cl.closed:
			return
		case msg := <-cl.send:
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := cl.conn.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
			err := cl.conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Handler) readPump(ctx context.Context, cancel context.CancelFunc, cl *client) {
	defer cancel()
	defer h.removeClient(cl)
	violations := 0
	// Per-socket command rate: 20/sec sustained, burst 40. Uniqueness and
	// version checks remain the authoritative guards; this only sheds floods.
	tokens := 40.0
	last := time.Now()
	for {
		_, raw, err := cl.conn.Read(ctx)
		if err != nil {
			return
		}
		now := time.Now()
		tokens += now.Sub(last).Seconds() * 20
		if tokens > 40 {
			tokens = 40
		}
		last = now
		if tokens < 1 {
			h.sendError(ctx, cl, "RATE_LIMITED", "Slow down and retry.", true, nil)
			continue
		}
		tokens--
		if len(raw) > MaxMessageBytes {
			h.sendError(ctx, cl, "INVALID_MESSAGE", "Message too large.", false, nil)
			return
		}
		var env Envelope
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&env); err != nil || env.V != ProtocolVersion {
			violations++
			h.sendError(ctx, cl, "INVALID_MESSAGE", "Message shape is invalid.", false, nil)
			if violations >= 3 {
				_ = cl.conn.Close(websocket.StatusCode(4010), "incompatible protocol")
				return
			}
			continue
		}
		h.handleCommand(ctx, cl, env, &violations)
		if violations >= 3 {
			return
		}
	}
}

func (h *Handler) removeClient(cl *client) {
	h.manager.mu.Lock()
	if set, ok := h.manager.clients[cl.sessionID]; ok {
		delete(set, cl)
	}
	h.manager.mu.Unlock()
	if cl.participant != nil {
		h.manager.trackDisconnect(cl.sessionID, cl.participant.ID)
	}
	_ = cl.conn.Close(websocket.StatusNormalClosure, "bye")
}

func (h *Handler) sendError(ctx context.Context, cl *client, code, message string, retryable bool, reqID *string) {
	sess, _ := h.svc.Store().GetSession(ctx, cl.sessionID)
	var ver *int64
	if sess.ID != uuid.Nil {
		v := sess.StateVersion
		ver = &v
	}
	env := Envelope{V: 1, Type: "session_error", SessionID: cl.sessionID.String(), SentAt: time.Now(), RequestID: reqID,
		Payload: map[string]any{"code": code, "message": message, "retryable": retryable, "currentVersion": ver}}
	raw, _ := json.Marshal(env)
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = cl.conn.Write(ctx2, websocket.MessageText, raw)
}

func (h *Handler) handleCommand(ctx context.Context, cl *client, env Envelope, violations *int) {
	switch env.Type {
	case "client.ping":
		payload := map[string]any{"serverTime": time.Now().UTC()}
		if ct, ok := env.Payload["clientTime"]; ok {
			payload["clientTime"] = ct
		}
		out := Envelope{V: 1, Type: "pong", SessionID: cl.sessionID.String(), SentAt: time.Now(), RequestID: env.RequestID, Payload: payload}
		raw, _ := json.Marshal(out)
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = cl.conn.Write(wctx, websocket.MessageText, raw)
	case "client.resync":
		h.sendSnapshot(ctx, cl)
	case "host.start_question", "host.end_question", "host.show_leaderboard", "host.end_session":
		if cl.role != "host" || cl.userID == nil {
			*violations++
			h.sendError(ctx, cl, "FORBIDDEN", "Host only.", false, env.RequestID)
			return
		}
		if env.Expected == nil || env.RequestID == nil || *env.RequestID == "" {
			h.sendError(ctx, cl, "INVALID_MESSAGE", "expectedVersion and requestId are required.", false, env.RequestID)
			return
		}
		ctype := strings.TrimPrefix(env.Type, "host.")
		var pos *int
		if ctype == "start_question" {
			p, ok := env.Payload["questionPosition"].(float64)
			if !ok {
				h.sendError(ctx, cl, "INVALID_MESSAGE", "questionPosition is required.", false, env.RequestID)
				return
			}
			pi := int(p)
			pos = &pi
		}
		sess, err := h.svc.HostCommand(ctx, cl.sessionID, *cl.userID, *env.RequestID, ctype, *env.Expected, pos)
		if err != nil {
			h.sendError(ctx, cl, "INVALID_STATE", "Command rejected for the current state.", true, env.RequestID)
			return
		}
		out := Envelope{V: 1, Type: "command_ack", SessionID: cl.sessionID.String(), StateVersion: sess.StateVersion, SentAt: time.Now(), RequestID: env.RequestID, Payload: map[string]any{"state": sess.Status}}
		raw, _ := json.Marshal(out)
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = cl.conn.Write(wctx, websocket.MessageText, raw)
	case "participant.submit_answer":
		if cl.role != "participant" || cl.participant == nil {
			*violations++
			h.sendError(ctx, cl, "FORBIDDEN", "Participants only.", false, env.RequestID)
			return
		}
		subID, _ := env.Payload["submissionId"].(string)
		qID, _ := env.Payload["questionId"].(string)
		optIDs := env.Payload["optionIds"]
		sid, serr := uuid.Parse(subID)
		qid, qerr := uuid.Parse(qID)
		oids := []uuid.UUID{}
		if arr, ok := optIDs.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					if id, err := uuid.Parse(s); err == nil {
						oids = append(oids, id)
					}
				}
			}
		}
		if serr != nil || qerr != nil || len(oids) != 1 {
			h.sendError(ctx, cl, "INVALID_MESSAGE", "submissionId, questionId, and one optionId are required.", false, env.RequestID)
			return
		}
		outcome, err := h.svc.Store().SubmitAnswer(ctx, cl.participant.ID, sid, oids)
		if err != nil {
			if errors.Is(err, ErrQuestionClosed) {
				h.sendError(ctx, cl, "QUESTION_CLOSED", "That round closed; the answer was not counted.", false, env.RequestID)
				return
			}
			h.sendError(ctx, cl, "INVALID_STATE", "Answer rejected.", false, env.RequestID)
			return
		}
		_ = qid
		outcomeText := "accepted"
		if outcome.Duplicate {
			outcomeText = "duplicate"
		}
		ack := Envelope{V: 1, Type: "answer_acknowledged", EventID: eventID(cl.sessionID, 0),
			SessionID: cl.sessionID.String(), SentAt: time.Now(), RequestID: env.RequestID,
			Payload: map[string]any{"submissionId": sid.String(), "questionId": outcome.QuestionID.String(), "outcome": outcomeText, "receivedAt": outcome.ReceivedAt}}
		raw, _ := json.Marshal(ack)
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = cl.conn.Write(wctx, websocket.MessageText, raw)
		// Host notification (count only, never selections).
		h.manager.BroadcastAnswerSubmitted(cl.sessionID, outcome.QuestionID)
	default:
		*violations++
		h.sendError(ctx, cl, "INVALID_MESSAGE", "Unknown message type.", false, env.RequestID)
	}
}

// BroadcastAnswerSubmitted notifies the host of a new answer (count-only, never selections).
func (m *Manager) BroadcastAnswerSubmitted(sessionID, questionID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var n int
	_ = m.svc.Store().Pool().QueryRow(ctx, `SELECT count(*) FROM participant_answer WHERE session_question_id=$1`, questionID).Scan(&n)
	sess, err := m.svc.Store().GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	m.BroadcastToRole(sessionID, "host", "answer_submitted", sess.StateVersion,
		map[string]any{"questionId": questionID.String(), "submittedCount": n})
}

// BroadcastToRole sends an event only to clients with the given role.
func (m *Manager) BroadcastToRole(sessionID uuid.UUID, role, eventType string, version int64, payload map[string]any) {
	m.mu.Lock()
	set := m.clients[sessionID]
	clients := make([]*client, 0, len(set))
	for c := range set {
		if c.role == role {
			clients = append(clients, c)
		}
	}
	m.mu.Unlock()
	now := time.Now()
	for _, c := range clients {
		env := Envelope{V: 1, Type: eventType, EventID: eventID(sessionID, version),
			SessionID: sessionID.String(), StateVersion: version, SentAt: now, Payload: payload}
		raw, _ := json.Marshal(env)
		select {
		case c.send <- raw:
		default:
			c.closeSlow()
		}
	}
}

var _ = context.Background
