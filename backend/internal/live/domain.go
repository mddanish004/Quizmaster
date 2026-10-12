package live

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Session states (REALTIME_DESIGN.md state machine).
const (
	StateLobby           = "lobby"
	StateQuestionOpen    = "question_open"
	StateQuestionResults = "question_results"
	StateLeaderboard     = "leaderboard"
	StateEnded           = "ended"
)

// Session is the live session aggregate (snapshot questions loaded separately).
type Session struct {
	ID               uuid.UUID
	QuizID           *uuid.UUID
	HostUserID       uuid.UUID
	QuizVersion      int
	JoinCode         string
	Status           string
	StateVersion     int64
	CurrentQuestion  *SessionQuestion
	QuestionStartsAt *time.Time
	QuestionEndsAt   *time.Time
	StartedAt        *time.Time
	EndedAt          *time.Time
	ExpiresAt        time.Time
	AllowLateJoin    bool
	ShowLeaderboard  bool
	ParticipantCount int
	QuestionCount    int
	CreatedAt        time.Time
}

// SessionQuestion is an immutable snapshot row.
type SessionQuestion struct {
	ID               uuid.UUID
	LiveSessionID    uuid.UUID
	SourceQuestionID *uuid.UUID
	Position         int
	Type             string
	Prompt           string
	Explanation      string
	Difficulty       string
	TimeLimitSeconds int
	ScoringMode      string
	BasePoints       int
	Options          []SessionOption
}

// SessionOption is an immutable snapshot option.
type SessionOption struct {
	ID             uuid.UUID
	Position       int
	Text           string
	IsCorrect      bool
	SourceOptionID *uuid.UUID
}

// Participant is a guest identity scoped to one session.
type Participant struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	DisplayName string
	Score       int
	JoinedAt    time.Time
}

// Sentinel errors.
var (
	ErrNotFound        = errors.New("session not found")
	ErrNotJoinable     = errors.New("session not joinable")
	ErrNameTaken       = errors.New("name taken")
	ErrInvalidState    = errors.New("invalid state")
	ErrVersionMismatch = errors.New("version mismatch")
	ErrQuestionClosed  = errors.New("question closed")
	ErrGone            = errors.New("session gone")
)

const joinAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// NewJoinCode generates a 6-character code (ambiguous chars excluded).
func NewJoinCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = joinAlphabet[int(b[i])%len(joinAlphabet)]
	}
	return string(b), nil
}

// NormalizeDisplayName applies NFKC-ish folding: trim/collapse, reject controls, 1-30 runes.
func NormalizeDisplayName(name string) (display, key string, err error) {
	display = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(display) < 1 || utf8.RuneCountInString(display) > 30 {
		return "", "", fmt.Errorf("display name must be 1-30 characters")
	}
	for _, r := range display {
		if r < 0x20 || r == 0x7f {
			return "", "", fmt.Errorf("display name contains control characters")
		}
	}
	key = strings.ToLower(display)
	return display, key, nil
}

// ComputeScore derives server-authoritative points (incorrect → 0).
func ComputeScore(scoringMode string, basePoints int, startedAt, endsAt, receivedAt time.Time, correct bool) int {
	if !correct {
		return 0
	}
	if scoringMode == "fixed" {
		return basePoints
	}
	duration := endsAt.Sub(startedAt)
	if duration <= 0 {
		return basePoints / 2
	}
	remaining := endsAt.Sub(receivedAt)
	ratio := float64(remaining) / float64(duration)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return int(math.Round(float64(basePoints) * (0.5 + 0.5*ratio)))
}

// NextPosition returns the required next question position for start_question.
func NextPosition(answered int) int { return answered }

// ValidTransition enforces the state machine for host commands.
func ValidTransition(from, command string) bool {
	switch command {
	case "start_question":
		return from == StateLobby || from == StateQuestionResults || from == StateLeaderboard
	case "end_question":
		return from == StateQuestionOpen
	case "show_leaderboard":
		return from == StateQuestionResults
	case "end_session":
		return from != StateEnded
	}
	return false
}
