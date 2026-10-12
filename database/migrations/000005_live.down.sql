ALTER TABLE live_session DROP CONSTRAINT IF EXISTS live_session_current_question_fk;
DROP TABLE IF EXISTS session_event;
DROP TABLE IF EXISTS answer_selection;
DROP TABLE IF EXISTS participant_answer;
DROP TABLE IF EXISTS participant;
DROP TABLE IF EXISTS session_option;
DROP TABLE IF EXISTS session_question;
DROP TABLE IF EXISTS live_session;
