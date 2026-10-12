package quiz

import (
	"testing"

	"github.com/google/uuid"
)

func validQuestion(pos int) Question {
	return Question{
		ID: uuid.New(), Type: "multiple_choice", Position: pos,
		Prompt: "Which keyword declares a variable?", Explanation: "var declares variables.",
		Difficulty: "easy", TimeLimitSeconds: 30, ScoringMode: "speed", BasePoints: 1000,
		Options: []Option{
			{ID: uuid.New(), Position: 0, Text: "let", IsCorrect: false},
			{ID: uuid.New(), Position: 1, Text: "var", IsCorrect: true},
		},
	}
}

func TestDraftAllowsEmpty(t *testing.T) {
	if fe := ValidateQuizWrite("T", "", nil, false); fe.HasErrors() {
		t.Fatalf("blank draft rejected: %v", fe)
	}
}

func TestPublishRequiresComplete(t *testing.T) {
	if fe := ValidateQuizWrite("T", "", nil, true); !fe.HasErrors() {
		t.Fatal("empty publish accepted")
	}
	q := validQuestion(0)
	if fe := ValidateQuizWrite("T", "", []Question{q}, true); fe.HasErrors() {
		t.Fatalf("valid quiz rejected: %v", fe)
	}
}

func TestValidationBounds(t *testing.T) {
	cases := []func(q *Question){
		func(q *Question) { q.Prompt = "" },
		func(q *Question) { q.TimeLimitSeconds = 5 },
		func(q *Question) { q.BasePoints = 50 },
		func(q *Question) { q.Difficulty = "extreme" },
		func(q *Question) { q.Options = q.Options[:1] },
		func(q *Question) { q.Options[1].IsCorrect = false },
		func(q *Question) { q.Position = 3 },
	}
	for i, mutate := range cases {
		q := validQuestion(0)
		mutate(&q)
		if fe := ValidateQuizWrite("T", "", []Question{q}, true); !fe.HasErrors() {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestDuplicateOptionTextRejected(t *testing.T) {
	q := validQuestion(0)
	q.Options[1].Text = "  LET "
	if fe := ValidateQuizWrite("T", "", []Question{q}, false); !fe.HasErrors() {
		t.Fatal("duplicate option text accepted")
	}
}
