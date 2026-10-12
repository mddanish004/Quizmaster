-- Fix live_session timestamp invariant: opening requires persisted deadlines,
-- but closing must retain them for history (the original equality check rejected
-- any closed row that kept its timestamps).
ALTER TABLE live_session DROP CONSTRAINT IF EXISTS live_session_check;
ALTER TABLE live_session ADD CONSTRAINT live_session_open_needs_deadline
  CHECK ((status <> 'question_open') OR (question_started_at IS NOT NULL AND question_ends_at IS NOT NULL));
