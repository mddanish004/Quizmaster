# Cost And Hosting Analysis (Render)

**Pricing research date:** 2026-10-10 (AWS era); Render figures must be
confirmed on the live pricing page before launch — platform prices change.
All currency is USD, before tax. The estimates below are planning ranges,
not quotes.

## Topology

One Render web service (**free tier** — no card; `starter` only if you want
no-sleep) running the single Docker image, plus managed Neon Postgres.
No EC2/ALB/NAT/Redis/CloudWatch/S3/backup-bucket to operate; Render provides
TLS, logs, and deploy history. Neon point-in-time restore is the first-line
backup (verify the plan's window); `deploy/scripts/backup.sh` covers manual
dumps.

## Assumptions

- Target users primarily India/Asia; Neon region adjacent to the Render
  region where possible.
- Very low: 100 creator visits, 10 live sessions, 50 participants/session/month.
- Moderate portfolio: 2,000 visits, 100 live sessions, ~100 participants avg,
  10 questions, ~20 WebSocket messages/participant plus broadcasts.
- Burst: one or a few sessions reach 200–500 simultaneous participants for
  under two hours; capacity must be measured before promising 500.
- Neon data under 1 GiB initially; AI usage low and capped. No paid support.

## Monthly Estimates

| Scenario | Render | Neon | Hugging Face | Other | Estimated total |
|---|---:|---:|---:|---:|---:|
| Local dev/test | $0 | $0 on eligible free project | $0.10 credit then capped | $0 | $0 unless quotas exceeded |
| Very low production | $0 (free tier, sleeps when idle) | $0–5 | $0–5 cap | domain ~$1–2/mo equivalent | **~$1–12** |
| Moderate portfolio | $0–7 (free, or starter for no-sleep) | $5–20 | $5–20 cap | domain | **~$6–49** |

SMTP relays (Resend/Postmark/Gmail-class) typically have a free tier covering
password-reset volumes; budget only if volume outgrows it.

Hugging Face's free user credit is small and subject to change; the provider
model determines pay-as-you-go cost. The app quotas (5 generations/user/day,
circuit breaker, no unbounded fallback) bound spend, and manual quiz authoring
is the zero-cost fallback.

## Charge Controls

- Render plan alerts + spend limit where offered; confirm instance plan before
  a planned burst (vertical resize is the first lever).
- Neon compute/storage limits and usage alerts; decide autosuspend vs
  always-on from measured wake latency.
- HF token spend cap + application quotas; revoke/rotate on anomaly.
- Tag nothing — there is no cloud inventory to tag. Record the Render service,
  Neon project/branch, and token owner in the status ledger instead.

## Scale Decision Points

Resize only when CPU/memory, connection queue, event p95, or load tests show
pressure. Split the frontend to a CDN/static host only when measured static
traffic or global latency warrants it (adds CORS/cookie complexity — needs an
ADR). Add a second instance/Redis/ALB only when downtime/scale requirements
justify roughly doubled compute plus the operational work — in-process live
actors cannot simply be replicated.
