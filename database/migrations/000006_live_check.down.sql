-- Best-effort down for local/test use only: fails if closed rows retain
-- timestamps (expected — the corrected check is the supported direction).
ALTER TABLE live_session DROP CONSTRAINT IF EXISTS live_session_open_needs_deadline;
ALTER TABLE live_session ADD CONSTRAINT live_session_check
  CHECK ((status = 'question_open') =
         (question_started_at IS NOT NULL AND question_ends_at IS NOT NULL));
