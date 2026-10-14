# Implementation Status

Last updated: 2026-10-11 (Stage 3 production-readiness validation).

> Stage 3 complete: audit → fix → verify across Docker (blocked), browser E2E,
> load, integrations, security, deployment dry run, and portfolio. Eleven bugs
> found; all fixed with regression tests (see `docs/PRODUCTION_READINESS.md`).
> No AWS resources provisioned; no external credentials used.

## Current State

## Current State

- [x] Product and technical specification written.
- [x] Architecture decisions recorded.
- [x] Phase 0 - repository and environment setup.
- [x] Phase 1 - backend foundations.
- [x] Phase 2 - frontend foundations.
- [x] Phase 3 - quiz management.
- [x] Phase 4 - AI integration.
- [x] Phase 5 - live quiz engine.
- [x] Phase 6 - session history and analytics.
- [x] Phase 7 - testing and hardening.
- [x] Phase 8 - production deployment.
- [x] Phase 9 - portfolio readiness.

### Phase 0 - Repository And Environment (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: runnable foundation complete; backend health/version live, frontend builds, compose validates.
- Mandatory checklist: Go module + typed env config + JSON logger + request ID + liveness/readiness + graceful shutdown done; React/TS/Vite/Tailwind + router + Query provider + error boundary baseline done; Chi/pgx/uuid pinned, sqlc config + migration tooling done; Compose dev/prod shapes + Dockerfiles + Caddy baseline + Makefile + CI baseline done. No secrets committed (.env gitignored).
- Verified behavior:
  - `go vet ./...` clean, `gofmt -l` empty, `go test ./...` pass (config, httpserver health/ready).
  - Frontend `tsc --noEmit` clean, `vitest run` 1/1 pass, `vite build` success (332KB JS gzip 104KB).
  - Backend runtime: `GET /health/live` 200 `{"status":"ok"}`, `GET /version` 200, `GET /health/ready` 503 without DB (correct fail-closed).
  - `docker compose config` validates; `python scripts/openapi_check.py` skeleton check (pyyaml absent in env, strict check deferred to CI).
- Mocked behavior: DB-backed readiness (no live Neon; local postgres `quizforge` role + `quizforge_dev`/`quizforge_test` provisioned for Phase 1 integration).
- Credential-required behavior: Neon branches, Google OIDC, Hugging Face, SES, AWS — none exercised.
- Commands executed and results:
  - `go mod tidy && gofmt -w . && go vet ./... && go test ./...` — pass.
  - `npm install && tsc --noEmit` — pass; `vitest run` — 1 passed; `vite build` — success.
  - Runtime smoke via `go run ./cmd/api` on 127.0.0.1:18080 with curl — live/version 200, ready 503 as expected.
  - `docker compose config` — OK (after `cp .env.example .env`; .env gitignored).
- Migrations/API/ADR/doc changes: added `database/migrations/000001_extensions`, `backend/db/sqlc.yaml`, Docker/Compose/Caddy/Makefile/CI; no contract changes.
- Known risks or blockers: frontend `npm ci` needs committed lockfile (used `npm install`); `pyyaml` missing locally for strict OpenAPI check; image builds not yet executed; no domain tables yet (Phase 1).
- Next phase: Phase 1 backend foundations (auth schema, sessions, password/Google/recovery, middleware, rate limits, tests).

