# Deployment And Operations

## Environments

| Environment | Compute | Database | External services |
|---|---|---|---|
| local | Docker Compose Caddy/frontend dev/API | dedicated Neon dev branch | mock AI/mail by default; optional real sandbox credentials |
| CI | built/test containers and host runners | Testcontainers PostgreSQL 18 or disposable Neon branch for Neon-specific smoke | mock AI/OAuth/mail |
| staging | same ARM64 images, optional stopped/scheduled EC2 or shared small host | Neon staging branch/project | Google test client, HF capped token, SES sandbox |
| production | one `t4g.small` EC2, Caddy + API | Neon Singapore production | Google production OIDC, HF capped, SES production |

Never point tests or local migrations at production. Environment is explicit and production startup refuses localhost/insecure database settings.

## Docker Images

### Backend

```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.27.2-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ .
ARG VERSION=dev COMMIT=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" \
    -o /out/quizforge ./cmd/api

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/quizforge /quizforge
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/quizforge"]
```

Build a separate migration target/binary (`cmd/migrate`) into a distroless migration image or invoke a pinned migration tool; do not embed automatic migrations in every API startup.

### Frontend/Caddy

```dockerfile
# syntax=docker/dockerfile:1.7
FROM node:24-alpine AS web-build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ .
RUN npm run build

FROM caddy:2-alpine
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
COPY --from=web-build /app/dist /srv
```

Pin base image digests in production after setup; Dependabot updates them. No secrets or runtime API URL are built into frontend except public same-origin configuration.

## Compose

```yaml
services:
  api:
    build:
      context: .
      dockerfile: backend/Dockerfile
    env_file: .env
    read_only: true
    tmpfs: [/tmp]
    security_opt: [no-new-privileges:true]
    cap_drop: [ALL]
    restart: unless-stopped
    healthcheck:
      test: [CMD, /quizforge, healthcheck, --url=http://127.0.0.1:8080/health/live]
      interval: 15s
      timeout: 3s
      retries: 3
      start_period: 10s
    networks: [app]

  web:
    build:
      context: .
      dockerfile: frontend/Dockerfile
    depends_on:
      api: {condition: service_healthy}
    ports: ["80:80", "443:443"]
    volumes:
      - caddy_data:/data
      - caddy_config:/config
    restart: unless-stopped
    security_opt: [no-new-privileges:true]
    cap_drop: [ALL]
    cap_add: [NET_BIND_SERVICE]
    networks: [app]

networks:
  app: {}
volumes:
  caddy_data: {}
  caddy_config: {}
```

Development uses `compose.override.yaml`: Vite dev server with source mount, Go hot reload if justified, ports bound to loopback, and Caddy optional. Production (`deploy/compose.prod.yaml`) has no bind-mounted source, uses immutable image digests, ships container logs via the `awslogs` driver in non-blocking mode to per-service CloudWatch groups (daemon default covers anything without an explicit driver), memory/CPU limits after measurement, and `stop_grace_period: 30s`. Neon and HF remain external. The full production topology is codified in `deploy/opentofu/` (see `docs/AWS_SETUP_GUIDE.md` and `docs/INFRASTRUCTURE_REVIEW.md`).

## Caddy

```caddyfile
{$APP_DOMAIN} {
  encode zstd gzip

  # Exclusive handlers first: try_files is a rewrite and would otherwise
  # shadow /api/* and /ws/* with the SPA fallback.
  handle /api/* {
    reverse_proxy api:8080 {
      health_uri /health/live
    }
  }
  handle /ws/* {
    reverse_proxy api:8080
  }

  handle {
    root * /srv
    try_files {path} /index.html
    file_server
  }

  header {
    -Server
    X-Content-Type-Options nosniff
    Referrer-Policy strict-origin-when-cross-origin
    X-Frame-Options DENY
  }

  log { output stdout; format json }
}
```

Add the tested CSP from `AUTH_SECURITY.md`; do not paste it before confirming Vite assets, Google redirect, and WebSocket URLs. Caddy manages Let's Encrypt certificates in its persistent volume. Port 80 remains open for ACME redirect; 443 serves traffic.

## AWS Resources

