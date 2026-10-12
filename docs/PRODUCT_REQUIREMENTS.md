# Product Requirements

## Vision And Success

QuizForge AI helps a creator move from an idea to a trustworthy live quiz, then gives a room of participants a responsive shared competition. It is successful when the complete flow is reliable on ordinary phones and networks, generated content remains visibly reviewable, and the repository provides credible engineering evidence.

The primary product metric for implementation is **successful live sessions**: sessions that enter the first question, end normally, persist all accepted answers, and produce a final leaderboard. Supporting engineering metrics are listed in the testing and portfolio sections; no adoption target is claimed before launch.

## Roles And Authorization

| Role | Identity | May do | Must never do |
|---|---|---|---|
| Visitor | none | View marketing, register, log in, enter join code | Read private quizzes or results |
| Creator | authenticated user | CRUD own quizzes, generate, publish, host, view own history/settings | Read or mutate another owner's resources |
| Host | creator who owns the live session | Start/advance/end own session, view full results and answer keys | Control sessions owned by others |
| Participant | guest token scoped to one live session | Resume identity, receive released state, submit one answer/question | See unreleased keys, set score, advance state, impersonate host |
| Operator | infrastructure/IAM role, no app UI | Deploy, inspect health/logs under least privilege | Use application owner endpoints by default |

Every authorization decision is made by the Go service from database ownership and server-issued identity. The UI is not an authorization boundary.

## MVP Scope

### Accounts

- Register with unique case-insensitive email and username, verified password rules, and acceptance of terms/privacy placeholders.
- Login by email-or-username plus password; login by Google OIDC.
- Logout one session and logout all sessions.
- View current user and update display name/username.
- Request/reset password using single-use expiring email tokens; enumeration-safe responses.
- Link Google only after authenticated confirmation when its verified email matches an existing account.

### Quiz Authoring

- Dashboard lists owned quizzes with status, question count, updated time, and last session.
- Create a blank quiz or an AI-generated draft.
- Edit title, description, questions, 2-6 options, exactly one correct option, explanation, difficulty, timer (10-180 seconds), and scoring mode.
- Add, remove, and reorder questions; autosave explicit field changes with version checks.
- Draft, publish, unpublish, duplicate via a later enhancement, and soft-delete to trash.
- Preview uses local/sandbox state and never creates analytics.
- Publishing validates at least one complete question and produces an incremented quiz version.

### AI Generation

- Inputs: topic, easy/medium/hard, 1-20 questions (MVP UI defaults 5, production cap 10 until load/cost evidence), target audience, optional instructions up to 1,000 characters, and an allowlisted language.
- The service validates provider output against the same domain rules as manual content.
- A generated quiz is saved only after the complete response passes JSON parsing, JSON Schema, domain validation, deduplication checks, and a single transaction.
- Generated content is labeled "AI-generated, review before presenting" and remains editable.
- Quota/provider errors keep the entered brief and offer manual creation; the UI never pretends a deterministic template is AI output.

### Live Sessions

- Starting a session snapshots the published quiz and creates a unique six-character code from `23456789ABCDEFGHJKLMNPQRSTUVWXYZ`.
- Guests join with a 1-30 character display name, receive an opaque resume cookie, and wait in a lobby.
- Host sees connection state and participant counts; participant names are not shown publicly by default.
- Host starts each question; server persists `started_at` and `ends_at` and automatically closes it.
- Participant submits one answer. The first valid answer wins; subsequent attempts are acknowledged as duplicates without changing data.
- Server calculates fixed or speed score in the answer transaction and updates the participant total exactly once.
- Results reveal only after question close. Host can show leaderboard, advance, or end.
- Refresh/reconnect returns an authoritative snapshot. Backend restart reconstructs active sessions and expires overdue questions.
- Session end freezes final standings and makes creator-only history available.

## User Stories And Acceptance Criteria

