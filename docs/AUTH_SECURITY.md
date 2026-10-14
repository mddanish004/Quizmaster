# Authentication, Authorization, And Security

## Authentication Choice

Use opaque server-side sessions, not application JWTs. Generate 32 random bytes with `crypto/rand`, send base64url only in a `__Host-qf_session` cookie, and store `SHA-256(token)` in `auth_session`. The database row supports revocation, idle/absolute expiry, last-seen metadata, and logout-all. This avoids refresh-token rotation and revocation lists while the frontend and API share one origin.

Cookie attributes: `Secure; HttpOnly; SameSite=Lax; Path=/` with no `Domain`; production HTTPS only. Creator idle expiry 24 hours, absolute expiry 30 days, rotated on login, privilege-sensitive changes, suspicious metadata change, and at least every 7 days. Update `last_seen_at` at most every 5 minutes to limit writes.

Participants receive a separate `__Host-qf_participant` opaque cookie scoped to the origin. MVP supports one actively resumed session per browser cookie; joining another replaces the resumable cookie but does not delete prior participation. This trade-off is documented in UI. A later cookie containing an encrypted map requires a new security review.

## Passwords

Use Argon2id with versioned encoded hashes. Initial parameters, calibrated on the production instance during deployment: memory 64 MiB, iterations 3, parallelism 2, 16-byte random salt, 32-byte key. Target verification around 100-300 ms without memory pressure; record selected parameters in the hash and recalibrate when instance class changes. Hashing concurrency is bounded (for example 2) so login bursts cannot exhaust 2 GiB RAM.

Passwords are 12-128 Unicode code points, checked against a common/breached-password denylist where locally feasible, and never trimmed silently. Allow paste/password managers. No arbitrary composition rules. Login uses a dummy hash for unknown users, generic errors, and rate limits.

## Google OIDC