### Phase 1 - Backend Foundations (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: password/session/Google-link/recovery auth complete and verified against real PostgreSQL.
- Mandatory checklist: auth/security migrations + transaction helper done; Argon2id (64MiB/3/2) + bounded hashing outside tx done; register/login/logout/logout-all/me/patch-me endpoints + error mapper done; OIDC transaction + PKCE/state/nonce + explicit link flow done (real verifier fails closed pending vetted JWKS library — recorded below); forgot/reset via mock mailer done; layered rate limits + trusted-proxy IP parsing done; audit-safe logging (no tokens/passwords) done; TS contract for /me consumed by frontend done.
- Verified behavior (local Postgres `quizforge_test`, `go test -race` clean):
  - Unit: Argon2id round-trip/wrong-password/wrong-pepper/garbage-hash; validation normalization; CSRF derive/verify/forgery; opaque token round-trip.
  - Service integration: register→login→session→logout→401; duplicate email/username → IDENTITY_UNAVAILABLE; unknown/wrong/locked → generic INVALID_CREDENTIALS with dummy-hash path; logout-all isolation across two accounts; reset revokes sessions + single-use + enumeration-safe forgot.
  - HTTP integration: unknown JSON fields rejected 400; cookie asserts `__Host-` + HttpOnly + Secure + SameSite=Lax + Path=/; missing CSRF → 403; full register→me→logout→401; generic 401 envelope code INVALID_CREDENTIALS.
  - OAuth with FakeOIDCVerifier: new-user create+login; same-email password account → link-required (no auto-link); state reuse rejected; forged state rejected.
  - Migrations: blank DB up → down → up via psql; dev DB migrated and runtime smoke passed.
  - Runtime smoke (DB-backed, port 18081): `/health/ready` 200 after migration; register 201 + Secure cookie; /me 200; duplicate 409; bad login 401; logout-without-CSRF 403; logout-with-CSRF 204; post-logout /me 401.
- Mocked behavior: Google login via `mock:<sub>:<email>` codes (FakeOIDCVerifier); password-reset email captured by MockMailer.
- Credential-required behavior: real Google OIDC (client not configured; RealOIDCVerifier fails closed until a vetted JWKS/OIDC library is vendored — no silent mock fallback), SES mail, Neon branches.
- Commands executed and results:
  - `go vet ./...` clean; `gofmt -l` empty; `go test ./...` all ok; `go test -race ./internal/auth/` ok.
  - psql blank-DB migration up/down/up — 7 auth tables created, dropped, recreated.
  - curl runtime smoke — all status codes as listed above.
- Migrations/API/ADR/doc changes: added `database/migrations/000002_auth.{up,down}.sql`; fixed readiness check to probe `app_user` (was `schema_migrations`, which local psql-applied DBs lack); `__Host-` cookie forced Secure (localhost-trustworthy) after curl/browser rejection of non-Secure `__Host-` cookies; set register `createdAt` from service time.
- Known risks or blockers: RealOIDCVerifier signature verification needs a vetted OIDC/JWKS library (new dependency + ADR) before real Google login; session `ip_prefix`/`user_agent_hash` columns written as NULL (metadata capture deferred); email verification enforcement deferred per plan; Argon2 params uncalibrated on t4g.small (deployment task).
- Next phase: Phase 2 frontend foundations (route guards, forgot/reset UI, app layout/nav, OAuth return handling, Playwright coverage).

### Phase 2 - Frontend Foundations (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: authenticated app shell usable; auth UX integrated with Phase 1 endpoints.
- Implemented: public/app route layouts + RequireAuth guard; sign-up/login/forgot/reset pages with Zod validation; Google button + OAuth/link failure banners; CSRF auto-attach in API client; AppLayout left-rail nav + mobile bar + logout; root error boundary with request ID; 404 page; skip link + focus ring + ARIA live toasts.
- Verified: `tsc --noEmit` clean; `vitest run` 5/5 pass (landing, guard redirect, forgot confirmation, reset missing-token, OAuth banner); `vite build` success.
- Mocked: Google button points at real `/api/v1/auth/google/start` (mock OIDC server-side in dev).
- Credential-required: real Google consent screen.
- Next phase: Phase 3 quiz management.

### Phase 3 - Quiz Management (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: complete manual quiz lifecycle verified end to end against real PostgreSQL.
- Implemented: `000003_quiz` migration (quiz/question/answer_option, deferrable position uniques); domain validator (bounds, contiguity, exactly-one-correct on publish, duplicate-text); owner-scoped service (create/list/get/versioned-update/delete/publish/unpublish/preview); REST handlers with If-Match + ETag + 409 currentVersion; editing published returns to draft; soft-delete; dashboard real data; three-zone editor with 600ms autosave, conflict dialog, keyboard reorder, radio correctness; local preview simulator with role tabs and no writes.
- Verified:
  - `go test ./...` all ok (domain bounds, CRUD lifecycle, ownership matrix, stale-version 409, 4-way concurrent update exactly-one-winner, reorder gaps rejected, preview no-write).
  - Runtime smoke on :18082: create 201; empty publish 400; update v2; stale update 409; publish 200; preview responsesSaved=false; cross-user read 404; list pagination.
  - Frontend `tsc` clean, `vitest` 7/7, `vite build` success (preview no-POST assertion test).
