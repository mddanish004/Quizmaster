# REST API Specification

## Protocol Rules

- Base path `/api/v1`; JSON UTF-8; timestamps RFC 3339 UTC; UUIDs lowercase canonical.
- Same-origin cookies authenticate creators and participants. Mutating creator endpoints require `X-CSRF-Token` and allowed `Origin`. Guest join is origin/rate-limit protected; subsequent participant REST mutations require participant cookie plus CSRF if used.
- Request bodies reject unknown fields and default to 1 MiB; AI instructions have lower field limits. `Content-Type: application/json` is mandatory where a body exists.
- `Idempotency-Key` (UUID, max 64 characters) is required for AI generation and live-session creation, supported for join. The same key with different body returns `409 IDEMPOTENCY_MISMATCH`.
- Resources include `version`; mutable quiz and host commands require `If-Match: "<version>"` or body `expectedVersion` where specified.

## Error Envelope

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "Some fields need attention.",
    "requestId": "01J...",
    "fields": {"questions.0.options": ["must contain 2 to 6 options"]},
    "retryable": false,
    "retryAfterSeconds": null
  }
}
```

Stable codes include `VALIDATION_FAILED`, `AUTH_REQUIRED`, `INVALID_CREDENTIALS`, `CSRF_FAILED`, `FORBIDDEN`, `NOT_FOUND`, `CONFLICT`, `VERSION_CONFLICT`, `RATE_LIMITED`, `AI_UNAVAILABLE`, `AI_INVALID_OUTPUT`, `SESSION_NOT_JOINABLE`, `INVALID_STATE`, `QUESTION_CLOSED`, and `INTERNAL`. Do not put stack/database/provider messages in responses.

## Common Schemas

```ts
type QuizSummary = {
  id: string; title: string; description: string;
  status: "draft" | "published" | "archived";
  questionCount: number; version: number;
  createdAt: string; updatedAt: string; publishedAt: string | null;
};

