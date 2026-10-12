package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// ErrorCode constants mirror docs/openapi.yaml stable codes.
const (
	CodeValidationFailed = "VALIDATION_FAILED"
	CodeAuthRequired     = "AUTH_REQUIRED"
	CodeInvalidCreds     = "INVALID_CREDENTIALS"
	CodeCSRF             = "CSRF_FAILED"
	CodeForbidden        = "FORBIDDEN"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeVersionConflict  = "VERSION_CONFLICT"
	CodeRateLimited      = "RATE_LIMITED"
	CodeAIUnavailable    = "AI_UNAVAILABLE"
	CodeAIInvalid        = "AI_INVALID_OUTPUT"
	CodeNotJoinable      = "SESSION_NOT_JOINABLE"
	CodeInvalidState     = "INVALID_STATE"
	CodeQuestionClosed   = "QUESTION_CLOSED"
	CodeInternal         = "INTERNAL"
	CodeGone             = "GONE"
)

// APIError is the stable error envelope.
type APIError struct {
	Code              string              `json:"code"`
	Message           string              `json:"message"`
	RequestID         string              `json:"requestId"`
	Fields            map[string][]string `json:"fields,omitempty"`
	Retryable         bool                `json:"retryable"`
	RetryAfterSeconds *int                `json:"retryAfterSeconds,omitempty"`
	Details           map[string]any      `json:"details,omitempty"`
}

func requestID(r *http.Request) string {
	if v := r.Context().Value(ctxKeyRequestID{}); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return uuid.NewString()
}

// WriteError renders the stable envelope. Internal details must never leak.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteErrorFields(w, r, status, code, message, nil, false, nil, nil)
}

// WriteErrorFields renders the envelope with optional fields.
func WriteErrorFields(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string][]string, retryable bool, retryAfter *int, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": APIError{
			Code: code, Message: message, RequestID: requestID(r),
			Fields: fields, Retryable: retryable, RetryAfterSeconds: retryAfter, Details: details,
		},
	})
}

// WriteJSON renders a success envelope payload.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON decodes a single JSON object with DisallowUnknownFields and 1MiB cap.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "Request body is invalid.")
		return false
	}
	return true
}
