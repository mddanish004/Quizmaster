# Testing Strategy

## Principles

Test domain invariants at the cheapest layer, then prove integrations where boundaries change behavior. External-service mocks make CI deterministic; separate credentialed smoke tests are reported honestly. A test is not "passed" unless executed in the current revision. Time, random IDs/codes, and AI providers are injected so edge cases are deterministic.

## Tooling

Frontend: Vitest, React Testing Library, `@testing-library/user-event`, MSW, Playwright, axe integration, optional Storybook only if component volume justifies it. Backend: standard `testing`, `httptest`, `testify/require` sparingly, `pgx` test repositories, Testcontainers PostgreSQL 18, WebSocket test clients, `go test -race`, fuzzing. Contract: OpenAPI/Spectral and generated TypeScript types. Load: k6 with `k6/websockets` or `xk6-websockets` only if core support lacks needed behavior. Security: govulncheck, npm audit, Trivy, gitleaks, OWASP ZAP baseline against staging when stable.

Stage 3 additions (2026-10-11, validated): Playwright E2E lives in
`frontend/e2e/` and runs via `scripts/e2e.sh` (disposable `quizforge_e2e`
database, built API on :8080, Vite dev on :5173, real Chrome). The E2E backend
runs with `RATE_LIMIT_ENABLED=false` because one suite performs more
registrations than the 5/hour/IP abuse budget; rate-limit behavior itself is
covered by backend unit/integration tests asserting 429 mappings. The Go load
harness lives in `backend/test/load/` (`burst`/`multi`/`soak` modes).

## Backend Test Matrix

| Layer | Coverage |
|---|---|
| Domain unit | quiz validation, state transition table, score/tie formula, normalization, authorization predicates, deadlines |
| Service | transaction outcome, idempotency, ownership, publish/snapshot, AI pipeline, auth/link/recovery |
| Repository integration | migrations, constraints, indexes/query semantics, concurrent answer uniqueness, cleanup |
| HTTP | decode/unknown fields, status/error envelope, cookies/CSRF/origin, pagination, ETag/version, no leakage |
| WebSocket | upgrade auth/origin, every command/event, role filtering, ordering/version gaps, heartbeat/backpressure/reconnect |
| Concurrency | 100 simultaneous answers, duplicate submission, host/timer close race, restart recovery; `-race` |
| Fuzz | JSON decoders, join/display normalization, event envelope, cursor decoder, prompt output parser |

Database integration tests run real PostgreSQL rather than SQLite. Neon-specific pooled-connection and wake/reconnect behavior runs against a disposable Neon branch in scheduled/manual smoke tests if credentials exist.

## Frontend Test Matrix

- UI primitives: keyboard/focus/ARIA, disabled/loading, long labels, reduced motion.
- Forms: client/server errors, duplicate identity, preserved input, password manager/paste behavior.
- Routes: auth gates, return paths, 404/403/error boundary, URL filters.
- Queries: skeleton/empty/stale/offline/retry, mutation invalidation, version conflict.
- Editor: add/delete/reorder, option correctness, autosave state, conflict, mobile panel flow.
- Live reducers: snapshot, ordered events, gap/resync, duplicate/lower version ignored, terminal state.
- Participant: send/ack/retry same submission ID, late rejection, no early correctness.
- Charts: accessible table/summary and correct aggregate labels.

## AI Tests

Script provider responses for valid JSON, fenced JSON, malformed/truncated body, schema mismatch, unknown fields, duplicate questions/options, missing correct ID, wrong count/language, huge response, timeout, cancellation, 401, 429 with/without retry, 500, safety rejection, and partial network read. Assert no partial quiz, bounded attempts/budget, exact error category, usage record, idempotency, and log redaction.

Maintain `testdata/ai-evals.jsonl` with diverse safe topics/difficulties and adversarial prompt-injection inputs. A credentialed evaluation reports first-pass schema success, retry success, latency, tokens/cost, duplicate heuristic rate, and a manually reviewed factual sample. These are measured build artifacts, not fixed acceptance claims until baseline is collected.

## Required End-To-End Scenarios

1. Register -> logout -> login -> generate with mock AI -> review/edit -> publish.
2. Manual quiz -> publish -> start -> second browser joins -> answer -> results -> leaderboard -> end -> history.
3. Google callback using a local fake OIDC provider -> dashboard -> create quiz; separate real Google staging smoke.
4. Participant disconnects before/after answer ack, reconnects, preserves one score.
5. Host refreshes during open question and regains correct timer/state/control.
6. 100 simulated participants answer simultaneously; one row/score each and correct aggregate.
7. User A attempts every read/write on User B quiz/session; receives non-leaking denial.
8. AI returns malformed output twice; error is actionable and database has no quiz.
9. Database connection drops temporarily: safe request behavior, readiness fails, pool reconnects, no false answer ack.
10. API restarts during open question: clients reconnect, overdue question closes once, results agree.
11. Expired join code, duplicate name, late join disabled, timer expiry, slow socket, stale host version.
12. Password reset invalidates old sessions; OAuth email collision requires explicit link.