type Question = {
  id: string; type: "multiple_choice"; position: number;
  prompt: string; explanation: string;
  difficulty: "easy" | "medium" | "hard";
  timeLimitSeconds: number; scoringMode: "fixed" | "speed";
  basePoints: number;
  options: {id: string; position: number; text: string; isCorrect: boolean}[];
};
```

Creator quiz responses include `isCorrect`; participant resources do not until results are released.

## Endpoint Matrix

| Method | Path | Auth | Purpose / main responses |
|---|---|---|---|
| `POST` | `/auth/register` | public | `201` user + CSRF; `409` duplicate |
| `POST` | `/auth/login` | public | `200` user + rotated cookie; generic `401` |
| `POST` | `/auth/logout` | creator + CSRF | `204` revoke current |
| `POST` | `/auth/logout-all` | creator + CSRF | `204` revoke all |
| `GET` | `/auth/google/start` | public | `302` OIDC |
| `GET` | `/auth/google/callback` | OAuth transaction | `303`; safe error redirect |
| `POST` | `/auth/google/link/start` | creator + CSRF | `200` authorization URL for explicit link flow |
| `GET` | `/auth/google/link/callback` | creator + OAuth transaction | `303`; link or safe conflict |
| `POST` | `/auth/password/forgot` | public | always `202` |
| `POST` | `/auth/password/reset` | reset token | `204` and revoke all sessions |
| `GET` | `/me` | creator | `200` current user and CSRF token |
| `PATCH` | `/me` | creator + CSRF | `200` updated user |
| `GET` | `/quizzes` | creator | paginated owned quiz summaries |
| `POST` | `/quizzes` | creator + CSRF | `201` blank draft |
| `GET` | `/quizzes/{quizId}` | owner | full creator quiz |
| `GET` | `/quizzes/{quizId}/preview` | owner | persisted preview document; no writes |
| `PATCH` | `/quizzes/{quizId}` | owner + CSRF | replace changed metadata/questions; `200` |
| `DELETE` | `/quizzes/{quizId}` | owner + CSRF | `204` soft-delete; active session unaffected |
| `POST` | `/quizzes/{quizId}/publish` | owner + CSRF | `200`; validates complete quiz |
| `POST` | `/quizzes/{quizId}/unpublish` | owner + CSRF | `200` draft |
| `POST` | `/ai/generations` | creator + CSRF | `201` draft quiz; `422/429/503` |
| `GET` | `/ai/generations/{id}` | requester | generation metadata, no raw secret data |
| `POST` | `/quizzes/{quizId}/live-sessions` | owner + CSRF | `201` lobby session snapshot |
| `GET` | `/live-sessions/{sessionId}` | host | host snapshot |
| `POST` | `/live-sessions/{sessionId}/commands` | host + CSRF | REST fallback/control command |
| `GET` | `/join/{code}` | public | public joinability metadata, no question/key |
| `POST` | `/join/{code}` | public | `201/200` participant and resume cookie |
| `GET` | `/play/{sessionId}/snapshot` | participant | role-filtered recovery snapshot |
| `GET` | `/live-sessions/{sessionId}/results` | host/owner | complete results after/while hosting |
| `GET` | `/sessions` | creator | filtered, ordered, paginated history |
| `GET` | `/sessions/{sessionId}` | owner | analytics details |
| `GET` | `/health/live` | public | process liveness |
| `GET` | `/health/ready` | public | readiness, minimal response |
| `GET` | `/version` | public | build commit/version, no environment secrets |

## Detailed Contracts

### Register

`POST /api/v1/auth/register`

```json
{
  "email": "ada@example.com",
  "username": "ada_l",
  "displayName": "Ada",
  "password": "correct horse battery staple"
}
```

Email maximum 254 and normalized; username `[A-Za-z0-9_]`, 3-30; display name 1-80 after trim; password 12-128 Unicode code points, checked against a denylist/common-password policy but no arbitrary symbol rule. `201` returns `{ "data": {"user": {...}, "csrfToken": "..."} }` and sets `__Host-qf_session`. Duplicate fields use `409 IDENTITY_UNAVAILABLE` without disclosing more than a registration user already attempted.

### Login

`POST /auth/login` with `{ "identifier": "ada@example.com", "password": "..." }`. `200` matches register response. Unknown, wrong, locked, and Google-only accounts use a carefully chosen non-enumerating `401 INVALID_CREDENTIALS`; recovery remains available. Apply IP and identifier-keyed token buckets.

### List Quizzes

`GET /quizzes?status=draft&query=go&sort=-updatedAt&pageSize=20&cursor=<opaque>`

`pageSize` 1-50. Cursor is a signed/base64url tuple of sort value and ID; invalid cursor is `400`. Allowed sorts: `updatedAt`, `createdAt`, `title`, prefixed `-` descending. Response:

```json
{"data":[{"id":"...","title":"Go Basics","status":"draft","questionCount":5,"version":3,"updatedAt":"..."}],"page":{"nextCursor":"...","hasMore":true}}
```

### Create And Update Quiz

`POST /quizzes` body `{ "title": "Untitled quiz", "description": "" }` returns `201` version 1.

`PATCH /quizzes/{id}` requires `If-Match: "3"` and accepts one atomic document:

```json
{
  "title": "Go Basics",
  "description": "A warm-up for junior Go developers",
  "questions": [{
    "id": "optional-existing-uuid",
    "type": "multiple_choice",
    "position": 0,
    "prompt": "Which keyword declares a variable?",
    "explanation": "var declares variables.",
    "difficulty": "easy",
    "timeLimitSeconds": 30,
    "scoringMode": "speed",
    "basePoints": 1000,
    "options": [
      {"id":"optional","position":0,"text":"let","isCorrect":false},
      {"id":"optional","position":1,"text":"var","isCorrect":true}
    ]
  }]
}
```

MVP limits: 0-50 draft questions, 2-6 options/question, contiguous positions, exactly one correct when complete. `200` returns new version; stale version returns `409 VERSION_CONFLICT` with `details.currentVersion`, never silently merges. Autosave debounces 600 ms and sends the full editor aggregate to simplify ordering consistency.

### Publish

`POST /quizzes/{id}/publish`, `If-Match`. Requires 1-50 fully valid questions. Returns updated quiz. If already published with same version it is idempotent. Editing a published quiz automatically changes it to draft in the same update; existing sessions retain snapshots.

### Preview

`GET /quizzes/{id}/preview` returns the current owner-visible draft transformed to the presentation shapes used by the local simulator, including correct answers because this is creator-only. It performs no writes and creates no join code, participant, answer, event, or analytics record. It returns `400` with field errors when the persisted draft cannot be simulated. The frontend may preview unsaved local edits directly; this endpoint verifies the saved version.

### Generate Quiz

`POST /ai/generations`, `Idempotency-Key` required:

```json
{
  "topic": "Go concurrency fundamentals",
  "difficulty": "medium",
  "questionCount": 5,
  "targetAudience": "Developers with six months of Go",
  "additionalInstructions": "Avoid trick questions.",
  "language": "en"
}
```

`201` returns `{ "data": {"generation": {"id":"...","status":"succeeded","model":"..."}, "quiz": <full creator quiz>} }`. `422 AI_INVALID_OUTPUT` means both bounded attempts failed validation; `429` includes retry time for app quota/provider limit; `503` is retryable outage. A failure record exists but no quiz rows do.

### Start Live Session

`POST /quizzes/{id}/live-sessions`, `Idempotency-Key`, body:

```json
{"showLeaderboardAfterEachQuestion":true,"allowLateJoin":true}
```

Requires owned published quiz. Returns `201`:

```json
{"data":{"id":"...","joinCode":"7KM4QX","joinUrl":"https://quiz.example/join/7KM4QX","status":"lobby","stateVersion":1,"participantCount":0,"questionCount":5}}
```

### Inspect And Join

`GET /join/{code}` returns only `{sessionId, status, joinable, title, participantCount}`. It does not return questions, owner details, or correct answers.

`POST /join/{code}` body `{ "displayName": "Ada" }`. Normalize NFKC, trim/collapse whitespace, reject control characters and 1-30 length. Profanity moderation is not an MVP dependency; hosts can remove participants later. A matching valid participant cookie returns `200` existing identity. New join returns `201`, sets `__Host-qf_participant`, and returns participant plus WebSocket URL. Duplicate normalized name returns `409 NAME_TAKEN` with generated neutral suggestions.

### Host Command REST Fallback

`POST /live-sessions/{id}/commands`:

```json
{"commandId":"uuid","type":"start_question","expectedVersion":4,"payload":{"questionPosition":0}}
```

Commands: `start_question`, `end_question`, `show_leaderboard`, `end_session`. Normal operation uses WebSocket; REST exists for retryable host control and testing. Same command ID returns same ack.

### Results

Host result response includes each snapshot question, option counts/percentages, correct option IDs, answer/accuracy/average-response metrics, participants and score/rank. Participant snapshots receive only their own answer/score plus aggregate counts after reveal. History is creator-only even if the join code is known.

## Correct-Answer Embargo

Participant join metadata, lobby snapshots, `question_started`, and open-question recovery payloads omit `isCorrect`, correct IDs, explanations that reveal answers, per-option counts, and other participants' selections. Correct IDs and explanation appear in participant `question_results` only after the persisted close transition. Creator editor/host channels may receive keys because ownership is revalidated.

## Caching

Authenticated and participant responses use `Cache-Control: no-store`. Static hashed assets are immutable one year. Public join metadata is `no-store`. Health/version may cache at most seconds. ETags are resource versions, not shared-cache permission.

## OpenAPI Maintenance

`docs/openapi.yaml` is OpenAPI 3.1 and source-controlled. CI runs Spectral lint, validates examples, generates TypeScript types with `openapi-typescript`, and fails on an uncommitted generated diff. Backend `httptest` contract tests cover every operation/status/security rule. A breaking change requires `/v2` or a documented compatibility window; additive fields are allowed but clients must ignore unknown response fields.
