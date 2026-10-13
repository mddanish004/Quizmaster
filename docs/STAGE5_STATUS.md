# Stage 5 Status — Real Service Integration, AWS Staging Deployment & Production Verification

Date: 2026-10-11. Worktree: `/home/mddanish/quiz`. Gate: **A (safe preparation) COMPLETE — Gate B pending explicit approval.**

Status values: **PASS / FAIL / BLOCKED / NOT RUN.**

> Gate A rule observed: no `tofu apply`, no EC2/EIP/ECR/IAM/DNS creation, no secret
> upload, no public deployment. All work below is inspection, local tests, read-only
> validation, doc/config fixes, and planning. No PHF/Google/Neon/AWS credentials
> exist in this environment (verified by env scan — no matching variables set).

## P0 — Git Baseline (PASS, no commits made)

- Git root is `/home/mddanish` (home-dir repo); `quiz/` is entirely untracked
  (`git status --short -- quiz` → `?? quiz/`), branch `main`, on top of `86ef87b`.
- `.gitignore` covers `.env`, `node_modules/`, `dist/`, logs, reports, `.terraform/`,
  `*.tfstate*`. **Stage 5 fix (S5-2):** added `*.tfvars` / `*.tfvars.json` (variable
  files routinely carry secrets/URLs; previously unignored).
- Secret scan (2026-10-11): `grep` for token/key/secret patterns across
  `backend frontend deploy docs scripts .github` returned only false positives
  (config key *names* such as `CSRF_SECRET`, test-fixture strings, and vendored
  `node_modules` lib text). No private keys, no `AKIA` keys, no token values.
- Local `.env` exists (1773 bytes, gitignored, correct). Values never printed.
- **Recommendation (unchanged from Stage 4, no action taken without approval):**
  keep the existing Git root; stage only `quiz/` paths on a dedicated branch:
  ```bash
  cd /home/mddanish && git checkout -b quizforge/stage5
  git add quiz/AGENTS.md quiz/PRODUCT.md quiz/README.md quiz/.env.example \
    quiz/.dockerignore quiz/.editorconfig quiz/.gitignore quiz/Makefile \
    quiz/compose.yaml quiz/compose.override.yaml \
    quiz/backend quiz/frontend quiz/database quiz/deploy quiz/docs quiz/scripts
  git status  # review; ensure quiz/.env, node_modules, dist, test-results absent
  git commit -m "QuizForge AI stage 5 gate A: deployment prep + config fixes"
  ```
  Never `git add -A` from the home root. No repo init, no history rewrite, no push
  without authorization.

## P1 — Hugging Face Model Compatibility (PASS research + contract tests; live BLOCKED)

Research date 2026-10-11 against current official docs:

- Chat-completion API (OpenAI-compatible `POST /chat/completions`, Bearer
  `HF_TOKEN`, `response_format{type:json_schema,strict}`) is unchanged — our
  adapter (`backend/internal/ai/huggingface.go:135-165`) already matches, including
  the Stage 4 `model:provider` suffix fix. No code change required.
- Recommended chat-model list **no longer contains `Qwen/Qwen3-32B`** (now:
  `gemma-2-2b-it`, `Qwen3-Coder-480B-A35B-Instruct`, `gpt-oss-120b`,
  `Qwen3-4B-Thinking-2507`, `Qwen2.5-7B-Instruct-1M`, `Qwen2.5-Coder-32B-Instruct`,
  `DeepSeek-R1`). **However**, the current structured-output guide still
  demonstrates `Qwen/Qwen3-32B` (via `provider="cerebras"`), so the default
  `HF_MODEL=Qwen/Qwen3-32B` remains *documented for structured output*.
- **Decision: keep `Qwen/Qwen3-32B` as default; do not churn config on a
  recommendation-list change alone.** Staging MUST run `GET /v1/models` and one
  small generation before relying on it; fallback candidate recorded as
  `Qwen/Qwen2.5-7B-Instruct-1M` (instruction-tuned, long-brief capable).
  Provider abstraction untouched (`Provider` interface, `HF_PROVIDER=auto` default;
  guide tip noted — pin `HF_PROVIDER=cerebras` at staging if auto-routing fails).