- Route 53 public hosted zone and `A` record to one Elastic IP; domain registration can remain with any registrar.
- EC2 Graviton `t4g.small`, Amazon Linux 2023 or Ubuntu LTS ARM64, 20 GiB encrypted gp3, detailed monitoring only if justified.
- Security group inbound TCP 80/443 from internet; no 22. Egress 443 and DNS as required; database is Neon public TLS.
- IAM instance role: SSM managed instance core, read selected SSM parameters, ECR pull, write named log groups/backup bucket, SES send if used. No wildcard administrator.
- ECR repositories for API/web, scan-on-push, immutable tags, lifecycle policy.
- SSM Parameter Store SecureString under `/quizforge/prod/*`; GitHub deploy role cannot read application secrets unless needed.
- CloudWatch log groups 14-30 day retention; alarms for instance status, CPU, memory/disk agent metrics, API 5xx/readiness, restart loop, AI failures.
- S3 versioned backup bucket, public access blocked, SSE, lifecycle 14 days; IAM restricted prefix.
- SES verified domain/sender for recovery email; remain in sandbox until verified production access.
- AWS Budget and cost anomaly alerts before launch.

No NAT Gateway, ALB, RDS, ElastiCache, or ECS is part of MVP.

## Host Bootstrap

Use infrastructure-as-code: a small OpenTofu module for the VPC/default-subnet decision, security group, instance/profile, EIP, ECR, parameters, logs, bucket, DNS, and alarms. Pin OpenTofu/provider versions and store state in an encrypted versioned S3 backend only after bootstrapping; for a single developer, a documented local encrypted state backup is acceptable initially but must not be public.

Cloud-init installs SSM agent if needed, Docker Engine/Compose plugin, creates `/opt/quizforge` owned by a deployment user, configures log rotation and unattended security updates, and enables the Compose systemd unit. Never curl an unpinned shell script as root in CI.

## Secrets

At deploy, an SSM command runs a root-owned script that fetches only the named parameter path via instance role, writes `/opt/quizforge/.env` mode `0600`, logs parameter names but not values, authenticates ECR, pulls digests, runs migration, and starts. Prefer native instance role for AWS APIs so SES/S3 do not require keys. Rotate session/HF/Google/database secrets with documented overlapping behavior; rotating session key revokes sessions if tokens are opaque and database hashes need no signing key.

## CI Pipeline

GitHub Actions pins third-party actions to commit SHAs and uses minimal permissions.

1. `frontend`: `npm ci`, format check, ESLint, `tsc --noEmit`, Vitest coverage, build.
2. `backend`: `gofmt -l` must be empty, `go vet`, `staticcheck`, `govulncheck`, unit tests, `go test -race` on concurrency packages.
3. `contract/schema`: Spectral OpenAPI lint, generated TS diff, migration up/down/up, DB integration, WebSocket tests.
4. `e2e`: Compose app with mocks, Playwright core flows and axe scans.
5. `images`: Build ARM64 (and amd64 for local if desired) with Buildx; Trivy filesystem/image; gitleaks; generate SBOM; sign/provenance via GitHub artifact attestations or Cosign keyless.
6. On protected `main`/release: push immutable `git-sha` tags/digests to ECR.
7. Production environment approval; GitHub OIDC assumes a narrowly scoped deploy role; SSM deployment runs migrations and rolling single-host replacement.
8. Smoke tests `/health/ready`, `/version`, static asset, login page, WebSocket upgrade; mark deployment only after expected commit is live.

PR CI uses mocks and disposable databases, never production credentials. Fork PRs receive no secrets. Environments protect production with reviewers; OIDC trust restricts repository, branch/environment, audience, and role session duration. Never store AWS access keys in GitHub.

## Deployment And Rollback

Deployment directory holds `compose.prod.yaml`, Caddyfile, current/previous image digest manifests, and scripts.

1. Preflight: CI green, migration compatibility checked, backups healthy.
2. Pull new images by digest.
3. Run backward-compatible migration using direct Neon URL.
4. Start new API container on a temporary internal port/project name and wait readiness, or accept a brief documented restart for MVP.
5. Switch/recreate service, verify commit and core smoke tests.
6. Retain previous digest. If health fails, restore previous digest immediately. Do not automatically reverse a migration; migrations must be backward compatible or a documented forward fix.
7. Notify/record deployment commit, migration version, checks, duration, result.

Single-host Compose cannot provide true zero downtime without extra proxy choreography; a 10-30 second reconnect window is acceptable for portfolio deployment and clients handle code `1012`. Do not claim zero downtime.

