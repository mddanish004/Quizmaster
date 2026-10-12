# Frontend Design

## Experience Direction

QuizForge uses a **live studio console** visual system. Authoring is quiet and operational: bright neutral canvas, graphite text, thin rules, compact toolbars, and dense information that rewards repeat use. Hosting and participation become theatrical through scale, decisive color fields, countdown motion, and full-viewport results, without imitating Mentimeter's layouts or palette.

This is one product at two tempos, not two design systems. Shared type, tokens, controls, and icon language keep it coherent. Avoid decorative dashboard cards, oversized marketing copy in the app, nested cards, purple-blue gradients, pill-shaped everything, and confetti/motion that obscures outcomes.

## Design Tokens

Implement Tailwind v4 theme tokens and CSS custom properties; never scatter arbitrary colors.

| Token | Light | Dark | Use |
|---|---|---|---|
| `canvas` | `#F7F8F6` | `#151716` | app background |
| `surface` | `#FFFFFF` | `#1E211F` | dialogs, repeated items, inputs |
| `ink` | `#161A18` | `#F4F6F3` | primary text |
| `muted` | `#5F6963` | `#A8B1AB` | secondary text |
| `line` | `#D9DFDB` | `#343A36` | borders/dividers |
| `brand` | `#087F5B` | `#45D39C` | primary action/focus support |
| `live` | `#FF5A4E` | `#FF7268` | live/urgent state, not general CTA |
| `score` | `#E3A008` | `#F6C453` | rank/points |
| `info` | `#1769E0` | `#70A5FF` | informational status |
| `danger` | `#C7342D` | `#FF7C73` | destructive/error |

Typography: self-host **Inter Variable** or use a system stack if font licensing/assets are not committed; `ui-monospace` for join codes and numerical timers. Display 48-72 px only in host/live presentation, never dashboard panels. App headings 24-32, section headings 18-20, body 16, compact metadata 13-14. Letter spacing stays `0`. Use tabular numerals for timer, rank, score, and counts.

Spacing uses a 4 px base with common values 4/8/12/16/24/32/48/64. Border radius: controls 6 px, cards/dialogs 8 px maximum, participant answer tiles 8 px. Shadows are limited to popovers/dialogs. Focus ring is 3 px with visible offset and contrast in both themes.

Breakpoints: `sm 640`, `md 768`, `lg 1024`, `xl 1280`, `2xl 1536`; layout decisions rely on container width as well. Minimum pointer target 44x44 CSS px. Content max widths: marketing 1200, app list 1280, auth/form 440; quiz editor is full work area.

## Navigation Model

- Public: wordmark, "Join quiz" and "Sign in"; marketing CTA "Create a quiz."
- Creator app: persistent left rail at `lg+` with Dashboard, Quizzes, Sessions, Settings; compact top bar with create and account menu. Mobile uses a top bar and modal navigation, not a crowded bottom bar because authoring is desktop-primary.
- Editor: breadcrumbs/back, title/status/save state, Preview, Publish/Start; question navigator left, canvas center, settings inspector right. Tablet collapses inspector into a sheet; mobile switches to one-panel step navigation.
- Host: no app shell. Full-viewport stage with a discreet control dock and a separate responsive presenter panel when windowed.
- Participant: no global nav; session identity and connection status are small, persistent, and never compete with answers.

## Screen Specifications

### 1. Marketing Landing

**Mode:** persuade. First viewport is a full-bleed interactive "live round" scene, not a split card hero: QuizForge AI name, literal offer ("AI-built quizzes, played live"), short proof-oriented copy, topic prompt, and a staged host/participant response field using clearly labeled synthetic demo data. A lower edge reveals the next band.

Bands: generate/review/host sequence; real product screenshots once implemented; architecture/reliability proof for portfolio audiences; feature comparison; CTA. No invented users, testimonials, or performance numbers. Loading is irrelevant for static content; demo interaction gracefully pauses under reduced motion. Mobile makes the prompt the focal action and preserves a visible next-section hint.

### 2. Sign-Up

Centered 440 px form, wordmark/back link, Google button first, divider, email/username/display name/password, terms links, submit, login link. Inline Zod errors after blur/submit, password requirements before typing only when useful, server conflict attached to field. Loading locks duplicate submit but keeps values. OAuth error returns to the form with a human message and retry.

### 3. Login

Google, identifier, password, show/hide icon with tooltip, forgot password, submit. Generic invalid-credential message; Caps Lock hint; rate-limit countdown. Never reveal whether an account is Google-only. Successful login respects a validated same-origin return path.

### 4. Dashboard

Top row: greeting at normal app scale, primary "New quiz" menu (`Blank`, `Generate with AI`). Main content is a sortable table/list of recent quizzes, then recent sessions; not a mosaic of large cards. First-use empty state offers the two create choices and one compact sample workflow, without fake content. Skeleton rows maintain dimensions. Partial failures isolate sections with retry.

