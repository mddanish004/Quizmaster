package live

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestScoringBoundaries(t *testing.T) {
	start := time.Now()
	ends := start.Add(30 * time.Second)
	if got := ComputeScore("fixed", 1000, start, ends, start.Add(time.Second), true); got != 1000 {
		t.Fatalf("fixed: %d", got)
	}
	if got := ComputeScore("speed", 1000, start, ends, start.Add(time.Second), false); got != 0 {
		t.Fatalf("incorrect: %d", got)
	}
	// Instant correct → 1000; last-moment correct → 500.
	if got := ComputeScore("speed", 1000, start, ends, start, true); got != 1000 {
		t.Fatalf("instant: %d", got)
	}
	if got := ComputeScore("speed", 1000, start, ends, ends, true); got != 500 {
		t.Fatalf("last moment: %d", got)
	}
	mid := ComputeScore("speed", 1000, start, ends, start.Add(15*time.Second), true)
	if mid != 750 {
		t.Fatalf("mid: %d", mid)
	}
}

func TestTransitionTable(t *testing.T) {
	cases := []struct {
		from, cmd string
		ok        bool
	}{
		{"lobby", "start_question", true},
		{"lobby", "end_question", false},
		{"lobby", "show_leaderboard", false},
		{"lobby", "end_session", true},
		{"question_open", "start_question", false},
		{"question_open", "end_question", true},
		{"question_open", "show_leaderboard", false},
		{"question_open", "end_session", true},
		{"question_results", "start_question", true},
		{"question_results", "end_question", false},
		{"question_results", "show_leaderboard", true},
		{"question_results", "end_session", true},
		{"leaderboard", "start_question", true},
		{"leaderboard", "end_question", false},
		{"leaderboard", "show_leaderboard", false},
		{"leaderboard", "end_session", true},
		{"ended", "start_question", false},
		{"ended", "end_session", false},
		{"bogus", "start_question", false},
		{"lobby", "bogus", false},
	}
	for _, c := range cases {
		if got := ValidTransition(c.from, c.cmd); got != c.ok {
			t.Fatalf("%s+%s: got %v want %v", c.from, c.cmd, got, c.ok)
		}
	}
}

func TestJoinCodeShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		code, err := NewJoinCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 {
			t.Fatalf("length: %q", code)
		}
		for _, r := range code {
			if !containsRune(joinAlphabet, r) {
				t.Fatalf("alphabet: %q", code)
			}
		}
		seen[code] = true
	}
	if len(seen) < 45 {
		t.Fatal("codes not random enough")
	}
	_ = uuid.New
}

func containsRune(s string, r rune) bool {
	for _, x := range s {
		if x == r {
			return true
		}
	}
	return false
}

func TestNormalizeDisplayName(t *testing.T) {
	d, k, err := NormalizeDisplayName("  Ada   Lovelace  ")
	if err != nil || d != "Ada Lovelace" || k != "ada lovelace" {
		t.Fatalf("got %q %q %v", d, k, err)
	}
	if _, _, err := NormalizeDisplayName("   "); err == nil {
		t.Fatal("blank accepted")
	}
	if _, _, err := NormalizeDisplayName("bad\x01name"); err == nil {
		t.Fatal("control chars accepted")
	}
	long := ""
	for i := 0; i < 31; i++ {
		long += "a"
	}
	if _, _, err := NormalizeDisplayName(long); err == nil {
		t.Fatal("long name accepted")
	}
}