## Backup And Disaster Recovery

- Rely on Neon's plan-specific restore window only after verifying it; free-plan history is not a backup guarantee for production.
- Nightly `pg_dump --format=custom` through direct TLS URL, encrypted S3 bucket, 14-day retention. Backup job failure alarms.
- Quarterly restore into a fresh Neon branch/database, migrate/check row counts and a sample session reconstruction, record measured RPO/RTO.
- Infrastructure is reproducible from IaC; Caddy cert/state can be regenerated. ECR retains last five releases.
- Target RPO 24 hours with logical backup (better if Neon restore available), target RTO 4 hours for solo-operated portfolio; measured drill replaces target claims.

## Monitoring And Runbooks

Dashboards: request rate/error/p95, active sockets/sessions, answer outcomes, broadcast drops, DB pool/query latency, memory/CPU/disk, container restarts, AI outcomes/cost usage. Alerts should be actionable and low-noise.

### Daily Retention Janitor

The API image doubles as the cleanup runner — no extra service to operate:

```ini
# /etc/systemd/system/quizforge-janitor.service
[Service]
Type=oneshot
EnvironmentFile=/opt/quizforge/.env
ExecStart=/usr/bin/docker run --rm --env-file /opt/quizforge/.env ${ECR_REGISTRY}/quizforge-api@${IMAGE_API_DIGEST} /quizforge janitor

# /etc/systemd/system/quizforge-janitor.timer
[Timer]
OnCalendar=daily
Persistent=true
```

Enable with `systemctl enable --now quizforge-janitor.timer`. The janitor only
deletes expired/transient rows (consumed OAuth/reset tokens, revoked sessions
older than 30 days, expired idempotency keys, events of sessions ended 30+ days
ago, generation metadata 90+ days old, trash 30+ days old); quiz results,
answers, snapshots, and live user data are never touched. Each run logs per-step
counts; a nonzero exit (partial failure) should alarm. Bounded batches make it
safe to re-run manually at any time.

Runbooks cover: API down, DB unavailable/waking, high memory/CPU credits, disk full, certificate failure, AI outage/quota, Google login failure, SES bounce, high 5xx, stuck live question, backup failure, and rollback. Each names detection, impact, diagnosis commands, mitigation, verification, and follow-up.

## Production Checklist

- DNS/TLS/headers scanned; debug ports closed; SSM works before closing SSH.
- Neon/HF/Google/SES region, limits, billing and credentials verified.
- Migrations and restore drill pass; backups/alarms/budgets active.
- OAuth production redirect/consent/domain configured.
- Load test meets selected capacity on exact instance; Argon2 calibrated (see below).
- Logs contain no secrets/content; retention configured. Image digest/SBOM/scan recorded; app runs non-root with dropped capabilities.
- Smoke test includes two browsers completing one live question and reconnecting.

## Step-By-Step First Deployment (operator runbook)

Estimated hands-on time: 2-4 hours excluding DNS propagation and Google verification.

### Neon Staging Branch Setup (do this first)

1. In the Neon console, create a **staging branch** off production (or a separate
   staging project for full isolation). Note the region — keep Singapore
   (`ap-southeast-1` adjacency) for pooler latency parity.
2. From the branch dashboard copy two connection strings:
   - **Pooled (runtime):** `...-pooler.<region>.aws.neon.tech/db?sslmode=require`
     → `DATABASE_URL`. The Go pool (`DB_MAX_CONNS=10`, 30 m lifetime, 5 m idle)
     is tuned for this endpoint; never point the runtime at the direct host.
   - **Direct (migrations/ops):** `....<region>.aws.neon.tech/db?sslmode=require`
     → `MIGRATIONS_DATABASE_URL`. Migrations, `pg_dump`, and `EXPLAIN` work
     requiring session semantics must use this one.
3. Safe validation against the **disposable branch only**:
   ```bash
   export MIGRATIONS_DATABASE_URL='<direct-url>'
   migrate -path database/migrations -database "$MIGRATIONS_DATABASE_URL" up
   psql "$MIGRATIONS_DATABASE_URL" -c '\dt'   # expect 18 tables
   # app smoke: DATABASE_URL='<pooled-url>' go run ./backend/cmd/api, then
   # curl /health/ready (200) + register→login→/me
   # tear down: drop the branch in the console when done
   ```