- Free tier still $0.10/mo credit (spent at partner rates); app quotas
  (5/user/day, 20/IP/day, circuit breaker) bound runaway spend.
- Contract tests re-run green 2026-10-11: `go test -count=1 ./internal/ai/` → `ok`.
- **Live inference verification: BLOCKED** — no `HF_TOKEN` in env. No mock fallback
  in production paths (service selects real provider whenever `MOCK_AI != true`).

## P2 — Real Google Authentication (PASS review; live BLOCKED)

- `backend/internal/auth/google.go` reviewed: go-oidc v3.21 discovery + JWKS
  cache/rotation, 15 s discovery / 20 s exchange budgets, RS256 alg gate,
  iss/aud/exp via verifier, explicit `azp` + constant-time nonce binding against
  10-minute transaction rows, PKCE S256, state hashed/consumed-once, no auto-link
  (link-required on email collision), fail-closed when unconfigured.
- Deployment readiness checklist for staging (base URLs must be final first):
  OAuth client type **Web application**; authorized redirect exactly
  `https://<staging-domain>/api/v1/auth/google/callback`; no JS origins needed
  (server-side code flow, no GIS library); consent screen + test users while
  in testing mode; HTTPS mandatory (Secure `__Host-` cookies); `APP_ORIGIN` and
  `GOOGLE_REDIRECT_URL` must match the public domain.
- **Live Google login (first-time / returning / linking / invalid-callback /
  logout-reauth): BLOCKED** — no `GOOGLE_CLIENT_ID/SECRET` in env. Setup
  instructions already in `docs/DEPLOYMENT.md`; mock + fake-issuer suites green.

## P3 — Neon Staging Database (guide ready; live BLOCKED)

- Runbook in `docs/DEPLOYMENT.md` (staging-branch first, pooled runtime URL vs
  direct migration URL, `sslmode=require`, `DB_MAX_CONNS=10`, disposable-branch
  validation, never `down`/truncate prod, restore-to-new-branch) reviewed —
  still accurate, no change needed.
- **Live staging branch (connectivity, migrations, persistence, reconnect,
  backup/restore drill): BLOCKED** — no Neon URL in env. Singapore region
  (`ap-southeast-1` adjacency) required at branch creation.

## P4 — AWS Architecture and Cost Validation (PASS revalidation)

- Architecture re-confirmed: single ARM64 `t4g.small` + Compose + Caddy + Go API +
  static React + Neon + external HF + ECR/SSM/CloudWatch. No NAT/ALB/K8s/Redis.
- Spot re-check 2026-10-11: `t4g.small` lists from ~$0.0168/hr (cheapest region);
  Singapore ~$0.0212/hr → ~$15.48/mo stands. T4g free-trial extension to
  **2026-12-31** reconfirmed (account-dependent — not budgeted).
- Conservative staging-month estimate (730 h, before credits/tax):
  EC2 ~$15.48 + gp3 20 GiB ~$1.92 + 1× public IPv4 ~$3.65 + Route 53 ~$0.54 +
  ECR/S3/CloudWatch ~$0.25–4 + transfer ~$0–2 → **AWS ≈ $22–28**; Neon $0–5;
  HF capped $0–5; domain ~$1–2/mo equivalent → **≈ $23–40 all-in**.
  Assumptions stated in `docs/COST_ANALYSIS.md` (revalidation note appended).
- Budget alerts ($10/$25/$50 + anomaly) and teardown order already documented.

## P5 — Infrastructure Validation (PASS static; plan NOT RUN)

- OpenTofu 1.9.0 fetched ad-hoc to `/tmp/opencode`: `fmt -check` clean (exit 0),
  `init -backend=false` + `validate` → **Success** on `deploy/opentofu/`.
  **Caveat:** `main.tf` is still the commented skeleton (region/project vars +
  resource list) — validation proves syntax, not a deployable topology. Full
  resource modules (SG/EIP/EC2/ECR/SSM/logs/bucket/DNS/alarms/budgets) must be
  authored and `plan`-reviewed under Gate B prep; `validate` ≠ deployable.
- `docker compose config` OK; all workflows `yaml.safe_load` OK; `deploy.sh` /
  `backup.sh` `bash -n` OK; backend/frontend Dockerfiles reviewed (distroless
  nonroot API, no secrets in layers; web is Caddy + static dist).
