# Stage 5 Gate B.1 Status — Cloud Prerequisites, Planning & Deployment Approval

Date: 2026-10-12. Worktree: `/home/mddanish/quiz`.
**No infrastructure provisioned, no secrets configured, no billable changes.**
Gate B.1 permits local changes, tests, read-only checks, plan generation, docs.

## 0. Repository & GitHub readiness (P0)

- Git root remains `/home/mddanish` (home-dir repo); `git ls-files quiz` is empty —
  **all of `quiz/` is still untracked**, unchanged from Stages 4–5. Home root also
  contains many unrelated untracked personal files (`.agents/`, `.cache/`, …).
- `.gitignore` (verified): `.env`, `node_modules/`, `dist/`, `*.log`, `coverage/`,
  `playwright-report/`, `test-results/`, `.docker/`, `.terraform/`, `*.tfstate*`,
  `*.tfvars`, `*.tfvars.json`. State files, plans, and build artifacts are excluded.
- Workflows live at `quiz/.github/workflows/` (`ci.yml`, `deploy.yml`, `security.yml`).
  `deploy.yml` triggers only on release tags `v*` + manual dispatch; `deploy-prod`
  uses the `production` environment (human approver) + OIDC role, no static keys.
  OIDC trust is pinned to `repo:<ORG/REPO>:environment:production` and
  `repo:<ORG/REPO>:ref:refs/tags/v*`.
- **Recommendation: convert `quiz/` into a standalone repository** before first
  deploy (clean history, correct OIDC `ORG/REPO`, no risk of sweeping home-dir
  files). Proposed actions (NOT executed — authorization required):
  ```bash
  # Option 1 (preferred): standalone repo, history preserved via filter
  git clone /home/mddanish quizforge && cd quizforge
  git filter-repo --subdirectory-filter quiz --force   # needs git-filter-repo
  # Option 2 (simpler): fresh repo with one squashed import commit
  mkdir quizforge && cp -r /home/mddanish/quiz quizforge/ && cd quizforge
  git init -b main && git add -A && git commit -m "QuizForge AI: stages 1-5 Gate B.1"
  ```
  Either way: never `git add -A` from `/home/mddanish`; never push without approval.

## 1. AWS account & region (P1) — BLOCKED, no access

- `aws` CLI: **not installed**; no `AWS_*` env vars; no `~/.aws` profile found.
  `sts get-caller-identity` and all quota/zone checks are therefore **NOT RUN**.
- Account ID / region config: unknown (nothing to redact — nothing observed).
- Region decision stands: `ap-southeast-1` (Neon Singapore adjacency).
- Exact setup (no keys in chat — use SSO/profile locally):
  ```bash
  # Install: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html
  aws configure sso   # or: aws configure  (IAM Identity Center preferred over static keys)
  export AWS_PROFILE=<name> AWS_REGION=ap-southeast-1
  aws sts get-caller-identity   # read-only sanity check
  ```
  Service quotas to confirm at plan time (console or `service-quotas`): EC2
  running On-Demand `t4g.small` (Standard), EC2 EIP (default 5 — need 1),
  EBS gp3. ECR/SSM/Route 53/CloudWatch/Budgets need no quota action.

## 2. Domain & DNS decision (P2) — operator chose existing domain + subdomain

- Staging hostname pattern: `staging.<parent-domain>`; **parent domain value
  still required from the operator** (e.g. `staging.example.com`).
- Authoritative DNS: operator's existing provider can keep the record (simple `A`
  → Elastic IP, TTL 300) — a Route 53 hosted zone is **not required** in that
  case; set `domain_name = ""` and create the record manually. Use the TF
  `aws_route53_record` only if the parent zone already lives in Route 53.
- Google OAuth redirect (exact, register after DNS is known):
  `https://staging.<parent-domain>/api/v1/auth/google/callback`.
  No JavaScript origins (server-side code flow, no GIS library).
- Caddy: serves `{$APP_DOMAIN}` with automatic Let's Encrypt issuance on first
  HTTPS hit; port 80 stays open for ACME redirect. DNS must resolve **before**
  the smoke test or TLS issuance fails (deploy itself succeeds over IP first).
- Option B (register new, declined): ~$1–15/yr + registrar/NS setup + propagation;
  revisit only if the existing domain cannot take a subdomain.

## 3. External services (P3) — all credential-blocked, instructions ready

| Service | Status | Plan |
|---|---|---|
| Neon staging | BLOCKED (no URL) | Console → Singapore project → branch `staging`; pooled → `DATABASE_URL`, direct → `MIGRATIONS_DATABASE_URL`; disposable-branch `up` check (18 tables). Full steps: `AWS_SETUP_GUIDE.md` §2 + `DEPLOYMENT.md` runbook |
| Hugging Face | BLOCKED (no token) | Fine-grained token (Inference Providers `write`) + spend cap; verify `GET /v1/models` for `Qwen/Qwen3-32B` strict-schema routing (fallback `Qwen/Qwen2.5-7B-Instruct-1M`, or pin `HF_PROVIDER=cerebras`); one small live generation at staging. Free tier is $0.10/mo — never unlimited |
| Google OAuth | BLOCKED (no client) | Web-application client; redirect per §2; consent screen + test users while Testing; real first-time/returning/link/invalid-callback/logout smoke at staging. No verification bypass, ever |
| Email (SES) | **NOT NEEDED for staging** | `MAIL_PROVIDER` defaults to `mock` (`config.go:129`); password-reset flow is testable without mail delivery. Do NOT create SES identities/send approvals for staging — revisit for production only |

## 4. OpenTofu configuration review (P4) — PASS as specified