4. Never run `down` migrations or `TRUNCATE` against production; restores go to
   a **new** branch first (see ROLLBACK.md). Free-plan restore history is not a
   backup guarantee — keep the nightly `pg_dump` job regardless of plan.
5. Wake-from-idle: Neon scales to zero on idle; the first request after idle
   pays cold-start latency. The app treats this as transient (bounded query
   timeouts, readiness probe fails until the pool reconnects, no false answer
   acks). Do not mistake wake latency for an outage; verify with a second
   request before paging.

1. **Neon first** — create the production project in `ap-southeast-1` (Singapore);
   record the pooled runtime URL and the direct migration URL in SSM SecureString
   (`/quizforge/prod/DATABASE_URL`, `.../MIGRATIONS_DATABASE_URL`).
2. **Migrate (dry)** — from a workstation with the direct URL, run
   `migrate -path database/migrations -database "$MIGRATIONS_DATABASE_URL" up`
   against a disposable branch first, then production. Record the migration version.
3. **Google** — create the OAuth client, register
   `https://quiz.example/api/v1/auth/google/callback` exactly, configure the
   consent screen; store client ID/secret in SSM.
4. **Hugging Face** — create a token with a fixed spend cap; verify
   `Qwen/Qwen3-32B` (or the ADR-chosen model) still offers strict JSON Schema
   via `/v1/models`; store the token in SSM. Default `AI_DAILY_USER_LIMIT=5`.
5. **AWS** — apply `deploy/opentofu/` (VPC decision, SG 80/443 only, `t4g.small`,
   EIP, ECR repos, SSM params, log groups, backup bucket, DNS, alarms, budgets).
   Never attach broad IAM credentials; GitHub deploys via the OIDC role.
6. **Secrets on host** — run the SSM fetch script to materialize
   `/opt/quizforge/.env` mode `0600` (names logged, values never).
7. **Push the release workflow** — push a `v*` tag (or run `workflow_dispatch`); CI builds ARM64 api/web/migrate images, scans with Trivy, pushes immutable digests to ECR. Pushes to `main` never deploy; the `production` environment requires a human approver.
8. **Deploy** — approve the `production` environment; the workflow runs
   `deploy/scripts/deploy.sh <api> <web>` over SSM (migrate → switch → readiness
   gate → digest log). Keep `previous.env` for rollback.
9. **Smoke** — `https://quiz.example/health/ready`, `/version` (expect the release
   commit), landing page, register/login, mock-generate a draft, host a live
   question with two browsers, refresh both mid-question, check history.
10. **Alarms/budgets** — confirm CloudWatch alarms and AWS Budget alerts fire
    to the operator mailbox; set the HF spend alert.

### Argon2 Calibration On The Instance Type

Do not ship guessed memory parameters: on first boot of a new instance class,
run this from the repo and record the numbers in the status ledger:

```bash
cd backend
go test ./internal/auth/ -run=NONE -bench=BenchmarkArgon2id -benchtime=5x
```

Adjust `password.go` constants only to keep verification in the 100–300 ms
band **on that hardware** without exceeding ~25% of instance RAM under an
8-parallel login burst, then re-run the full auth suite. Reference baselines:

| Hardware | Single verify | 8-parallel burst |
|---|---|---|
| Dev workstation (12 threads, 15 GiB) | ~62 ms | ~244 ms total |
| `t4g.small` (2 vCPU, 2 GiB) | **measure on instance** | **measure on instance** |

Current parameters (64 MiB, 3 iterations, parallelism 2) are the secure
baseline; never weaken them merely for faster benchmarks.

## Cost Controls And Shutdown
- Idle baseline is one `t4g.small` + ECR storage + Neon usage + HF credits;
  confirm the current figures in `docs/COST_ANALYSIS.md` before launch and set
  the AWS Budget to 120% of that baseline with an anomaly alert.
- HF free credits ($0.10/month at last check) cover smoke tests only; the app
  quota (5/user/day) plus the provider circuit breaker bound runaway spend.
- To pause spend: stop the EC2 instance (EIP retained while allocated),
  keep ECR images (lifecycle: last five), keep the Neon project (branch for
  restore), and revoke the HF token. Full teardown order: DNS → instance →
  EIP release → ECR cleanup → SSM secret deletion → Neon project removal.
  Record each step with timestamps in the status ledger.
