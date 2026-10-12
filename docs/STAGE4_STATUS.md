# Stage 4 Status — Real Integrations, Docker & Staging Readiness

Date: 2026-10-11. Worktree: `/home/mddanish/quiz`.
Environment deltas vs Stage 3: **Docker daemon available** (server 28.1.1 amd64,
default context); no HF/Google/Neon/AWS credentials in env (verified).
Status values: **PASS / FAIL / BLOCKED / NOT RUN**.

## P0 — Repository Safety

- Git root is `/home/mddanish` (home-dir repo); `quiz/` entirely untracked.
- `.gitignore` covers `.env`, `node_modules/`, `dist/`, logs, reports; Stage 4 added `.terraform/` + `*.tfstate*` (lockfile intentionally tracked).
- Secret scan over code/docs: clean (multiple runs, including Stage 4).
- Recommended boundary (no action taken without approval):
  ```bash
  cd /home/mddanish && git checkout -b quizforge/stage4
  git add quiz/AGENTS.md quiz/PRODUCT.md quiz/README.md quiz/.env.example \
    quiz/.dockerignore quiz/.editorconfig quiz/.gitignore quiz/Makefile \
    quiz/compose.yaml quiz/compose.override.yaml \
    quiz/backend quiz/frontend quiz/database quiz/deploy quiz/docs quiz/scripts
  git status  # review; ensure quiz/.env, node_modules, dist, test-results absent
  git commit -m "QuizForge AI stages 1-4: full-stack implementation + verification"
  ```
  Never `git add -A` from the home root (would sweep unrelated personal files);
  never force-push or rewrite history.

## Task Tracker

