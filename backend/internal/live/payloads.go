package live

import (
	"time"

	"github.com/google/uuid"
)

// hostPayload builds the host view of question_started (keys included; ownership revalidated).
func hostPayload(sessionID uuid.UUID, sess Session, questions []SessionQuestion, pos int) map[string]any {
	q := questions[pos]
	opts := make([]any, 0, len(q.Options))
	correct := []string{}
	for _, o := range q.Options {
		opts = append(opts, map[string]any{"id": o.ID.String(), "text": o.Text, "position": o.Position, "isCorrect": o.IsCorrect})
		if o.IsCorrect {
			correct = append(correct, o.ID.String())
		}
	}
	return map[string]any{
		"question":  map[string]any{"id": q.ID.String(), "position": q.Position, "prompt": q.Prompt, "options": opts, "correctOptionIds": correct},
		"startedAt": sess.QuestionStartsAt, "endsAt": sess.QuestionEndsAt,
		"serverTime": time.Now().UTC(), "scoringMode": q.ScoringMode,
		"submittedCount": 0,
	}
}

// participantQuestionPayload omits correctness and answer-revealing explanation.
func participantQuestionPayload(q SessionQuestion, started, ends time.Time) map[string]any {
	opts := make([]any, 0, len(q.Options))
	for _, o := range q.Options {
		opts = append(opts, map[string]any{"id": o.ID.String(), "text": o.Text, "position": o.Position})
	}
	return map[string]any{
		"question":  map[string]any{"id": q.ID.String(), "position": q.Position, "prompt": q.Prompt, "options": opts},
		"startedAt": started, "endsAt": ends,
		"serverTime": time.Now().UTC(), "scoringMode": q.ScoringMode,
	}
}

// snapshotPayload builds the role-filtered snapshot for connect/resync.
func snapshotPayload(role string, sess Session, questions []SessionQuestion, p *Participant, store *Store) map[string]any {
	base := map[string]any{
		"sessionId": sess.ID.String(), "state": sess.Status, "stateVersion": sess.StateVersion,
		"serverTime": time.Now().UTC(), "participantCount": sess.ParticipantCount,
		"questionCount": sess.QuestionCount,
	}
	if role == "host" {
		base["joinCode"] = sess.JoinCode
		if sess.CurrentQuestion != nil {
			opts := make([]any, 0, len(sess.CurrentQuestion.Options))
			for _, o := range sess.CurrentQuestion.Options {
				opts = append(opts, map[string]any{"id": o.ID.String(), "text": o.Text, "position": o.Position, "isCorrect": o.IsCorrect})
			}
			base["currentQuestion"] = map[string]any{"id": sess.CurrentQuestion.ID.String(), "position": sess.CurrentQuestion.Position, "prompt": sess.CurrentQuestion.Prompt, "options": opts}
		}
		return base
	}
	if p != nil {
		base["participant"] = map[string]any{"id": p.ID.String(), "displayName": p.DisplayName, "score": p.Score}
	}
	if sess.Status == StateQuestionOpen && sess.CurrentQuestion != nil && sess.QuestionStartsAt != nil && sess.QuestionEndsAt != nil {
		base["question"] = participantQuestionPayload(*sess.CurrentQuestion, *sess.QuestionStartsAt, *sess.QuestionEndsAt)["question"]
		base["questionStartedAt"] = sess.QuestionStartsAt
		base["questionEndsAt"] = sess.QuestionEndsAt
	}
	return base
}
