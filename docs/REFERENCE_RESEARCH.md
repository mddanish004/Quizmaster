# Reference Research

**Research date:** 2026-10-10  
**Primary reference:** [Mentimeter AI Quiz Generator](https://www.mentimeter.com/features/ai-quiz-generator)

## Method And Limitations

Research used public Mentimeter marketing pages and official help-center articles. The reference page returned meaningful server-rendered text but also displayed a JavaScript-required notice. The authenticated editor, dashboard, generated output quality, private analytics views, and end-to-end account onboarding were not accessed with an account. Consequently, exact layouts, transitions, API behavior, internal limits, reconnect algorithms, and authorization design are not treated as verified. Public documentation sometimes changes independently of cached pricing copy; plan limits must be rechecked before using them in comparisons.

## Verified Reference Features

The public AI page confirms topic/prompt generation, pasted or uploaded material, multiple-choice, true/false and open-ended output, full editability, code-based joining from any device without participant accounts, live scores, and live leaderboards. It currently states a free-plan limit of 50 participants per month. See the [AI Quiz Generator page](https://www.mentimeter.com/features/ai-quiz-generator).

The official creation guide confirms a dashboard-to-editor flow, starting from blank, import, or AI, plus multiple interactive/content slide families and presentation mode. See [Create an interactive Menti](https://help.mentimeter.com/en/articles/375437-create-your-first-presentation).

The official AI-assistant guide confirms a chat-based assistant inside the editor that can create a presentation, modify existing material, and work from uploaded files; generated work lands in the standard editor for review. See [Create and edit Mentis with the AI assistant](https://help.mentimeter.com/en/articles/16730234-create-and-edit-mentis-with-the-ai-assistant).

Quiz Competition documentation confirms:

- select-answer and typed-answer quiz slides;
- configured correct answers and alternate text answers;
- fixed scoring or time-based scoring where faster correct answers receive 500-1000 points;
- per-question time limits, optional music, and top-10 leaderboard slides;
- a lobby, participant nicknames/avatars, live countdown, automatic result reveal, accumulated scores, and a documented 2,000-player maximum;
- device recognition that can restore a prior nickname/avatar.

Sources: [Create a Quiz Competition](https://help.mentimeter.com/en/articles/410463-how-to-create-a-quiz-competition), [Host a Quiz Competition](https://help.mentimeter.com/en/articles/4305015-how-to-host-the-quiz-competition), and [Participate in a Quiz Competition](https://help.mentimeter.com/en/articles/2968253-participating-in-a-quiz-competition).

Participation documentation verifies join by direct link, QR code, or temporary numeric code on phone, tablet, or computer. Preview mode provides separate presenter/audience views and test votes without storing results, but the documented preview is desktop-only. Sources: [How to participate](https://help.mentimeter.com/en/articles/410537-how-to-participate-in-a-menti) and [Preview your Mentis](https://help.mentimeter.com/en/articles/5784859-preview-your-mentis).

Result documentation confirms shareable result views plus paid PDF and Excel exports. See [Share results after the Menti](https://help.mentimeter.com/en/articles/410572-share-your-results-after-the-menti).

## Assumed Or Inferred, Not Verified

- The exact AI input controls for question count, difficulty, audience, language, and prompt revision were not confirmed in the unauthenticated experience.
- Drag-and-drop question reordering is plausible from the editor category but was not directly verified.
- Real-time delivery appears to use WebSockets because the host troubleshooting article names a WebSocket endpoint, but the wire protocol and persistence model are private.
- Automatic host reconnection, backend restart recovery, and precise late-answer semantics were not documented.
- Draft/publish status, quiz versioning, REST APIs, storage schema, and session-token design are implementation details not visible from public pages.
- Mobile responsiveness is verified for participation, but not every creator/editor workflow.
- Authentication options beyond visible email/Google entry and references to Microsoft/SSO in other contexts were not exhaustively tested.

## Product Lessons

The engaging core is not AI alone. It is the short loop of lobby anticipation, a clear deadline, immediate personal acknowledgement, collective reveal, and periodic leaderboard movement. The editor must keep AI content ordinary and editable; the live product must keep answers and timing extraordinary and trustworthy. QuizForge will preserve that category logic while using original language, visual design, data model, and scoring implementation.

## Feature Comparison

| Capability | Reference status | QuizForge MVP | Later |
|---|---|---:|---:|
| Topic-to-quiz AI generation | Verified | Yes | Improve evaluation |
| Generate from files | Verified | No | Optional advanced |
| Editable generated content | Verified | Yes | - |
| Manual quiz creation | Verified category/editor behavior | Yes | - |
| Single-answer multiple choice | Verified | Yes | - |
| True/false | Verified | No | Level 2 |
| Typed/open answer | Verified | No | Optional |
| Multiple-correct | Not verified on reference page | No | Level 2 |
| Configurable timer | Verified | Yes | - |
| Fixed and speed scoring | Verified | Yes | More formulas later |
| Live lobby and nickname | Verified | Yes | Avatars later |
| Join code/link | Verified | Yes | - |
| QR join | Verified | No | Level 2, low cost |
| Guest play without account | Verified | Yes | - |
| Correct/incorrect feedback | Verified | Yes | - |
| Live results | Verified | Yes | Advanced charts later |
| Leaderboard | Verified | Yes | - |
| Preview without stored votes | Verified | Yes | - |
| Quiz templates | Verified | No | Optional |
| Duplicate/reuse | Public reuse implied, exact behavior not verified | No | Level 2 |
| Session history/analytics | Result views verified | Basic history | Rich analytics Level 2 |
| CSV/Excel export | Excel verified | No | CSV Level 2 |
| Host refresh recovery | Not publicly verified | Yes | - |
| Backend restart recovery | Not publicly verified | Yes | - |
| AI provenance/usage records | Not publicly verified | Yes | - |

## Original Additions

- Durable, inspectable live state with explicit recovery guarantees.
- Per-generation model/provider/latency/token/cost metadata without storing secrets.
- Optimistic concurrency on quiz editing and host commands.
- Clear separation between generated draft, human-edited quiz, and immutable live snapshot.
- Public portfolio evidence: OpenAPI contract, state-machine docs, k6 results, threat model, and incident runbooks.
- Accessibility-first participant feedback and reduced-motion presentation mode.

## Source Notes

All links above are first-party Mentimeter pages. The product UI is dynamic and account-dependent; the research intentionally does not reproduce screenshots or visual assets. "Verified" means a behavior was explicitly described by those public sources as of the research date, not that its implementation was reverse engineered.

## External Constraint Source Register

The implementation decisions also use these first-party sources, all checked on the research date:

- Hugging Face [Inference Providers pricing](https://huggingface.co/docs/inference-providers/pricing), [chat completion](https://huggingface.co/docs/inference-providers/tasks/chat-completion), and [structured outputs](https://huggingface.co/docs/inference-providers/guides/structured-output).
- Neon [current free-plan announcement](https://neon.com/blog/neon-free-plan-1-gb-per-project), [compute/pooling behavior](https://neon.com/docs/manage/endpoints/), and [Singapore regional latency listing](https://neon.com/demos/regional-latency).
- AWS [EC2 On-Demand pricing](https://aws.amazon.com/ec2/pricing/on-demand/), [T4g specifications/trial](https://aws.amazon.com/ec2/instance-types/t4/), [Lambda pricing](https://aws.amazon.com/lambda/pricing/), [API Gateway pricing](https://aws.amazon.com/api-gateway/pricing/), [WebSocket quotas](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-execution-service-websocket-limits-table.html), [IPv4 pricing](https://aws.amazon.com/vpc/pricing/), [Route 53 pricing](https://aws.amazon.com/route53/pricing/), and [new Free Tier program](https://aws.amazon.com/free/free-tier-faqs/).
- Google [web-server OAuth flow](https://developers.google.com/identity/protocols/oauth2/web-server), [OIDC reference](https://developers.google.com/identity/openid-connect/reference), and [app audience/testing rules](https://support.google.com/cloud/answer/15549945).
- Runtime/framework sources: [Go releases](https://go.dev/doc/devel/release), [React versions](https://react.dev/versions), [Vite releases](https://vite.dev/blog), [Tailwind releases](https://tailwindcss.com/blog), [TanStack Query docs](https://tanstack.com/query/latest/docs/framework/react/reference), and [PostgreSQL version policy](https://www.postgresql.org/support/versioning/).

Where a live price table was dynamic or the user's account determines eligibility, this package labels the number as an estimate and requires revalidation rather than treating search output as a quote.
