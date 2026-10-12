-- Phase 3: quiz authoring aggregate (see DATABASE_DESIGN.md).

CREATE TABLE quiz (
  id uuid PRIMARY KEY,
  owner_user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE RESTRICT,
  title varchar(120) NOT NULL,
  description varchar(500) NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft','published','archived')),
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  published_at timestamptz,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'published') = (published_at IS NOT NULL))
);
CREATE INDEX quiz_owner_updated_idx
  ON quiz(owner_user_id, updated_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE question (
  id uuid PRIMARY KEY,
  quiz_id uuid NOT NULL REFERENCES quiz(id) ON DELETE CASCADE,
  position smallint NOT NULL CHECK (position >= 0),
  type text NOT NULL CHECK (type IN ('multiple_choice')),
  prompt varchar(500) NOT NULL,
  explanation varchar(1000) NOT NULL DEFAULT '',
  difficulty text NOT NULL CHECK (difficulty IN ('easy','medium','hard')),
  time_limit_seconds smallint NOT NULL DEFAULT 30
    CHECK (time_limit_seconds BETWEEN 10 AND 180),
  scoring_mode text NOT NULL DEFAULT 'speed'
    CHECK (scoring_mode IN ('fixed','speed')),
  base_points integer NOT NULL DEFAULT 1000
    CHECK (base_points BETWEEN 100 AND 10000),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (quiz_id, position) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX question_quiz_idx ON question(quiz_id, position);

CREATE TABLE answer_option (
  id uuid PRIMARY KEY,
  question_id uuid NOT NULL REFERENCES question(id) ON DELETE CASCADE,
  position smallint NOT NULL CHECK (position >= 0),
  text varchar(300) NOT NULL,
  is_correct boolean NOT NULL DEFAULT false,
  UNIQUE (question_id, position) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX answer_option_question_idx ON answer_option(question_id, position);