- Re-inspected `deploy/opentofu/`: ~30 resources matching the approved single-host
  architecture. EC2 `t4g.small` (var-overridable), first default-subnet AZ
  (single-AZ by design), AMI via SSM public parameter (no hardcoded ID).
- SG: ingress 80/443 only (no SSH); egress allowlist 443/80/DNS/NTP — covers
  Neon/HF/Google/ECR/SSM/CloudWatch/LE/packages/NTP, denies the rest.
- IAM: instance role (SSM-core managed + scoped inline; SES send omitted);
  deploy role OIDC trust per §0; `SendCommand` limited to the app instance +
  `AWS-RunShellScript`. ECR auth `*` is unavoidable (documented).
- ECR ×3 (api/web/migrate, immutable, scan-on-push, keep-5); SSM only one public
  `String` (`APP_DOMAIN`) — 7 secrets out-of-band as `SecureString`.
- CloudWatch: 3 log groups (14 d) + status-check/CPU alarms → SNS (enabled once
  `alert_email` is set — **now provided, see §11**). S3: versioned/SSE-S3/
  access-blocked/14-day lifecycle. Budget at `budget_limit_usd` (actual 80% +
  forecast 100%). DNS record conditional on `domain_name`.
- State: no backend configured (deliberate until the operator bootstraps the
  encrypted versioned S3 bucket + lockfile); staging/prod keys must differ.
- Bootstrap (`user_data.sh.tmpl`): idempotent, secret-free, installs SSM agent,
  Docker, pinned Compose, awslogs-default daemon config. No incorrect defaults
  found; architecture unchanged.

## 5. Real infrastructure plan (P5) — NOT RUN (blockers named)

Blockers: (a) no AWS credentials in this environment; (b) `staging.tfvars`
missing two operator values — `github_repo` (still `CHANGEME/quizforge`) and
parent `domain_name` (may stay `""` for IP-first). Nothing invented.
First-plan command set (operator machine, after §11 items):
```bash
cd deploy/opentofu && cp terraform.tfvars.example staging.tfvars  # fill values
tofu init && tofu fmt -check && tofu validate && tflint
tofu plan -var-file=staging.tfvars -out=staging.plan   # review, never -auto-approve
```
Expected: ≈ 30 creates, 0 destroys; any replacement/deletion is a stop signal.
Plans/state are gitignored and never committed.

## 6. Deployment costs (P6) — reconfirmed, assumptions explicit

AWS ≈ **$22–28/mo** (EC2 $15.48 + EBS $1.92 + IPv4 $3.65 + R53 ≤$0.54 + ECR/S3/CW
≤$4 + transfer ≤$2); all-in ≈ **$23–40** with Neon ($0–5), HF capped ($0–5),
domain (~$1–2 equivalent). Assumes 730 h, Singapore pricing, no credits.
Zero-traffic idle burn: EC2 + EIP + EBS (~$21) — stopping the instance pauses
compute only. Trial (to 2026-12-31, account-dependent) may zero compute until
expiry; steady state is the table above. Alerts: budget 80% actual + 100%
forecast to the operator mailbox + SNS subscription (must be confirmed).

## 7. Deployment & rollback readiness (P7) — sequence verified, no hard cycle

Order: `apply` → secrets to SSM → DNS record → tag `v*` (CI builds/pushes 3
images) → SSM `deploy.sh` (preflight → ECR login → **migrate via direct URL** →
compose switch → janitor timer → readiness gate → `/version`) → TLS/DNS smoke →
REST/WS/E2E → alarms/janitor/backup checks. Dependency audit: ECR must precede
push (TF creates repos first — satisfied); images must precede deploy (tag build
runs before the SSM step — satisfied); DNS needed only for TLS/OAuth, not for
deploy (soft ordering — satisfied); secrets precede containers (SSM step writes
`.env` before compose — satisfied). **No circular dependency.**
Rollback: `previous.env` digests re-run (migrations never auto-reversed);
DB recovery into a **new** Neon branch; teardown order DNS → instance → EIP
release → ECR → SSM → Neon → bucket empty+delete. Full runbook:
`AWS_SETUP_GUIDE.md` §§11–13, `ROLLBACK.md`.

## 8. Outstanding prerequisites — what YOU must configure personally

1. AWS access on your machine (`configure sso`, region `ap-southeast-1`) + confirm
   EC2/EIP quotas. 2. Parent domain value (or confirm IP-first with `domain_name=""`).
   3. `github_repo` ORG/REPO slug. 4. Neon staging branch URLs. 5. HF capped token.
   6. Google web-client + consent + test users. 7. Seven SSM SecureString values
   (private CLI step — never in chat). 8. Confirm the SNS subscription email.
   9. Approve `tofu apply` (Gate B.2) — then a `v*` tag ships staging.

## 9. Approval boundary — exact Gate B.2 asks

(a) `tofu init/plan/apply` for staging (≈ 30 resources, ~$22–28/mo base);
(b) creating the Route 53 A record if the zone is in Route 53 (else you create it
manually); (c) first `v*` tag release → ECR pushes + SSM deploy (public staging
URL live); (d) running one small live HF generation + one real Google test login.
Excluded: domain registration, production data, SES, any second environment.

## Verdict

**Ready**: code, IaC, CI/CD, runbooks, costs, sequence. **Blocked**: AWS identity,
three config values, four external credentials (§8). **Spends money**: §6 table
only after you approve (a). After approval I will: init → fmt/validate/tflint →
plan → present the exact plan diff for a second explicit `apply` confirmation →
then guide first deploy, smoke, and drills. **STOP — no provisioning in B.1.**
