package quiz

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Domain types mirror the API contract (creator-visible, includes correctness).
type Option struct {
	ID        uuid.UUID
	Position  int
	Text      string
	IsCorrect bool
}

type Question struct {
	ID               uuid.UUID
	Type             string
	Position         int
	Prompt           string
	Explanation      string
	Difficulty       string
	TimeLimitSeconds int
	ScoringMode      string
	BasePoints       int
	Options          []Option
}

type Quiz struct {
	ID          uuid.UUID
	OwnerUserID uuid.UUID
	Title       string
	Description string
	Status      string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
	Questions   []Question
}

// FieldError collects validation failures keyed by field path.
type FieldError map[string][]string

func (f FieldError) add(path, msg string) {
	f[path] = append(f[path], msg)
}

func (f FieldError) Error() string { return "quiz validation failed" }

// HasErrors reports whether any violation was recorded.
func (f FieldError) HasErrors() bool { return len(f) > 0 }

// ValidateQuizWrite checks an update payload. Complete=true enforces publish rules.
func ValidateQuizWrite(title, description string, questions []Question, complete bool) FieldError {
	fe := FieldError{}
	if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 120 {
		fe.add("title", "must be 1-120 characters")
	}
	if utf8.RuneCountInString(description) > 500 {
		fe.add("description", "must be at most 500 characters")
	}
	if len(questions) > 50 {
		fe.add("questions", "must contain at most 50 questions")
	}
	if complete && len(questions) < 1 {
		fe.add("questions", "published quizzes need at least 1 question")
	}
	seen := map[int]bool{}
	for i, q := range questions {
		p := fmt.Sprintf("questions.%d", i)
		if q.Position != i || seen[q.Position] {
			fe.add(p+".position", "positions must be contiguous from 0")
		}
		seen[q.Position] = true
		validateQuestion(fe, p, q, complete)
	}
	return fe
}

func validateQuestion(fe FieldError, p string, q Question, complete bool) {
	if q.Type != "multiple_choice" {
		fe.add(p+".type", "must be multiple_choice")
	}
	if utf8.RuneCountInString(q.Prompt) < 1 || utf8.RuneCountInString(q.Prompt) > 500 {
		fe.add(p+".prompt", "must be 1-500 characters")
	}
	if utf8.RuneCountInString(q.Explanation) > 1000 {
		fe.add(p+".explanation", "must be at most 1000 characters")
	}
	switch q.Difficulty {
	case "easy", "medium", "hard":
	default:
		fe.add(p+".difficulty", "must be easy, medium, or hard")
	}
	if q.TimeLimitSeconds < 10 || q.TimeLimitSeconds > 180 {
		fe.add(p+".timeLimitSeconds", "must be 10-180")
	}
	switch q.ScoringMode {
	case "fixed", "speed":
	default:
		fe.add(p+".scoringMode", "must be fixed or speed")
	}
	if q.BasePoints < 100 || q.BasePoints > 10000 {
		fe.add(p+".basePoints", "must be 100-10000")
	}
	if len(q.Options) < 2 || len(q.Options) > 6 {
		fe.add(p+".options", "must contain 2 to 6 options")
	}
	if complete && len(q.Options) >= 2 {
		correct := 0
		for _, o := range q.Options {
			if o.IsCorrect {
				correct++
			}
		}
		if correct != 1 {
			fe.add(p+".options", "must have exactly one correct option")
		}
	}
	seen := map[int]bool{}
	texts := map[string]bool{}
	for j, o := range q.Options {
		op := fmt.Sprintf("%s.options.%d", p, j)
		if o.Position != j || seen[o.Position] {
			fe.add(op+".position", "positions must be contiguous from 0")
		}
		seen[o.Position] = true
		if utf8.RuneCountInString(o.Text) < 1 || utf8.RuneCountInString(o.Text) > 300 {
			fe.add(op+".text", "must be 1-300 characters")
		}
		key := strings.ToLower(strings.Join(strings.Fields(o.Text), " "))
		if texts[key] {
			fe.add(op+".text", "duplicate option text")
		}
		texts[key] = true
	}
}
