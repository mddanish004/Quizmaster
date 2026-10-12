package live

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
)

// Store owns live SQL. Every mutation commits before any broadcast (callers broadcast).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the pool for service transactions.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// CreateSessionWithSnapshot inserts the session + immutable copies in one tx.
// Retries join-code collision in the service (up to 5 times).
func (s *Store) CreateSessionWithSnapshot(ctx context.Context, sess Session, questions []quiz.Question) (Session, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO live_session(id,quiz_id,host_user_id,quiz_version,join_code,status,state_version,expires_at,allow_late_join,show_leaderboard,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,'lobby',1,$6,$7,$8,now(),now())`,
		sess.ID, sess.QuizID, sess.HostUserID, sess.QuizVersion, sess.JoinCode, sess.ExpiresAt, sess.AllowLateJoin, sess.ShowLeaderboard)
	if err != nil {
		return Session{}, err
	}
	for _, q := range questions {
		sqid := uuid.New()
		var sourceQ *uuid.UUID
		if q.ID != uuid.Nil {
			id := q.ID
			sourceQ = &id
		}
		if _, err := tx.Exec(ctx, `INSERT INTO session_question(id,live_session_id,source_question_id,position,type,prompt,explanation,difficulty,time_limit_seconds,scoring_mode,base_points)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			sqid, sess.ID, sourceQ, q.Position, q.Type, q.Prompt, q.Explanation, q.Difficulty, q.TimeLimitSeconds, q.ScoringMode, q.BasePoints); err != nil {
			return Session{}, err
		}
		for _, o := range q.Options {
			var sourceO *uuid.UUID
			if o.ID != uuid.Nil {
				id := o.ID
				sourceO = &id
			}
			if _, err := tx.Exec(ctx, `INSERT INTO session_option(id,session_question_id,source_option_id,position,text,is_correct) VALUES($1,$2,$3,$4,$5,$6)`,
				uuid.New(), sqid, sourceO, o.Position, o.Text, o.IsCorrect); err != nil {
				return Session{}, err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO session_event(live_session_id,version,event_type,public_payload,host_payload) VALUES($1,1,'session_started','{}','{}')`, sess.ID); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return s.GetSession(ctx, sess.ID)
}

// SessionRow loads session + counts (no questions).
func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (Session, error) {
	var sess Session
	var currentQID *uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT s.id,s.quiz_id,s.host_user_id,s.quiz_version,s.join_code,s.status,s.state_version,
		s.current_question_id,s.question_started_at,s.question_ends_at,s.started_at,s.ended_at,s.expires_at,
		s.allow_late_join,s.show_leaderboard,s.created_at,
		(SELECT count(*) FROM participant p WHERE p.live_session_id=s.id),
		(SELECT count(*) FROM session_question q WHERE q.live_session_id=s.id)
		FROM live_session s WHERE s.id=$1`, id).Scan(
		&sess.ID, &sess.QuizID, &sess.HostUserID, &sess.QuizVersion, &sess.JoinCode, &sess.Status, &sess.StateVersion,
		&currentQID, &sess.QuestionStartsAt, &sess.QuestionEndsAt, &sess.StartedAt, &sess.EndedAt, &sess.ExpiresAt,
		&sess.AllowLateJoin, &sess.ShowLeaderboard, &sess.CreatedAt, &sess.ParticipantCount, &sess.QuestionCount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	if currentQID != nil {
		q, err := s.getQuestion(ctx, *currentQID)
		if err != nil {
			return Session{}, err
		}
		sess.CurrentQuestion = &q
	}
	return sess, nil
}

// GetSessionByCode finds an active session by join code.
func (s *Store) GetSessionByCode(ctx context.Context, code string) (Session, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM live_session WHERE join_code=$1 AND status<>'ended'`, code).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrGone
		}
		return Session{}, err
	}
	return s.GetSession(ctx, id)
}

// ListQuestions returns ordered snapshot questions with options.
func (s *Store) ListQuestions(ctx context.Context, sessionID uuid.UUID) ([]SessionQuestion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,live_session_id,source_question_id,position,type,prompt,explanation,difficulty,time_limit_seconds,scoring_mode,base_points FROM session_question WHERE live_session_id=$1 ORDER BY position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionQuestion
	ids := []uuid.UUID{}
	for rows.Next() {
		var q SessionQuestion
		if err := rows.Scan(&q.ID, &q.LiveSessionID, &q.SourceQuestionID, &q.Position, &q.Type, &q.Prompt, &q.Explanation, &q.Difficulty, &q.TimeLimitSeconds, &q.ScoringMode, &q.BasePoints); err != nil {
			return nil, err
		}
		out = append(out, q)
		ids = append(ids, q.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		orows, err := s.pool.Query(ctx, `SELECT id,session_question_id,position,text,is_correct FROM session_option WHERE session_question_id=ANY($1) ORDER BY session_question_id,position`, ids)
		if err != nil {
			return nil, err
		}
		defer orows.Close()
		byQ := map[uuid.UUID][]SessionOption{}
		for orows.Next() {
			var o SessionOption
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
		}
	}
	return out, nil
}

func (s *Store) getQuestion(ctx context.Context, id uuid.UUID) (SessionQuestion, error) {
	var q SessionQuestion
	err := s.pool.QueryRow(ctx, `SELECT id,live_session_id,source_question_id,position,type,prompt,explanation,difficulty,time_limit_seconds,scoring_mode,base_points FROM session_question WHERE id=$1`, id).Scan(
		&q.ID, &q.LiveSessionID, &q.SourceQuestionID, &q.Position, &q.Type, &q.Prompt, &q.Explanation, &q.Difficulty, &q.TimeLimitSeconds, &q.ScoringMode, &q.BasePoints)
	if err != nil {
		return SessionQuestion{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,position,text,is_correct FROM session_option WHERE session_question_id=$1 ORDER BY position`, id)
	if err != nil {
		return SessionQuestion{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var o SessionOption
		if err := rows.Scan(&o.ID, &o.Position, &o.Text, &o.IsCorrect); err != nil {
			return SessionQuestion{}, err
		}
		q.Options = append(q.Options, o)
	}
	return q, rows.Err()
}

// JoinOrResume inserts a participant or returns the existing row for a resume token.
func (s *Store) JoinOrResume(ctx context.Context, sessionID uuid.UUID, display, key string, tokenHash []byte) (Participant, bool, error) {
	// Resume path: same browser cookie rejoining.
	var p Participant
	err := s.pool.QueryRow(ctx, `SELECT id,live_session_id,display_name,score,joined_at FROM participant WHERE resume_token_hash=$1 AND live_session_id=$2`,
		tokenHash, sessionID).Scan(&p.ID, &p.SessionID, &p.DisplayName, &p.Score, &p.JoinedAt)
	if err == nil {
		_, _ = s.pool.Exec(ctx, `UPDATE participant SET last_seen_at=now(),left_at=NULL WHERE id=$1`, p.ID)
		return p, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Participant{}, false, err
	}
	p = Participant{ID: uuid.New(), SessionID: sessionID, DisplayName: display}
	err = s.pool.QueryRow(ctx, `INSERT INTO participant(id,live_session_id,display_name,display_name_key,resume_token_hash) VALUES($1,$2,$3,$4,$5) RETURNING joined_at`,
		p.ID, sessionID, display, key, tokenHash).Scan(&p.JoinedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Participant{}, false, ErrNameTaken
		}
		return Participant{}, false, err
	}
	return p, true, nil
}