### 5. AI Generator

Single focused form beside a live summary preview on wide screens, stacked on mobile. Controls: topic, segmented difficulty, question-count stepper, audience, language menu, optional instructions. Cost/quota note is factual and only shown when known. Submission switches the right side to staged progress ("Preparing brief", "Generating", "Checking structure"), not a fake percentage. Timeout/provider errors preserve inputs and offer retry/manual draft. Success enters editor with an AI-review banner and each question marked unreviewed until touched/confirmed.

### 6. Quiz Editor

Desktop three-zone workbench:

```text
+ Questions (240) +-------- Question canvas (minmax 520) --------+ Settings (300) +
| 01 prompt       | Prompt                                       | Timer          |
| 02 prompt       | [A option] [B option]                         | Scoring        |
| + Add question  | explanation / correctness                    | Difficulty     |
`-----------------+-----------------------------------------------+----------------+
```

Question rows support buttons and keyboard reorder (`Alt+Arrow` plus announced result); drag handles are additive. Option correctness uses radio buttons for single-answer, not color alone. Save states: `Saving`, `Saved`, `Offline changes`, `Conflict`; autosave does not move focus. Deletion is undoable toast for 8 seconds unless already synchronized, then confirmation for whole quiz only. Stale version opens a comparison/reload dialog, never overwrites.

Mobile shows question list, edit form, and settings as route-like panels with a sticky action bar. Inputs never sit in a horizontally squeezed desktop canvas. Long prompts wrap; option controls have stable grid tracks.

### 7. Preview

Desktop split between presenter and a phone-proportioned participant viewport with synchronized local state and reset. Mobile previews one role at a time via tabs. A clear "Preview, responses aren't saved" status appears in chrome, not over content. No real join code or session is created.

### 8. Live Host Lobby

Full viewport. Quiz name and "Waiting for players" dominate; join code in large monospaced text, copy/share and QR action, participant count, and scrolling participant roster visible to host. Start remains disabled until quiz snapshot is ready; zero participants may still start after confirmation. Connection warnings do not hide controls. Late join policy is visible. Mobile host view prioritizes code/count/start over roster.

### 9. Live Host Presentation

Question prompt occupies top/center; answers use a stable 2x2 or single-column grid based on count and never resize when submissions arrive. Timer is a large numeric/ring combination with non-color urgency. Bottom dock: previous state indicator, submission count, end question, more menu; familiar Lucide icons with tooltips. Results replace answer tiles with Recharts bars using accessible patterns/labels; correct answer has icon+label. Leaderboard is a full stage, top three emphasized without hiding ranks 4-10. Keyboard controls are documented in a help dialog, not persistent explanatory text.

### 10. Participant Join

Brand/product name is visible; one six-character segmented-looking input backed by a single accessible input, paste supported, uppercase normalized. Direct join URL skips code entry. Invalid/expired/not-yet-open states are distinct. Submit is large and thumb reachable.

### 11. Participant Waiting Room

Display-name form if not set, then a calm waiting state with quiz title, chosen name, connection indicator, participant count if enabled, and leave action. Avoid endless animation; one subtle live pulse pauses under reduced motion. Reconnect status is an inline banner with attempt and manual retry.

### 12. Participant Question

Prompt, server-synced timer, and 2-6 large answer buttons. After tap, disable alternatives only when request is in flight, show "Sending"; after ack, show "Answer received" and selected choice. If the connection drops before ack, retain submission ID and retry. A late rejection says the round closed and does not imply the answer counted. No correctness before reveal.

On 320 px width the longest allowed option wraps with at least 16 px padding and no overlap. Screen readers hear time at sensible thresholds (30, 10, 5 seconds), not every tick. Keyboard number keys may select only when focus is not in an input and the UI exposes the mapping visually.

### 13. Participant Results

Clear correct/incorrect/no-answer status using icon, heading, and color; selected and correct options; points earned; explanation; waiting-for-host status. Motion is a short state transition and respects reduced motion. It never reveals other participant selections.

### 14. Leaderboard

Top 10 with rank, display name, score delta, total; current participant is shown separately if outside top 10. Stable row heights and keyed FLIP-style rank movement under normal motion; instant update under reduced motion. Names truncate safely with full accessible label/title, and abusive control characters never render.

### 15. Session History

Filterable table by date/quiz/status, cursor pagination, row action to detail. Detail header summarizes attendance, completion, accuracy, duration; question sections use labeled bar charts and a participant table. Empty state points to hosting, not generation. CSV export appears only when implemented and reports generation failures accessibly.

### 16. User Settings

Sections, not nested cards: profile, linked login methods, password, active sessions, data/account. Destructive account deletion is last and requires recent authentication plus typed confirmation. OAuth linking explains conflict action. Save feedback is inline and toast; sensitive changes revoke/rotate sessions as specified.

### 17. Error Pages

404 offers Dashboard and Join. 403 does not reveal resource existence. 500 provides request ID and retry. Offline page distinguishes cached shell from unavailable API. Maintenance/restart keeps participant retrying with capped backoff. Copy is calm and specific; no jokes during a live failure.

## Component Architecture

```text
frontend/src/
+-- app/                 # router, providers, layouts, error boundaries
+-- components/
|   +-- ui/              # Button, IconButton, Dialog, Menu, Field, Toast
|   `-- data/            # DataTable, EmptyState, StatusBadge, ChartFrame
+-- features/
|   +-- auth/
|   +-- dashboard/
|   +-- quizzes/
|   |   +-- editor/
|   |   `-- preview/
|   +-- ai-generation/
|   +-- live-host/
|   +-- participation/
|   +-- results/
|   `-- settings/
+-- lib/                 # api client, query keys, validation, formatters
+-- generated/           # OpenAPI-generated types; no manual edits
+-- styles/              # theme and global layers
`-- test/                # fixtures, MSW, render helpers
```

Feature modules export route components and a small public API. UI primitives contain no domain fetching. Domain forms keep Zod schemas close to feature DTO adapters. Use Radix UI primitives for dialog/menu/tooltip/tabs, styled locally with Tailwind, and Lucide icons. Do not add a full themed component framework.

## State Ownership

- TanStack Query owns REST server state. Query keys: `['me']`, `['quizzes', filters]`, `['quiz', id]`, `['sessions', filters]`, `['session', id, 'results']`.
- Form state stays in React Hook Form. Draft editor normalizes to a form aggregate and submits versioned updates; avoid copying every query into global state.
- WebSocket live state lives in a feature-scoped reducer/store initialized only from versioned events/snapshots. It does not become TanStack cache until terminal results invalidate/refetch REST queries.
- URL owns filters, active dashboard tab, editor question ID, and history pagination. Secrets/tokens never enter URL or local storage.
- UI-only preferences such as theme may use local storage. Auth is learned from `/me`, never a readable token.

Query defaults: ordinary lists `staleTime` 30 seconds, quiz detail 5 seconds while editing, results 15 seconds, `retry` once for safe reads and never blindly for mutations. Invalidate precise keys after mutation. Abort fetches on route cancellation. Use optimistic updates only for reversible low-risk UI such as title/list metadata; question aggregate waits for server version.

## Forms And Feedback

- One field component binds label, description, control, and stable error ID. Required is announced, not represented only by an asterisk.
- Client validation improves speed; server validation remains authoritative and maps `fields` paths back into RHF.
- Submit errors stay near the action; global toast is supplementary. Success toasts are reserved for background/route-separated actions, not every autosave.
- Toasts pause on hover/focus, remain in an ARIA live region, and never contain the only recovery action.
- Destructive confirmation states the object and consequence. Removing one question uses undo; deleting account/quiz uses dialog.

## Loading, Empty, Error, Offline

Skeletons mirror final row/panel dimensions and appear only after a short delay to avoid flash. Keep existing data during background refresh with a subtle status. Every query boundary handles loading, empty, access denied, not found, retryable error, and offline. Route-level error boundaries report request ID and reset on navigation; a separate root boundary protects the shell. WebSocket state never replaces errors with an infinite spinner.

## Accessibility

- Semantic landmarks/headings; skip link; focus moves to route heading/dialog and returns to invoker.
- Full keyboard authoring, menus, dialogs, tabs, reorder, answers, and host controls. No drag-only interaction.
- WCAG 2.2 AA contrast; status always uses icon/text in addition to color; charts have data tables/summaries.
- Timer announcements are throttled; results use polite live regions; critical connection loss may be assertive once.
- `prefers-reduced-motion` removes rank movement, count pulses, and staged transitions without hiding content.
- Minimum target 44x44; zoom to 200%; reflow at 320 CSS px; landscape phone handling.
- Test with axe, keyboard-only, VoiceOver/Safari and NVDA/Firefox at least once per release candidate.

## Performance Budgets

- Initial route JavaScript gzip target under 200 KiB excluding lazy chunks; Recharts and editor libraries lazy-load.
- LCP under 2.5 seconds on a representative mobile profile for landing/join; interaction latency under 100 ms for local selection.
- Avoid rendering every participant; virtualize host roster/history over 100 rows.
- WebSocket event reducer is O(1) or O(log n) for common events; chart aggregation comes from server.
- Use SVG/icons and CSS for UI; no heavyweight animation library until measured need. QR library may be lazy-loaded.

## Frontend Verification

Vitest + Testing Library + MSW for components/integration; Playwright for auth/editor/live multi-context flows; `jest-axe` or `vitest-axe`; Storybook is optional, not an MVP dependency. Visual checks cover 320x568, 390x844, 768x1024, 1280x800, and 1536x864 with long titles/options, empty lists, error banners, timer at 0, top-10 names, and 200% zoom.