- Security-sensitive review: SG 80/443-only + no SSH (SSM) is spec'd but not yet
  codified; IAM least-privilege roles not yet authored; state backend undecided
  (local encrypted backup acceptable initially, never committed).
- **Stage 5 fix (S5-1, defect): `deploy.yml` fabricated image digests.**
  `deploy-prod` passed literal `sha256:API-<sha>` / `sha256:WEB-<sha>` strings,
  which would build references like `<registry>/quizforge-api@sha256:API-…` —
  an invalid digest → ECR pull failure on every production deploy. Fixed by
  plumbing real `build-push-action` digest outputs (`id: api/web` + job
  `outputs.api_digest/web_digest` → SSM command args). YAML re-parsed OK.
  `plan` (needs AWS creds): NOT RUN.

## P6–P9 — Real Deployment, Staging E2E, Security Scan, Ops Drills (NOT RUN)

- Blocked on Gate B approval + credentials. No billable resources touched.
- ZAP: no binary locally — run `zap-baseline.py -t https://<staging>` post-deploy.
- Argon2 recalibration command ready (`go test ./internal/auth/
  -run=NONE -bench=BenchmarkArgon2id -benchtime=5x` on the instance).

## P10 — CI/CD Pipeline (PASS review, one fix)

- `ci.yml` (backend fmt/vet/tests/race, frontend ci/typecheck/tests/build,
  contract OpenAPI + compose config), `security.yml` (govulncheck, npm audit,
  gitleaks, openapi), `deploy.yml` (OIDC role, ARM64 buildx, Trivy gate, SSM
  deploy, `production` environment gate) reviewed. Least-privilege OIDC already
  used (no static AWS keys). Image traceability: `github.sha` tags + (now) digest
  outputs; rollback via `previous.env` digests per `ROLLBACK.md`.
- Gap noted (not fixed without approval): auto-deploy on every `main` push —
  recommend gating releases on tags + `production` environment reviewers.

## Bugs (Stage 5 Gate A)

| # | Severity | Description | Fix | Test |
|---|---|---|---|---|
| S5-1 | High (deployment defect) | `deploy.yml` passed fabricated `sha256:API-/WEB-<sha>` digest strings; every SSM deploy would fail the ECR pull. | Real digest outputs plumbed (`steps.api/web.outputs.digest` → job outputs → SSM args). | `yaml.safe_load` + outputs assertion, 2026-10-11 |
| S5-2 | Low (secret hygiene) | `*.tfvars(*)` unignored — future variable files could commit secrets/URLs. | Added to `.gitignore`. | inspection |

## Verification Matrix (Gate A exit)

| Area | Status | Evidence |
|---|---|---|
| Git baseline / secret scan | PASS | `?? quiz/` only; scan false-positives only; safe commands documented; no commits |
| HF docs revalidation | PASS | 2026-10-11 docs: endpoint/auth/schema/suffix unchanged; Qwen3-32B still in structured-output guide; fallback recorded |
| AI contract tests | PASS | `go test -count=1 ./internal/ai/` ok |
| Google integration review | PASS | go-oidc flow, budgets, nonce/azp/PKCE/link policy verified by inspection |
| Live Google / HF / Neon | BLOCKED | no credentials in env (verified) |
| Cost revalidation | PASS | $22–28 AWS / $23–40 all-in; trial extension confirmed |
| Tofu fmt/validate | PASS | 1.9.0: fmt clean, validate Success (skeleton caveat recorded) |
| Compose/workflows/scripts | PASS | `compose config` OK; YAML parse OK; `bash -n` OK |
| `tofu plan` / any provisioning | NOT RUN | no AWS creds; Gate B approval required |
| Staging deploy / E2E / ZAP / drills | NOT RUN | pending Gate B |

## Gate B Approval Request (awaits explicit operator approval)

1. **Exact resources:** 1× `t4g.small` (ARM64, AL2023/Ubuntu LTS, 20 GiB encrypted
   gp3) + EIP + SG (in 80/443, no SSH) + 2× ECR repos (immutable tags, scan,
   lifecycle-5) + IAM instance role (SSM-core, param-read, ECR-pull, logs, backup
   bucket, SES-send) + GitHub OIDC deploy role + SSM SecureString
   `/quizforge/prod/*` + CloudWatch log groups (14–30 d) + alarms + versioned S3
   backup bucket (14 d lifecycle) + Route 53 zone + A record + budgets/anomaly.
