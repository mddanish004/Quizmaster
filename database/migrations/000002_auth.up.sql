-- Phase 1: identity and security tables (see DATABASE_DESIGN.md).

CREATE TABLE app_user (
  id uuid PRIMARY KEY,
  email citext NOT NULL UNIQUE,
  username citext NOT NULL UNIQUE,
  display_name varchar(80) NOT NULL,
  password_hash text,
  email_verified_at timestamptz,
  status text NOT NULL DEFAULT 'active'
    CHECK (status IN ('active','locked','pending_deletion')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (length(email::text) <= 254),
  CHECK (username::text ~ '^[a-zA-Z0-9_]{3,30}$')
);

CREATE TABLE auth_identity (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('google')),
  provider_subject varchar(255) NOT NULL,
  provider_email citext,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, provider_subject),
  UNIQUE (user_id, provider)
);
CREATE INDEX auth_identity_user_idx ON auth_identity(user_id);

CREATE TABLE auth_session (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  idle_expires_at timestamptz NOT NULL,
  absolute_expires_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  ip_prefix inet,
  user_agent_hash bytea,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_session_token_idx ON auth_session(token_hash) WHERE revoked_at IS NULL;
CREATE INDEX auth_session_user_idx ON auth_session(user_id, created_at DESC);

CREATE TABLE oauth_transaction (
  id uuid PRIMARY KEY,
  state_hash bytea NOT NULL UNIQUE CHECK (octet_length(state_hash) = 32),
  nonce_hash bytea NOT NULL CHECK (octet_length(nonce_hash) = 32),
  pkce_verifier_ciphertext bytea NOT NULL,
  return_path varchar(200) NOT NULL DEFAULT '/app',
  kind text NOT NULL DEFAULT 'login' CHECK (kind IN ('login','link')),
  link_user_id uuid REFERENCES app_user(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE password_reset_token (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX password_reset_active_idx ON password_reset_token(user_id) WHERE consumed_at IS NULL;

CREATE TABLE email_verification_token (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_key (
  user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  scope text NOT NULL,
  key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
  request_hash bytea NOT NULL,
  response_status integer NOT NULL,
  response_body jsonb NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, scope, key_hash)
);