// GetParticipantByToken resolves a resume token to its participant.
func (s *Store) GetParticipantByToken(ctx context.Context, tokenHash []byte) (Participant, error) {
	var p Participant
	err := s.pool.QueryRow(ctx, `SELECT id,live_session_id,display_name,score,joined_at FROM participant WHERE resume_token_hash=$1`, tokenHash).Scan(
		&p.ID, &p.SessionID, &p.DisplayName, &p.Score, &p.JoinedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Participant{}, ErrNotFound
		}
		return Participant{}, err
	}
	return p, nil
}

// AnswerOutcome is the committed answer result.
type AnswerOutcome struct {
	AnswerID   uuid.UUID
	QuestionID uuid.UUID
	IsCorrect  bool
	Points     int
	ResponseMs int
	ReceivedAt time.Time
	Duplicate  bool
}

// SubmitAnswer runs the answer transaction with database clock receipt.
// Unique conflicts return the original outcome without rescoring.
func (s *Store) SubmitAnswer(ctx context.Context, participantID, submissionID uuid.UUID, optionIDs []uuid.UUID) (AnswerOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AnswerOutcome{}, err
	}
	defer tx.Rollback(ctx)
	var sessionID uuid.UUID
	var status string
	var currentQ *uuid.UUID
	var startsAt, endsAt *time.Time
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `SELECT s.id,s.status,s.current_question_id,s.question_started_at,s.question_ends_at,s.expires_at
		FROM live_session s JOIN participant p ON p.live_session_id=s.id WHERE p.id=$1 FOR UPDATE OF s`, participantID).Scan(
		&sessionID, &status, &currentQ, &startsAt, &endsAt, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AnswerOutcome{}, ErrNotFound
		}
		return AnswerOutcome{}, err
	}
	var received time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&received); err != nil {
		return AnswerOutcome{}, err
	}
	if received.After(expiresAt) {
		return AnswerOutcome{}, ErrGone
	}
	if status != StateQuestionOpen || currentQ == nil {
		return AnswerOutcome{}, ErrQuestionClosed
	}
	if endsAt != nil && received.After(*endsAt) {
		return AnswerOutcome{}, ErrQuestionClosed
	}
	if len(optionIDs) != 1 {
		return AnswerOutcome{}, ErrQuestionClosed
	}
	// Verify the option belongs to the current question; load correctness + scoring.
	var isCorrect bool
	var scoringMode string
	var basePoints int
	err = tx.QueryRow(ctx, `SELECT o.is_correct,q.scoring_mode,q.base_points FROM session_option o
		JOIN session_question q ON q.id=o.session_question_id
		WHERE o.id=$1 AND o.session_question_id=$2`, optionIDs[0], *currentQ).Scan(&isCorrect, &scoringMode, &basePoints)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AnswerOutcome{}, ErrQuestionClosed
		}
		return AnswerOutcome{}, err
	}
	responseMs := 0
	if startsAt != nil {
		responseMs = int(received.Sub(*startsAt).Milliseconds())
		if responseMs < 0 {
			responseMs = 0
		}
	}
	points := ComputeScore(scoringMode, basePoints, derefTime(startsAt, received), derefTime(endsAt, received), received, isCorrect)
	answerID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO participant_answer(id,participant_id,session_question_id,submission_id,received_at,is_correct,score_awarded,response_ms)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, answerID, participantID, *currentQ, submissionID, received, isCorrect, points, responseMs)
	if err != nil {
		if isUniqueViolation(err) {
			// The transaction is aborted; roll back and return the original
			// outcome from a fresh read without rescoring.
			_ = tx.Rollback(ctx)
			var out AnswerOutcome
			qerr := s.pool.QueryRow(ctx, `SELECT id,session_question_id,is_correct,score_awarded,response_ms,received_at FROM participant_answer WHERE participant_id=$1 AND submission_id=$2`,
				participantID, submissionID).Scan(&out.AnswerID, &out.QuestionID, &out.IsCorrect, &out.Points, &out.ResponseMs, &out.ReceivedAt)
			if qerr != nil {
				if errors.Is(qerr, pgx.ErrNoRows) {
					qerr = s.pool.QueryRow(ctx, `SELECT id,session_question_id,is_correct,score_awarded,response_ms,received_at FROM participant_answer WHERE participant_id=$1 AND session_question_id=$2`,
						participantID, *currentQ).Scan(&out.AnswerID, &out.QuestionID, &out.IsCorrect, &out.Points, &out.ResponseMs, &out.ReceivedAt)
				}
				if qerr != nil {
					return AnswerOutcome{}, qerr
				}
			}
			out.Duplicate = true
			return out, nil
		}
		return AnswerOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO answer_selection(participant_answer_id,session_option_id) VALUES($1,$2)`, answerID, optionIDs[0]); err != nil {
		return AnswerOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE participant SET score=score+$1,last_seen_at=now() WHERE id=$2`, points, participantID); err != nil {
		return AnswerOutcome{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnswerOutcome{}, err
	}
	return AnswerOutcome{AnswerID: answerID, QuestionID: *currentQ, IsCorrect: isCorrect, Points: points, ResponseMs: responseMs, ReceivedAt: received}, nil
}

