package quiz

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service enforces quiz ownership, validation, and lifecycle rules.
type Service struct {
	store *Store
}

// NewService builds a Service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{store: NewStore(pool)} }

// Store exposes the store for live/reporting modules (owner checks stay in callers).
func (s *Service) Store() *Store { return s.store }

// CreateBlank creates an empty draft.
func (s *Service) CreateBlank(ctx context.Context, owner uuid.UUID, title, description string) (Quiz, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Untitled quiz"
	}
	if fe := ValidateQuizWrite(title, description, nil, false); fe.HasErrors() {
		return Quiz{}, fe
	}
	now := time.Now()
	return s.store.CreateDraft(ctx, uuid.New(), owner, title, description, now)
}

// ListSummaries pages owned quizzes.
func (s *Service) ListSummaries(ctx context.Context, owner uuid.UUID, status, query, sort string, pageSize int, cursor string) ([]Summary, string, bool, error) {
	if pageSize < 1 || pageSize > 50 {
		return nil, "", false, errors.New("pageSize must be 1-50")
	}
	switch sort {
	case "", "updatedAt", "-updatedAt", "createdAt", "-createdAt", "title", "-title":
	default:
		return nil, "", false, errors.New("invalid sort")
	}
	if status != "" && status != "draft" && status != "published" && status != "archived" {
		return nil, "", false, errors.New("invalid status")
	}
	return s.store.List(ctx, owner, status, query, sort, pageSize, cursor)
}

// GetForOwner loads the aggregate or ErrNotFound.
func (s *Service) GetForOwner(ctx context.Context, id, owner uuid.UUID) (Quiz, error) {
	return s.store.Get(ctx, id, owner)
}

// Update replaces the aggregate after domain validation (draft rules).
func (s *Service) Update(ctx context.Context, id, owner uuid.UUID, expectedVersion int, title, description string, questions []Question) (Quiz, error) {
	if fe := ValidateQuizWrite(title, description, questions, false); fe.HasErrors() {
		return Quiz{}, fe
	}
	return s.store.Replace(ctx, id, owner, expectedVersion, title, description, questions, time.Now())
}

// Delete soft-deletes.
func (s *Service) Delete(ctx context.Context, id, owner uuid.UUID) error {
	return s.store.SoftDelete(ctx, id, owner)
}

// Publish validates completeness then flips status.
func (s *Service) Publish(ctx context.Context, id, owner uuid.UUID, expectedVersion int) (Quiz, error) {
	q, err := s.store.Get(ctx, id, owner)
	if err != nil {
		return Quiz{}, err
	}
	if q.Version != expectedVersion {
		return Quiz{}, &ErrVersionConflict{CurrentVersion: q.Version}
	}
	if q.Status == "published" {
		return q, nil // idempotent
	}
	if fe := ValidateQuizWrite(q.Title, q.Description, q.Questions, true); fe.HasErrors() {
		return Quiz{}, fe
	}
	return s.store.SetStatus(ctx, id, owner, expectedVersion, "published", time.Now())
}

// Unpublish returns to draft.
func (s *Service) Unpublish(ctx context.Context, id, owner uuid.UUID, expectedVersion int) (Quiz, error) {
	return s.store.SetStatus(ctx, id, owner, expectedVersion, "draft", time.Now())
}

// Preview returns the persisted aggregate without writes (owner-only, includes keys).
func (s *Service) Preview(ctx context.Context, id, owner uuid.UUID) (Quiz, error) {
	q, err := s.store.Get(ctx, id, owner)
	if err != nil {
		return Quiz{}, err
	}
	if fe := ValidateQuizWrite(q.Title, q.Description, q.Questions, false); fe.HasErrors() {
		return Quiz{}, fe
	}
	return q, nil
}
