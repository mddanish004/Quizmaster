-- Phase 5: live session tables (see DATABASE_DESIGN.md).
-- Snapshots are immutable copies; scoring/answers reference snapshot rows.

CREATE TABLE live_session (
  id uuid PRIMARY KEY,
  quiz_id uuid REFERENCES quiz(id) ON DELETE SET NULL,
  host_user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE RESTRICT,
  quiz_version integer NOT NULL CHECK (quiz_version > 0),
  join_code char(6) NOT NULL CHECK (join_code ~ '^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{6}$'),
  status text NOT NULL DEFAULT 'lobby'
    CHECK (status IN ('lobby','question_open','question_results','leaderboard','ended')),
  state_version bigint NOT NULL DEFAULT 1 CHECK (state_version > 0),
  current_question_id uuid,
  question_started_at timestamptz,
  question_ends_at timestamptz,
  started_at timestamptz,
  ended_at timestamptz,
  expires_at timestamptz NOT NULL,
  allow_late_join boolean NOT NULL DEFAULT true,
  show_leaderboard boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'question_open') =
         (question_started_at IS NOT NULL AND question_ends_at IS NOT NULL))
);
CREATE UNIQUE INDEX live_session_active_code_uq ON live_session(join_code)
  WHERE status <> 'ended';
CREATE INDEX live_session_host_idx ON live_session(host_user_id, created_at DESC);
CREATE INDEX live_session_recovery_idx ON live_session(status, question_ends_at)
  WHERE status <> 'ended';

CREATE TABLE session_question (
  id uuid PRIMARY KEY,
  live_session_id uuid NOT NULL REFERENCES live_session(id) ON DELETE CASCADE,
  source_question_id uuid REFERENCES question(id) ON DELETE SET NULL,
  position smallint NOT NULL CHECK (position >= 0),
  type text NOT NULL CHECK (type IN ('multiple_choice')),
  prompt varchar(500) NOT NULL,
  explanation varchar(1000) NOT NULL DEFAULT '',
  difficulty text NOT NULL CHECK (difficulty IN ('easy','medium','hard')),
  time_limit_seconds smallint NOT NULL CHECK (time_limit_seconds BETWEEN 10 AND 180),
  scoring_mode text NOT NULL CHECK (scoring_mode IN ('fixed','speed')),
  base_points integer NOT NULL CHECK (base_points BETWEEN 100 AND 10000),
  UNIQUE (live_session_id, position)
);
CREATE INDEX session_question_session_idx ON session_question(live_session_id, position);

ALTER TABLE live_session ADD CONSTRAINT live_session_current_question_fk
  FOREIGN KEY (current_question_id) REFERENCES session_question(id) ON DELETE RESTRICT;

CREATE TABLE session_option (
  id uuid PRIMARY KEY,
  session_question_id uuid NOT NULL REFERENCES session_question(id) ON DELETE CASCADE,
  source_option_id uuid REFERENCES answer_option(id) ON DELETE SET NULL,
  position smallint NOT NULL CHECK (position >= 0),
  text varchar(300) NOT NULL,
  is_correct boolean NOT NULL,
  UNIQUE (session_question_id, position)
);

CREATE TABLE participant (
  id uuid PRIMARY KEY,
  live_session_id uuid NOT NULL REFERENCES live_session(id) ON DELETE CASCADE,
  display_name varchar(30) NOT NULL,
  display_name_key varchar(30) NOT NULL,
  resume_token_hash bytea NOT NULL UNIQUE CHECK (octet_length(resume_token_hash) = 32),
  score integer NOT NULL DEFAULT 0 CHECK (score >= 0),
  connection_count integer NOT NULL DEFAULT 0 CHECK (connection_count >= 0),
  joined_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  left_at timestamptz,
  UNIQUE (live_session_id, display_name_key)
);
CREATE INDEX participant_session_score_idx ON participant(live_session_id, score DESC, joined_at);

CREATE TABLE participant_answer (
  id uuid PRIMARY KEY,
  participant_id uuid NOT NULL REFERENCES participant(id) ON DELETE CASCADE,
  session_question_id uuid NOT NULL REFERENCES session_question(id) ON DELETE CASCADE,
  submission_id uuid NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  is_correct boolean NOT NULL,
  score_awarded integer NOT NULL CHECK (score_awarded >= 0),
  response_ms integer NOT NULL CHECK (response_ms >= 0),
  UNIQUE (participant_id, session_question_id),
  UNIQUE (participant_id, submission_id)
);
CREATE INDEX participant_answer_question_idx ON participant_answer(session_question_id);

CREATE TABLE answer_selection (
  participant_answer_id uuid NOT NULL REFERENCES participant_answer(id) ON DELETE CASCADE,
  session_option_id uuid NOT NULL REFERENCES session_option(id) ON DELETE CASCADE,
  PRIMARY KEY (participant_answer_id, session_option_id)
);

CREATE TABLE session_event (
  live_session_id uuid NOT NULL REFERENCES live_session(id) ON DELETE CASCADE,
  version bigint NOT NULL CHECK (version > 0),
  event_type varchar(50) NOT NULL,
  participant_id uuid REFERENCES participant(id) ON DELETE SET NULL,
  public_payload jsonb NOT NULL DEFAULT '{}',
  host_payload jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (live_session_id, version),
  CHECK (jsonb_typeof(public_payload) = 'object'),
  CHECK (jsonb_typeof(host_payload) = 'object')
);