func derefTime(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}

// Transition applies a host state change under row lock and appends the versioned event.
// Returns the new state version. Timer close uses CloseExpiredQuestion instead.
func (s *Store) Transition(ctx context.Context, sessionID uuid.UUID, expectedVersion int64, mutate func(sess *lockedSession) (eventType string, publicPayload, hostPayload map[string]any, err error)) (Session, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	var ls lockedSession
	err = tx.QueryRow(ctx, `SELECT id,status,state_version,current_question_id,question_started_at,question_ends_at FROM live_session WHERE id=$1 FOR UPDATE`, sessionID).Scan(
		&ls.ID, &ls.Status, &ls.Version, &ls.CurrentQ, &ls.StartsAt, &ls.EndsAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	if ls.Version != expectedVersion {
		return Session{}, ErrVersionMismatch
	}
	ls.tx = tx
	etype, pub, host, err := mutate(&ls)
	if err != nil {
		return Session{}, err
	}
	newVersion := ls.Version + 1
	if _, err := tx.Exec(ctx, `INSERT INTO session_event(live_session_id,version,event_type,public_payload,host_payload) VALUES($1,$2,$3,$4,$5)`,
		sessionID, newVersion, etype, pub, host); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return s.GetSession(ctx, sessionID)
}

type lockedSession struct {
	tx       pgx.Tx
	ID       uuid.UUID
	Status   string
	Version  int64
	CurrentQ *uuid.UUID
	StartsAt *time.Time
	EndsAt   *time.Time
}

// Exec runs a statement in the transition transaction.
func (l *lockedSession) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := l.tx.Exec(ctx, sql, args...)
	return err
}