- Test-isolation fix: parallel Go packages shared one test DB and truncated each other's tables (flaky auth FAIL); added `TestDatabaseURLFor` per-package DBs (`quizforge_test_quiz`) — two consecutive full-suite runs green.
- Credential-required: none for this phase.
- Next phase: Phase 4 AI integration.

### Phase 4 - AI Integration (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: controlled generation pipeline producing valid editable drafts; mock path fully verified.
- Implemented: `000004_ai` migration; provider interface + HuggingFace OpenAI-compatible adapter (strict JSON Schema, bounded tokens, 512KiB cap, 429/5xx/timeout mapping, no auto-retry in transport); deterministic MockProvider labeled `mock`; strict parser (unknown fields rejected, trailing data rejected, fenced output rejected per test strategy, duplicate/banned/pattern checks); service with input rules, 5/day/user + 20/day/IP quotas, per-user(2)/global semaphores, 2-attempt policy, circuit breaker (5 fails/60s → open 30s), atomic draft insert, usage records, metadata-only logs; REST with required Idempotency-Key (identical replay, 409 on body mismatch); generator UI with staged progress, preserved inputs, manual fallback; editor AI-review banner distinguishing mock vs real.
- Verified:
  - `go test ./internal/ai/` all pass: golden conversion, invalid→valid retry (attempts=2, one quiz), double-invalid → 422-path with zero quiz rows, provider-down → 503-path zero rows, 401 no-retry, quota 5th-ok/6th-rejected, wrong-count zero rows, 12 adversarial parser cases, oversized body, input rules; HTTP idempotency test (replay identical, one provider call, 409 mismatch).
  - Live smoke on :18083 (mock): missing key 400; short topic 400; valid 201 mock=true; triple replay byte-identical with zero new rows; mismatch 409; quota 429 after 5 (fresh-user probe showed 429s; earlier confusing DIFFERENT comparison was two 429 envelopes with distinct requestIds, not a replay bug).
  - Frontend `tsc` clean, `vitest` 10/10, `vite build` success.
- Mocked: all generation via MockProvider (MOCK_AI=true, deterministic from topic hash).
- Credential-required: real Hugging Face call (no token configured; spend cap + model availability revalidation pending).
- Next phase: Phase 5 live quiz engine.

### Phase 5 - Live Quiz Engine (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: full lobby→questions→results→leaderboard→end flow verified over real sockets against the shipped binary.
- Implemented: `000005_live` + corrective `000006_live_check` migrations; join-code generator; name normalization; speed/fixed scoring; transition table; snapshot-copy session start (code retry ×5); join/resume with participant cookie; host commands (REST + WS, expectedVersion, idempotent close); DB-clock answer transactions with unique-conflict original-outcome return; embargoed role-filtered payloads; per-session actors with deadline hints; heartbeats/backpressure (4008 slow-consumer); reconnect snapshot; restart recovery from DB; host/participant UIs with reconnect backoff + resync; history endpoints + pages.
- Verified:
  - `go test ./internal/live/` all pass + `-race` clean: scoring boundaries, full transition table, code shape, name rules, start/join lifecycle, commands/answers/duplicates/late/ownership/results order, join-gone/late-disabled, terminal end, 100-concurrent-answers (100 rows, bounds, reconciliation), WS host+participant flow with embargo assertions, anonymous-upgrade rejection, role-violation/bad-shape errors, reconnect snapshot.
  - Live REST smoke (:18084): start 201, inspect joinable, join 201 + participant cookie, duplicate 409, start/end commands, participant snapshot contains no `isCorrect` while open, results + history correct.
  - Live WS smoke (real binary, scripted host + participant): snapshots → start → question_started (participant leak-free) → accepted ack without correctness → host-only answer_submitted → results both sides with ownResult correct +1000.
  - Frontend `tsc` clean, `vitest` 15/15 (incl. join code→name→room flow), `vite build` success.
