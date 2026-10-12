package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/httpserver"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
)

// Handler exposes AI generation endpoints.
type Handler struct {
	svc *Service
}

// NewHandler builds a Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts AI endpoints; caller wraps with auth+CSRF.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/ai/generations", h.generate)
	r.Get("/ai/generations/{generationId}", h.get)
}

func generationDTO(g Generation) map[string]any {
	var quizID any
	if g.QuizID != nil {
		quizID = g.QuizID.String()
	}
	var errCode any
	if g.ErrorCode != nil {
		errCode = *g.ErrorCode
	}
	return map[string]any{
		"id": g.ID.String(), "status": g.Status, "provider": g.Provider, "model": g.Model,
		"attempts": g.Attempts, "inputTokens": g.InputTokens, "outputTokens": g.OutputTokens,
		"latencyMs": g.LatencyMs, "errorCode": errCode, "quizId": quizID,
		"createdAt": g.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > 64 {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Idempotency-Key is required.")
		return
	}
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Request body is invalid.")
		return
	}
	if status, resp, found, err := h.svc.CheckIdempotency(r.Context(), u.ID, []byte(key), bodyBytes); err == nil && found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(resp)
		return
	} else if err != nil && errors.Is(err, ErrIdempotencyMismatch) {
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "Idempotency-Key was already used with a different request.")
		return
	}
	var body struct {
		Topic                  string `json:"topic"`
		Difficulty             string `json:"difficulty"`
		QuestionCount          int    `json:"questionCount"`
		TargetAudience         string `json:"targetAudience"`
		AdditionalInstructions string `json:"additionalInstructions"`
		Language               string `json:"language"`
	}
	dec := json.NewDecoder(bytes.NewReader(bodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Request body is invalid.")
		return
	}
	if body.Language == "" {
		body.Language = "en"
	}
	out, err := h.svc.Generate(r.Context(), GenerateInput{
		Topic: body.Topic, Difficulty: body.Difficulty, QuestionCount: body.QuestionCount,
		TargetAudience: body.TargetAudience, AdditionalInstructions: body.AdditionalInstructions,
		Language: body.Language, UserID: u.ID, ClientIP: httpserver.ClientIP(r, h.svc.Config().TrustedProxies), IdempotencyKey: key,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrValidation):
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Some fields need attention.")
		case errors.Is(err, ErrQuota):
			secs := 3600
			httpserver.WriteErrorFields(w, r, http.StatusTooManyRequests, httpserver.CodeRateLimited, "AI quota reached. Try again tomorrow.", nil, true, &secs, nil)
		case errors.Is(err, ErrInvalidOutput):
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeAIInvalid, "The AI response failed validation. Your brief is preserved — retry or start a manual draft.")
		default:
			httpserver.WriteError(w, r, http.StatusServiceUnavailable, httpserver.CodeAIUnavailable, "AI generation is unavailable. Your brief is preserved — retry or start a manual draft.")
		}
		return
	}
	// Build the response once so idempotent replays return identical bytes.
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{
		"generation": generationDTO(out.Generation),
		"quiz":       quizDTOOf(out.Quiz),
		"mock":       out.Mock,
	}})
	_ = h.svc.SaveIdempotency(r.Context(), u.ID, []byte(key), bodyBytes, http.StatusCreated, payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(payload)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "generationId"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Generation not found.")
		return
	}
	g, err := h.svc.GetForUser(r.Context(), id, u.ID)
	if err != nil {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Generation not found.")
		return
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": generationDTO(g)})
}

func quizDTOOf(q quiz.Quiz) map[string]any {
	qs := make([]any, 0, len(q.Questions))
	for _, qu := range q.Questions {
		opts := make([]any, 0, len(qu.Options))
		for _, o := range qu.Options {
			opts = append(opts, map[string]any{"id": o.ID.String(), "position": o.Position, "text": o.Text, "isCorrect": o.IsCorrect})
		}
		qs = append(qs, map[string]any{
			"id": qu.ID.String(), "type": qu.Type, "position": qu.Position,
			"prompt": qu.Prompt, "explanation": qu.Explanation, "difficulty": qu.Difficulty,
			"timeLimitSeconds": qu.TimeLimitSeconds, "scoringMode": qu.ScoringMode,
			"basePoints": qu.BasePoints, "options": opts,
		})
	}
	return map[string]any{
		"id": q.ID.String(), "title": q.Title, "description": q.Description,
		"status": q.Status, "questionCount": len(q.Questions), "version": q.Version,
		"createdAt":   q.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"updatedAt":   q.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"publishedAt": nil, "questions": qs,
	}
}