Use authorization-code flow with PKCE on the server and scopes `openid email profile`. Store short-lived one-time state/nonce transaction (10 minutes); validate exact redirect URI, state, nonce, signature/JWKS, issuer, audience, authorized party if present, expiry/issued time, and `email_verified`. Google states redirect URIs must match exactly and recommends state for CSRF protection: [web-server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server) and [OIDC reference](https://developers.google.com/identity/openid-connect/reference).

Implementation (Stage 4, ADR-014): ID tokens are verified with `github.com/coreos/go-oidc/v3` — provider discovery against `https://accounts.google.com`, JWKS caching with automatic refresh on unknown key IDs (rotation-safe), RS256-only header gate, issuer/audience/expiry enforced by the library, plus explicit `azp` and constant-time `nonce` checks. The nonce plaintext lives only in the 10-minute transaction row (`oauth_transaction.nonce_text`) and is compared by the verifier; only the state/nonce hashes are long-lived concepts. Token exchange uses the discovered token endpoint with the PKCE verifier and 20 s budgets; every failure is fail-closed. Google `sub` remains the sole identity key; verified email only marks `email_verified_at`.

Identity key is Google `sub`, never email. Do not store Google access/refresh tokens because no Google APIs are called after login.

### Account Linking Policy

- New verified Google email with no local user: create user plus Google identity; derive collision-safe username and let user edit it.
- Existing user already linked to same Google `sub`: login.
- Existing password account with matching email but no Google identity: do **not** silently link. Return a safe link-required screen; user authenticates the existing password in the same flow, then transactionally links.
- Existing identity conflicts or email belongs to another linked subject: block and direct to recovery/support; never move identity automatically.
- Linking/unlinking from settings requires recent authentication (within 10 minutes). Do not allow removal of the last login method.

Testing status supports at most 100 test users and authorizations expire according to Google's current rules; production uses an external consent screen and only non-sensitive identity scopes. Branding/domain verification may still be required. See [Google app audience](https://support.google.com/cloud/answer/15549945) and [verification help](https://support.google.com/cloud/answer/13463073).

## CSRF And CORS

SameSite is defense in depth, not the sole control. The server derives a stable per-session CSRF token as `HMAC-SHA-256(CSRF_SECRET, session_id || token_hash)`; `/me` returns it, and all state-changing cookie-authenticated requests require `X-CSRF-Token` verified in constant time plus an exact `Origin` allowlist. The HMAC secret stays in configuration and the token changes when the session rotates. OAuth uses independent state. WebSocket upgrade validates Origin and cookies.

Production CORS is disabled for cross-origin callers because the Go binary serves frontend/API on one origin. Local development allowlists one exact Vite origin with credentials; never `*`. Simple public endpoints remain rate limited and do not rely on CORS as authorization.

## Authorization Matrix

| Resource/action | Check |
|---|---|
| Read/update/delete/publish quiz | authenticated user equals `quiz.owner_user_id`; deleted filter |
| Start session | owns quiz, quiz published and valid |
| Host command/socket | authenticated user equals `live_session.host_user_id` |
| Creator results/history | session host/owner equals authenticated user |
| Join | active code, state/policy allows join, rate limit |
| Participant socket/snapshot/answer | resume token maps to participant and same session |
| Answer | current snapshot question is open, selection belongs to it, deadline and uniqueness pass |

Use queries that include owner/session IDs rather than fetch-then-trust client ownership. Cross-owner IDs return `404` where revealing existence is unnecessary. Never accept user ID, participant ID, score, correctness, timestamp, or state from client as authority.

## Recovery And Email

Password forgot always returns `202`. Generate 32-byte token, store hash, expire in 30 minutes, single use, invalidate older tokens, and send an HTTPS same-origin link through an email interface backed by SMTP in production. Reset transaction locks/consumes token, changes hash, and revokes all sessions. Rate limit by IP and normalized account key; do not put token in logs/referrer-bearing third-party pages. Set `Referrer-Policy: no-referrer` on recovery routes.

Email verification can be required before AI generation/live hosting while still permitting login and editing. Google verified email satisfies verification. In local/test, an explicit mail-capture/mock provider exposes links only to developer tooling; production never logs them.

## Threat Model

| Threat | Main mitigation | Verification |
|---|---|---|
| SQL injection | `sqlc`/parameterized `pgx`; no user-built SQL fragments; allowlisted sort | malicious query API tests; static review |
| Stored/reflected XSS | React text rendering; no `dangerouslySetInnerHTML`; CSP; sanitize filenames; plain-text quiz fields | payload E2E; CSP report checks |
| CSRF | SameSite cookie, Origin, per-session header token; OAuth state | missing/wrong/cross-origin tests |
| Broken access control / IDOR | owner-scoped service/repository queries; role matrix | two-user API suite for every resource |
| Credential stuffing | Argon2id, IP+identifier token buckets, generic error, optional CAPTCHA only after evidence | rate-limit and timing envelope tests |
| Session theft/fixation | Secure HttpOnly host cookie, rotate on auth, no URL/localStorage tokens, CSP | cookie assertions; old token rejection |
| OAuth mix-up/link takeover | exact redirect, state/nonce/PKCE, issuer/audience/JWKS, explicit linking | forged claim/state/link tests |
| WebSocket impersonation | cookie identity + Origin + session-role lookup on upgrade and commands | cross-session/role integration tests |
| Score manipulation | server time/correctness/formula; atomic unique answer; no score input | tampered payload and race tests |
| Premature key exposure | role-specific DTOs/events; immutable snapshot; release-state query | snapshot/WS schema tests before/after close |
| Prompt injection | delimit untrusted input, strict schema/domain validation, escaped output | adversarial prompt corpus |
| API/AI cost abuse | quotas, in-flight limits, body caps, provider budget/circuit | concurrency/quota tests; alerts |
| Sensitive logging | structured allowlist fields and redaction; no content/token | log capture assertions and secret scan |
| Secrets in repo/image | Render env + GitHub secrets, `.env` ignored, image history/Trivy/gitleaks | CI scans and image inspection |
| Container escape/privilege | non-root, read-only FS where possible, drop caps, no Docker socket | Compose config/Trivy checks |
| Dependency compromise | lock files, Dependabot/Renovate, govulncheck, npm audit, pinned actions | scheduled CI |
| DoS/slow clients | server read/write timeouts, body limits, bounded queues, deadlines, rate limits | k6 and slow-client tests |

## Security Headers

The Go `SecurityHeaders` middleware sets these (Render edge adds HSTS); the
header list below is enforced in code, not in a proxy:

```text
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self' https://accounts.google.com
Strict-Transport-Security: max-age=31536000; includeSubDomains
X-Content-Type-Options: nosniff
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: camera=(), microphone=(), geolocation=()
Cross-Origin-Opener-Policy: same-origin-allow-popups
```

OAuth redirect navigation may require careful COOP testing. Do not add `'unsafe-inline'` casually. QR scanning is not needed; the app displays QR, so camera stays disabled.

## Rate Limits

Single-instance in-memory token buckets are acceptable for MVP and reset on restart; database invariants remain security boundaries. Suggested defaults:

- register 5/hour/IP; login 10/10 minutes/IP and 5/10 minutes/identifier;
- forgot password 5/hour/IP and 3/hour/account;
- AI 5/day/user, 20/day/IP, 2 concurrent/user;
- quiz writes 60/min/user;
- join 20/min/IP, 5/min/code/IP;
- WS upgrades 10/min/IP; message limits in real-time spec.

Return `429`, `Retry-After`, and generic messages. Behind Render, trust forwarded IP only from the known proxy network. Multi-instance scale moves limit state to Redis.

## Data And Secret Handling

- Secrets: `DATABASE_URL`, migration URL, HF token, Google client secret, session/CSRF keys, SMTP credentials. Production loads them from Render env / GitHub Actions secrets.
- Token hashes use SHA-256 because tokens have 256 bits of entropy; passwords use Argon2id.
- Minimize PII: creator email; guest display name; coarse IP prefix and hashed agent only for security. No full IP retention beyond short access logs unless justified.
- Never send participant names to AI. Never expose answer keys in generic event logs.
- Account export/deletion is a post-MVP UI enhancement but schema/retention must not prevent it.

## Security Release Gate

Before production: threat-model review; two-user authorization suite; CSRF/origin tests including WebSocket; OAuth test and production redirect audit; dependency/container/secret scans; TLS/header scan; restore drill; abuse load test; verify no debug endpoints; rotate test credentials; Render/Neon/HF spend alerts; document open risks. High-severity known vulnerabilities or failing ownership tests block deployment.
