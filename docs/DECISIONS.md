## ADR-016: Render Single-Service Production (AWS Retirement)

**Status:** Accepted (2026-10-14)
**Decision:** Production is one Render web service running a single Docker image in which the Go binary serves the API **and** the Vite SPA on one origin, plus managed Neon Postgres. All AWS artifacts are removed: OpenTofu IaC, EC2/ECR/SSM/CloudWatch/S3/SES, the Caddy reverse proxy, the split api/web images, and the SSM deploy driver. Releases are `v*` tags; the `deploy` workflow runs migrations (pinned `migrate/migrate` image, direct Neon URL) **before** POSTing the Render deploy hook (`autoDeploy: false`), then smoke-checks `/health/ready` + `/version`. Password-reset mail moves from the never-implemented SES plan to a generic stdlib SMTP relay (`MAIL_PROVIDER=smtp`, `SMTP_*` env). Security headers (CSP/HSTS) moved from the Caddyfile into the Go `SecurityHeaders` middleware.
**Why:** One origin preserves the simplest cookie/CSRF posture (no CORS, `SameSite=Lax`, exact-Origin checks unchanged); one image + one service removes the EC2 host, ECR, SSM secret plumbing, and Caddy as operational surface while keeping native WebSockets and in-process live actors untouched.
**Rejected:** Render backend + Vercel frontend split (adds CORS, cross-site cookies, two deploy targets for no product benefit); keeping EC2 (host patching, IAM/SSM plumbing, ~$22–28/mo base for a portfolio workload).
**Consequence:** Render **free tier** is the default (no card; sleeps after
~15 min idle, wakes in ~60 s — fine for demo-driven live quizzes; `starter`
only if no-sleep is ever needed); DB migrations are CI-driven rather than
host-driven; historical Stage 4/5 docs still describe the AWS era and are
kept as records only.

## ADR-015: Groq Free Tier For Staging AI Generation

**Status:** Accepted (Stage 5, 2026-10-14)
**Decision:** Staging sets `AI_PROVIDER=groq` (Groq free tier, no card, OpenAI-compatible `https://api.groq.com/openai/v1`, model `openai/gpt-oss-120b` with strict `json_schema`). Hugging Face stays the config default and primary provider (HF-first architecture unchanged); staging overrides via environment only.
**Why:** Live probe returned HF HTTP 402 "no remaining credits" on both `auto` and `:deepinfra` routes — the account's Inference Provider credits are exhausted and the operator requires zero-cost staging. Groq's free tier documents strict structured outputs on `gpt-oss` models and needs only an API key; the existing OpenAI-shaped adapter was shared (`doChatCompletion`) so the change is one small adapter + wiring.
**Rejected:** Funding HF (paid, against operator constraint); silent mock (would fake the "real AI" smoke test); local tiny model on t4g.small (quality/cost of engineering worse than a hosted free tier).
**Consequence:** New SSM secret `GROQ_API_KEY`; staging generation quality now depends on `openai/gpt-oss-120b` + Groq free-tier limits (30 RPM, daily caps — fine for smoke, not load); production still targets HF-funded routing. If Groq 400s on schema keywords (`pattern`/`const`), relax the Groq-side schema (server validation remains the gate).

## ADR-014: Vetted OIDC Library For Google Verification

**Status:** Accepted (Stage 4)
**Decision:** Verify Google ID tokens with `github.com/coreos/go-oidc/v3` (OIDC discovery, JWKS caching/rotation, RS256 signature verification) instead of hand-rolled JWT cryptography. Nonce plaintext is persisted short-lived (10-minute transaction row) and compared by hash; `azp` and header `alg` are additionally restricted.
**Why:** Hand-rolled RSA/JWKS verification is a classic vulnerability source; go-oidc is the maintained standard and keeps the fail-closed posture without custom crypto.
**Rejected:** Manual JWKS/RSA verification (too easy to get wrong); skipping nonce persistence (would leave replay/binding unbound in the real flow).
**Consequence:** New dependency (audited via govulncheck/Dependabot); one small migration for nonce storage; mock verifier retained for tests/local.

## ADR-001: Modular Monolith

**Status:** Accepted  
**Decision:** One Go service contains auth, quiz, AI orchestration, live sessions, and reporting behind internal module boundaries.  
**Why:** A solo developer gets atomic database transactions, one deployable, one observability surface, and understandable failure modes.  
**Rejected:** Microservices add network contracts, distributed transactions, deployment overhead, and idle cost without current scale evidence.  
**Consequence:** Modules may communicate through explicit service interfaces, not cyclic package imports. Extraction requires measured contention or independent scaling need.

## ADR-002: Chi Over Gin Or Bare `net/http`

**Status:** Accepted  
**Decision:** Use Chi for routing/middleware and standard `net/http` request/response types.  
**Why:** Chi is small, idiomatic, composable, and does not impose binding/validation conventions. Bare `ServeMux` is viable but would require recreating route grouping and middleware ergonomics; Gin is productive but brings its own context and a broader framework surface.  
**Consequence:** JSON binding, validation, errors, and middleware remain project-owned and explicit.

## ADR-003: `pgx` + `sqlc`

**Status:** Accepted  
**Decision:** `pgx/v5` provides pooling/transactions; `sqlc` generates typed query code from reviewed SQL.  
**Why:** It preserves PostgreSQL features, visible SQL and compile-time result types. Plain `pgx` alone creates repetitive scanning code; GORM hides query behavior and constraints that are central interview material.  
**Consequence:** Complex transactional services may use `pgx.Tx` around generated queries. Generated files are not manually edited. Runtime uses Neon's pooled URL; migrations use a direct URL.

