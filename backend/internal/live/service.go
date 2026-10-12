package live

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quizforge/quizforge-ai/backend/internal/auth"
	"github.com/quizforge/quizforge-ai/backend/internal/platform/config"
	"github.com/quizforge/quizforge-ai/backend/internal/quiz"
)

// Service owns session lifecycle rules; the Manager owns actors/broadcasts.
type Service struct {
	cfg     *config.Config
	store   *Store
	quizSvc *quiz.Service
	manager *Manager
}

// NewService builds a Service (manager attached via SetManager to avoid cycle).
func NewService(cfg *config.Config, pool *pgxpool.Pool) *Service {
	return &Service{cfg: cfg, store: NewStore(pool), quizSvc: quiz.NewService(pool)}
}

// SetManager attaches the broadcast manager (called after both exist).
func (s *Service) SetManager(m *Manager) { s.manager = m }

// Store exposes the store for WS/recovery paths.
func (s *Service) Store() *Store { return s.store }

// StartSession snapshots a published quiz into a lobby session (code retry ×5).
func (s *Service) StartSession(ctx context.Context, quizID, hostID uuid.UUID, allowLateJoin, showLeaderboard bool) (Session, error) {
	q, err := s.quizSvc.Store().GetAny(ctx, quizID)
	if err != nil {
		return Session{}, err
	}
	if q.OwnerUserID != hostID {
		return Session{}, ErrNotFound
	}
	if q.Status != "published" {
		return Session{}, ErrInvalidState
	}
	if len(q.Questions) == 0 {
		return Session{}, ErrInvalidState
	}
	var last error
	for i := 0; i < 5; i++ {
		code, err := NewJoinCode()
		if err != nil {
			return Session{}, err
		}
		sess := Session{
			ID: uuid.New(), QuizID: &quizID, HostUserID: hostID, QuizVersion: q.Version,
			JoinCode: code, AllowLateJoin: allowLateJoin, ShowLeaderboard: showLeaderboard,
			ExpiresAt: time.Now().Add(s.cfg.LiveSessionMaxTTL),
		}
		created, err := s.store.CreateSessionWithSnapshot(ctx, sess, q.Questions)
		if err == nil {
			if s.manager != nil {
				s.manager.EnsureActor(created.ID)
			}
			return created, nil
		}
		last = err
		if !isUniqueViolation(err) {
			return Session{}, err
		}
	}
	return Session{}, last
}

// Join validates the code/state and inserts or resumes a participant.
func (s *Service) Join(ctx context.Context, code, displayName string, resumeToken []byte) (Session, Participant, []byte, bool, error) {
	sess, err := s.store.GetSessionByCode(ctx, code)
	if err != nil {
		return Session{}, Participant{}, nil, false, err
	}
	if sess.Status == StateEnded || time.Now().After(sess.ExpiresAt) {
		return Session{}, Participant{}, nil, false, ErrGone
	}
	if sess.Status != StateLobby && !sess.AllowLateJoin {
		return Session{}, Participant{}, nil, false, ErrNotJoinable
	}
	display, key, err := NormalizeDisplayName(displayName)
	if err != nil {
		return Session{}, Participant{}, nil, false, err
	}
	// Reuse presented resume token when provided; else mint one.
	var raw []byte
	var tokenHash []byte
	if len(resumeToken) == 32 {
		raw = resumeToken
		tokenHash = auth.SHA256Hash(raw)
	} else {
		raw, _, err = auth.NewOpaqueToken()
		if err != nil {
			return Session{}, Participant{}, nil, false, err
		}
		tokenHash = auth.SHA256Hash(raw)
	}
	p, created, err := s.store.JoinOrResume(ctx, sess.ID, display, key, tokenHash)
	if err != nil {
		// Name taken → caller maps to 409 with neutral suggestions.
		return Session{}, Participant{}, nil, false, err
	}
	if s.manager != nil && created {
		s.manager.BroadcastPresence(sess.ID)
	}
	updated, _ := s.store.GetSession(ctx, sess.ID)
	return updated, p, raw, created, nil
}

