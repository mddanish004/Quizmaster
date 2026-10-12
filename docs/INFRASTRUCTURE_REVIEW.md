# Infrastructure Review (Stage 5 Gate A.2)

Date: 2026-10-11. Scope: `deploy/opentofu/` (6 files), `backend/Dockerfile.migrate`,
`deploy/compose.prod.yaml` (logging), `deploy/scripts/deploy.sh`, `.github/workflows/deploy.yml`.

## Files created / updated

| File | Change |
|---|---|
| `deploy/opentofu/versions.tf` | NEW: TF ≥1.9, AWS ~>5.0, S3-backend-by-step guidance (no backend configured yet) |
| `deploy/opentofu/variables.tf` | NEW: 16 non-secret inputs (region/env/type/volumes/domain/repo/email/budget/retention/SSM path/tags) |
| `deploy/opentofu/main.tf` | REWRITTEN: full resource graph (~30 resources, was a commented skeleton) |
| `deploy/opentofu/outputs.tf` | NEW: URL, instance, IP, ECRs, role ARN, bucket, SSM path, log groups |
| `deploy/opentofu/user_data.sh.tmpl` | NEW: idempotent AL2023/ARM64 bootstrap (SSM, Docker, pinned Compose, awslogs default) |
| `deploy/opentofu/terraform.tfvars.example` | NEW: documented non-secret example |
| `backend/Dockerfile.migrate` | NEW: pinned `migrate/migrate:v4.18.3` + reviewed SQL (built + CLI-verified locally) |
| `deploy/compose.prod.yaml` | logging `json-file` → `awslogs` (non-blocking) per service group |
| `deploy/scripts/deploy.sh` | S5-3: writes `previous.env` (ROLLBACK.md assumed it); non-secret `deploy.env`; sources `.env` for the migration URL; installs janitor systemd units |
| `.github/workflows/deploy.yml` | S5-1 (prior): real digests; S5-4: migrate image build; Trivy on api+web; trigger `main` push → release tags `v*` + dispatch |

## Validation results (all executed 2026-10-11)

| Check | Result |
|---|---|
| `tofu fmt -check` | CLEAN (after `tofu fmt`) |
| `tofu init -backend=false` + `tofu validate` | **Success** |
| `tflint` 0.58.1 (bundled terraform ruleset) | 0 findings |
| Workflows + compose YAML `safe_load` | OK |
| `bash -n` deploy.sh/backup.sh | OK |
| `docker compose config` (dev) | OK |
| `compose.prod.yaml config` | OK (with stubbed host-only `env_file` path) |
| `Dockerfile.migrate` build + `migrate --help` | OK |
| `go vet ./...` | clean |
| `go test -count=1 ./...` (per-package DBs) | **8/8 ok** (initial run without `TEST_DATABASE_URL*` reproduced the known shared-DB flake; documented, not a code issue) |
| Secret scan (tokens/keys/passwords) | clean |
| `tofu plan` vs AWS | NOT RUN (no credentials — honestly reported, never claimed) |

## Security decisions

1. **No SSH anywhere**: no ingress 22, no key pair, admin via SSM Session Manager (managed core policy only).
2. **IMDSv2 enforced** (`http_tokens=required`); metadata tags off.
3. **Encrypted EBS** (gp3, default `aws/ebs` key); encrypted ECR (AES256) + S3 (SSE-S3, versioned, 4-way public-access block, 14-day lifecycle).
4. **Secrets never in TF**: only one `String` param (`APP_DOMAIN`); the 7 secret names are created out-of-band as `SecureString`, read at deploy into root-owned `0600` `.env`; plan reviewed for value leakage (none — names only).
5. **Least-privilege IAM**: instance role scoped to SSM path, ECR pull on 3 repos, log-group streams, backup prefix; ECR auth (`*`) is unavoidable and documented; SES send **omitted** until a verified identity exists.
6. **Deploy role**: OIDC-only trust pinned to `repo:…:environment:production` + `refs/tags/v*`; `SendCommand` limited to the app instance + `AWS-RunShellScript` document.
7. **Egress allowlist** (443/80/DNS/NTP) instead of 0.0.0.0/0-all; covers Neon/HF/Google/ECR/SSM/CW/LE/packages/NTP.
8. **Immutable, scanned images**: ECR immutable tags, scan-on-push, keep-last-5, Trivy HIGH/CRITICAL gate in CI; releases only on `v*` tags + `production` environment reviewers.
9. **Non-blocking log delivery** so a logging outage cannot wedge containers; no secret values flow through user-data (template vars are region/env/groups only).
10. **State**: local until the operator bootstraps the encrypted versioned S3 backend with lockfile; staging/prod must never share a key.

## Remaining external prerequisites (all BLOCKED, named owners)

AWS account + alert email · Neon staging branch URLs · HF capped token + `/v1/models` check ·
Google web-client + consent + test users · parent domain (or DNS skipped) · GitHub repo slug +
3 repo secrets · SNS email confirmation · Gate B approval for `apply`.

## Costs (see COST_ANALYSIS.md inventory)

AWS ≈ **$22–28/mo** base; all-in ≈ **$23–40**. Trial covers compute to 2026-12-31
(account-dependent); post-trial steady state is the same range (it is already the
no-credit estimate). Idle burn is dominated by EC2 + EIP + EBS.

## Remaining deployment risks

1. `validate`/`tflint` ≠ deployable — first `plan` may surface account limits (EIP quota, EBS) or AMI/SSM-parameter drift.
2. GitHub OIDC thumbprint pinned — re-verify at apply; mismatch breaks the deploy role.
3. `migrate/migrate:v4.18.3` ARM64 pull happens in CI (multi-arch) — first tag build proves it.
4. awslogs driver needs the instance role at container start; mis-scoped IAM fails closed (containers won't start) — readiness gate catches it.
5. Docker Compose plugin pin may age — Dependabot does not cover user-data; annual bump.
6. SNS email unconfirmed = silent alarms; budget covers cost blindness only after confirmation.
7. Single-AZ host, no zero-downtime deploys (10–30 s reconnect window, clients handle `1012`).

## Gate B readiness

**Ready to request approval.** Code-complete, locally validated, documented, and
costed. First `plan` review + `apply` + secrets + DNS + tag-release remain behind
explicit operator approval. `tofu apply -auto-approve` is never used.
