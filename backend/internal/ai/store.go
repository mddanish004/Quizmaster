package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Generation is the persisted metadata record (no raw prompt/output).
type Generation struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	QuizID       *uuid.UUID
	Provider     string
	Model        string
	Status       string
	Attempts     int
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	ErrorCode    *string
	CreatedAt    time.Time
}

// Store owns ai_generation + idempotency SQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ErrIdempotencyMismatch is returned when a key is reused with a different body.
var ErrIdempotencyMismatch = errors.New("idempotency mismatch")

// CreateRunning inserts a running generation record.
func (s *Store) CreateRunning(ctx context.Context, id, userID uuid.UUID, provider, model, fingerprint string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ai_generation(id,user_id,provider,model,prompt_template,status,attempts,request_fingerprint) VALUES($1,$2,$3,$4,'quiz-v1.0.0','running',1,$5)`,
		id, userID, provider, model, fingerprint)
	return err
}

// FinalizeSuccess marks succeeded and links the quiz.
func (s *Store) FinalizeSuccess(ctx context.Context, id, quizID uuid.UUID, usage Usage, attempts int) error {
	_, err := s.pool.Exec(ctx, `UPDATE ai_generation SET status='succeeded',quiz_id=$2,input_tokens=$3,output_tokens=$4,latency_ms=$5,attempts=$6 WHERE id=$1`,
		id, quizID, usage.InputTokens, usage.OutputTokens, usage.Latency.Milliseconds(), attempts)
	return err
}

// FinalizeFailure marks failed with an error code.
func (s *Store) FinalizeFailure(ctx context.Context, id uuid.UUID, code string, attempts int) error {
	_, err := s.pool.Exec(ctx, `UPDATE ai_generation SET status='failed',error_code=$2,attempts=$3 WHERE id=$1`, id, code, attempts)
	return err
}

// GetForUser loads a generation owned by the user.
func (s *Store) GetForUser(ctx context.Context, id, userID uuid.UUID) (Generation, error) {
	var g Generation
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,quiz_id,provider,model,status,attempts,input_tokens,output_tokens,latency_ms,error_code,created_at FROM ai_generation WHERE id=$1 AND user_id=$2`,
		id, userID).Scan(&g.ID, &g.UserID, &g.QuizID, &g.Provider, &g.Model, &g.Status, &g.Attempts, &g.InputTokens, &g.OutputTokens, &g.LatencyMs, &g.ErrorCode, &g.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Generation{}, ErrNotFound
		}
		return Generation{}, err
	}
	return g, nil
}

// ErrNotFound is returned for missing generations.
var ErrNotFound = errors.New("generation not found")

// CountSince counts a user's generations since a time (quota).
func (s *Store) CountSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ai_generation WHERE user_id=$1 AND created_at>$2`, userID, since).Scan(&n)
	return n, err
}

// CheckIdempotency returns a stored response when the same key+body repeats.
func (s *Store) CheckIdempotency(ctx context.Context, userID uuid.UUID, scope string, key, body []byte) (status int, resp []byte, found bool, err error) {
	kh := sha256.Sum256(key)
	bh := sha256.Sum256(body)
	var storedHash []byte
	var storedStatus int
	var storedBody []byte
	err = s.pool.QueryRow(ctx, `SELECT request_hash,response_status,response_body FROM idempotency_key WHERE user_id=$1 AND scope=$2 AND key_hash=$3`,
		userID, scope, kh[:]).Scan(&storedHash, &storedStatus, &storedBody)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, false, nil
		}
		return 0, nil, false, err
	}
	if string(storedHash) != string(bh[:]) {
		return 0, nil, true, ErrIdempotencyMismatch
	}
	return storedStatus, storedBody, true, nil
}

// SaveIdempotency stores a response for 24h.
func (s *Store) SaveIdempotency(ctx context.Context, userID uuid.UUID, scope string, key, body []byte, status int, resp []byte) error {
	kh := sha256.Sum256(key)
	bh := sha256.Sum256(body)
	_, err := s.pool.Exec(ctx, `INSERT INTO idempotency_key(user_id,scope,key_hash,request_hash,response_status,response_body,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+interval '24 hours')
		ON CONFLICT (user_id,scope,key_hash) DO NOTHING`, userID, scope, kh[:], bh[:], status, resp)
	return err
}

// Fingerprint builds the hex request fingerprint (prefix-safe for logs).
func Fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
