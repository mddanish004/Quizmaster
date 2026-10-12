package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// rawOutput mirrors the provider schema for strict decoding.
type rawOutput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Questions   []struct {
		Type    string `json:"type"`
		Prompt  string `json:"prompt"`
		Options []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"options"`
		CorrectOptionIDs []string `json:"correctOptionIds"`
		Explanation      string   `json:"explanation"`
		Difficulty       string   `json:"difficulty"`
	} `json:"questions"`
}

// ParseProviderOutput decodes one JSON object with unknown fields rejected,
// then enforces schema + domain + quality. Fenced or prose-wrapped output is
// rejected per the test strategy (providers are asked for strict JSON).
func ParseProviderOutput(data []byte) (GeneratedQuiz, error) {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
	dec.DisallowUnknownFields()
	var raw rawOutput
	if err := dec.Decode(&raw); err != nil {
		return GeneratedQuiz{}, fmt.Errorf("decode: %w", err)
	}
	// Reject anything after the first JSON value (extra values or prose).
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return GeneratedQuiz{}, fmt.Errorf("trailing data")
	}
	return validateRaw(raw)
}

var bannedOptionPhrases = []string{"all of the above", "none of the above"}

func validateRaw(raw rawOutput) (GeneratedQuiz, error) {
	fail := func(format string, args ...any) (GeneratedQuiz, error) {
		return GeneratedQuiz{}, fmt.Errorf(format, args...)
	}
	if utf8.RuneCountInString(raw.Title) < 1 || utf8.RuneCountInString(raw.Title) > 120 {
		return fail("title length invalid")
	}
	if utf8.RuneCountInString(raw.Description) > 500 {
		return fail("description too long")
	}
	if len(raw.Questions) < 1 || len(raw.Questions) > 20 {
		return fail("question count %d out of range", len(raw.Questions))
	}
	out := GeneratedQuiz{Title: raw.Title, Description: raw.Description}
	seenPrompts := map[string]bool{}
	for i, q := range raw.Questions {
		p := fmt.Sprintf("questions.%d", i)
		if q.Type != "multiple_choice" {
			return fail("%s: bad type", p)
		}
		if utf8.RuneCountInString(q.Prompt) < 1 || utf8.RuneCountInString(q.Prompt) > 500 {
			return fail("%s: prompt length", p)
		}
		norm := strings.ToLower(strings.Join(strings.Fields(q.Prompt), " "))
		if seenPrompts[norm] {
			return fail("%s: duplicate prompt", p)
		}
		seenPrompts[norm] = true
		if len(q.Options) < 2 || len(q.Options) > 6 {
			return fail("%s: option count", p)
		}
		if len(q.CorrectOptionIDs) != 1 {
			return fail("%s: exactly one correct id required", p)
		}
		if q.Difficulty != "easy" && q.Difficulty != "medium" && q.Difficulty != "hard" {
			return fail("%s: difficulty", p)
		}
		if utf8.RuneCountInString(q.Explanation) > 1000 {
			return fail("%s: explanation too long", p)
		}
		gq := GeneratedQuestion{Type: q.Type, Prompt: q.Prompt, CorrectOptionID: q.CorrectOptionIDs[0], Explanation: q.Explanation, Difficulty: q.Difficulty}
		seenIDs := map[string]bool{}
		seenText := map[string]bool{}
		foundCorrect := false
		for _, o := range q.Options {
			if len(o.ID) != 1 || o.ID[0] < 'a' || o.ID[0] > 'f' {
				return fail("%s: option id pattern", p)
			}
			if seenIDs[o.ID] {
				return fail("%s: duplicate option id", p)
			}
			seenIDs[o.ID] = true
			if utf8.RuneCountInString(o.Text) < 1 || utf8.RuneCountInString(o.Text) > 300 {
				return fail("%s: option text length", p)
			}
			if !utf8.ValidString(o.Text) || strings.ContainsAny(o.Text, "\x00\x01\x02\x03\x04\x05\x06\x07") {
				return fail("%s: option text encoding", p)
			}
			lower := strings.ToLower(o.Text)
			for _, banned := range bannedOptionPhrases {
				if strings.Contains(lower, banned) {
					return fail("%s: banned option phrase", p)
				}
			}
			tk := strings.ToLower(strings.Join(strings.Fields(o.Text), " "))
			if seenText[tk] {
				return fail("%s: duplicate option text", p)
			}
			seenText[tk] = true
			if o.ID == q.CorrectOptionIDs[0] {
				foundCorrect = true
			}
			gq.Options = append(gq.Options, GeneratedOption{ID: o.ID, Text: o.Text})
		}
		if !foundCorrect {
			return fail("%s: correct id not in options", p)
		}
		out.Questions = append(out.Questions, gq)
	}
	return out, nil
}
