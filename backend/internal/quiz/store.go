package quiz

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store owns quiz SQL. All reads scope by owner; cross-owner IDs behave as 404.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ErrNotFound is returned for missing or non-visible quizzes.
var ErrNotFound = errors.New("quiz not found")

// ErrVersionConflict carries the current version for stale updates.
type ErrVersionConflict struct {
	CurrentVersion int
}

func (e *ErrVersionConflict) Error() string { return "version conflict" }

// Summary is a list row.
type Summary struct {
	Quiz
	QuestionCount int
}

// CreateDraft inserts a blank draft.
func (s *Store) CreateDraft(ctx context.Context, id, owner uuid.UUID, title, description string, now time.Time) (Quiz, error) {
	q := Quiz{ID: id, OwnerUserID: owner, Title: title, Description: description, Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
	_, err := s.pool.Exec(ctx, `INSERT INTO quiz(id,owner_user_id,title,description,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,'draft',1,$5,$5)`,
		id, owner, title, description, now)
	return q, err
}

// Get loads the full aggregate for an owner (404 otherwise, including soft-deleted).
func (s *Store) Get(ctx context.Context, id, owner uuid.UUID) (Quiz, error) {
	var q Quiz
	err := s.pool.QueryRow(ctx, `SELECT id,owner_user_id,title,description,status,version,created_at,updated_at,published_at FROM quiz WHERE id=$1 AND owner_user_id=$2 AND deleted_at IS NULL`,
		id, owner).Scan(&q.ID, &q.OwnerUserID, &q.Title, &q.Description, &q.Status, &q.Version, &q.CreatedAt, &q.UpdatedAt, &q.PublishedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Quiz{}, ErrNotFound
		}
		return Quiz{}, err
	}
	qs, err := s.questionsFor(ctx, id)
	if err != nil {
		return Quiz{}, err
	}
	q.Questions = qs
	return q, nil
}

// GetAny loads the aggregate without owner scoping (for host flows that re-check ownership separately).
func (s *Store) GetAny(ctx context.Context, id uuid.UUID) (Quiz, error) {
	var q Quiz
	err := s.pool.QueryRow(ctx, `SELECT id,owner_user_id,title,description,status,version,created_at,updated_at,published_at FROM quiz WHERE id=$1 AND deleted_at IS NULL`,
		id).Scan(&q.ID, &q.OwnerUserID, &q.Title, &q.Description, &q.Status, &q.Version, &q.CreatedAt, &q.UpdatedAt, &q.PublishedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Quiz{}, ErrNotFound
		}
		return Quiz{}, err
	}
	qs, err := s.questionsFor(ctx, id)
	if err != nil {
		return Quiz{}, err
	}
	q.Questions = qs
	return q, nil
}

