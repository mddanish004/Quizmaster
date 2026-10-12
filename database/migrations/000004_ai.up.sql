-- Phase 4: AI generation records (see AI_INTEGRATION.md).
-- Raw prompts/outputs omitted by default; aggregates retained.

CREATE TABLE ai_generation (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  quiz_id uuid REFERENCES quiz(id) ON DELETE SET NULL,
  provider text NOT NULL,
  model text NOT NULL,
  prompt_template text NOT NULL,
  status text NOT NULL CHECK (status IN ('running','succeeded','failed')),
  input_tokens integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  latency_ms integer NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
  attempts integer NOT NULL DEFAULT 1 CHECK (attempts BETWEEN 1 AND 2),
  error_code text,
  request_fingerprint text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_generation_user_idx ON ai_generation(user_id, created_at DESC);
