# Cost And Hosting Analysis

**Pricing research date:** 2026-10-10; revalidated 2026-10-11 (t4g.small from
~$0.0168/hr cheapest region, Singapore ~$0.0212/hr → ~$15.48/mo stands; T4g trial
extension to 2026-12-31 reconfirmed, account-dependent, not budgeted).
All currency is USD, before tax, exchange fees, support plans, and domain registration. Prices and free programs change; run AWS Pricing Calculator and inspect account-specific offers immediately before provisioning.

## Assumptions

- Production region `ap-southeast-1` (Singapore), colocated with Neon's Singapore region; target users primarily India/Asia.
- Month = 730 hours. One ARM64 Linux instance, 20 GiB gp3, one public IPv4, one hosted zone, approximately 1 GiB container storage, under 10 GiB internet egress, under 1 GiB logs/month.
- Very low: 100 creator visits, 10 live sessions, 50 participants/session/month.
- Moderate portfolio: 2,000 visits, 100 live sessions, average 100 participants, 10 questions, roughly 20 WebSocket messages/participant plus broadcasts.
- Burst: one or a few sessions reach 200-500 simultaneous participants for under two hours; capacity must be measured before promising 500.
- Neon data under 1 GiB initially; AI usage is low and capped. No paid support.

## Option A: Single EC2 Container Host

Topology: `t4g.small` (2 vCPU, 2 GiB), Caddy + Go containers, external Neon/HF/Google/SES.

### Strengths

- Native persistent WebSockets and per-session goroutines.
- Same process owns timers, connection queues, and broadcasts; PostgreSQL provides restart recovery.
- One multi-stage backend image and simple Compose deployment.
- Predictable base bill and straightforward profiling.
- No API Gateway message/connection accounting or callback API.

### Costs And Operations

ARM64 requires multi-arch-compatible dependencies. Two GiB is enough only with bounded Argon2/AI concurrency and measured socket memory. The developer owns OS patches, Docker updates, disk/CPU alarms, instance recovery, and deployment rollback. It is a single availability-zone failure domain.

Estimated monthly base, based on current public pricing snapshots:

| Item | Estimate | Basis |
|---|---:|---|
| EC2 `t4g.small` | $15.48 | $0.0212/hour x 730; verify in calculator |
| 20 GiB gp3 | $1.92 | regional estimate; AWS's public gp3 example is $0.08/GB-month in its cited region ([EBS](https://aws.amazon.com/ebs/general-purpose/)) |
| one public IPv4 | $3.65 | $0.005/hour ([VPC pricing](https://aws.amazon.com/vpc/pricing/)) |
| Route 53 hosted zone + low queries | ~$0.54 | $0.50/zone and $0.40/million standard queries ([Route 53](https://aws.amazon.com/route53/pricing/)) |
| ECR, S3 backup, CloudWatch | $0.25-$4 | usage estimate, retention-sensitive |
| Data transfer | $0-$2 | depends on current allowance and geography |
| **AWS subtotal** | **about $22-$28** | no credits |

The official T4g page currently advertises a `t4g.small` free trial of up to 750 hours/month through 2026-12-31, but eligibility and region are account-dependent and it expires: [T4g](https://aws.amazon.com/ec2/instance-types/t4/). Do not use it as the steady-state estimate.

## Option B: Lambda + API Gateway

REST uses HTTP API/Lambda. Real time requires API Gateway WebSocket routes (`$connect`, `$disconnect`, commands), Lambda authorizers/handlers, a durable connection registry, and server-initiated calls through the management API. Timers need EventBridge Scheduler/Step Functions or delayed durable work; presence/broadcast requires fan-out; state lives in PostgreSQL/DynamoDB/Redis rather than process memory.

### Strengths

- Near-zero compute charge at genuinely idle traffic.
- Per-event scaling and managed compute patching.
- Lambda includes 1 million requests and 400,000 GB-seconds/month in the documented free tier; account conditions apply ([Lambda pricing](https://aws.amazon.com/lambda/pricing/)).
- API Gateway documents free allowances for new eligible accounts and charges by messages plus connection minutes ([API Gateway pricing](https://aws.amazon.com/api-gateway/pricing/)).

### Weaknesses For This Product

- Cannot retain actors/sockets/timers between invocations; the central live design must be rewritten as distributed handlers.
- More services, IAM policies, failure modes, local emulation, logs, and costs to explain.
- Cold starts affect connect/command paths; Neon connections require careful pooling.
- API Gateway WebSockets have a 2-hour maximum connection duration and 10-minute idle timeout, with 32 KiB frames/128 KiB messages ([quotas](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-execution-service-websocket-limits-table.html)).
- Every server broadcast becomes management API calls/messages; concurrency is cheap but orchestration complexity is not.
- Containerization remains possible for Lambda images but no longer resembles the local long-running Go server.

At the stated traffic, Lambda + Gateway usage could remain under a few dollars and eligible free allowances, excluding observability/data services. A representative US East public example prices WebSocket messages at $1/million and connection time at $0.25/million minutes; Singapore rates must be checked. Low cloud spend does not offset the development/operational cost for a portfolio whose core is persistent live interaction.

## Option C: Hybrid Static Frontend + Long-Running Backend

Host the Vite build on S3/CloudFront and keep Go/WebSockets on EC2. This reduces static bandwidth/CPU and improves global caching, but adds a deployment target, CSP/CORS/cookie considerations (unless carefully placed behind one domain), certificates/distributions, invalidation, and little savings at portfolio traffic. A second hybrid that places REST in Lambda while sockets stay on EC2 splits auth/domain code and is rejected.

The S3/CloudFront frontend is a sensible later optimization when measured static traffic or global latency warrants it. It is not the initial recommendation.

## Decision

**Select Option A: one `t4g.small` EC2 instance in Singapore plus Neon Singapore.** Reliable real-time behavior and low total engineering complexity matter more than eliminating a roughly $22-$28 AWS base bill. The application architecture, Docker development path, and production runtime remain nearly identical. Vertically resize for a planned event; add Redis/ALB/replicas only after load evidence.

## Scenario Estimates

| Scenario | AWS | Neon | Hugging Face | Other | Estimated total |
|---|---:|---:|---:|---:|---:|
| Local development/testing | $0 | $0 on eligible free project | $0.10 credit then capped | $0 | $0 unless quotas exceeded |
| Very low production | $22-$28 | $0-$5 | $0-$5 cap | domain ~$1-$2/mo equivalent | **$23-$40** |
| Moderate portfolio | $25-$35 | $5-$20 | $5-$20 cap | domain/SES | **$36-$77** |
| Planned burst, resized `t4g.medium` for month | $38-$50 | $10-$30 | unchanged by participants | domain/SES | **$49-$102** |

These are planning ranges, not quotes. Neon's current free plan public material states 100 projects, 100 CU-hours/project/month, up to 2 CU and 1 GiB storage/project, but the limits changed repeatedly in 2025-2026. See the latest [Neon free-plan announcement](https://neon.com/blog/neon-free-plan-1-gb-per-project) and pricing console before relying on it. Paid usage currently has no advertised minimum in Neon's pricing explanation, but exact compute/storage rates require the live pricing page/account.

Hugging Face's free user credit is only $0.10/month and subject to change ([official pricing](https://huggingface.co/docs/inference-providers/pricing)). The provider/model determines pay-as-you-go cost. Configure a hard application quota and HF spending cap; manually creating quizzes remains the zero-cost fallback.

## Staging Resource Inventory (Stage 5 Gate A.2 — `deploy/opentofu/`)

One environment (`staging`) in `ap-southeast-1`. Estimates are USD/mo before
credits/tax; the T4g trial (to 2026-12-31, account-dependent) may zero the
compute line until expiry, after which the same table is the steady state.

| AWS service | Resources | Est. mo. | Trial / free assumption | Security purpose | Cleanup |
|---|---|---:|---|---|---|
| EC2 `t4g.small` (730 h) | 1 | $15.48 | trial may cover to 2026-12-31 | single AZ host; IMDSv2, no SSH key | `tofu destroy` (stop first to pause: keep EIP) |
| EBS gp3 20 GiB + EIP | 1 + 1 | $1.92 + $3.65 | none (always billed while allocated) | encrypted root; stable IP for DNS/TLS | released/deleted by destroy; **confirm EIP release** |
| ECR (api/web/migrate) | 3 repos, ~1 GiB | $0.10–0.50 | first 500 MB-mo free (verify) | immutable + scanned images, keep-5 | lifecycle auto-expires; delete repos last |
| CloudWatch logs (14 d) + metrics/alarms | 3 groups, 2 alarms | $0.10–1 | basic metrics free | audit trail, status/CPU alarms | log groups deleted by destroy |
| S3 backups (versioned, 14 d) | 1 bucket, <5 GiB | $0.10–0.50 | none assumed | encrypted off-site `pg_dump` | **empty versions first**, then destroy |
| Route 53 zone + A record | 0–1 zone | $0–0.54 | only if domain configured | exact-FQDN TLS + OAuth redirect | delete record before zone |
| SNS + Budgets | 1 topic, 1 budget | $0 | free tiers | spend/ anomaly alerts | destroy removes |
| SSM parameters (8 Strings/SecureStrings) | 8 | $0 | standard params free | secret storage, no TF values | delete path recursively |
| Data transfer | <10 GiB egress | $0–2 | allowance varies | — | — |
| **AWS subtotal** | | **$22–28** | | | |
| Neon staging branch | 1 | $0–5 | free-project limits | isolated data, TLS pooled/direct | delete branch |
| Hugging Face capped token | 1 | $0–5 | $0.10 credit, then cap | quotas + circuit breaker | revoke token |
| Domain (optional) | 1 | ~$1–2 equivalent | registrar pricing | identity for TLS/OAuth | let lapse / delete zone |

Teardown order: DNS record → instance (stop) → EIP release → ECR cleanup →
SSM deletion → Neon branch removal → bucket empty+delete. Full steps in
`docs/AWS_SETUP_GUIDE.md` §13.

## AWS Free Tier Warning

AWS changed its program on 2025-07-15. New customers may receive $100 at signup and earn up to $100 more; free-plan duration and service access differ, credits expire, and joining an Organization can affect eligibility. Legacy accounts follow different offers. Sources: [AWS announcement](https://aws.amazon.com/blogs/aws/aws-free-tier-update-new-customers-can-get-started-and-explore-aws-with-up-to-200-in-credits/) and [Free Tier FAQ](https://aws.amazon.com/free/free-tier-faqs/). Budget the no-credit amount.

## Charge Controls

- AWS Budget alerts at $10, $25, $50 and forecast threshold; Cost Anomaly Detection.
- One explicit public IPv4; delete unattached volumes, snapshots, addresses, test instances, NAT gateways, and load balancers.
- CloudWatch retention 14-30 days, sampled info logs, alarms on ingestion.
- ECR lifecycle retains latest five releases; S3 backup lifecycle 14 days then delete.
- Neon max autoscaling/usage limits, scale-to-zero for dev, usage alerting; production decision on autosuspend based on wake latency.
- HF application/provider hard caps and no automatic unbounded fallback.
- Tag all AWS resources `Project=QuizForge`, `Environment`, `ManagedBy`.

## Performance-Cost Decision Points

Resize only when CPU credit balance, memory, connection queue, event p95, or load tests show pressure. Move static assets only when they materially affect egress/LCP. Introduce ALB+Redis+second instance only when downtime/scale requirements justify roughly doubled compute plus load-balancer/Redis cost and operational work.