Use Playwright multiple browser contexts for host and 2-5 participants; k6 owns high concurrency.

### WebSocket Client Rules (learned 2026-10-11)

- A command acknowledgement and its broadcast may arrive in either order for
  the commanding client. Correlate with `requestId`, order with `stateVersion`.
- Never stop reading while a connection must survive: quiet clients miss the
  20s ping watchdog and are pruned. Browsers auto-pong; Go harnesses must drain.
- With `coder/websocket`, cancelling a `Read` context closes the connection.
  Harness reads must use contexts that outlive the read or expect closure.

## Load And Reliability Plan

### Scripts

- REST baseline: login fixture users, quiz list/detail, start/join; avoid AI/provider in primary load.
- Live ramp: 10 -> 50 -> 100 -> 200 sockets in one session, join over 60 seconds, answer within a 5-second burst, observe results.
- Multi-session: 10 sessions x 50 participants, stagger questions.
- Soak: 100 sockets for 2 hours with heartbeats/reconnects to detect leaks.
- Abuse: invalid/duplicate answer and slow readers without affecting healthy p95.

### Initial Acceptance Targets

On production-equivalent `t4g.small`, warm Neon, release build:

- 200 participants in one session and 10 active sessions within memory/CPU limits;
- answer acceptance error under 0.1% excluding intentional late/duplicate cases;
- HTTP read p95 <300 ms, write p95 <500 ms;
- committed event to healthy connected client p95 <250 ms;
- no race detector findings; no unbounded goroutine/heap growth after disconnect;
- process RSS target <1.2 GiB at test peak, leaving OS/Caddy/headroom;
- restart recovery reaches readiness and reconstructs sessions within 30 seconds.

These are engineering gates to test, not current achievements. If missed, publish measured limits and tune before raising capacity claims.

## Performance Diagnostics

Likely bottlenecks: Argon2 memory, Neon network/wake latency, one transaction per answer, hot participant-score updates, JSON/broadcast copies, slow socket queues, and chart/report aggregate queries. Capture Go CPU/heap/goroutine profiles in a protected environment, DB pool wait metrics, `pg_stat_statements`, `EXPLAIN ANALYZE`, k6 trends, and container/host memory. Optimize evidence-first: prepared queries/batching aggregates, payload reuse, pool tuning, vertical resize; do not add Redis/Kafka reflexively.

## Coverage And Quality Gates

Do not use one repository-wide percentage as a quality substitute. Gates:

- 100% transition table cases and score boundary cases;
- every API operation has success, validation, unauthenticated, unauthorized, and relevant conflict tests;
- every WebSocket command/event has schema/role/state tests;
- changed Go packages target >=80% statement coverage, security/live core >=90% where meaningful;
- frontend changed feature logic target >=80%, with interaction assertions rather than snapshots alone;
- zero high/critical known exploitable dependency/image findings or a documented time-bounded exception;
- all required E2E flows green in release candidate.

## Failure Injection

Inject provider delay/429/invalid data; DB query timeout and connection termination; process `SIGTERM`; clock advance; broadcast queue saturation; network offline in Playwright; duplicate/reordered client events; and disk/log pressure in staging where safe. Verify bounded failure, accurate UI status, no secret leakage, and recovery.

## Accessibility And Responsive QA

Automated axe is necessary, not sufficient. Release checklist covers keyboard-only full creator/live flow, focus after navigation/dialog/error, screen-reader labels/status/timer throttling, contrast, 200% zoom, 320 px reflow, touch targets, long strings, reduced motion, and chart alternatives. Test current Chrome/Firefox/Safari/Edge plus iOS Safari/Android Chrome at least manually for core participant flow.

## Test Data

Factories create deterministic users/quizzes/sessions; never use production dumps. Include long Unicode names, normalization-equivalent names, 6-option/50-question maxima, zero/200 participants, deadline boundary, ties, deleted source quiz, and hostile strings. Synthetic/demo data is labeled.

## Release Evidence

CI stores JUnit/coverage, Playwright traces/screenshots on failure, migration log, OpenAPI diff, SBOM/scans, k6 summary for release runs, and image digests. README metrics are updated only from an identified artifact/commit/environment. Credential-required checks are listed separately as passed, failed, or not run.
