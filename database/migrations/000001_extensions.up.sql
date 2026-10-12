-- Phase 0: migration bookkeeping + extensions.
-- golang-migrate creates schema_migrations automatically; this migration
-- ensures required extensions exist.
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
