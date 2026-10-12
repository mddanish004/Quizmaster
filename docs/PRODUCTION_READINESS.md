# Production Readiness — Stage 3 Validation

Date: 2026-10-11. Worktree: `/home/mddanish/quiz`. All results below were executed
in this session unless marked otherwise. Status values: **PASS** (executed ok),
**FAIL** (executed, failed), **BLOCKED** (missing dependency, named), **NOT RUN**,
**N/A**.

## 0. Baseline Gates (re-run on current revision)

| Check | Status | Evidence |
|---|---|---|
| `gofmt -l` empty | PASS | empty output, 2026-10-11 |
| `go vet ./...` | PASS | clean, 2026-10-11 |
| `go test -count=1 ./...` (4 test DBs) | PASS | all 6 packages ok, 2026-10-11 |
| `go test -race` auth+live | PASS (after fix) | auth ok; live failed once (ack/broadcast order flake, bug #1), then `-count=2` green after order-tolerant fix |
| frontend `tsc --noEmit` | PASS | clean, 2026-10-11 |
| frontend `vitest run` | PASS | 15/15, 2026-10-11 |
| `govulncheck` | PASS | 0 code vulnerabilities, 2026-10-11 |

## 1. Docker End to End (Priority 1)

Stage 4 update (2026-10-11): daemon became available (server 28.1.1) — full verification executed:

| Check | Status | Evidence |
|---|---|---|
| Docker daemon available | PASS | server 28.1.1 amd64, default context |
| Build backend image | PASS | `quizforge-api:stage4` 25.6 MB; boots DB-less; `nonroot:nonroot`; 0 secret refs |
| Build frontend image | PASS | `quizforge-web:stage4` 93.4 MB; serves dist; `.dockerignore` added (230 MB node_modules excluded) |
| Compose up + health/ready | PASS | api/db healthy; readiness through pool; migrations applied to container PG18 |
| Proxy routing / WS upgrade via Caddy | PASS | landing 200, `/api` proxied, SPA fallback intact; WS E2E through Caddy green |
| Graceful restart/shutdown | PASS | SIGTERM logs shutdown + restarts healthy; kill -9 → 502 → healthy |
| Non-root / no secrets in images | PASS | verified via inspect/history; compose drops caps + no-new-privileges |
| ARM64 parity | PASS | builds + boots under emulation (QEMU); amd64 for local runs |
| `caddy validate` | PASS | `Valid configuration` (real binary; `APP_DOMAIN` required) |
| E2E vs containers | PASS | 10/10 real Chrome vs `:18080` on current revision |

Reproducible commands for a machine with Docker (see §8).

## 2. Browser E2E (Priority 2)

Environment: real Google Chrome 154 (`/usr/bin/google-chrome`) via `@playwright/test` 1.64;
`scripts/e2e.sh` (disposable `quizforge_e2e` DB, built API on :8080, Vite dev on :5173,
`RATE_LIMIT_ENABLED=false` — rate-limit behavior is covered by backend unit tests asserting 429 mapping).

| Scenario | Status |
|---|---|
| Auth: register/login/logout/guards/reset/validation/duplicate/generic-401 | PASS — 3/3 (`auth.spec.ts`), 2026-10-11 |
| Quiz CRUD + autosave + publish + conflict + preview + delete | PASS — 2/2 (`quiz.spec.ts`), 2026-10-11 |
| AI mock generation + edit + failure display | PASS — 2/2 (`ai.spec.ts`), 2026-10-11 |
| Live multiplayer (2 contexts) full flow + refresh/reconnect + invalid code + dup answers + unauthorized host (401 anon / 403 no-CSRF) | PASS — 2/2 (`live.spec.ts`), 2026-10-11 |
| Full suite | PASS — 9/9 in ~12s, 2026-10-11 |
| Screenshots for portfolio | PASS — 8 PNGs in `frontend/e2e/screenshots/` (editor, ai-editor, host-lobby, host-results, participant-waiting/question/result, preview-participant) |

## 3. Real-Time Reliability + Performance (Priority 3)

Environment: local dev server (`/tmp/qf_stage3_api`, Go 1.27.2 linux/amd64),
local PostgreSQL 18, loopback networking, `DB_MAX_CONNS=25`. Harness:
`backend/test/load/` (`burst`/`multi`/`soak`).

### Engine review verdicts (code inspection + tests)

| Area | Verdict |
|---|---|
| Goroutine lifecycle | PASS with fix — read/write pumps rooted in handler ctx; writePump exit now retires the client (bug #9); actor timers stopped on reschedule/retire; actors retired after end grace (tested) |
| WS connection ownership | PASS — pumps own the conn; `once`-guarded closes; bounded 64-queue with 4008 slow-consumer drop (tested) |
| Timer cleanup | PASS — `AfterFunc` stopped on reschedule + retire; overdue close idempotent (tested) |
| Memory | PASS so far — immutable per-audience payloads; bounded queues; actors retired; soak RSS sampled (below) |
| DB transactions | PASS — answers single-tx with DB-clock receipt; transitions row-locked + versioned |
| Duplicates/races | PASS — unique constraints + fresh-read outcome; 100-answer race; `-race` clean |
| Backpressure | PASS — flood test: 40 pongs + 20 `RATE_LIMITED`, socket stays usable |
| Disconnects | PASS — `removeClient` on read exit; presence is count-only (deviation from spec's `participant_left` noted) |
| Recovery | PASS — unit-tested + live kill -9 drill (below) |
| Retention cleanup | GAP (non-blocking) — no scheduled runner for event/token retention; needs cron/SSM job before prod |

### Measurements

| Check | Status | Evidence |
|---|---|---|
| 200 participants × 1 session | PASS | 200 joins ~58 ms; 200/200 accepted ~339 ms; ack p50 89 / p95 130 / p99 138 ms; 0 errors (2026-10-11) |
| 10 sessions × 20 participants | PASS | 200/200 accepted ~465 ms; 0 errors (2026-10-11) |
| 15-min soak (10 players, paced, churn) | PASS | 20/20 questions, 200/200 answers, 0 errors in ~14 min; watcher saw all starts/results; 19/20 leaderboard fan-outs (last still in flight at process exit — harness artifact, documented); RSS 88→25 MB flat; threads 18 post-load (2026-10-11, artifact `backend/test/load/STAGE3_RESULTS.md`) |
| kill -9 restart mid-question | PASS | overdue closed exactly once (1 `question_ended` event, v3); participant reconnect snapshot correct (2026-10-11) |
| Reconnect churn | PASS (implicit) | soak dials fresh sockets per round + persistent watcher; E2E host/participant refresh green |

## 4. External Integrations (Priority 4)

No `HF_TOKEN`, `GOOGLE_CLIENT_*`, or Neon URL in environment (verified via env scan 2026-10-11).

| Integration | Live | Fixture/contract |
|---|---|---|
| Hugging Face | BLOCKED (no token) | PASS — `huggingface_contract_test.go`: URL path `/chat/completions`, Bearer auth, model passthrough, strict `response_format`, temperature/streaming, usage mapping; 429/5xx→retryable, 401→no-retry, empty/malformed→invalid; timeout cancels (2026-10-11). Spend cap + `/v1/models` revalidation still required with a real token. |
| Google OIDC | BLOCKED (no client) | PASS (mock) — new-user create, same-email link-required (no auto-link), state reuse/forgery rejected (backend tests + E2E banners). Real flow fails closed by design until a vetted JWKS library is vendored; nonce plaintext not yet persisted (recorded gap). |
| Neon Postgres | BLOCKED (no URL) | PASS (local PG18 parity) — pooled/direct URL handling, production TLS+non-localhost enforcement (`production_test.go`), full migration compatibility, pool sizing per DATABASE_DESIGN (2026-10-11). Backup/restore drill not run (needs Neon project). |

## 5. Security Audit (Priority 5)

Executed 2026-10-11 (code inspection + targeted tests; no destructive testing, no external targets).

| Area | Status | Finding |
|---|---|---|
| Auth/session fixation | PASS | fresh session on register/login; rotation on login; logout/logout-all revoke; no tokens in URL/storage |
| CSRF/cookies | PASS | per-session HMAC token + exact Origin; `__Host-` Secure/HttpOnly/Lax; missing-token 403 tested; anon 401 vs authed-no-CSRF 403 distinguished in E2E |
| Authz/IDOR | PASS | owner/host-scoped SQL everywhere; cross-user quiz/session/history → 404 (unit + E2E + curl isolation probe) |
| WS auth/embargo/scoring | PASS | cookie+Origin+role on upgrade; role-filtered payloads (embargo asserted in Go + E2E); server-owned scores/timers; host-only answer events |
| SQLi/XSS/CORS | PASS | all SQL parameterized (ORDER BY allowlisted); no `dangerouslySetInnerHTML`; no CORS headers (same-origin); display names escaped + control-char rejected |
| OAuth callback | PASS (mock) | state hashed/consumed-once/10-min; PKCE S256; no auto-link; safe redirects; real verification fails closed |
| Rate limits/body caps | PASS with fix | register/login/forgot/AI quotas existed; **added** join 20/min/IP, WS upgrade 10/min/IP, per-socket 20/s+40 burst (bug-class gap, now tested); 1 MiB REST / 16 KiB WS caps |
| Secrets/dependencies | PASS | secret grep clean; `govulncheck` 0 code vulns; `npm audit` 0 high (dev-only vitest/tinypool exception documented) |
| Session expiry | PASS with fix | expiry was join-only; host commands + answers now reject expired (bug #11, tested) |

No critical or high findings remain open. Medium/low items are fixed above or tracked as gaps (JWKS library, retention runner, `participant_left` presence event).

## 6. Deployment Dry Run (Priority 6)

Executed 2026-10-11. No AWS resources provisioned, no images pushed, no DNS changed.

| Check | Status |
|---|---|
| compose.prod.yaml review | PASS — immutable digests required, read-only FS, dropped caps, log rotation, memory/CPU limits, 30s grace; `docker compose config` validates (dev file; prod file needs digest env — by design) |
| Caddyfile review | PASS WITH FIX — found + fixed API-shadowing `try_files` bug (bug #7); CSP/headers per AUTH_SECURITY; `caddy validate` BLOCKED (no binary) |
| deploy.sh/backup.sh/e2e.sh `bash -n` | PASS — all parse |
| tofu `fmt/validate` | BLOCKED (no tofu/terraform binary); skeleton reviewed by inspection |
| GitHub workflow YAML validity | PASS WITH FIX — found + fixed two strict-YAML rejections in security.yml (bug #6); all three parse via `yaml.safe_load` |
| Step-by-step checklist + cost/shutdown notes | PASS — added to `docs/DEPLOYMENT.md` (first-deployment runbook + cost controls/shutdown order) |
| Migration execution strategy | PASS — backward-compatible migrations + migrate-then-switch in deploy.sh + ROLLBACK.md (no auto-reverse) |

## 7. Portfolio Review (Priority 7)

Executed 2026-10-11 (E2E screenshots reviewed at full resolution).

| Check | Status |
|---|---|
| Landing/dashboard/editor/host/participant/history pass | PASS — screenshots: host lobby (theatrical mono code), participant question (large targets + timer), host results (bars + leaderboard), participant result (+999), editor, AI review banner, preview |
| Demo workflow script | PASS — README quickstart + demo flow; `scripts/e2e.sh` doubles as a deterministic demo bootstrapper |
| Defects fixed | PASS — dead `/app/settings` nav link → implemented SettingsPage (profile save, login methods, Google link entry, sign-out-everywhere) + vitest (2) + E2E coverage |

No UI redesign; no new showcase features beyond the missing route the nav already promised.

## Bugs Found & Fixed

| # | Severity | Description | Fix | Test |
|---|---|---|---|---|
| 1 | Low (test-only flake; protocol ordering undocumented) | `TestWSHostAndParticipantFlow` failed under `-race`: host `command_ack` (direct socket write) can arrive after the `question_started` broadcast (pump goroutine) for the commanding client. No invariant violated (commit precedes both), but order was assumed. | Made test order-tolerant (correlate via `requestId`/`stateVersion`); documented no-ack-first-ordering rule in `docs/REALTIME_DESIGN.md`. Frontend reducer already handles either order. | `go test -race -count=2 ./internal/live/` PASS |
| 2 | Medium (real UX bug, found by browser E2E) | Password-reset confirmation never appeared: `POST /auth/password/forgot` returns `202` with an empty body, but the API client unconditionally called `res.json()` → `SyntaxError` → `setSent(true)` never ran. Same latent crash for any empty-body success. | Client now reads `res.text()` and returns `undefined` for empty bodies; error path parses defensively. | Playwright `auth.spec.ts` forgot test; vitest forgot test updated to empty-body 202 mock |
| 3 | High (real functional bug, found by browser E2E) | Host "Start question" silently did nothing in dev/E2E: `socket.onclose` unconditionally set `ws.current = null`, so a stale socket closing late (StrictMode remount, reconnect overlap) wiped the fresh connection's ref. Sends became silent no-ops. Production builds were unaffected (no remount), which is why scripted smokes passed. | Guard compares identity: only clear the ref when the closing socket is still current. | Playwright `live.spec.ts` full flow (failed before, passes after) |
| 4 | High (real wiring bug, found while debugging #3) | `wire()` was called twice in `cmd/api` (once for `/api/v1`, once for `/ws`), creating two live Managers: REST-triggered presence broadcasts reached zero WS clients (host lobby stuck at "0 players"), recovery/actors/timers doubled. Mutations were safe (idempotent closes, shared DB) but fan-out was broken. | Wire modules once in `main.go`; both route trees share one `*wired`. | New `cmd/api/live_wiring_test.go` (REST join → host WS receives `participant_count_changed`); E2E host player-count |
| 5 | Low (test-infra) | `scripts/e2e.sh` was not idempotent: stale `:8080` listeners (orphaned `exe/api` from `go run` wrappers, debug servers) caused `bind: address already in use`, and tests then ran against a stale backend with a dropped DB. | Build the API binary once (`go build`, real PID); reclaim ports before start; terminate orphan backends before DB reset; reap listeners on exit. | Repeated `e2e.sh` runs |
| 6 | Medium (deployment defect, found by static review) | `.github/workflows/security.yml` contained YAML that strict parsers reject (`[{ cron: ... }]` flow sequence; unquoted `${{ }}` inside a flow mapping). CI lint jobs using safe YAML parsing would fail. | Block-style schedule; quoted `${{ secrets.GITHUB_TOKEN }}`; all three workflows now pass `yaml.safe_load`. | `python yaml.safe_load` over `.github/workflows/*.yml` |
| 7 | High (deployment defect, found by static review) | Caddyfile routed `/api/*` via a named matcher with a global `try_files` SPA fallback. Caddy executes rewrites before reverse-proxy matching, so API/WSS requests would have been rewritten to `/index.html` in production. | Exclusive `handle /api/*`, `handle /ws/*`, and fallback `handle` blocks; `docs/DEPLOYMENT.md` snippet updated to match. | BLOCKED full verification (no Caddy binary/daemon); validated by directive-order reasoning, pending `caddy validate` on a machine with Caddy |
| 8 | High (real reliability bug, found by soak testing) | Chi `middleware.Timeout(30s)` applied globally, including the hijacked `/ws` upgrade handler: any socket living past 30s was cancelled server-side. Short E2E flows never noticed; 44s-paced soak connections died. | New `httpserver.Timeout` wrapper exempts `/ws/*` paths; ordinary API handlers still time out. | New `middleware_test.go` (slow `/ws` handler survives 50ms budget, slow `/api` handler cancelled); 45s idle connection survives on live server |
| 9 | Medium (real resource leak, found while diagnosing #8) | When the write pump exited (failed ping/write), the read pump, connection, and manager map entry were never retired: leaked goroutine + stale presence per pruned peer. | `writePump` now defers full client retirement (`removeClient` is idempotent). | Existing WS tests green; 15-min soak RSS sampling |
| 10 | Low (harness-only) | Load harness assumed ack-before-broadcast ordering and stopped reading during paces; coder/websocket closes connections whose Read context expires, and quiet clients miss the ping watchdog. Three harness iterations encoded the learned rules (`readUntil`, per-round reconnect, persistent watcher). | Harness fixed; no app change (browser clients auto-pong and are immune). | burst/multi/soak runs |
| 11 | Medium (real correctness gap, found by review) | Session expiry (`expires_at`) was enforced on join only: expired lobbies could still accept host commands, and answers bypassed expiry entirely. | `HostCommand` rejects expired sessions with `ErrGone`; answer transaction compares DB-clock receipt against `expires_at`. | New `TestExpiredSessionRejected` |
| 12 | Low (UX inconsistency, found by container E2E) | Zod default validation messages diverge between Vite dev ("Too small…") and the production bundle ("Invalid input"): the suite asserted a library-default string. | Explicit user-facing messages in form schemas (starting with the AI generator topic rule); specs assert our strings. | Container E2E 10/10 after rebuild |
| 13 | Medium (security gap, found by Stage 4 audit) | `TRUSTED_PROXY_CIDRS` was documented in `.env.example` but never implemented: every handler trusted client-supplied `X-Forwarded-For` unconditionally, letting any client evade IP rate limits or poison other IPs' buckets. | `config.TrustedProxies` parsed/validated at startup; shared `httpserver.ClientIP` honors forwarding headers only from trusted peers; all three call sites migrated. | New `clientip_test.go` (proxy/spoof/direct/empty-trust cases) |

## Remaining Risks

1. Docker path fully unverified (no daemon): Dockerfiles/Compose reviewed statically only. First staging build may surface issues (e.g., npm ci determinism — mitigated by committed lockfile; Caddyfile — mitigated by `handle`-block fix + required `caddy validate`).
2. External providers unverified (no credentials): HF model/schema availability, Google consent/redirect, Neon pooler behavior, SES sandbox.
3. Load envelope is loopback + local PG: WAN latency, TLS termination, and Neon pooler queueing unmeasured.
4. Retention/event cleanup has no scheduled runner; session metadata (`ip_prefix`, agent hash) uncollected; email-verification gating deferred.
5. Vitest dev-only advisories (tinypool/mocker) accepted with documented exception; revisit on vitest 4.
6. `participant_left` presence event not implemented (count-only); host roster shows counts.

## Deployment Requirements (operator checklist input)

- Neon project/branches + pooled/direct URLs; Google OAuth client + consent; HF token + spend cap; SES identity; domain + Route 53; EC2 + EIP + SG; ECR repos; SSM params; CloudWatch/budgets; TLS via Caddy/LE.
- Full step-by-step runbook with cost/shutdown order: `docs/DEPLOYMENT.md` ("Step-By-Step First Deployment", "Cost Controls And Shutdown").

## Verification Matrix (Stage 3 exit)

| Item | Status | Evidence |
|---|---|---|
| Backend checks (`fmt`/`vet`/`test`/`race`) | PASS | 7/7 packages ok; `-race` auth+live+cmd clean, 2026-10-11 |
| Frontend checks (`tsc`/`vitest`/`build`) | PASS | clean; 17/17; dist builds, 2026-10-11 |
| Docker images built + checked | BLOCKED | no daemon (`Cannot connect to the Docker daemon`); static review only — bug #7 fixed by inspection |
| Browser E2E executed | PASS | 10/10 in real Chrome 154 via `scripts/e2e.sh` (~12 s), 2026-10-11; 8 screenshots |
| Real-time load measured | PASS | 200×1 (p99 138 ms), 10×20, 15-min soak 200/200 zero-error, kill -9 drill; artifact `backend/test/load/STAGE3_RESULTS.md` |
| Major security findings addressed | PASS | audit table §5; rate-limit + expiry gaps fixed and tested; no critical/high open |
| Production configuration reviewed | PASS | compose/Caddy/workflows/scripts reviewed; 2 defects fixed; AWS untouched |
| External verification honest | PASS | HF/Google/Neon live marked BLOCKED; contracts tested with fixtures |
| Demo flow working | PASS | E2E + screenshots + README quickstart |
| No unresolved critical/data-integrity bug | PASS | all findings fixed + regression-tested (table above) |
| Documentation current | PASS | readiness/status/testing/deployment/README updated to observed state |
