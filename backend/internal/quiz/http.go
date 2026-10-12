package quiz

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/httpserver"
)

// Handler exposes quiz REST endpoints (owner-scoped).
type Handler struct {
	svc  *Service
	auth *auth.Handler
}

// NewHandler builds a Handler.
func NewHandler(svc *Service, authH *auth.Handler) *Handler {
	return &Handler{svc: svc, auth: authH}
}

// Routes mounts quiz endpoints; caller wraps with auth middleware.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/quizzes", h.list)
	r.Post("/quizzes", h.create)
	r.Get("/quizzes/{quizId}", h.get)
	r.Patch("/quizzes/{quizId}", h.update)
	r.Delete("/quizzes/{quizId}", h.delete)
	r.Post("/quizzes/{quizId}/publish", h.publish)
	r.Post("/quizzes/{quizId}/unpublish", h.unpublish)
	r.Get("/quizzes/{quizId}/preview", h.preview)
}

func ownerOf(r *http.Request) (uuid.UUID, bool) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		return uuid.Nil, false
	}
	return u.ID, true
}

func quizID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "quizId"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// ifMatch parses If-Match: "<version>".
func ifMatch(r *http.Request) (int, bool) {
	h := r.Header.Get("If-Match")
	if len(h) < 3 || !strings.HasPrefix(h, `"`) || !strings.HasSuffix(h, `"`) {
		return 0, false
	}
	n, err := strconv.Atoi(h[1 : len(h)-1])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func mapQuizError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	var vc *ErrVersionConflict
	if errors.As(err, &vc) {
		httpserver.WriteErrorFields(w, r, http.StatusConflict, httpserver.CodeVersionConflict,
			"That quiz changed elsewhere. Reload and try again.", nil, false, nil,
			map[string]any{"currentVersion": vc.CurrentVersion})
		return true
	}
	if errors.Is(err, ErrNotFound) {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return true
	}
	var fe FieldError
	if errors.As(err, &fe) {
		fields := map[string][]string(fe)
		httpserver.WriteErrorFields(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed,
			"Some fields need attention.", fields, false, nil, nil)
		return true
	}
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternal, "Something went wrong.")
	return true
}

func summaryDTO(s Summary) map[string]any {
	return map[string]any{
		"id": s.ID.String(), "title": s.Title, "description": s.Description,
		"status": s.Status, "questionCount": s.QuestionCount, "version": s.Version,
		"createdAt": s.CreatedAt.UTC().Format(timeFormat), "updatedAt": s.UpdatedAt.UTC().Format(timeFormat),
		"publishedAt": nullableTime(s.PublishedAt),
	}
}

func quizDTO(q Quiz) map[string]any {
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
		"createdAt": q.CreatedAt.UTC().Format(timeFormat), "updatedAt": q.UpdatedAt.UTC().Format(timeFormat),
		"publishedAt": nullableTime(q.PublishedAt), "questions": qs,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	q := r.URL.Query()
	pageSize := 20
	if v := q.Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "pageSize must be 1-50.")
			return
		}
		pageSize = n
	}
	sort := q.Get("sort")
	if sort == "" {
		sort = "-updatedAt"
	}
	items, _, hasMore, err := h.svc.ListSummaries(r.Context(), owner, q.Get("status"), q.Get("query"), sort, pageSize, q.Get("cursor"))
	if err != nil {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Invalid list parameters.")
		return
	}
	data := make([]any, 0, len(items))
	for _, s := range items {
		data = append(data, summaryDTO(s))
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": data, "page": map[string]any{"nextCursor": nil, "hasMore": hasMore}})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	if body.Title == "" {
		body.Title = "Untitled quiz"
	}
	q, err := h.svc.CreateBlank(r.Context(), owner, body.Title, body.Description)
	if mapQuizError(w, r, err) {
		return
	}
	w.Header().Set("ETag", `"`+strconv.Itoa(q.Version)+`"`)
	httpserver.WriteJSON(w, r, http.StatusCreated, map[string]any{"data": quizDTO(q)})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, valid := quizID(r)
	if !valid {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	q, err := h.svc.GetForOwner(r.Context(), id, owner)
	if mapQuizError(w, r, err) {
		return
	}
	w.Header().Set("ETag", `"`+strconv.Itoa(q.Version)+`"`)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": quizDTO(q)})
}