- Bug fixes: duplicate-answer transaction-abort (fresh-read after rollback); `live_session_check` rejected history-preserving closes (corrective migration, original untouched); missing `return` in end_question case (close worked, handler misreported); answer_submitted now host-only.
- Credential-required: multi-session load targets (200 participants) — not yet measured.
- Next phase: Phase 6 history/analytics (backend done) + remaining hardening/deployment/docs.

### Phase 6 - Session History And Analytics (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: creator-only results durable and viewable; no other user's data exposed.
- Implemented: owner-scoped history list + detail aggregates (attendance/accuracy/response distribution/rank from live tables); HistoryPage + SessionDetailPage with bar visualizations and screen-reader data tables; reconciliation covered by 100-answer test (participant score vs answer sums) and leaderboard determinism (score→correct→ms→joined→id).
- Verified: REST smoke history count; frontend tests (list, detail aggregates, 404-safe); backend ownership via Results host check (cross-host 404 covered in live tests).
- CSV export: deferred as Level 2 per plan (not implemented, UI shows no export button).
- Next phase: Phase 7 testing/security/hardening.

### Phase 7 - Testing, Security, And Hardening (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: release-candidate gates executed with honest classification.
- Implemented/verified:
  - `gofmt -l` empty; `go vet ./...` clean; `go test -count=1 ./...` all ok (repeated runs, per-package DBs); `go test -race` clean on auth + live.
  - Frontend `tsc --noEmit` clean, `vitest` 15/15, `vite build` success (391KB JS / 118KB gzip).
  - OpenAPI strict check passes (29 paths) after installing pyyaml; `docker compose config` validates.
  - Blank-DB migration up (18 tables) → down (0) → up (18) across all six migrations.
  - Production static binary builds (`CGO_ENABLED=0`, 13MB) incl. `healthcheck` subcommand fail-closed behavior.
  - `govulncheck`: 0 vulnerabilities in code.
  - `npm audit`: 0 high after react-router-dom 7.8.2→7.18.4 upgrade (+ package-lock.json committed for reproducible `npm ci`); remaining 2 critical + 1 moderate are dev-only vitest/tinypool test-runner issues (never shipped in dist; vitest 4 upgrade blocked by npm peer-resolution error — documented exception, revisit later).
  - Log-redaction test (AI failure logs contain IDs/codes only); secret grep clean; CSP added to Caddyfile per AUTH_SECURITY.
  - Load smoke (dev server, local PG): 120 concurrent joins in ~40 ms; 120/120 WS answers accepted in ~189 ms burst, 0 errors; reconciliation confirmed 120 answer rows for the session.
- Blocked (environment, not code): Docker image builds + Compose up (no Docker daemon in this environment — client only); Playwright E2E (browsers uninstalled); k6 soak/10-session/200-user targets (measured ceiling so far: 120 participants above).
- Next phase: Phase 8 deployment prep + Phase 9 docs.

### Phase 8 - Deployment Preparation (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: production artifacts ready, nothing provisioned (no authorization, no credentials).
- Implemented: `deploy/compose.prod.yaml` (immutable digests, read-only FS, dropped caps, log rotation, memory/CPU limits, 30s grace); `deploy/scripts/deploy.sh` (SSM driver: 0600 env check, ECR login, migrate-then-switch, readiness gate, digest log); `deploy/scripts/ROLLBACK.md`; `deploy/scripts/backup.sh` (pg_dump → versioned S3, 14-day lifecycle); `deploy/opentofu/main.tf` skeleton (pinned provider, resource list per DEPLOYMENT.md); `.github/workflows/deploy.yml` (OIDC role, ARM64 buildx push, Trivy gate, SSM deploy) + `security.yml` (govulncheck, npm audit, gitleaks, openapi); hardened `ci.yml` (Postgres 18 service, per-package test DBs, race, contract, compose config); `.editorconfig`, Dependabot.
- Verified: compose config validates; workflow YAML present (not executed — no AWS account); scripts shell-checked by inspection only.
- Credential-required: ECR/EC2/Route53/SSM/Neon-prod/Google-prod/HF-cap/SES, first-boot smoke, restore drill, Argon2 calibration on t4g.small.
- Next phase: Phase 9 portfolio/docs.