## ADR-004: Opaque Server-Side Sessions

**Status:** Accepted  
**Decision:** Use random opaque tokens in Secure HttpOnly cookies; store only SHA-256 token hashes and session metadata in PostgreSQL.  
**Why:** Immediate revocation, logout-all, rotation, and OAuth linking are simpler than JWT revocation. One database lookup/request is acceptable and cacheable later. JWT access/refresh complexity has no benefit for this same-origin monolith.  
**Consequence:** Same-origin deployment is required for the simplest cookie/CSRF posture. Session cleanup is scheduled.

## ADR-005: PostgreSQL-Durable Live State With In-Process Actors

**Status:** Accepted  
**Decision:** Per-session goroutines serialize controls and broadcasts; every transition, deadline, answer, and score is durable in PostgreSQL before acknowledgement.  
**Why:** This demonstrates Go concurrency while surviving host refresh and process restart without mandatory Redis.  
**Consequence:** One backend instance is the MVP topology. Answer uniqueness and version checks remain database-enforced because actors alone are not a consistency boundary.

## ADR-006: Immutable Session Snapshots

**Status:** Accepted  
**Decision:** Starting a live session copies published questions/options/settings into session tables.  
**Why:** Later edits must not change a running or historical competition, and participant-facing queries can avoid the editable author model.  
**Consequence:** Snapshot storage is duplicated but small. Source IDs are retained for traceability and may be null after source deletion.

## ADR-007: Hugging Face Provider With Strict Validation

**Status:** Accepted  
**Decision:** Use Hugging Face's OpenAI-compatible chat endpoint through a Go provider interface, initially `Qwen/Qwen3-32B` if still offered by a provider with JSON Schema support. Model/provider are configuration, not code constants.  
**Why:** Hugging Face's official structured-output guide uses this model, and routing permits provider changes. As of the research date, free users receive only $0.10/month of credits, explicitly subject to change, so it is a development trial rather than a production budget. See [HF pricing](https://huggingface.co/docs/inference-providers/pricing) and [structured outputs](https://huggingface.co/docs/inference-providers/guides/structured-output).  
**Consequence:** Deployment must verify `/v1/models`, schema support, license, regional availability, price, and a fixed spend cap. On failure, manual authoring remains available; no fake AI fallback.

## ADR-008: EC2 Over Lambda For MVP

**Status:** Accepted  
**Decision:** Run Caddy and the Go API on one `t4g.small` EC2 instance in `ap-southeast-1`, colocated with Neon Singapore.  
**Why:** Native long-lived WebSockets, in-process actors, one container artifact, predictable cost, and straightforward restart recovery outweigh serverless idle savings. API Gateway WebSockets require connection tables, route Lambdas, callback API calls, and distributed timers/state.  
**Consequence:** Accept single-instance availability and OS/container patching. Use SSM, automated deployment, backups, monitoring, and a documented vertical/multi-instance path.

## ADR-009: Caddy Serves The SPA And Proxies API/WSS

**Status:** Accepted  
**Decision:** Use Caddy for automatic TLS, static compressed assets, security headers, SPA fallback, and reverse proxy.  
**Why:** It avoids an ALB and a separate frontend hosting pipeline at initial scale.  
**Consequence:** Frontend releases are part of the deployment bundle. CloudFront/S3 can be introduced later if asset traffic or global latency warrants it.

## ADR-010: REST For Resources, WebSocket For Live Commands/Events

**Status:** Accepted  
**Decision:** CRUD, auth, joins, history, and authoritative snapshot retrieval are REST. After joining, time-sensitive host commands and participant answers use one same-origin WebSocket protocol.  
**Why:** REST remains observable and cache-friendly; WebSockets efficiently broadcast bidirectional session changes. SSE cannot carry client commands without a second channel.  
**Consequence:** Every WebSocket command has request ID, expected state/version as applicable, typed ack/error, and an equivalent recovery snapshot.

## ADR-011: UUIDv7 Internal IDs And Separate Public Codes

**Status:** Accepted  
**Decision:** Use database-generated UUIDv7 where PostgreSQL version/extension supports it, otherwise application-generated UUIDv7; use random six-character join codes for discovery.  
**Why:** UUIDv7 indexes better than random v4 and does not expose row counts. Join codes are short-lived handles, never authorization credentials.  
**Consequence:** All join and participant actions still require scoped tokens/rate limits.

## ADR-012: Contract-First DTOs With OpenAPI Checked In

**Status:** Accepted  
**Decision:** Maintain `docs/openapi.yaml` as the reviewed public REST contract and generate TypeScript API types during CI. Go handlers use explicit DTOs and contract tests; database structs never escape.  
**Why:** This keeps frontend/backend/error behavior synchronized without forcing code-generation architecture throughout Go.  
**Consequence:** Any API change updates OpenAPI, examples, generated client types, and contract tests in the same change.

## ADR-013: Visual Direction Is A Live Studio Console

**Status:** Accepted for specification  
**Decision:** Authoring surfaces use a quiet, precise control-room language; participant and host presentation surfaces increase contrast, scale, and motion around the live state. Branding uses graphite, white, electric mint, signal coral, and gold accents rather than Mentimeter's visual identity.  
**Why:** The same product serves concentrated editing and shared performance. A two-tempo system makes the mode change legible without becoming two brands.  
**Consequence:** The implementation must use shared tokens, accessible status semantics, and purposeful transitions. It must not recreate Mentimeter layouts or substitute generic gradient-heavy SaaS cards.