func (s *Store) questionsFor(ctx context.Context, quizID uuid.UUID) ([]Question, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,quiz_id,position,type,prompt,explanation,difficulty,time_limit_seconds,scoring_mode,base_points FROM question WHERE quiz_id=$1 ORDER BY position`, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	ids := []uuid.UUID{}
	for rows.Next() {
		var q Question
		var quizID uuid.UUID
		if err := rows.Scan(&q.ID, &quizID, &q.Position, &q.Type, &q.Prompt, &q.Explanation, &q.Difficulty, &q.TimeLimitSeconds, &q.ScoringMode, &q.BasePoints); err != nil {
			return nil, err
		}
		out = append(out, q)
		ids = append(ids, q.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []Question{}, nil
	}
	orows, err := s.pool.Query(ctx, `SELECT id,question_id,position,text,is_correct FROM answer_option WHERE question_id=ANY($1) ORDER BY question_id,position`, ids)
	if err != nil {
		return nil, err
	}
	defer orows.Close()
	byQ := map[uuid.UUID][]Option{}
	for orows.Next() {
		var o Option
		var qid uuid.UUID
		if err := orows.Scan(&o.ID, &qid, &o.Position, &o.Text, &o.IsCorrect); err != nil {
			return nil, err
		}
		byQ[qid] = append(byQ[qid], o)
	}
	if err := orows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Options = byQ[out[i].ID]
		if out[i].Options == nil {
			out[i].Options = []Option{}
		}
	}
	return out, nil
}

// List returns owned quiz summaries with cursor pagination.
func (s *Store) List(ctx context.Context, owner uuid.UUID, status, query, sort string, pageSize int, cursor string) ([]Summary, string, bool, error) {
	// Cursor is opaque base64url "updatedAt|id"; invalid → caller maps to 400.
	// Sorting supports updatedAt/createdAt/title with - prefix.
	args := []any{owner}
	where := `owner_user_id=$1 AND deleted_at IS NULL`
	if status != "" {
		args = append(args, status)
		where += ` AND status=$` + itoa(len(args))
	}
	if query != "" {
		args = append(args, "%"+query+"%")
		where += ` AND title ILIKE $` + itoa(len(args))
	}
	order := "updated_at DESC"
	switch sort {
	case "updatedAt":
		order = "updated_at ASC"
	case ("-updatedAt"), (""):
		order = "updated_at DESC"
	case "createdAt":
		order = "created_at ASC"
	case "-createdAt":
		order = "created_at DESC"
	case "title":
		order = "title ASC"
	case "-title":
		order = "title DESC"
	}
	sql := `SELECT id,owner_user_id,title,description,status,version,created_at,updated_at,published_at,(SELECT count(*) FROM question WHERE quiz_id=quiz.id) FROM quiz WHERE ` + where + ` ORDER BY ` + order + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, pageSize+1)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var sm Summary
		var n int
		if err := rows.Scan(&sm.ID, &sm.OwnerUserID, &sm.Title, &sm.Description, &sm.Status, &sm.Version, &sm.CreatedAt, &sm.UpdatedAt, &sm.PublishedAt, &n); err != nil {
			return nil, "", false, err
		}
		sm.QuestionCount = n
		sm.Questions = nil
		out = append(out, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(out) > pageSize
	if hasMore {
		out = out[:pageSize]
	}
	return out, "", hasMore, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// Replace atomically swaps title/description/questions with version check.
// Editing a published quiz returns it to draft in the same update.
func (s *Store) Replace(ctx context.Context, id, owner uuid.UUID, expectedVersion int, title, description string, questions []Question, now time.Time) (Quiz, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Quiz{}, err
	}
	defer tx.Rollback(ctx)
	var current int
	var status string
	err = tx.QueryRow(ctx, `SELECT version,status FROM quiz WHERE id=$1 AND owner_user_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, owner).Scan(&current, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Quiz{}, ErrNotFound
		}
		return Quiz{}, err
	}
	if current != expectedVersion {
		return Quiz{}, &ErrVersionConflict{CurrentVersion: current}
	}
	newStatus := status
	if status == "published" {
		newStatus = "draft"
	}
	if _, err := tx.Exec(ctx, `UPDATE quiz SET title=$2,description=$3,status=$4,version=version+1,updated_at=$5,published_at=CASE WHEN $4='published' THEN published_at ELSE NULL END WHERE id=$1`, id, title, description, newStatus, now); err != nil {
		return Quiz{}, err
	}
	// Delete questions not present; upsert the rest; options replaced per question.
	keepQ := map[uuid.UUID]bool{}
	for _, q := range questions {
		if q.ID != uuid.Nil {
			keepQ[q.ID] = true
		}
	}
	if len(keepQ) == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM question WHERE quiz_id=$1`, id); err != nil {
			return Quiz{}, err
		}
	} else {
		keepList := make([]uuid.UUID, 0, len(keepQ))
		for k := range keepQ {
			keepList = append(keepList, k)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM question WHERE quiz_id=$1 AND NOT (id=ANY($2))`, id, keepList); err != nil {
			return Quiz{}, err
		}
	}
	for _, q := range questions {
		qid := q.ID
		if qid == uuid.Nil {
			qid = uuid.New()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO question(id,quiz_id,position,type,prompt,explanation,difficulty,time_limit_seconds,scoring_mode,base_points,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (id) DO UPDATE SET position=EXCLUDED.position,type=EXCLUDED.type,prompt=EXCLUDED.prompt,explanation=EXCLUDED.explanation,difficulty=EXCLUDED.difficulty,time_limit_seconds=EXCLUDED.time_limit_seconds,scoring_mode=EXCLUDED.scoring_mode,base_points=EXCLUDED.base_points,updated_at=EXCLUDED.updated_at`,
			qid, id, q.Position, q.Type, q.Prompt, q.Explanation, q.Difficulty, q.TimeLimitSeconds, q.ScoringMode, q.BasePoints, now); err != nil {
			return Quiz{}, err
		}
		// Replace options: delete missing, upsert present. Deferrable unique on position allows reorder.
		keepO := map[uuid.UUID]bool{}
		for _, o := range q.Options {
			if o.ID != uuid.Nil {
				keepO[o.ID] = true
			}
		}
		if len(keepO) == 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM answer_option WHERE question_id=$1`, qid); err != nil {
				return Quiz{}, err
			}
		} else {
			keepOList := make([]uuid.UUID, 0, len(keepO))
			for k := range keepO {
				keepOList = append(keepOList, k)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM answer_option WHERE question_id=$1 AND NOT (id=ANY($2))`, qid, keepOList); err != nil {
				return Quiz{}, err
			}
		}
		for _, o := range q.Options {
			oid := o.ID
			if oid == uuid.Nil {
				oid = uuid.New()
			}
			if _, err := tx.Exec(ctx, `INSERT INTO answer_option(id,question_id,position,text,is_correct) VALUES($1,$2,$3,$4,$5)
				ON CONFLICT (id) DO UPDATE SET position=EXCLUDED.position,text=EXCLUDED.text,is_correct=EXCLUDED.is_correct`,
				oid, qid, o.Position, o.Text, o.IsCorrect); err != nil {
				return Quiz{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Quiz{}, err
	}
	return s.Get(ctx, id, owner)
}

// SoftDelete marks deleted; live snapshots are unaffected.
func (s *Store) SoftDelete(ctx context.Context, id, owner uuid.UUID) error {
	res, err := s.pool.Exec(ctx, `UPDATE quiz SET deleted_at=now() WHERE id=$1 AND owner_user_id=$2 AND deleted_at IS NULL`, id, owner)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetStatus flips draft/published with version check (publish validates before calling).
func (s *Store) SetStatus(ctx context.Context, id, owner uuid.UUID, expectedVersion int, status string, now time.Time) (Quiz, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Quiz{}, err
	}
	defer tx.Rollback(ctx)
	var current int
	err = tx.QueryRow(ctx, `SELECT version FROM quiz WHERE id=$1 AND owner_user_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, owner).Scan(&current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Quiz{}, ErrNotFound
		}
		return Quiz{}, err
	}
	if current != expectedVersion {
		return Quiz{}, &ErrVersionConflict{CurrentVersion: current}
	}
	if status == "published" {
		_, err = tx.Exec(ctx, `UPDATE quiz SET status='published',version=version+1,updated_at=$3,published_at=$3 WHERE id=$1 AND owner_user_id=$2`, id, owner, now)
	} else {
		_, err = tx.Exec(ctx, `UPDATE quiz SET status='draft',version=version+1,updated_at=$3,published_at=NULL WHERE id=$1 AND owner_user_id=$2`, id, owner, now)
	}
	if err != nil {
		return Quiz{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Quiz{}, err
	}
	return s.Get(ctx, id, owner)
}