// CloseExpiredQuestion idempotently closes an open question past deadline.
func (s *Store) CloseExpiredQuestion(ctx context.Context, sessionID uuid.UUID) (Session, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, false, err
	}
	defer tx.Rollback(ctx)
	var version int64
	var currentQ *uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE live_session SET status='question_results',state_version=state_version+1,updated_at=now()
		WHERE id=$1 AND status='question_open' AND question_ends_at<=now()
		RETURNING state_version,current_question_id`, sessionID).Scan(&version, &currentQ)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, false, nil // already closed or not due
		}
		return Session{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO session_event(live_session_id,version,event_type,public_payload,host_payload) VALUES($1,$2,'question_ended','{"reason":"timer"}','{"reason":"timer"}')`, sessionID, version); err != nil {
		return Session{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, false, err
	}
	sess, err := s.GetSession(ctx, sessionID)
	return sess, true, err
}

// AppendEvent inserts a non-state event (presence/count) without version bump.
func (s *Store) AppendEvent(ctx context.Context, sessionID uuid.UUID, eventType string, publicPayload, hostPayload map[string]any) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO session_event(live_session_id,version,event_type,public_payload,host_payload)
		SELECT $1,state_version,$2,$3,$4 FROM live_session WHERE id=$1`, sessionID, eventType, publicPayload, hostPayload)
	return err
}

// Leaderboard returns ranked participants (score desc, correct desc, response ms asc, joined).
func (s *Store) Leaderboard(ctx context.Context, sessionID uuid.UUID, limit int) ([]LeaderRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.display_name,p.score,p.joined_at,
		COALESCE((SELECT count(*) FROM participant_answer a WHERE a.participant_id=p.id AND a.is_correct),0) AS correct,
		COALESCE((SELECT sum(a.response_ms) FROM participant_answer a WHERE a.participant_id=p.id),0) AS ms
		FROM participant p WHERE p.live_session_id=$1
		ORDER BY p.score DESC, correct DESC, ms ASC, p.joined_at ASC, p.id ASC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaderRow
	rank := 0
	for rows.Next() {
		var r LeaderRow
		if err := rows.Scan(&r.ParticipantID, &r.DisplayName, &r.Score, &r.JoinedAt, &r.Correct, &r.ResponseMs); err != nil {
			return nil, err
		}
		rank++
		r.Rank = rank
		out = append(out, r)
	}
	return out, rows.Err()
}

// LeaderRow is one ranked participant.
type LeaderRow struct {
	ParticipantID uuid.UUID
	DisplayName   string
	Score         int
	Rank          int
	Correct       int
	ResponseMs    int
	JoinedAt      time.Time
}

// QuestionStats aggregates per-option counts for results.
type QuestionStats struct {
	QuestionID    uuid.UUID
	OptionCounts  map[string]int
	AnswerCount   int
	CorrectCount  int
	AvgResponseMs int
}

// StatsFor returns aggregates for one snapshot question.
func (s *Store) StatsFor(ctx context.Context, questionID uuid.UUID) (QuestionStats, error) {
	st := QuestionStats{QuestionID: questionID, OptionCounts: map[string]int{}}
	rows, err := s.pool.Query(ctx, `SELECT o.id::text,count(s.participant_answer_id) FROM session_option o
		LEFT JOIN answer_selection s ON s.session_option_id=o.id
		WHERE o.session_question_id=$1 GROUP BY o.id`, questionID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return st, err
		}
		st.OptionCounts[id] = n
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	_ = s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE is_correct),COALESCE(avg(response_ms)::int,0) FROM participant_answer WHERE session_question_id=$1`,
		questionID).Scan(&st.AnswerCount, &st.CorrectCount, &st.AvgResponseMs)
	return st, nil
}

