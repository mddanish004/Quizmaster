-- Stage 4: persist the OIDC nonce plaintext for the short-lived transaction.
-- The nonce is a random binding value (not a credential); rows expire in
-- 10 minutes and are consumed once, then deleted by the auth cleanup job.
ALTER TABLE oauth_transaction ADD COLUMN IF NOT EXISTS nonce_text TEXT;