type optionWrite struct {
	ID        *string `json:"id"`
	Position  int     `json:"position"`
	Text      string  `json:"text"`
	IsCorrect bool    `json:"isCorrect"`
}

type questionWrite struct {
	ID               *string       `json:"id"`
	Type             string        `json:"type"`
	Position         int           `json:"position"`
	Prompt           string        `json:"prompt"`
	Explanation      string        `json:"explanation"`
	Difficulty       string        `json:"difficulty"`
	TimeLimitSeconds int           `json:"timeLimitSeconds"`
	ScoringMode      string        `json:"scoringMode"`
	BasePoints       int           `json:"basePoints"`
	Options          []optionWrite `json:"options"`
}

func toDomainQuestions(in []questionWrite) ([]Question, error) {
	out := make([]Question, 0, len(in))
	for _, qw := range in {
		q := Question{
			Type: qw.Type, Position: qw.Position, Prompt: qw.Prompt, Explanation: qw.Explanation,
			Difficulty: qw.Difficulty, TimeLimitSeconds: qw.TimeLimitSeconds,
			ScoringMode: qw.ScoringMode, BasePoints: qw.BasePoints,
		}
		if qw.ID != nil {
			id, err := uuid.Parse(*qw.ID)
			if err != nil {
				return nil, err
			}
			q.ID = id
		}
		for _, ow := range qw.Options {
			o := Option{Position: ow.Position, Text: ow.Text, IsCorrect: ow.IsCorrect}
			if ow.ID != nil {
				id, err := uuid.Parse(*ow.ID)
				if err != nil {
					return nil, err
				}
				o.ID = id
			}
			q.Options = append(q.Options, o)
		}
		out = append(out, q)
	}
	return out, nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, valid := quizID(r)
	if !valid {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	ver, hasVer := ifMatch(r)
	if !hasVer {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "If-Match with current version is required.")
		return
	}
	var body struct {
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Questions   []questionWrite `json:"questions"`
	}
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	domain, err := toDomainQuestions(body.Questions)
	if err != nil {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "Invalid question or option IDs.")
		return
	}
	q, err := h.svc.Update(r.Context(), id, owner, ver, body.Title, body.Description, domain)
	if mapQuizError(w, r, err) {
		return
	}
	w.Header().Set("ETag", `"`+strconv.Itoa(q.Version)+`"`)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": quizDTO(q)})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, valid := quizID(r)
	if !valid {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	if mapQuizError(w, r, h.svc.Delete(r.Context(), id, owner)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, valid := quizID(r)
	if !valid {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	ver, hasVer := ifMatch(r)
	if !hasVer {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "If-Match with current version is required.")
		return
	}
	q, err := h.svc.Publish(r.Context(), id, owner, ver)
	if mapQuizError(w, r, err) {
		return
	}
	w.Header().Set("ETag", `"`+strconv.Itoa(q.Version)+`"`)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": quizDTO(q)})
}

func (h *Handler) unpublish(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, valid := quizID(r)
	if !valid {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	ver, hasVer := ifMatch(r)
	if !hasVer {
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationFailed, "If-Match with current version is required.")
		return
	}
	q, err := h.svc.Unpublish(r.Context(), id, owner, ver)
	if mapQuizError(w, r, err) {
		return
	}
	w.Header().Set("ETag", `"`+strconv.Itoa(q.Version)+`"`)
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": quizDTO(q)})
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerOf(r)
	if !ok {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeAuthRequired, "Sign in to continue.")
		return
	}
	id, valid := quizID(r)
	if !valid {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Quiz not found.")
		return
	}
	q, err := h.svc.Preview(r.Context(), id, owner)
	if mapQuizError(w, r, err) {
		return
	}
	httpserver.WriteJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{
		"quiz": quizDTO(q), "persistedVersion": q.Version, "responsesSaved": false,
	}})
}