| ID | Story | Acceptance criterion |
|---|---|---|
| AUTH-01 | As a creator, I register securely | Valid account returns `201`, password is Argon2id-hashed, duplicates return stable `409` codes, session cookie is issued |
| AUTH-02 | As a creator, I use Google | State/nonce/issuer/audience/signature are validated; new account or explicit link is deterministic |
| QUIZ-01 | As a creator, I author a quiz | CRUD and reorder survive reload; stale updates return `409 VERSION_CONFLICT` |
| QUIZ-02 | As a creator, I publish | Incomplete quiz gets field errors; valid snapshot increments version and becomes hostable |
| AI-01 | As a creator, I generate | Only fully validated output is inserted; provider failure creates no partial quiz |
| LIVE-01 | As a host, I start a session | Unique code and share URL are returned idempotently and lobby is durable |
| LIVE-02 | As a guest, I join | Same resume cookie rejoins the same participant; normalized duplicate names are rejected with suggestions |
| LIVE-03 | As a participant, I answer | Unreleased/late/invalid/duplicate answers cannot score; accepted response receives an event ID |
| LIVE-04 | As a room, we see results | Counts equal accepted answers and correct IDs appear only after close |
| LIVE-05 | As a user, I reconnect | Current state, remaining server time, prior answer acknowledgement, and score restore without duplication |
| OPS-01 | As an operator, I restart backend | Active sessions reload; elapsed questions close once; no accepted answer is lost |

## Feature Levels

### Level 1: Mandatory

Everything in the MVP sections above, responsive and accessible UI, Docker development/production paths, AWS deployment, observability, automated tests, OpenAPI, and operator documentation.

### Level 2: Prioritized Portfolio Enhancements

| Priority | Feature | Cost | Resume value | Reason |
|---:|---|---:|---:|---|
| 1 | Quiz duplication and drag/keyboard reorder | Low | Medium | Completes author workflow |
| 2 | QR-code join | Low | Medium | Strong demo value, little domain risk |
| 3 | True/false | Low | Low | Reuses single-answer model |
| 4 | CSV export and participation analytics | Medium | High | Demonstrates reporting and data design |
| 5 | Multiple-correct answers | Medium | High | Exercises schema, scoring, and UI |
| 6 | Individual question regeneration | Medium | High | Shows controlled AI mutation |
| 7 | Dark mode | Low/medium | Low | Polish after core accessibility |
| 8 | Language selection and AI explanations | Medium | Medium | Valuable only after evaluation coverage |
| 9 | Configurable scoring | Medium | Medium | Avoid formula proliferation early |

Keyboard navigation is not optional enhancement work: WCAG-conformant keyboard operation is MVP.

### Level 3: Evaluate Later

PDF/document ingestion, semantic review, team workspaces, image questions, public templates, version history, and multi-instance scale. Each requires a separate ADR and must not enter an MVP phase opportunistically.

## Non-Functional Requirements

- Availability target: 99.5% monthly for the single-instance portfolio deployment, explicitly excluding announced maintenance and upstream outages; this is a target, not an SLA.
- Normal authenticated REST reads: p95 under 300 ms at application boundary with warm database; writes under 500 ms.
- WebSocket event propagation: p95 under 250 ms from accepted command to connected client in the deployment region under target load.
- AI generation: 45-second provider timeout, 60-second endpoint budget, visible progress after 500 ms.
- Initial tested capacity: 200 participants/session, 10 active sessions, 1,000 open sockets per `t4g.small`; promotion target 500/session only after k6 and memory evidence.
- Accessibility: WCAG 2.2 AA, including contrast, focus, labels, status announcements, target size, reduced motion.
- Browser baseline: current and previous major Chrome, Edge, Firefox, and Safari; current iOS Safari and Android Chrome.
- Privacy: collect email for creators and display name/session events for guests; no advertising profiles or hidden AI training consent.

## Out Of Scope For MVP

Payments, organizations, native apps, asynchronous self-paced quizzes, uploaded documents, media hosting, public discovery, chat, arbitrary HTML/Markdown in quiz text, user-defined scripts, multi-region HA, Kubernetes, Redis, and more than one backend instance.

## Analytics And Retention

- Product events are first-party structured events, not raw keystrokes: registration completed, generation outcome, publish, session started/ended, join, accepted answer, reconnect.
- Do not send question/answer text, passwords, cookies, OAuth codes, participant tokens, or AI prompts to general logs/analytics.
- Ended session details are retained 365 days by default; creators may delete them earlier. Expired auth/reset/OAuth records are deleted daily. Sanitized aggregate metrics may remain.
- Account deletion is a verified, asynchronous-safe transaction: revoke sessions immediately, then cascade owned quiz/session personal data; retain only non-identifying operational logs according to log retention.

## Roadmap Guardrail

A feature may enter a phase only when its acceptance criteria, schema impact, API impact, security impact, test plan, and rollback are named. Optional work never blocks an MVP release and must not weaken the live-session invariants.