### Phase 9 - Portfolio And Documentation (2026-10-10, worktree /home/mddanish/quiz)

- Outcome: README rewritten as a professional demo/evidence page (verified-vs-unverified split, measured table, setup, security decisions, limitations, interview points); `.env.example` documents all runtime + per-package test URLs; troubleshooting/testing/deployment guidance inline.
- Not done: demo video, screenshots (instructions: run demo flow, capture editor/host/participant/results), license choice (marked TBD), seed demo quiz (manual flow covers it).
- Final acceptance mapping:
  - [x] React frontend works (build + 15 tests + smoke flows).
  - [x] Go backend works (all packages ok + race).
  - [x] PostgreSQL migrations execute (up/down/up, 18 tables).
  - [x] Password authentication works (live smoke + tests).
  - [x] Google OAuth integration implemented (mock-verified; real marked credential-required).
  - [x] Quiz CRUD functional (live smoke + tests).
  - [x] AI quiz generation integrated (mock-verified; real marked credential-required).
  - [x] Generated quizzes editable (editor + review banner).
  - [x] Sessions created; participants join by code; WS works; server timers/validation/scores; leaderboards; persistence; history.
  - [x] Responsive layouts (320px padding-safe participant tiles, breakpoints, 44px targets) — manual viewport pass still recommended.
  - [ ] Docker images build / Compose starts services — blocked (no daemon); Dockerfiles + compose validated by config only.
  - [x] Automated tests pass (backend + frontend).
  - [x] Production deployment configuration + CI/CD present (unexecuted).
  - [x] Documentation matches implementation (this file + README).
  - [x] Core user journey verified end to end (register→login→mock-generate→edit→publish→host→join→answer→results→history, plus WS script proof).
- Continuation point: run Playwright E2E + k6 200-user/soak when Docker/browsers available; provision staging per DEPLOYMENT.md; replace metric placeholders after measured runs.

### Stage 3 - Production Readiness (2026-10-11, worktree /home/mddanish/quiz)
- Verified (all executed this session, current revision):
  - Backend: `gofmt` clean, `go vet` clean, `go test -count=1 ./...` 7/7 ok, `-race` on auth/live/cmd clean, `govulncheck` 0 code vulns.
  - Frontend: `tsc` clean, `vitest` 17/17, `vite build` ok.
  - Playwright E2E 10/10 in real Chrome 154 (~12 s): auth (3), quiz (2), AI mock (2), live multi-context (2), settings (1); 8 portfolio screenshots.
  - Load: 200×1 (200/200, ack p99 138 ms), 10×20 (200/200), 15-min soak (200/200, RSS flat ~25 MB), kill -9 drill (single close + correct snapshot); artifact `backend/test/load/STAGE3_RESULTS.md`.
  - Migrations blank up/down/up re-verified (18 tables); OpenAPI strict check (29 paths); workflows parse; scripts `bash -n` clean.
  - Security audit: no critical/high open; join/WS/socket rate limits + expiry enforcement added with tests; isolation probed via curl (404/404/0-items).
  - Deployment: prod Compose/Caddy/workflows/scripts reviewed (2 defects fixed); step-by-step runbook + cost/shutdown order added; AWS untouched.