// OwnAnswer loads a participant's answer for a question, if any.
func (s *Store) OwnAnswer(ctx context.Context, participantID, questionID uuid.UUID) (AnswerOutcome, bool) {
	var out AnswerOutcome
	err := s.pool.QueryRow(ctx, `SELECT id,session_question_id,is_correct,score_awarded,response_ms,received_at FROM participant_answer WHERE participant_id=$1 AND session_question_id=$2`,
		participantID, questionID).Scan(&out.AnswerID, &out.QuestionID, &out.IsCorrect, &out.Points, &out.ResponseMs, &out.ReceivedAt)
	if err != nil {
		return AnswerOutcome{}, false
	}
	return out, true
}

// ListActiveSessions returns non-ended sessions for restart recovery.
func (s *Store) ListActiveSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM live_session WHERE status<>'ended'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(ids))
	for _, id := range ids {
		sess, err := s.GetSession(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, sess)
	}
	return out, nil
}

// HistoryForOwner pages owned sessions (reporting reads; live owns writes).
func (s *Store) HistoryForOwner(ctx context.Context, owner uuid.UUID, pageSize int) ([]SessionHistoryRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,COALESCE((SELECT q.title FROM quiz q WHERE q.id=s.quiz_id),'Deleted quiz'),
		s.status,(SELECT count(*) FROM participant p WHERE p.live_session_id=s.id),
		(SELECT count(*) FROM session_question q WHERE q.live_session_id=s.id),
		s.created_at,s.ended_at FROM live_session s WHERE s.host_user_id=$1 ORDER BY s.created_at DESC LIMIT $2`, owner, pageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionHistoryRow
	for rows.Next() {
		var r SessionHistoryRow
		if err := rows.Scan(&r.ID, &r.QuizTitle, &r.Status, &r.ParticipantCount, &r.QuestionCount, &r.CreatedAt, &r.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SessionHistoryRow is one history entry.
type SessionHistoryRow struct {
	ID               uuid.UUID
	QuizTitle        string
	Status           string
	ParticipantCount int
	QuestionCount    int
	CreatedAt        time.Time
	EndedAt          *time.Time
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sub := range []string{"SQLSTATE 23505", "duplicate key"} {
		if len(msg) >= len(sub) && contains(msg, sub) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
