package ai

import (
	"strings"
	"testing"
)

const goldenJSON = `{
  "title": "Go Basics",
  "description": "warm-up",
  "questions": [
    {"type":"multiple_choice","prompt":"Which keyword declares a variable?","options":[{"id":"a","text":"let"},{"id":"b","text":"var"}],"correctOptionIds":["b"],"explanation":"var declares.","difficulty":"easy"},
    {"type":"multiple_choice","prompt":"Which is Go's loop?","options":[{"id":"a","text":"while"},{"id":"b","text":"for"}],"correctOptionIds":["b"],"explanation":"for loops.","difficulty":"easy"}
  ]
}`

func TestParseGolden(t *testing.T) {
	gq, err := ParseProviderOutput([]byte(goldenJSON))
	if err != nil {
		t.Fatalf("golden: %v", err)
	}
	if len(gq.Questions) != 2 || gq.Questions[0].CorrectOptionID != "b" {
		t.Fatalf("golden: %+v", gq)
	}
}

func TestParseAdversarial(t *testing.T) {
	dup := strings.Replace(goldenJSON, "Which is Go's loop?", "Which keyword declares a variable?", 1)
	cases := map[string]string{
		"markdown fences":   "```json\n" + goldenJSON + "\n```",
		"trailing prose":    goldenJSON + "\nHope you like it!",
		"duplicate prompts": dup,
		"wrong correct id":  strings.Replace(goldenJSON, `"correctOptionIds":["b"]`, `"correctOptionIds":["z"]`, 1),
		"two correct":       strings.Replace(goldenJSON, `"correctOptionIds":["b"]`, `"correctOptionIds":["a","b"]`, 1),
		"unknown field":     strings.Replace(goldenJSON, `"difficulty":"easy"`, `"difficulty":"easy","hacker":1`, 1),
		"malformed":         goldenJSON[:100],
		"empty":             `{}`,
		"one option":        strings.Replace(goldenJSON, `{"id":"a","text":"let"},`, ``, 1),
		"banned phrase":     strings.Replace(goldenJSON, `"text":"for"`, `"text":"All of the above"`, 1),
		"bad option id":     strings.Replace(goldenJSON, `"id":"a"`, `"id":"zz"`, 1),
		"duplicate options": strings.Replace(goldenJSON, `"text":"var"`, `"text":"let"`, 1),
	}
	for name, body := range cases {
		if _, err := ParseProviderOutput([]byte(body)); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestParseOversizedRejected(t *testing.T) {
	big := strings.Repeat("x", 600*1024)
	body := `{"title":"` + big + `","description":"","questions":[]}`
	if _, err := ParseProviderOutput([]byte(body)); err == nil {
		t.Fatal("oversized accepted")
	}
}

func TestValidateInputRules(t *testing.T) {
	uid := [16]byte{1}
	_ = uid
	base := GenerateInput{Topic: "Go", Difficulty: "medium", QuestionCount: 5, Language: "en"}
	if err := validateInput(base); err == nil {
		t.Fatal("short topic accepted")
	}
	base.Topic = "Go concurrency"
	base.QuestionCount = 0
	if err := validateInput(base); err == nil {
		t.Fatal("zero count accepted")
	}
	base.QuestionCount = 5
	base.Difficulty = "extreme"
	if err := validateInput(base); err == nil {
		t.Fatal("bad difficulty accepted")
	}
}