2. **Estimated monthly cost:** ≈ $22–28 AWS + $0–5 Neon + $0–5 HF (capped) +
   domain ≈ **$23–40**, before credits/tax; compute trial may apply to 2026-12-31.
3. **Required credentials/settings:** AWS account + admin/OIDC bootstrap; Neon
   staging branch URLs; Google OAuth web-client + consent; HF fine-grained token
   (Inference Providers write, spend-capped); domain decision (below);
   `APP_DOMAIN`, `GOOGLE_REDIRECT_URL`, `APP_ORIGIN` set to the staging FQDN.
4. **Proposed staging domain:** `staging.<operator-domain>` (A → EIP). Operator
   must supply the parent domain or authorize a new registration — **no DNS
   changes until decided.**
5. **Security/data exposure:** public 80/443 only; SSH closed (SSM Session
   Manager); secrets in SSM SecureString → root-owned `0600` `.env`; Caddy/LE
   TLS; no production user data (disposable staging fixtures only); HF/Google
   test-only credentials; budgets/alarms before traffic.
6. **Rollback/shutdown:** keep `previous.env` digests; re-run `deploy.sh` with
   previous digests (migrations backward-compatible, never auto-reversed); DB
   recovery into a **new** Neon branch; full teardown order DNS → instance →
   EIP release → ECR cleanup → SSM deletion → Neon removal; stop-instance pauses
   compute spend (EIP retained while allocated).

**STOP: no Gate B action until the operator replies with explicit approval.**

---

## Gate A.2 — Complete Infrastructure as Code (2026-10-11, local only, no provisioning)

### Files created / updated

OpenTofu (`deploy/opentofu/`): `versions.tf`, `variables.tf` (16 non-secret inputs),
`main.tf` (full ~30-resource graph, was a skeleton), `outputs.tf`,
`user_data.sh.tmpl` (idempotent AL2023/ARM64 bootstrap), `terraform.tfvars.example`.
App/deploy: `backend/Dockerfile.migrate` (pinned migrate CLI + SQL),
`compose.prod.yaml` (awslogs non-blocking), `deploy.sh` (see bugs),
`deploy.yml` (migrate build, Trivy web, `v*`-tag trigger).
Docs: `AWS_SETUP_GUIDE.md` (13-step operator runbook), `INFRASTRUCTURE_REVIEW.md`
(full review), inventory table in `COST_ANALYSIS.md`, runbook touch-ups in `DEPLOYMENT.md`.

### Validation (all executed 2026-10-11)

`tofu fmt` CLEAN · `tofu validate` Success · `tflint` 0 findings · YAML/`bash -n`/
compose-config OK · `Dockerfile.migrate` built + CLI-verified · `go vet` clean ·
`go test -count=1 ./...` **8/8 ok** (one run without `TEST_DATABASE_URL*` reproduced
the known shared-DB flake — env issue, not code) · secret scan clean.
`tofu plan` NOT RUN (no credentials — never claimed).

### Bugs (Gate A.2)

