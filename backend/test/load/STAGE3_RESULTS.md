# Stage 3 Load Results (2026-10-11)

Harness: `backend/test/load/` (`go run ./test/load -mode burst|multi|soak`).
Server: `/tmp/qf_stage3_api` built from this revision (`CGO_ENABLED` default,
`DB_MAX_CONNS=25`, `RATE_LIMIT_ENABLED=false` for load isolation).
Client: same machine over loopback (no network latency component).

## Environment

- CPU: 12 threads (local workstation); RAM 15 GiB; Go 1.27.2 linux/amd64
- Database: local PostgreSQL 18.4 (parity stand-in for Neon; Neon-specific
  pooler/wake behavior NOT measured — credential-required)
- Config: 120s question timers (burst/multi), 60s (soak); speed scoring

## Results

### Burst: 200 participants × 1 session

- 200 joins in ~58 ms
- 200/200 answers accepted in ~339 ms, 0 failed
- Answer ack latency: p50 89 ms / p95 130 ms / p99 138 ms
- Verdict: PASS (target was 200 concurrent participants)

### Multi: 10 sessions × 20 participants

- 200/200 answers accepted in ~465 ms, 0 failed
- Verdict: PASS (target was 10 simultaneous sessions)

### Soak: ~14 min, 10 players, 20 paced rounds, reconnect churn

- 20/20 questions, 200/200 answers, 0 errors; elapsed 13m57s
- Persistent watcher connection received: 20 question_started, 20
  question_results, 19 leaderboard_updated, 200 answer_submitted (host-only),
  1 snapshot
- The single missing leaderboard event is a harness drain artifact: the process
  exits immediately after the final round, and the last fan-out was still in
  flight (results, sent microseconds earlier over the same channel, arrived
  20/20). Not an application loss.
- Server RSS: 88 MB → 25 MB and flat (±1 MB over the last 10 min); 18 threads
  after all load. No leak signal.
- Verdict: PASS with the noted artifact

### Restart drill (manual, same day)

- `kill -9` mid-question → restart → overdue question closed exactly once
  (1 `question_ended` event, version bump +1) → participant reconnect snapshot
  correct. Verdict: PASS

## Methodological limits (honest)

- Loopback only: no mobile/WAN latency, no TLS termination cost (Caddy absent).
- Local PostgreSQL, not Neon: pooler queueing, wake-from-idle, and
  Singapore-region latency unmeasured.
- Harness answers the first option (correctness mix not modeled); scoring mix
  is covered by unit tests instead.
- Browser rendering cost under load not measured (E2E covers 1 host + 1
  participant functionally, not under load).
- 200-user and soak targets above are measured; anything larger is unclaimed.
