# AWS Staging Setup Guide (manual, operator-run)

Target: one reproducible staging environment in `ap-southeast-1`. No step here
provisions anything by itself — `tofu apply` (step 10) still needs the separate
Gate B approval. Never paste secrets into source files, chat, tickets, or logs;
enter them only at the consoles/CLIs named below.

## 1. AWS account + billing alerts

1. Create (or reuse) the AWS account; enable MFA on the root user; create an
   admin IAM user/role for daily use — never use root keys.
2. In Billing → Budgets, confirm you can receive mail, then note the account ID
   from STS (`aws sts get-caller-identity`) — you will need it for nothing in
   files (all ARNs are Terraform references), only for console navigation.
3. The Terraform `monthly` budget + SNS alerts are created by code; you only
   supply `alert_email` and confirm the SNS subscription email when it arrives.

## 2. Neon staging branch

1. Neon console → project (Singapore region) → Branches → **Create branch**
   (e.g. `staging`), or a separate staging project for full isolation.
2. Copy both connection strings from the branch dashboard:
   - Pooled (`...-pooler.…aws.neon.tech…?sslmode=require`) → `DATABASE_URL`
   - Direct (`….…aws.neon.tech…?sslmode=require`) → `MIGRATIONS_DATABASE_URL`
3. Disposable validation only (see `docs/DEPLOYMENT.md` runbook): run `up` from
   `database/migrations` against the branch, expect 18 tables, then keep the
   branch for staging. Never point tests at it.

## 3. Hugging Face token

1. Account → Settings → Tokens → **fine-grained token** with *Inference
   Providers* `write` permission (docs: chat-completion auth).
2. Set a fixed spend cap/alert in billing; free credit is only $0.10/mo.
3. Before staging traffic, open `GET https://router.huggingface.co/v1/models`
   with the token and confirm `Qwen/Qwen3-32B` is routed with strict JSON Schema;
   fallback `Qwen/Qwen2.5-7B-Instruct-1M`, or pin `HF_PROVIDER=cerebras`.

## 4. Google OAuth client

1. Google Cloud Console → APIs & Services → Credentials → **Create OAuth client**,
   type **Web application**.
2. Authorized redirect URI (exact): `https://<staging-fqdn>/api/v1/auth/google/callback`.
   No JavaScript origins (server-side code flow).
3. Configure the OAuth consent screen; while in *Testing*, add the operator test
   accounts under Test users.
4. Keep client ID + secret ready for step 7.

## 5. Domain + DNS

1. Use an existing parent domain: note its Route 53 hosted zone (or create the
   zone and point the registrar's NS records at it — allow propagation time).
2. Set `domain_name = "example.com"` (+ optional `subdomain`) in `staging.tfvars`;
   Terraform creates only the `A` record for the staging FQDN → Elastic IP.
   Leave `domain_name = ""` to skip DNS entirely (Caddy then has no hostname —
   deployment smoke still works over HTTP/IP, TLS waits for DNS).

## 6. GitHub Actions OIDC (no static keys)

1. After `apply`, read outputs: `deploy_role_arn`, `instance_id`, ECR URLs
   (`tofu output`). No keys are created anywhere — GitHub mints short-lived
   tokens per run, trusted only for `repo:<ORG/REPO>:environment:production`
   and `refs/tags/v*`.
2. GitHub repo → Settings → Secrets and variables → Actions:
   - Secret `DEPLOY_ROLE_ARN` = `deploy_role_arn` output
   - Secret `PROD_INSTANCE_ID` = `instance_id` output (rename per environment)
   - Secret `ECR_REGISTRY` = `<account>.dkr.ecr.ap-southeast-1.amazonaws.com`
3. Environments → `production`: add required reviewers (a human gate). Releases
   ship on tags `v*` (`git tag v0.1.0 && git push origin v0.1.0`) or manual
   `workflow_dispatch` — pushes to `main` never deploy.

## 7. Supply secrets (out-of-band, host-side)

On your workstation (never in git):

```bash
aws ssm put-parameter --region ap-southeast-1 \
  --name "/quizforge/staging/DATABASE_URL" --type SecureString --value '…' \
  --tags Key=Project,Value=QuizForge
# Repeat for: MIGRATIONS_DATABASE_URL, CSRF_SECRET (32+ random bytes),
# PASSWORD_PEPPER (32+ random bytes), GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET,
# HF_TOKEN. Generate randoms with: openssl rand -hex 32
```

The instance role can read only `/quizforge/staging/*`. A root-owned host script
(see step 11 / `deploy/scripts`) materializes `/opt/quizforge/.env` mode `0600`
from these parameters — parameter *names* appear in logs, values never do.

## 8. Run a plan

```bash
cd deploy/opentofu
cp terraform.tfvars.example staging.tfvars   # fill CHANGEME values
tofu init                                     # provider download
tofu fmt -check && tofu validate
tofu plan -var-file=staging.tfvars -out=staging.plan
```

No AWS credentials are needed until this step; everything before is offline.

## 9. Review the plan

- Expect ≈ 30 resources: SG + 7 rules, EIP + association, EC2, IAM role/profile/
  2 policies, OIDC provider + deploy role/policy, 3 ECR repos + 3 lifecycle
  rules, 1 SSM String param, 3 log groups, SNS topic/subscription (if email),
  2 alarms, S3 bucket + 4 bucket configs, budget (if email), Route 53 record
  (if domain). Zero surprises or it does not ship.
- Confirm no secret *values* in the plan (only the SSM param *name*).
- Confirm costs against `docs/COST_ANALYSIS.md` (≈ $22–28 AWS base).

## 10. Apply (Gate B approval required)

```bash
tofu apply staging.plan     # NEVER -auto-approve; type "yes" by hand
```

Record outputs, then configure the three GitHub secrets (step 6).

## 11. First host bootstrap check

```bash
aws ssm start-session --target <instance-id>   # no SSH keys, ever
sudo tail -30 /var/log/quizforge-bootstrap.log  # expect "complete"
docker compose version
```

If bootstrap failed, re-run `/var/lib/cloud/instance/scripts/part-001`
(or reboot) — the script is idempotent.

## 12. Deploy + verify

1. Push tag `vX.Y.Z`; approve the `production` environment; watch the SSM run:
   env-permission preflight → ECR login → migrate → compose switch → janitor
   timer install → readiness gate → `/version` identity.
2. Verify: `https://<fqdn>/health/ready`, `/version` (release commit), landing,
   register/login, AI draft (real HF), publish, two-browser live question with
   mid-question refresh, history, logout. See `docs/DEPLOYMENT.md` checklist.
3. Calibrate Argon2 on the instance and record it in the status ledger.

## 13. Destroy staging safely (when done)

```bash
# 1. Empty the backup bucket versions first (S3 blocks non-empty deletes):
#    console or: aws s3 rm s3://<bucket> --recursive (repeat per version if needed)
tofu destroy -var-file=staging.tfvars   # Gate B approval required, type "yes"
# 2. Release order safety: plan shows DNS record → instance → EIP → repos →
#    roles → bucket → logs. Confirm the EIP is released (it bills while allocated).
# 3. Revoke the HF token; delete the Neon staging branch; remove GitHub secrets.
```
