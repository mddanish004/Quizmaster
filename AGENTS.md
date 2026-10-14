# OpenCode Engineering Rules

These rules are mandatory for every implementation agent working in this repository.

## Read Before Work

1. Read this file, `README.md`, `PRODUCT.md`, `docs/IMPLEMENTATION_STATUS.md`, and the specification documents relevant to the current phase before changing code.
2. Read existing code, migrations, tests, generated contracts, and recent decisions before editing. The specification is authoritative until a recorded ADR intentionally changes it.
3. Work on one coherent phase or explicitly bounded checklist item at a time. Do not begin a dependent phase while its prerequisite gate is open.

## Architecture Constraints

- Keep a Go modular monolith, React/Vite frontend, Neon PostgreSQL, Groq AI provider, REST resources, and WebSocket live protocol.
- Use Chi, `pgx`, and `sqlc` unless a new accepted ADR changes the choice.
- PostgreSQL is the durable authority. In-memory actors/timers/caches may accelerate behavior but must recover from persisted state.
- Use opaque database-backed cookie sessions. Do not substitute JWTs, Firebase/Auth0, or another auth system.
- Do not add microservices, Kubernetes, Kafka, Redis, a second database, or paid SaaS without measured need and an accepted ADR.
- Preserve the API and event contracts. Any intentional change updates OpenAPI, affected docs, generated types, tests, and ADR/status in the same change.

## Implementation Discipline

- Write working production code, not pseudocode, placeholders, empty handlers, TODO-only functions, or mocked core flows.
- Use environment variables through typed validated configuration. Never hardcode or commit credentials, tokens, production URLs, `.env`, private keys, or user data.
- Prefer standard library and current repository patterns. Add a dependency only when it removes meaningful risk/complexity; record why in the phase notes.
- Keep handlers thin, business rules in services/domain packages, SQL in query/migration files, and external providers behind interfaces.
- Use parameterized queries and explicit transactions. Maintain forward and down migrations; never edit a merged migration.
- Treat client IDs, timers, correctness, ownership, roles, and scores as untrusted. Enforce all authority server-side.
- Never expose correct answers through participant endpoints/events before the persisted result state.
- Preserve unrelated user changes. Keep changes scoped and update tests beside functionality.
- Format and lint generated and handwritten code according to repository scripts. Do not manually edit generated `sqlc` or OpenAPI TypeScript files.

## Test And Evidence Rules

- Run the smallest relevant tests during development and the full phase gate before marking a checklist item complete.
- Fix failing checks; do not disable, skip, weaken, or rewrite assertions merely to obtain green output without explaining the behavior change.
- Never claim a test, migration, image, deployment, external integration, or performance target passed unless it was executed and its output was observed for the current revision.
- Classify evidence in `docs/IMPLEMENTATION_STATUS.md` as:
  - **verified:** executed against the stated revision/environment;
  - **mocked:** behavior verified with a local fake/mock only;
  - **credential-required:** implemented but not tested with the real external account;
  - **not implemented:** no working end-to-end behavior yet;
  - **blocked:** exact blocker and required authority/input are named.
- External credentials unavailable: use a faithful mock and test failure behavior, mark real smoke test `credential-required`, and never fabricate success.
- Deployment is verified only after health/version, migration version, logs, and a core smoke flow succeed against the deployed URL.

## Phase Completion Gate

Before proceeding to the next phase:

1. Complete every mandatory task and acceptance criterion for the phase in `docs/IMPLEMENTATION_PLAN.md` or mark an accurate blocker.
2. Run required format, lint, unit, integration, contract, security, build, and E2E commands for that phase.
3. Review `git diff` for secrets, generated noise, unrelated edits, error handling, and documentation drift.
4. Apply all new migrations to a blank database and test supported rollback/up path.
5. Update OpenAPI/generated types and `docs/DECISIONS.md` for changed contracts/decisions.
6. Update `README.md` and operational docs when behavior or commands changed.
7. Update `docs/IMPLEMENTATION_STATUS.md` with commit/worktree revision, date, exact commands/results, verified/mocked/credential-required behavior, remaining risks, and next phase.
8. Leave the repository runnable. A phase with known failing required checks is not complete.

## Live And AI Invariants

- A WebSocket event is broadcast only after its database mutation commits.
- Answer acknowledgement occurs only after commit. Duplicate submission returns the original outcome and never scores twice.
- Timers use persisted server timestamps; browser timers are display only.
- Host and participant reconnect from an authoritative role-filtered snapshot.
- AI output reaches quiz tables only after parsing, schema validation, domain validation, and one successful transaction.
- Provider content, cookies, tokens, answer selections/keys, passwords, and reset/OAuth codes never enter general logs.

## Git And Documentation

- Make small coherent commits only when asked; never rewrite user history or discard unrelated changes.
- Decision changes require an ADR entry with status, rationale, rejected alternatives, and consequences.
- Keep the visible checklist honest. Checked means implemented and verified at the stated level.
- Report blockers and incomplete work directly. Never use "production-ready" for unverified deployment or load/security behavior.

## Prohibited Shortcuts

- No core functionality hidden behind a feature flag permanently.
- No in-memory-only users, quizzes, scores, sessions, or timers.
- No client-calculated score or client-selected receipt timestamp.
- No wildcard production CORS/origins, readable auth tokens in local storage, or secrets in Docker build arguments.
- No `panic` for expected request/provider/database errors.
- No broad IAM credentials in GitHub; use OIDC and least privilege.
- No benchmark/adoption claims without a stored, reproducible artifact.
