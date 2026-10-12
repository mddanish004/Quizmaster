# Implementation Plan

## Recommended Repository

```text
quizforge-ai/
+-- frontend/
|   +-- src/
|   |   +-- app/                    # router/providers/layouts
|   |   +-- components/{ui,data}/
|   |   +-- features/{auth,dashboard,quizzes,ai-generation,live-host,participation,results,settings}/
|   |   +-- generated/api.ts
|   |   +-- lib/{api,query-client,errors,format}.ts
|   |   +-- styles/index.css
|   |   `-- test/{fixtures,msw,render}.tsx
|   +-- e2e/
|   +-- public/
|   +-- Dockerfile
|   +-- package.json
|   `-- vite.config.ts
+-- backend/
|   +-- cmd/{api,migrate}/
|   +-- internal/
|   |   +-- platform/{config,database,httpserver,logging,ratelimit}/
|   |   +-- auth/{domain,service,repository,http,google,mail}/
|   |   +-- quiz/{domain,service,repository,http}/
|   |   +-- ai/{provider,service,huggingface,http}/
|   |   +-- live/{domain,service,repository,http,ws,actor}/
|   |   `-- reporting/{service,repository,http}/
|   +-- db/{query,sqlc.yaml}/
|   +-- testutil/
|   +-- Dockerfile
|   +-- go.mod
|   `-- go.sum
+-- database/migrations/
+-- deploy/{caddy,compose,scripts,opentofu}/
+-- docs/
+-- scripts/
+-- test/load/
+-- .github/workflows/{ci,security,deploy}.yml
+-- compose.yaml
+-- compose.override.yaml
+-- .env.example
+-- Makefile
+-- AGENTS.md
+-- PRODUCT.md
`-- README.md
```

Go packages are singular lowercase and expose minimal APIs. TypeScript components use `PascalCase.tsx`, hooks `useThing.ts`, feature files kebab-case where route-oriented, tests beside units or under feature `__tests__`. SQL query names are verbs (`GetQuizForOwner`). API JSON is camelCase; database is snake_case.

Pin exact dependencies in lock files and use the latest supported patch within selected majors at Phase 0. Research snapshot: Go 1.27.2 ([release history](https://go.dev/doc/devel/release)), React 19.3 ([versions](https://react.dev/versions)), Vite 8.1 ([announcement](https://vite.dev/blog/announcing-vite8-1)), Tailwind 4.3 ([blog](https://tailwindcss.com/blog)), TanStack Query v5 ([docs](https://tanstack.com/query/latest/docs/framework/react/reference)), PostgreSQL 18.6 current as researched ([PostgreSQL](https://www.postgresql.org/docs/current/)). Revalidate compatibility before initializing.

## Standard Commands

The Makefile is the stable interface:

```text
make bootstrap       install pinned dev tools/dependencies
make dev             start local development
make build           build frontend/backend
make fmt             apply formatters
make lint            format check, ESLint/typecheck, vet/staticcheck
make test            unit tests
make test-integration
make test-race
make e2e
make openapi-check
make migrate-up
make migrate-down
make compose-up
make load-smoke
make security
```

### Phase 0 - Repository And Environment

**Objective:** reproducible empty application shells, toolchain, contracts, and local container workflow.

**Prerequisites:** specification accepted; Neon dev branch URL available for manual use, but mocks/test containers permit progress without credentials.

**Mandatory tasks**

- Initialize Go module and `cmd/api`; typed env config with validation; JSON logger; request ID; liveness/readiness; graceful shutdown.
- Initialize React/TS/Vite/Tailwind, router, Query provider, root error boundary, baseline tokens, lint/type/test config.
- Pin Chi, pgx, sqlc, `golang-migrate`, `github.com/coder/websocket`, Radix primitives, and the named frontend libraries after compatibility checks.
- Create Compose dev/prod shapes, Dockerfiles, Caddy baseline, Makefile, `.gitignore`, EditorConfig.
- Check in OpenAPI lint/generation script and `sqlc` config.
- CI baseline for format/lint/unit/build/security; Dependabot/Renovate config.
- Add architecture test preventing cyclic module imports if useful without custom framework.

**Files:** root tooling; `backend/cmd/api`, `internal/platform`; `frontend/src/app`; Docker/Compose; `.github/workflows/ci.yml`.

**Database/APIs:** no domain tables; migration version mechanism; `/health/live`, `/health/ready`, `/version`.

**Tests:** config matrix; health/readiness; graceful cancellation; frontend route/error render; container health.

**Commands:** `make bootstrap fmt lint test build openapi-check`; `docker compose config`; `docker compose up --build`; curl health/version.

**Acceptance/DoD:** fresh clone reaches healthy shell from documented commands; no secrets; CI green; images run non-root; exact evidence/status recorded. Optional: staging IaC skeleton may wait for Phase 8.

**Deliverable:** runnable foundation, not feature placeholders presented as complete.

### Phase 1 - Backend Foundations

**Objective:** database, migrations, password/Google/session/recovery authentication, authorization middleware.

**Prerequisites:** Phase 0 gate; database test environment.

**Mandatory tasks**

- Implement auth/security migrations from database design, sqlc queries, transaction helper.
- Argon2id calibration/defaults and bounded hashing; opaque session/cookie/CSRF middleware; cleanup job.
- Register/login/logout/logout-all/current user/profile endpoints and stable error mapper.
- OIDC transaction, PKCE/state/nonce/JWKS verification and explicit account-link flow.
- Password forgot/reset provider interface, mock mail, SES adapter configuration.
- Layered rate limits and trusted proxy handling; audit-safe logging.
- Generate TS types from OpenAPI and consume `/me` contract.

**Files:** `database/migrations/0001_*`; `internal/auth/*`; `internal/platform/ratelimit`; OpenAPI auth schemas.

**APIs:** auth and `/me` endpoints in API specification.

**Tests:** Argon2 format/rehash; session rotate/revoke/expiry; cookie/CSRF/origin; brute limits; OAuth forged/expired/link conflicts; two-account isolation; recovery single-use; DB integration.

**Commands:** `make migrate-up test test-integration test-race lint openapi-check security`; manual/mock browser login.

**Acceptance/DoD:** all password flows work end-to-end; Google is mocked and marked credential-required until real staging smoke; no session token in DB/log/client JS; blank DB up/down/up succeeds.

**Deliverable:** authenticated backend and generated frontend contract. Optional email verification enforcement may be deferred, but recovery strategy/provider cannot be a stub.

### Phase 2 - Frontend Foundations

**Objective:** accessible app shell and complete authentication UX integrated with Phase 1.

**Prerequisites:** auth contract stable.

**Mandatory tasks**

- Implement public/app/editor/host/participant route layouts and route guards.
- UI primitives: button/icon button, form field, input, dialog, menu, tabs, toast, skeleton, status, error/empty state.
- Sign-up/login/forgot/reset/OAuth return, current-user bootstrap, logout, responsive navigation.
- Theme tokens and dark-capable variables; dark-mode toggle may remain Level 2.
- MSW fixtures, error boundary, offline/query defaults, request-ID display.
- Accessibility keyboard/focus/live-region baseline and responsive checks.

**Files:** `frontend/src/app`, `components`, `features/auth`, `styles`, tests.

**Database/APIs:** no new DB; consumes Phase 1 endpoints.

**Tests:** form/client-server validation, auth redirects, loading/errors/offline, keyboard/axe, Playwright register/login/logout/reset mock OAuth.

**Commands:** `npm run lint typecheck test build`; `make e2e`; manual 320/390/768/1280 viewport pass.

**Acceptance/DoD:** authentication is usable without mouse, errors preserve input, unauthorized routes redirect safely, frontend has no token storage, CI green.

**Deliverable:** polished shell/auth experience, not dashboard mock data.

### Phase 3 - Quiz Management

**Objective:** durable manual creation, dashboard CRUD, editor, lifecycle, preview.

**Prerequisites:** creator identity/UI foundations.

**Mandatory tasks**

- Authoring migrations/queries/domain aggregate and owner-scoped services.
- Cursor list/filter/sort, create/get/versioned update/delete/publish/unpublish endpoints.
- Publication validator and session-independent version behavior.
- Dashboard real data, empty/loading/error states.
- Three-zone editor, autosave/version conflict, add/delete/reorder, validation, correctness controls.
- Local preview with host/participant views and no stored result.
- Soft-delete cleanup policy/job.

**Files:** `internal/quiz/*`; migrations 0002; `features/dashboard`, `features/quizzes`; OpenAPI.

**Tests:** domain bounds; ownership for every endpoint; concurrent version update; publish invalid/valid; reorder constraints; editor autosave/conflict/reload; preview non-persistence; E2E manual publish.

**Commands:** full lint/test/integration/e2e/openapi/migration gates.

**Acceptance/DoD:** User A cannot infer/mutate User B data; reload preserves ordered aggregate; published quiz is complete; max-size quiz remains usable; status updated.

**Deliverable:** complete manual quiz workflow. Duplication and true/false remain optional Level 2.

### Phase 4 - AI Integration

**Objective:** controlled Hugging Face generation produces valid editable drafts without partial writes.

**Prerequisites:** quiz aggregate and create transaction.

**Mandatory tasks**

- AI generation migration/queries, provider interface, HF HTTP adapter, prompt/schema version.
- Quotas, in-flight semaphore, timeouts, two-attempt policy, circuit breaker, usage/error records, idempotency.
- JSON/schema/domain/quality pipeline and atomic conversion to quiz.
- Generator UI with preserved inputs/progress/failure/manual fallback and review labels.
- Fixture provider and adversarial/evaluation corpus; log-redaction assertions.

**Files:** `internal/ai/*`; migration 0003; `features/ai-generation`; testdata; OpenAPI.

**Tests:** every case in AI doc, concurrency quota/idempotency, no partial DB, UI states, mocked E2E.

**Commands:** full gates plus `go test ./internal/ai/...`; optional `make ai-smoke` only with sandbox credential.

**Acceptance/DoD:** mocked path verified; real HF explicitly verified or credential-required; failed response inserts no quiz; model/provider configurable; free quota not presented as unlimited.

**Deliverable:** working AI-assisted draft pipeline. Individual regeneration/document upload excluded.

### Phase 5 - Live Quiz Engine

**Objective:** reliable live session from lobby through final leaderboard with recovery.

**Prerequisites:** published quiz, auth, stable contracts.

**Mandatory tasks**

- Live snapshot/participant/answer/event migrations and queries.
- Start/join/snapshot/results REST; six-character collision retry; participant resume cookie.
- State machine, versioned transactional transitions, scoring/ties, deadline recovery.
- Actor manager, bounded hub/client pumps, origin/role validation, protocol envelopes, heartbeat/backpressure/graceful restart.
- Host lobby/presentation/results/leaderboard and participant join/wait/question/result screens.
- Reconnect/resync, duplicate submission, stale host, host refresh/backend restart behavior.
- Metrics for sockets/sessions/answers/event latency/drops.

**Files:** migration 0004; `internal/live/*`; `features/live-host`, `participation`; OpenAPI/event schemas; load scripts.

**Tests:** transition table, scoring boundaries/ties, role payload embargo, WS commands/events, 100-answer race, timer/host race, slow client, reconnect/restart; Playwright multi-context; initial k6 smoke.

**Commands:** `make test test-integration test-race e2e load-smoke`; run API under `-race`; inspect goroutine/heap.

**Acceptance/DoD:** required live E2E passes; accepted answer scores once; no early key; host/API refresh recovery works; 200-user target is either verified with artifact or accurately lowered; repository functioning.

**Deliverable:** core portfolio differentiator. QR code is the first optional enhancement after gate.

### Phase 6 - History And Analytics

**Objective:** useful durable creator results without overbuilding BI.

**Prerequisites:** ended live sessions and result invariants.

**Mandatory tasks**

- Owner-only history/detail queries, cursor pagination, aggregates for attendance/accuracy/response time/rank.
- History table and detail charts with accessible data alternatives.
- Retention cleanup and creator deletion behavior.
- Reconciliation command/test comparing cached participant score to answer sums.

**Files:** `internal/reporting/*`; `features/results`; optional indexes migration.

**Tests:** aggregate correctness including zero answers/ties/deleted source; ownership; query plans at synthetic scale; chart accessibility; E2E history after session.

**Commands:** full gates plus query `EXPLAIN` script.

**Acceptance/DoD:** results match live records, are owner-only, performant at target dataset, and accessible. CSV export is Level 2.

**Deliverable:** credible session reporting.

### Phase 7 - Testing And Hardening

**Objective:** close failure, security, accessibility, and performance gaps before deployment.

**Prerequisites:** complete MVP behavior.

**Mandatory tasks**

- Complete required E2E matrix and coverage gaps; fuzz/chaos/failure injection.
- Threat-model verification, two-user authorization matrix, CSRF/Origin/OAuth/WS abuse tests.
- Full k6 ramp/multi-session/soak on production-equivalent ARM; profile and tune.
- Responsive/browser/screen-reader/manual accessibility pass.
- Dependency/image/secret scans, CSP/report-only then enforce, log privacy review.
- Backup/restore scripts and local/staging drill.

**Files:** test/load, security workflow, runbooks, updated docs/evidence.

**Database/APIs:** only evidence-driven fixes/indexes; contract changes require compatibility review.

**Commands:** every Makefile gate, k6 profiles, ZAP baseline, Trivy/govulncheck/npm audit/gitleaks, restore test.

**Acceptance/DoD:** testing strategy release gates met; measured capacity documented; no high-risk open auth/score/key issue; all gaps classified, no invented pass.

**Deliverable:** release candidate and evidence bundle.

### Phase 8 - Deployment

**Objective:** reproducible AWS production deployment with safe CI/CD and operations.

**Prerequisites:** hardened release candidate, accounts/domain/credentials and budget approval.

**Mandatory tasks**

- IaC for named AWS resources, budgets, IAM/OIDC, logs/alarms/backup.
- Build/sign/push ARM64 images; bootstrap host via SSM; materialize secrets safely.
- Configure Neon production, Google consent/redirect, HF spend cap, SES/domain, DNS/TLS.
- Deploy backward-compatible migration/images; smoke expected commit and real WebSocket flow.
- Exercise rollback and database restore; record costs and runbooks.

**Files:** `deploy/*`, deploy workflow, operational docs.

**Tests:** IaC validate/plan, image scan, migration, smoke, live two-browser, alarm/backup/rollback/restore tests.

**Commands:** documented IaC plan/apply, deploy workflow, `make smoke-prod`; commands must name environment and avoid broad destructive targets.

**Acceptance/DoD:** URL works over HTTPS, exact commit/version visible, production secrets absent from repo/log/image, monitoring/budgets/backups active, rollback verified. Without real infrastructure mark credential-required, not complete.

**Deliverable:** verified deployment.

### Phase 9 - Portfolio Readiness

**Objective:** present verified engineering work clearly to reviewers/interviewers.

**Prerequisites:** deployed/staged working system and stored evidence.

**Mandatory tasks**

- README concise demo, screenshots, architecture/state diagrams, local setup, trade-offs, measured results, security/testing status.
- Seed a demonstration quiz/session without fake adoption data.
- Record 60-90 second video: AI generate/review, second-device join/answer, host reveal/leaderboard, refresh recovery, history.
- Publish test/load/cost table with commit/environment/date and methodology.
- Curate issues/roadmap, license, contribution/security policy, release notes.
- Rehearse interview narratives: actor vs DB authority; answer race; session vs JWT; HF validation; EC2 vs Lambda; Neon pooling; failure/recovery; scale path.

**Tests:** clean-clone setup by following README; all links/media; production demo; accessibility quick pass; no secrets/synthetic metrics ambiguity.

**Commands:** full CI/release smoke; link checker; repository secret scan.

**Acceptance/DoD:** a reviewer can understand, run, and see the product; every metric is reproducible; known limitations are candid.

**Deliverable:** flagship repository. Resume bullets use only measured placeholders replaced with evidence.

## Portfolio Measurement Plan

Collect: maximum verified sockets/participants/session; active sessions; p50/p95/p99 event and answer-commit latency; HTTP p95; error/drop/reconnect rate; RSS/goroutines/CPU; restart recovery time; schema-valid AI first/retry rate and latency/cost; tests/coverage by critical package; build/deploy/rollback time; monthly AWS/Neon/HF actual cost. Always attach date, commit, environment, dataset/script, duration, and limitations.

Interview value comes from explaining invariants and trade-offs, not a dependency list: why commits precede broadcasts; why an immutable snapshot exists; how unique constraints and idempotency stop double scores; how timers recover; why sessions beat JWT here; why a $22-$28 EC2 base is cheaper in engineering time than distributed Lambda WebSockets; and what evidence would trigger Redis/multiple instances.
