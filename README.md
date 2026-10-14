# QuizForge AI

AI-built quizzes, played live. Creators register, generate or author a quiz, publish it, host a synchronized session, and guests join from their phones with a six-character code. Scores and leaderboards update in real time; results persist for review.

> **What is verified (2026-10-11, local + containers):** full user journey against real PostgreSQL — register → login → AI-mock generate → edit → publish → host → join → answer → results → history. `go test ./...` green incl. `-race`; frontend `tsc`/`vitest` (17 tests)/`vite build` green; Playwright E2E **10/10 in real Chrome** vs dev backend **and** vs the containerized single-image build; Docker image builds (nonroot, serves API + SPA on one origin); migration up/down/up green; 200-participant load (p99 138 ms), 10×20, 15-min soak, kill-9 drill. **Not yet verified:** real Hugging Face, Google, Neon, Render deployment (no credentials here).

## Architecture

- **Frontend:** React 19 + TypeScript + Vite + Tailwind v4 + React Router + TanStack Query + RHF/Zod. Same-origin API client; auth from `/me`, never localStorage tokens.
- **Backend:** Go modular monolith (Chi, pgx, `coder/websocket`). Handlers thin; services own transactions; PostgreSQL is the durable authority; in-process session actors only accelerate routing/timers.
- **Data:** PostgreSQL (Neon in production, local Postgres for dev/test). Numbered up/down migrations.
- **AI:** provider interface; Hugging Face OpenAI-compatible adapter first, strict schema + domain validation, bounded retries, quotas, usage records; deterministic mock for tests/demos (always labeled `mock`).
- **Auth:** opaque DB-backed sessions in `__Host-` Secure HttpOnly cookies; Argon2id; per-session CSRF + Origin checks; Google OIDC authorization-code + PKCE with explicit link policy (real verifier fails closed until a vetted JWKS library is vendored).
- **Prod:** one Render web service (single Docker image: Go API + Vite SPA on one origin, TLS at the edge) + managed Neon Postgres; deploys via the `deploy` workflow (migrate → deploy hook → smoke).

```mermaid
flowchart LR
  Browser[React browser client] -->|HTTPS REST + WSS, single origin| Render[Render web service: Go + static UI]
  Render --> Neon[(PostgreSQL)]
  Render --> HF[Hugging Face inference]
  Render --> Google[Google OIDC]
  Render --> SMTP[SMTP relay]
```

## Quickstart (local)

```bash
cp .env.example .env
# provision local postgres role/dbs (dev + 4 test DBs):
sudo -u postgres psql -c "CREATE ROLE quizforge LOGIN PASSWORD 'quizforge' SUPERUSER;" \
  -c "CREATE DATABASE quizforge_dev OWNER quizforge;" \
  -c "CREATE DATABASE quizforge_test OWNER quizforge;" \
  -c "CREATE DATABASE quizforge_test_quiz OWNER quizforge;" \
  -c "CREATE DATABASE quizforge_test_ai OWNER quizforge;" \
  -c "CREATE DATABASE quizforge_test_live OWNER quizforge;"
export DATABASE_URL="postgres://quizforge:quizforge@localhost:5432/quizforge_dev?sslmode=disable"
for f in database/migrations/*.up.sql; do psql "$DATABASE_URL" -q -f "$f"; done
cd backend && DATABASE_URL="$DATABASE_URL" HTTP_ADDR=127.0.0.1:8080 APP_ENV=test CSRF_SECRET=test-csrf-secret-0123456789abcdef MOCK_AI=true MOCK_OIDC=true go run ./cmd/api
cd frontend && npm install && npm run dev
```

Demo flow: register at `http://localhost:5173/register` → **Generate with AI** (mock) or **New quiz** → edit → **Publish** → **Start live** → open `/join`, enter the code on a second browser/phone → answer → host ends the question → results + leaderboard → `/app/sessions` for history.

## Commands

```bash
make build | make test | make lint | make e2e
make migrate-up            # needs MIGRATIONS_DATABASE_URL + `migrate` tool
docker compose up --build  # needs a Docker daemon + .env
```

Backend tests need the four test databases (see `TEST_DATABASE_URL*` in `.env.example`); they run real PostgreSQL, never SQLite. Frontend: `npm run typecheck`, `npm run test -- --run`, `npm run build`.

## Measured results (local, 2026-10-11; artifact: `backend/test/load/STAGE3_RESULTS.md`)

| Check | Result |
|---|---|
| Backend unit + integration + WS protocol | all `ok`, incl. `-race` on auth/live |
| Playwright E2E (real Chrome 154) | 10/10 specs pass in ~12 s (auth, quiz, AI mock, live multi-context, settings), vs dev backend and vs containerized prod builds |
| 4-way concurrent quiz update | exactly one winner, three `VERSION_CONFLICT` |
| 100 concurrent answers (test) | 100 rows, one score each, aggregates match |
| 200-participant single session (load harness) | 200 joins in ~58 ms; 200/200 accepted in ~339 ms; ack p50 89 ms / p95 130 ms / p99 138 ms; 0 errors |
| 10 sessions × 20 participants | 200/200 accepted in ~465 ms; 0 errors |
| 15-minute soak (10 players, paced rounds, reconnect churn) | in progress at time of writing — see artifact |
| kill -9 restart mid-question | overdue question closed exactly once; reconnect snapshot correct |
| Answer embargo | participant payloads/snapshots contain no `isCorrect` while open (asserted in Go + E2E) |
| Migrations | blank DB up → down (0 tables) → up (18 tables) |
| `govulncheck` | 0 vulnerabilities in code |
| `npm audit` high | 0 high after react-router 7.18.4 upgrade; 2 critical + 1 moderate remain, all dev-only (vitest/tinypool test runner, never shipped) |

## Security decisions

Opaque sessions (revocable, logout-all) over JWTs; Argon2id 64MiB×3×p2 hashed outside transactions; constant-shape login with dummy-hash path; `__Host-` Secure cookies + per-session CSRF + exact Origin; owner-scoped SQL everywhere (cross-owner reads 404); server-owned timers/scores (client timestamps never trusted); answer keys embargoed by role until durable close; AI output schema+domain validated before any write; metadata-only logs (redaction test); CSP + hardened headers in Caddy; quotas + rate limits with DB constraints as the final guard.

## Known limitations

- Real Hugging Face/Google/Neon/Render paths are implemented but **credential-required** (mock providers + fixture contracts used locally; Google verification now uses go-oidc with fake-issuer tests; production must still run the live smoke).
- The single Docker image serves API + SPA; `docker build -f backend/Dockerfile .` + `docker compose up --build` reproduces production locally (first Render deploy remains to be executed).
- No second AI provider fallback; no CSV export; quiz duplication deferred; `sqlc` codegen not yet wired (handwritten parameterized pgx with reviewed SQL; config checked in).
- Session `ip_prefix`/`user_agent_hash` recorded as NULL; email-verification gating deferred; janitor timer must still be scheduled on the host.

## Docs

`PRODUCT.md` · `docs/` (requirements, architecture, database, API + `openapi.yaml`, realtime, AI, frontend, auth/security, deployment, testing, plan, decisions, **status with evidence**).

## Interview talking points

Commits precede broadcasts; immutable session snapshots; uniqueness + `expectedVersion` + row locks stop double scores and stale hosts; timers are persisted deadlines with idempotent closes; opaque sessions beat JWTs here (revocation, OAuth linking); single Render service over separate frontend/backend hosts (one origin: no CORS, Lax cookies, one deployable); mock/real provider honesty (never silent fallback).

## License

TBD — choose a license before public release. Independent implementation; no Mentimeter assets.