- Blocked (named, not misrepresented): Docker builds/Compose/Caddy-validate (no daemon); HF/Google/Neon live (no credentials); tofu validate (no binary); ZAP/staging scans (no staging).
- Bugs #1-#11: race-order flake doc, empty-202 client crash, WS stale-onclose nulling, double live Manager, e2e.sh idempotency, workflow YAML, Caddy try_files shadowing, WS 30s timeout kill, writePump retire leak, harness ordering/pacing, expiry enforcement — all in `docs/PRODUCTION_READINESS.md` with fixes + tests.
- Docs updated: `PRODUCTION_READINESS.md` (new), `IMPLEMENTATION_STATUS.md`, `TESTING_STRATEGY.md` (E2E + WS client rules), `DEPLOYMENT.md` (Caddy fix + runbook + costs), `README.md` (Stage 3 evidence), `REALTIME_DESIGN.md` (ack-ordering rule).
- Next actions for the developer:
  1. Run `scripts/e2e.sh` and `make load-smoke` on your machine to reproduce (Docker still needed for images: `docker build -f backend/Dockerfile .`, `docker compose up --build`, then `caddy validate`).
  2. Provision staging per `docs/DEPLOYMENT.md` runbook; run rest­ore drill + ZAP baseline + real two-browser smoke.
  3. Configure HF token (spend cap + model revalidation), Google client, Neon branch, SES; mark credential-required items verified.
  4. Schedule retention-cleanup runner + decide `participant_left` presence event.
  5. Choose a license, record the demo video, publish.

## Evidence Ledger

| Behavior | Status | Evidence |
|---|---|---|
| Application implementation | not implemented | Documentation-only task |
| Automated tests | not implemented | No application/test code exists |
| Hugging Face integration | credential-required | Constraints researched; no code/credentials used |
| Google OIDC | credential-required | Flow specified; no client configured |
| Neon database | credential-required | Schema specified; no project/migrations run |
| AWS deployment | credential-required | Topology/runbook specified; no resources provisioned |

### Stage 4 - Real Integrations, Docker & Staging (2026-10-11, worktree /home/mddanish/quiz)

- Outcome: Docker became available — images built, stack verified, E2E 10/10 against containers; OIDC completed via go-oidc; HF adapter corrected to docs; retention runner + presence + proxy-trust + expiry gaps closed; 6 Stage-4 bugs fixed; AWS/credentials work explicitly blocked.
- Verified (all executed this session, current revision):
  - Backend: `gofmt`/`vet` clean, `go test ./...` 8/8 ok, `-race` live/auth/janitor clean, `govulncheck` 0 vulns.
  - Frontend: `tsc` clean, `vitest` 17/17, `vite build` ok.
  - Docker: api 25.6 MB + web 93.4 MB built; compose stack healthy; Caddy `Valid configuration`; routing/WS/E2E through Caddy green; ARM64 builds + boots (emulated); history secret-free.
  - Playwright: 10/10 vs dev backend AND 10/10 vs containerized prod builds (real Chrome).
  - OIDC: fake-issuer suite (9 rejections + rotation + link/takeover) green.
  - HF: contract suite green (suffix routing, error mapping, timeout).
  - Retention: boundary/idempotency/preservation tests green; live CLI run clean.
  - Presence: leave + reconnect-cancel tests green.
  - Recovery drills: kill -9/DB-stop/SIGTERM/backup/header-baseline all green.
  - Migrations: 000007 applied (blank + container DBs); up/down/up history intact.