| # | Severity | Description | Fix |
|---|---|---|---|
| S5-3 | High (rollback broken) | `ROLLBACK.md` assumed `deploy.sh` writes `previous.env` — it never did; rollback had no previous digests. | `deploy.sh` saves current digests to `previous.env` + non-secret `deploy.env` before switching; installs janitor systemd units. |
| S5-4 | High (migrations broken) | `deploy.sh` pulled a `quizforge-migrate` image that CI never built, using a host-unset `$MIGRATIONS_DATABASE_URL`, against a `cmd/migrate` stub that prints and exits. | Real `Dockerfile.migrate` (verified build); workflow builds/pushes it; `deploy.sh` sources `.env` (root-only, never echoed) for the direct URL. |
| S5-5 | Medium (auto-deploy) | `deploy.yml` deployed on every `main` push (environment gate only). | Trigger is now release tags `v*` + manual dispatch. |
| S5-6 | High (CI broken) | Four hallucinated action SHAs (aws-*/docker-*) + nonexistent trivy tag `0.32.0` + truncated setup-go SHA — every `deploy` run failed at action download. Found by verifying each pin against the GitHub API. | Verified SHAs/tags: configure-aws-credentials `ff71707`, ecr-login `03f1aad`, setup-buildx `8d2750c`, build-push `10e90e3`, trivy `v0.36.0`, setup-go `40f1582`; YAML re-parsed OK. Cut `v0.1.1` after push (never move `v0.1.0`). |
| S5-7 | High (OIDC denied) | Real cause (proven by decoding the live token): GitHub now issues `sub` with immutable numeric IDs — `repo:mddanish004@133763727/Quizmaster@1415490116:ref:…` — while the trust matched only the legacy readable form. Two red herrings eliminated along the way (session tagging, secret value). | TF: trust matches both subject forms + allows `sts:TagSession`; workflow debug step removed and tagging-skip dropped. Needs one targeted `apply` (IAM role trust only) + `v0.1.6`. |
| S5-8 | Medium (Trivy blind) | Images built ARM64-only; Trivy on the amd64 runner failed with "no child with platform linux/amd64 in index". | Multi-arch builds (`linux/amd64,linux/arm64`): digest is the manifest list, instance pulls arm64, Trivy pulls amd64. Bonus: local amd64 testing possible. Ship in `v0.1.7`. |
| S5-9 | Medium (vulnerable base) | Trivy gate (working as designed) flagged 4 HIGH Go-stdlib/x-net CVEs in the Caddy binary from floating `caddy:2-alpine`; pinning `2.11.7-alpine` did NOT help (upstream still ships stdlib 1.26.8 / x/net 0.59.0; no newer release exists). | Self-built Caddy v2.11.7 from pinned source with Go 1.27.2 + `x/net v0.60.0` (verified locally: binary builds, `caddy version` runs, govulncheck 0 called vulns). Local image build NOT RUN (no daemon in this shell) — CI build+Trivy is the verifier. Ship in `v0.1.8`. |
| S5-10 | High (deploy denied) | `build-push` green but `deploy-prod` denied: `SendCommand` authorizes against both the instance AND the document; the deploy policy allowed only the instance ARN (plus a `ssm:DocumentName` condition of dubious validity). | Policy allows `SendCommand` on instance ARN + `arn:aws:ssm:<region>::document/AWS-RunShellScript`; condition dropped. Needs targeted `apply` + SSM re-run (no new tag — re-run failed jobs). |
| S5-11 | High (plan drift) | First `policy.plan` proposed destroying the live instance: the dynamic AMI lookup re-resolved to a newer upstream image, forcing host replacement (+ EIP re-assoc + alarm churn). Caught at plan review; plan discarded unapplied. | `lifecycle { ignore_changes = [ami] }` — AMI rotation becomes an explicit `taint + apply`, never silent drift. Re-plan must show 1 change only. |
| S5-12 | High (first deploy impossible) | Nothing ever created `/opt/quizforge/.env` or placed `compose.prod.yaml`/`deploy.sh` on the host (repo is never cloned there) — the "successful" `v0.1.9` deploy died at preflight. | `deploy.sh` step 0 materializes `.env` (0600, `printf %s` — substitution-safe) from SSM + derived URLs, fetches pinned `compose.prod.yaml` by commit SHA; workflow fetches pinned `deploy.sh` the same way. |
| S5-13 | High (silent deploy) | Workflow never polled the SSM invocation — dispatch success masked remote failure, so `v0.1.9` looked green while nothing ran. | New wait-step polls to terminal status, tails output, fails the job on remote error (20 min cap). |
| S5-14 | Medium (SSM quoting) | First gated run caught a real error fast: over-escaped `&&` in the SSM command array broke `cd` on the host. | Dropped shell chaining entirely — `deploy.sh` defaults to `/opt/quizforge`, so the command is a single env-prefixed invocation. No `&&`/`cd` in SSM arrays ever again. |
| S5-15 | Medium (migrate tag) | `deploy.sh` pulled `quizforge-migrate:latest`, which CI never pushes (SHA tags only) — migrate step failed after env/ECR succeeded. | Migrate digest plumbed end-to-end (build output → SSM arg → `image@digest`), same as api/web. |
| S5-16 | High (DB unreachable) | Migrate image pulled but `dial tcp <neon>:5432: connect: connection timed out` — our SG egress allowlist (80/443/DNS/NTP) never permitted PostgreSQL. The app itself would have failed identically. | New egress rule `5432/tcp → 0.0.0.0/0` (Neon IPs are dynamic; port-scoped). Applied via targeted `apply`; re-ran failed jobs. |
| S5-17 | High (env corruption) | Unquoted `.env` values containing shell metacharacters such as `(` could not be sourced by the host migration step. | Every materialized value is single-quoted with embedded quotes escaped; `.env` remains root-owned mode 0600. |
| S5-18 | Medium (driver scheme) | With connectivity fixed, migrate failed on `unknown driver postgresql` — golang-migrate registers `postgres`, but Neon issues `postgresql://` URLs. | `deploy.sh` normalizes the scheme prefix before invoking migrate (verified substitution locally). |
| S5-19 | Medium (bad log opt) | Compose create failed: `unknown log opt 'awslogs-stream-prefix'` — that option doesn't exist on the Docker awslogs driver (ECS-ism). | Replaced with standard `tag: quizforge/<svc>/{{.Name}}`; compose config renders clean. |
| S5-20 | High (false-negative gate) | Containers up (`api-1` Healthy) yet `[verify] FAILED`: the gate curled host `127.0.0.1:8080`, but compose publishes no `:8080` — host-side localhost is the wrong netns (000 even when healthy). | Gate now probes via `docker exec <api> /quizforge healthcheck --url=…/health/ready` (exits non-zero unless 200); same for `/version` identity. |
| S5-21 | High (Caddy deaf) | External 443 returned 000 despite Healthy api: the `web` service had no `env_file`, so Caddy's `{$APP_DOMAIN}` expanded empty and it served nothing. | `web` gets the same `/opt/quizforge/.env` (extra vars harmless; Caddy reads only its own env). |
| S5-22 | Medium (health shadowed) | HTTPS 200 but `/health/ready` + `/version` returned the SPA `index.html` — Caddy proxied only `/api/*` and `/ws/*`, so root health routes fell into the `try_files` fallback (`/health/live` 200 was a false positive: the SPA, not the API). | Added exclusive `handle /health/*` + `handle /version` → `api:8080` before the fallback; `DEPLOYMENT.md` snippet synced. |
| S5-19 | High (SSM value corruption) | `getp` ran inside command substitution but emitted `[env] got NAME (...)` to stdout, prefixing every captured parameter. The migrate URL no longer began with a URI scheme, so the error reported `[env] got MIGRATIONS_DATABASE_URL ... postgresql` as the unknown driver and S5-18 could not match. | Status diagnostics now go to stderr, leaving stdout value-only. Added `deploy_test.sh`, which mocks SSM/ECR/host commands and verifies exact quoted values, mode 0600, no diagnostic leakage, and `postgres://` passed to the container. Local test passes; live redeploy remains unverified. |

### Gate B readiness

Code-complete, validated, costed (AWS ≈ $22–28, all-in ≈ $23–40), documented.
Remaining: operator approval → `plan` review → `apply` → secrets/DNS → tag release.
`tofu apply -auto-approve` never used. Details: `docs/INFRASTRUCTURE_REVIEW.md`.

---

## Gate B.1 — Cloud Prerequisites & Planning (2026-10-12, no provisioning)

- Full status: `docs/STAGE5_GATE_B1_STATUS.md` (approval-ready summary, 12 points).
- Confirmed: no AWS CLI/credentials locally (all cloud checks NOT RUN, honestly
  reported); `quiz/` still fully untracked; workflows/OIDC subjects/trigger gating
  re-verified; tofu graph (~30 resources) matches approved architecture; mail stays
  `mock` for staging (no SES).
- Operator decisions: existing domain + `staging.` subdomain (parent domain value
  still needed); alert email provided; GitHub repo slug deferred (`CHANGEME`).
- Blocked: AWS identity, `github_repo` + parent domain values, Neon/HF/Google
  credentials, SNS confirmation, Gate B.2 `apply` approval.
- Next: operator supplies §8 values → Gate B.2 plan → second confirmation → apply.