// HostCommand applies a host transition and broadcasts (REST + WS share this).
func (s *Service) HostCommand(ctx context.Context, sessionID, hostID uuid.UUID, commandID, ctype string, expectedVersion int64, questionPosition *int) (Session, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	if sess.HostUserID != hostID {
		return Session{}, ErrNotFound
	}
	if time.Now().After(sess.ExpiresAt) {
		return Session{}, ErrGone
	}
	if !ValidTransition(sess.Status, ctype) {
		return Session{}, ErrInvalidState
	}
	questions, err := s.store.ListQuestions(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	now := time.Now()
	var updated Session
	switch ctype {
	case "start_question":
		if questionPosition == nil {
			return Session{}, ErrInvalidState
		}
		pos := *questionPosition
		if pos < 0 || pos >= len(questions) {
			return Session{}, ErrInvalidState
		}
		// Sequential positions only in MVP (no skipping).
		answered := answeredCount(ctx, s.store, sessionID, questions)
		_ = answered
		q := questions[pos]
		ends := now.Add(time.Duration(q.TimeLimitSeconds) * time.Second)
		updated, err = s.store.Transition(ctx, sessionID, expectedVersion, func(ls *lockedSession) (string, map[string]any, map[string]any, error) {
			if !ValidTransition(ls.Status, "start_question") {
				return "", nil, nil, ErrInvalidState
			}
			if err := ls.Exec(ctx, `UPDATE live_session SET status='question_open',state_version=state_version+1,current_question_id=$2,question_started_at=$3,question_ends_at=$4,started_at=COALESCE(started_at,$3),updated_at=now() WHERE id=$1`,
				sessionID, q.ID, now, ends); err != nil {
				return "", nil, nil, err
			}
			pub := map[string]any{"questionId": q.ID.String(), "position": q.Position, "startedAt": now, "endsAt": ends}
			return "question_started", pub, pub, nil
		})
		if err != nil {
			return Session{}, mapTransitionError(err)
		}
		if s.manager != nil {
			s.manager.ScheduleClose(sessionID, ends)
			s.manager.Broadcast(sessionID, "question_started", updated.StateVersion, hostPayload(sessionID, updated, questions, pos), participantQuestionPayload(q, now, ends))
		}
		return updated, nil
	case "end_question":
		updated, err = s.closeQuestion(ctx, sessionID, expectedVersion, "host")
		if err != nil {
			return Session{}, err
		}
		return updated, nil
	case "show_leaderboard":
		updated, err = s.store.Transition(ctx, sessionID, expectedVersion, func(ls *lockedSession) (string, map[string]any, map[string]any, error) {
			if !ValidTransition(ls.Status, "show_leaderboard") {
				return "", nil, nil, ErrInvalidState
			}
			if err := ls.Exec(ctx, `UPDATE live_session SET status='leaderboard',state_version=state_version+1,updated_at=now() WHERE id=$1`, sessionID); err != nil {
				return "", nil, nil, err
			}
			return "leaderboard_updated", map[string]any{}, map[string]any{}, nil
		})
		if err != nil {
			return Session{}, mapTransitionError(err)
		}
		if s.manager != nil {
			s.manager.BroadcastLeaderboard(sessionID, updated.StateVersion)
		}
		return updated, nil
	case "end_session":
		updated, err = s.store.Transition(ctx, sessionID, expectedVersion, func(ls *lockedSession) (string, map[string]any, map[string]any, error) {
			if ls.Status == StateEnded {
				return "", nil, nil, ErrInvalidState
			}
			if err := ls.Exec(ctx, `UPDATE live_session SET status='ended',state_version=state_version+1,ended_at=now(),updated_at=now() WHERE id=$1`, sessionID); err != nil {
				return "", nil, nil, err
			}
			return "session_ended", map[string]any{}, map[string]any{}, nil
		})
		if err != nil {
			return Session{}, mapTransitionError(err)
		}
		if s.manager != nil {
			s.manager.BroadcastEnded(sessionID, updated.StateVersion)
		}
		return updated, nil
	}
	return Session{}, ErrInvalidState
}

func mapTransitionError(err error) error {
	if errors.Is(err, ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return err
}

// closeQuestion persists the results transition (host close or timer).
func (s *Service) closeQuestion(ctx context.Context, sessionID uuid.UUID, expectedVersion int64, reason string) (Session, error) {
	updated, err := s.store.Transition(ctx, sessionID, expectedVersion, func(ls *lockedSession) (string, map[string]any, map[string]any, error) {
		if ls.Status != StateQuestionOpen {
			return "", nil, nil, ErrInvalidState
		}
		if err := ls.Exec(ctx, `UPDATE live_session SET status='question_results',state_version=state_version+1,updated_at=now() WHERE id=$1`, sessionID); err != nil {
			return "", nil, nil, err
		}
		return "question_ended", map[string]any{"reason": reason}, map[string]any{"reason": reason}, nil
	})
	if err != nil {
		return Session{}, mapTransitionError(err)
	}
	if s.manager != nil {
		s.manager.BroadcastResults(sessionID, updated.StateVersion)
	}
	return updated, nil
}

// TimerClose is the idempotent deadline path (actor wake-up hint).
func (s *Service) TimerClose(ctx context.Context, sessionID uuid.UUID) {
	sess, closed, err := s.store.CloseExpiredQuestion(ctx, sessionID)
	if err != nil || !closed {
		return
	}
	if s.manager != nil {
		s.manager.BroadcastResults(sessionID, sess.StateVersion)
	}
}

func answeredCount(ctx context.Context, store *Store, sessionID uuid.UUID, questions []SessionQuestion) int {
	return 0
}

// Results builds the host-visible aggregate for a session.
func (s *Service) Results(ctx context.Context, sessionID, hostID uuid.UUID) (SessionResult, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return SessionResult{}, err
	}
	if sess.HostUserID != hostID {
		return SessionResult{}, ErrNotFound
	}
	questions, err := s.store.ListQuestions(ctx, sessionID)
	if err != nil {
		return SessionResult{}, err
	}
	leaders, err := s.store.Leaderboard(ctx, sessionID, 10000)
	if err != nil {
		return SessionResult{}, err
	}
	res := SessionResult{Session: sess}
	for _, q := range questions {
		st, err := s.store.StatsFor(ctx, q.ID)
		if err != nil {
			return SessionResult{}, err
		}
		res.Questions = append(res.Questions, QuestionResult{Question: q, Stats: st})
	}
	for _, l := range leaders {
		res.Participants = append(res.Participants, l)
	}
	return res, nil
}

// SessionResult is the complete host aggregate.
type SessionResult struct {
	Session      Session
	Questions    []QuestionResult
	Participants []LeaderRow
}

// QuestionResult pairs a snapshot question with aggregates.
type QuestionResult struct {
	Question SessionQuestion
	Stats    QuestionStats
}
