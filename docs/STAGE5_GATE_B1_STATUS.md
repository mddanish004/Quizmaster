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
  files). **DONE 2026-10-12 (operator-approved):** exported to
  `/tmp/qf-export/quizforge`, committed (199 files, leak-check clean), pushed to
  `github.com/mddanish004/Quizmaster` (`main`). TF `github_repo` default +
  example now `mddanish004/Quizmaster`; alert email set.
  ```bash
  # Option 1 (preferred): standalone repo, history preserved via filter
  git clone /home/mddanish quizforge && cd quizforge
  git filter-repo --subdirectory-filter quiz --force   # needs git-filter-repo
  # Option 2 (simpler): fresh repo with one squashed import commit
  mkdir quizforge && cp -r /home/mddanish/quiz quizforge/ && cd quizforge
  git init -b main && git add -A && git commit -m "QuizForge AI: stages 1-5 Gate B.1"
  ```
  Either way: never `git add -A` from `/home/mddanish`; never push without approval.

## 1. AWS account & region (P1) — identity confirmed, quotas NOT RUN

- Operator-verified 2026-10-12 via CloudShell: account `460169892311`;
  `t4g.small` offered in **all AZs of both `ap-southeast-1` (a/b/c) and
  `ap-south-2` (a/b/c)** — an earlier console "not offered" report was a view
  issue, disproven by `describe-instance-type-offerings`.
- Region decision: **stay `ap-southeast-1`** (Neon Singapore adjacency keeps DB
  round trips at ~1–5 ms vs ~50–80 ms from Hyderabad). No config change needed.
- Still NOT RUN from here (no credentials in this environment): quotas (EIP,
  EBS), ECR/SSM/Route 53/Budgets pre-checks — operator runs at plan time.
- Operator confirmed 2026-10-12: account-level $30 monthly budget + 80% email
  alert created (TF per-project budget follows at apply).

## 2. Domain & DNS decision (P2) — operator chose existing domain + subdomain

- Staging hostname fixed: `quizzy.mddanish.dev`. Operator-checked 2026-10-12:
  **no Route 53 zone** — DNS is external, so `domain_name = ""` and the operator
  creates one manual record post-apply: **A, host `quizzy`, value = Elastic IP
  from `tofu output public_ip`, TTL 300**. Caddy issues TLS on first HTTPS hit
  after DNS resolves; deploy itself succeeds over IP first.
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
| Neon staging | **MIGRATED 2026-10-12** — operator ran `migrate up` from CloudShell against the staging branch: 7/7 ok (`extensions`, `auth`, `quiz`, `ai`, `live`, `live_check`, `oauth_nonce`). Table-count **confirmed 18/18** 2026-10-12. Note: operator briefly pasted the pooled URL in chat → password rotated before use; nothing stored. `channel_binding=require` may need stripping for pgx if the smoke connect fails. Guide: `AWS_SETUP_GUIDE.md` §2 + `DEPLOYMENT.md` runbook |
| Hugging Face | **ROUTING VERIFIED 2026-10-12** — live catalog confirms `Qwen/Qwen3-32B` served by `deepinfra` (`live`, `supports_structured_output=True`; also live on nscale/featherless-ai without structured output). Default `HF_MODEL` stands; `HF_PROVIDER=auto` is acceptable with deepinfra as the known-good route. One small live generation still to run at staging. Free tier $0.10/mo |
| Google OAuth | **READY 2026-10-12** — operator created web client + consent (Testing mode, 2 test users). Staging hostname fixed: `quizzy.mddanish.dev`; redirect `https://quizzy.mddanish.dev/api/v1/auth/google/callback`. Real login smoke waits for staging. No verification bypass, ever |
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

## 5. Real infrastructure plan (P5) — PLAN GENERATED 2026-10-12, apply NOT run

Operator-run in CloudShell (tofu 1.9.0, AWS provider 5.100.0, account
`460169892311`, `ap-southeast-1`): `init` + `validate` Success;
`fmt -check` flags only `staging.tfvars` alignment (cosmetic — run `tofu fmt`);
**`plan`: 38 to add, 0 to change, 0 to destroy**, saved to `staging.plan`.
Review: creates match the approved graph (SG+7 rules, EIP+assoc, t4g.small/
IMDSv2-required/encrypted gp3 in default subnet, instance+deploy roles, OIDC
provider, 3 ECR repos + lifecycles, 1 SSM String, 3 log groups, SNS+sub, 2
alarms, S3×5, budget). No DNS resources (external DNS — correct); no secret
values (APP_DOMAIN placeholder only); no replacements/deletions; tags present.
`apply "staging.plan"` awaits explicit Gate B.2 approval — never `-auto-approve`.

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

## 9. Approval boundary — APPLY EXECUTED 2026-10-12 (operator-approved)

`tofu apply "staging.plan"` succeeded: instance `i-0f63357367b2376b1`, EIP
`18.136.59.140`, ECR `…/quizforge-{api,web}`, deploy role
`…:role/quizforge-staging-github-deploy`, bucket
`quizforge-staging-backups-20261012144502463400000001`, log groups
`/quizforge-staging/{api,web,host}`, SSM `/quizforge/staging/` (operator-uploaded 2026-10-12).
Spend now active (~$21/mo idle base). Remaining: SNS confirm → manual `quizzy`
A record → GitHub secrets → `v*` tag → live smoke.

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