| Priority | Task | Status | Evidence |
|---|---|---|---|
| P1 | Google OIDC via vetted library (discovery/JWKS/verify/nonce) | PASS | go-oidc v3.21 (ADR-014); migration `000007_oauth_nonce`; RS256 gate + iss/aud/exp + azp + constant-time nonce; 20 s budgets, fail-closed |
| P1 | OIDC regression suite (bad sig/aud/iss/exp/nonce/state/key/rotation/link) | PASS | `oidc_test.go`: 9 rejection cases + rotation + exchange-failure + unconfigured + URL-shape + full link/takeover flow through real verifier; `go test -race` clean; govulncheck clean |
| P2 | HF provider review vs current docs | PASS | 2026-10-11 docs: OpenAI-compatible `/chat/completions` + Bearer + `response_format{type:json_schema,strict}` confirmed; **fixed** nonstandard `X-HF-Provider` header → documented `model:provider` suffix (contract-tested); free tier still $0.10 credit; default-model revalidation flagged (Qwen3-32B absent from current recommended lists) |
| P2 | Real HF generation | BLOCKED | no `HF_TOKEN` in env (verified); staging setup instructions in AI_INTEGRATION + DEPLOYMENT; mock path re-verified |
| P3 | Neon integration / staging guide | PASS (guide) + BLOCKED (live) | TLS enforcement + migration parity tested locally; staging-branch runbook added to DEPLOYMENT.md; no Neon URL in env — live branch validation blocked |
| P4 | Backend prod image build | PASS | `quizforge-api:stage4`, 25.6 MB, `nonroot:nonroot`, boots DB-less health-only, 0 secret refs in history (2026-10-11) |
| P4 | Frontend prod image build | PASS (after fix) | `quizforge-web:stage4`, 93.4 MB, serves dist; **bug**: Zod default messages diverge dev-vs-prod → explicit form messages (bug #12) |
| P4 | Compose up + health/ready + routing + WS via Caddy | PASS | tmp stack healthy; landing 200, `/api/*` proxied (410 probe), SPA fallback intact — validates `handle`-block fix |
| P4 | `caddy validate` production Caddyfile | PASS | `Valid configuration` from real Caddy binary (requires `APP_DOMAIN` set — unset env yields a misleading global-option error; noted for runbook) |
| P4 | E2E against containerized app | PASS | 10/10 in real Chrome vs `http://localhost:18080` (Caddy + prod builds), 2026-10-11 |
| P4 | Image security (non-root, no secrets, sizes) | PASS | api nonroot + read-only + dropped caps in compose; web inherits caddy default user with compose caps dropped; history grep clean |
| P4 | ARM64 production parity | PASS | `buildx --platform linux/arm64` builds; emulated run boots + listens (QEMU); amd64 used for local verification only |
| P5 | Retention cleanup runner + tests | PASS | `platform/janitor` (bounded, idempotent, history-preserving) + `/quizforge janitor` CLI + systemd-timer runbook in DEPLOYMENT.md; boundary + idempotency + preservation tests green |
| P6 | Presence (`participant_left`) audit/implement | PASS | Contract requires it: implemented grace-period (5 s) host-only `participant_left` with reconnect-cancel + no-duplicate-identity guarantee; frontend explicit case; `TestPresenceLeave` + `TestPresenceReconnectCancelsLeave` green |
| P7 | Argon2 benchmark + runtime config checks | PASS (partial) | Workstation baseline: 62 ms single verify, 8-parallel burst 244 ms (12-thread desktop — NOT EC2 calibration; instance recalibration required, exact command documented). Config: cookies/CSRF/proxy/origin/body/timeout/pool/health verified; trusted-proxy gap fixed (bug #13) |
| P8 | Staging prep verification + checklist | PASS (static) | OpenTofu 1.9.0 installed ad-hoc: `fmt -check` clean, `init -backend=false` + `validate` Success on skeleton (cache removed); no AWS creds → `plan/apply` NOT RUN; runbook + cost/shutdown docs current |
| P9 | Recovery drills + ZAP baseline (local) | PASS (partial) | kill -9 api → 502 → healthy + correct domain responses; DB stop → generic 500 (no leak, no false ack) → pool recovers unaided; backup pg_dump 70 KB/18 tables; SIGTERM logs shutdown + restarts healthy; header/method/body/route baseline clean (CSP present, TRACE/OPTIONS 405, 2 MB→400, unknown→404). ZAP itself BLOCKED (no binary): run `zap-baseline.py -t https://staging-url` once staging exists |
| P10 | Portfolio/license decision | PASS (partial) | Screenshots re-verified; dead Settings link implemented; README evidence refreshed; **license choice explicitly deferred — requires developer approval** (distribution policy unknown; current README marks TBD) |

## Bugs (Stage 4)

| # | Severity | Description | Fix | Test |
|---|---|---|---|---|
| S4-1 | Medium (spec deviation) | Nonstandard `X-HF-Provider` header for provider routing; current HF docs specify the `model:provider` suffix. | Suffix routing (`model:provider`, `auto` = server-side); contract test asserts suffix + no header. | `huggingface_contract_test.go` |
| S4-2 | Medium (deployment defect) | `security.yml` rejected by strict YAML parsers (flow-sequence schedule; unquoted `${{ }}` in flow mapping). | Block-style schedule; quoted expression; all workflows `yaml.safe_load` clean. | parser check |
| S4-3 | High (deployment defect) | Caddy global `try_files` SPA fallback would rewrite `/api/*` + `/ws/*` before proxy matching (Caddy executes rewrites first). | Exclusive `handle /api/*`, `handle /ws/*`, fallback `handle`; DEPLOYMENT.md snippet synced. | `caddy validate`: Valid configuration; live traffic through Caddy (landing 200, `/api` 410-probe, E2E 10/10) |
| S4-4 | Low (UX inconsistency) | Zod default messages diverge dev ("Too small…") vs prod bundle ("Invalid input"). | Explicit form messages; specs assert our strings. | Container E2E 10/10 |
| S4-5 | Medium (security gap) | `TRUSTED_PROXY_CIDRS` documented but unenforced: all handlers trusted spoofable `X-Forwarded-For`. | Parsed/validated at startup; shared `ClientIP` honors forwarding only from trusted peers; 3 call sites migrated. | `clientip_test.go` |
| S4-6 | Low (test hygiene) | `scripts/e2e.sh` predated per-package janitor DB; CI lacked it. | `quizforge_test_janitor` + `TEST_DATABASE_URL_JANITOR` locally and in `ci.yml`. | Full suite green |

## Verification Matrix (final — fill at exit)

| Area | Status | Evidence | Remaining blocker |
|---|---|---|---|
| Google OIDC implementation | PASS | go-oidc v3.21 (ADR-014): discovery/JWKS-cache+rotation/RS256/iss/aud/exp + explicit alg/azp/nonce checks; 9 rejection cases + rotation + link/takeover flow green; `-race` + govulncheck clean | vetted-library requirement closed |
| Real Google login | BLOCKED | No `GOOGLE_CLIENT_ID/SECRET` in env (verified); mock + fake-issuer paths green | Operator must create OAuth client + consent, run staging callback smoke |
| Hugging Face provider | PASS | Matches 2026-10-11 docs (endpoint/auth/payload/schema/usage); suffix-routing fix contract-tested; quotas/retries/circuit unchanged | Default-model revalidation at staging (`/v1/models`) |
| Real Hugging Face generation | BLOCKED | No `HF_TOKEN` in env (verified); mock + fixture paths green | Operator must provision capped token, run one small generation + failure drill |
| Neon integration | BLOCKED (live) / PASS (ready) | No Neon URL in env; TLS enforcement + migration parity tested; staging-branch runbook in DEPLOYMENT.md | Disposable-branch validation per runbook |
| Docker builds | PASS | api 25.6 MB nonroot + web 93.4 MB built from clean context (`.dockerignore` added); 0 secret refs; ARM64 builds + boots under emulation | None (amd64 verified; ARM64 emulated) |
| Docker Compose | PASS | Full stack healthy; kill -9/DB-stop/SIGTERM/backup drills green | None |
| Caddy validation | PASS | `Valid configuration` from real binary; live routing verified (SPA + `/api` + `/ws` E2E) | None |
| Retention cleanup | PASS | Bounded/idempotent janitor + CLI + systemd runbook; boundary/idempotency/preservation tests green; live `janitor` run clean | Schedule the timer on the host |
| Presence/reconnection | PASS | Grace-period host-only `participant_left` + reconnect-cancel + no-duplicate tests green; frontend case added | None |
| Argon2 calibration | PASS (baseline) | Workstation 62 ms single / 244 ms burst-8; `BenchmarkArgon2id` + instance runbook in DEPLOYMENT.md | t4g.small measurement on the instance |
| Staging configuration | PASS (static) | tofu `fmt` + `validate` Success; workflows parse; scripts `bash -n`; runbook + costs current | `plan/apply` needs AWS creds + approval |
| Security checks | PASS | Audit table closed; govulncheck 0; npm 0 high; header/method/body/route baseline clean; isolation probed | ZAP vs staging when it exists |
| Browser E2E | PASS | 10/10 real Chrome vs dev backend AND vs containerized prod builds | None |