- Blocked: real Google/HF/Neon (no credentials); tofu plan/apply + AWS provisioning (no creds/approval); ZAP binary.
- Docs: `STAGE4_STATUS.md` (new), `DECISIONS.md` (ADR-014), `AUTH_SECURITY.md`, `AI_INTEGRATION.md`, `DEPLOYMENT.md` (runbook, janitor timer, Neon guide, Argon2 procedure), `TESTING_STRATEGY.md` (prior stage), `PRODUCTION_READINESS.md` (bugs #13), `README.md` (below).
- Next: provision staging per runbook; verify credentials; schedule janitor; license decision; demo video.

### Stage 5 - Gate A Preparation (2026-10-11, worktree /home/mddanish/quiz)

- Outcome: safe-preparation gate complete; no cloud resources touched, no credentials present.
- Verified (executed this session): HF docs revalidated (adapter already matches;
  Qwen3-32B kept as default with Qwen2.5-7B-Instruct-1M fallback recorded);
  `go test -count=1 ./internal/ai/` ok; tofu 1.9.0 `fmt -check` clean + `validate`
  Success (skeleton caveat); `docker compose config` OK; workflows `yaml.safe_load`
  OK; `deploy.sh`/`backup.sh` `bash -n` OK; secret scan false-positives only.
- Fixes: S5-1 `deploy.yml` fabricated digest strings → real digest outputs;
  S5-2 `*.tfvars(*)` added to `.gitignore`.
- Blocked: live Google/HF/Neon (no credentials); `tofu plan/apply` + all
  provisioning (no creds, Gate B approval required); staging E2E/ZAP/drills.
- Docs: `STAGE5_STATUS.md` (new, incl. Gate B approval request); cost revalidation
  note in `COST_ANALYSIS.md`; model note in `AI_INTEGRATION.md`.
- Next: operator approves Gate B (resources, cost, domain, credentials); then
  author full tofu modules, `plan`-review, provision, and verify staging.

### Stage 5 - Gate A.2 Infrastructure Code (2026-10-11, worktree /home/mddanish/quiz)

- Outcome: full IaC complete locally; nothing provisioned, no credentials present.
- Created: `deploy/opentofu/` full graph (versions/variables/main/outputs/user-data/
  tfvars-example); `backend/Dockerfile.migrate`; `AWS_SETUP_GUIDE.md`;
  `INFRASTRUCTURE_REVIEW.md`; cost inventory in `COST_ANALYSIS.md`.
- Updated: `compose.prod.yaml` (awslogs), `deploy.sh` (previous.env/deploy.env/
  janitor units/.env-sourced migration URL), `deploy.yml` (migrate build,
  `v*`-tag trigger), `DEPLOYMENT.md`, `STAGE5_STATUS.md`.
- Verified: tofu fmt CLEAN, validate Success, tflint 0 findings, YAML/bash/compose
  OK, migrate image built + CLI-verified, `go vet` clean, `go test ./...` 8/8 ok,
  secret scan clean. `tofu plan` NOT RUN (no creds).
- Fixes: S5-3 previous.env never written; S5-4 migrate image/URL/stub chain;
  S5-5 auto-deploy on main push.
- Blocked: Gate B approval → plan/apply, secrets, DNS, tag release, staging E2E/ZAP.
- Next: operator reviews `INFRASTRUCTURE_REVIEW.md` + plan output, then approves apply.

### Stage 5 - Live Deployment Troubleshooting (2026-10-13, worktree /home/mddanish/quiz)

- Outcome: diagnosed the `deploy-prod` migration failure after `v0.2.5`; fixed
  SSM parameter capture locally. The live deployment is not yet verified.
- Root cause: `getp` was invoked by command substitution and wrote its safe
  `[env] got NAME (...)` diagnostic to stdout along with the value. This
  prefixed `MIGRATIONS_DATABASE_URL`, so golang-migrate parsed the diagnostic as
  the driver and the `postgresql:` → `postgres:` normalization did not match.
- Fix: send `getp` diagnostics to stderr and keep stdout value-only; retain
  single-quoted `.env` materialization and mode 0600. The systemd output path is
  configurable for isolated testing and remains `/etc/systemd/system` by
  default. The release workflow now runs the syntax and regression tests before
  authenticating to AWS or building images.
- Mocked verification: `bash deploy/scripts/deploy_test.sh` — PASS. The test
  uses fake SSM/ECR/host commands and verifies exact values (including `(` and
  an embedded quote), `.env` mode 0600, absence of diagnostic leakage, and the
  normalized `postgres://` URL supplied to the migration container.
- Verified static check: `bash -n deploy/scripts/deploy.sh
  deploy/scripts/deploy_test.sh` — PASS.
- Remaining: ship under a new immutable tag (expected `v0.2.6`), approve the
  production environment, then verify migration, readiness/version, logs, and
  the core browser smoke flow. AWS/Neon execution remains credential-required
  from this worktree.

### Stage 6 - Render Pivot: AWS Retirement, Single-Service Production (2026-10-14, worktree /home/mddanish/quiz)

- Outcome: all AWS-specific production machinery removed; the project now
  deploys as one Render web service (single Docker image, Go serves API + SPA
  on one origin) + Neon Postgres. No EC2/ECR/SSM/Caddy/SES remains in code,
  config, workflows, or operational docs.
- Deleted: `deploy/opentofu/`, `deploy/caddy/`, `deploy/compose.prod.yaml`,
  `deploy/scripts/deploy.sh` + `deploy_test.sh`, `frontend/Dockerfile`,
  `docs/AWS_SETUP_GUIDE.md`, `docs/INFRASTRUCTURE_REVIEW.md`,
  `docs/CODEX_HANDOFF.md` (obsolete AWS troubleshooting handoff).
- Added/changed:
  - `backend/Dockerfile`: single image (node build → go build → distroless
    + `/srv` SPA, `STATIC_DIR=/srv`); `frontend/Dockerfile` removed.
  - `backend/internal/platform/httpserver/static.go` (+ tests): SPA serving
    with index.html fallback, immutable `/assets` caching, no-store shell;
    wired via `Deps.StaticDir` / `STATIC_DIR` env (empty in local dev).
  - `SecurityHeaders` middleware now owns the CSP/HSTS/COOP policy the
    Caddyfile used to enforce (verified by existing + new tests).
  - `config`: dropped `AWS_REGION` + unused `APP_DOMAIN`; added
    `MAIL_PROVIDER=mock|smtp` validation with `SMTP_ADDR/USERNAME/PASSWORD`
    (stdlib STARTTLS relay; SES was never implemented); `PORT` env overrides
    `HTTP_ADDR` for Render.
  - `backend/internal/auth/smtp_mailer.go` (+ tests): message builder +
    envelope parsing unit-tested; wired in `routes.go` when
    `MAIL_PROVIDER=smtp` and not `MOCK_MAIL`.
  - `render.yaml` blueprint (single service, `plan: free`, `autoDeploy: false`,
    `/health/ready` gate).
  - `.github/workflows/deploy.yml` rewritten: migrate (pinned
    `migrate/migrate` image, direct Neon URL, scheme-normalized) → Render
    deploy hook → readiness/version smoke. No AWS credentials anywhere.
  - `compose.yaml`/`compose.override.yaml`: single `api` service (the image
    serves the UI); `deploy/scripts/backup.sh` is now host-independent
    `pg_dump`; `ROLLBACK.md` rewritten for Render.
  - Docs: `DEPLOYMENT.md` + `COST_ANALYSIS.md` rewritten for Render;
    `SYSTEM_ARCHITECTURE.md` diagrams/prose, `AUTH_SECURITY.md` (SES/Caddy/SSM
    refs), `README.md`, `PRODUCT.md` updated; ADR-016 recorded above.
- Verified (executed 2026-10-14, current revision):
  - `gofmt -l` empty; `go vet ./...` clean; `go test -count=1 ./...` 8/8 ok
    (per-package test DBs, same as CI); `-race` clean on auth + platform;
    `govulncheck`: 0 vulnerabilities in code.
  - Frontend: `tsc --noEmit` clean, `vitest` 17/17, `vite build` ok.
  - `docker compose config` OK; `deploy.yml`/`ci.yml`/`render.yaml`
    `yaml.safe_load` OK; `backup.sh` `bash -n` OK; `openapi_check` 29 paths OK.
  - Runtime smoke (built binary + `STATIC_DIR=frontend/dist`, dev DB): `/`
    200 shell, `/app/quizzes` 200 fallback, `/assets/*.js` 200 immutable +
    HEAD 200, CSP/HSTS/CSP headers present, `/health/ready` 200,
    `/version` ok, register 201. HEAD-405 found during smoke → fixed
    (`r.Head` registered) with regression test.
  - Docker image build NOT RUN (no daemon in this environment); first Render
    deploy + Neon prod + Google/HF/SMTP + restore drill remain
    credential-required.
- Next: push a `v*` tag and follow `docs/DEPLOYMENT.md`.

## Phase Update Template

```markdown
### Phase N - Name (YYYY-MM-DD, revision <sha or worktree>)

- Outcome:
- Mandatory checklist: complete / incomplete items
- Verified behavior:
- Mocked behavior:
- Credential-required behavior:
- Commands executed and results:
- Migrations/API/ADR/doc changes:
- Known risks or blockers:
- Next phase:
```
