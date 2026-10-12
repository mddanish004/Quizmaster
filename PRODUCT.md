# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

React with TypeScript, Vite, Tailwind CSS, React Router, TanStack Query, React Hook Form, Zod, Recharts, and WebSockets; Go modular-monolith backend; Neon PostgreSQL; Hugging Face Inference Providers; Docker; AWS; GitHub Actions. The requester explicitly selected this stack.

## Users

- Quiz creators: registered educators, trainers, interviewers, team leads, and developers who create or generate quizzes and review results.
- Hosts: a creator operating a synchronous session from a desktop or projected display.
- Participants: account-free guests answering from a phone, tablet, or desktop, often under time pressure and on unreliable networks.
- Portfolio reviewers: engineers and hiring teams evaluating the implementation, system design, tests, security, and deployment evidence.

## Product Purpose

QuizForge AI is a production-minded portfolio SaaS that turns a topic and audience brief into an editable quiz, then runs that quiz as a synchronized, server-authoritative live competition. Success means the complete create, host, join, answer, score, recover, and analyze flow works reliably and is understandable in a technical interview.

## Positioning

The product combines inspectable AI generation with a deliberately modest, durable real-time architecture: generated content is always schema-validated and editable, while live state and scoring are persisted in PostgreSQL and recover after browser or process restarts.

## Operating Context

Creators work in a focused desktop editor and dashboard. Hosts typically present to a room or remote call. Participants use a second screen, most commonly a phone. Development is incremental by one solo developer, local services are containerized, Neon and AI inference remain managed services, and production runs within a modest AWS budget.

## Capabilities and Constraints

- Password and Google authentication are mandatory.
- AI quiz generation uses Hugging Face first but must degrade honestly when quota or service is unavailable.
- Multiple-choice is the MVP question type; true/false and multiple-correct are prioritized follow-ups.
- Real-time participation, six-character join codes, guest identity, timers, scoring, leaderboards, reconnect, and durable recovery are core.
- Go is the main backend language. React and Tailwind are required. Neon is the primary database.
- Use a modular monolith. Do not introduce Kubernetes, Kafka, microservices, or Redis in the MVP.
- All custom application services are Dockerized. Secrets never enter source control or images.
- The product is inspired by the category, but must not copy Mentimeter branding, proprietary assets, code, or exact layouts.

## Brand Commitments

The provisional name is QuizForge AI. The voice is direct, encouraging, and precise rather than childish or corporate. The experience should feel like a capable live studio: calm while authoring, energetic during play, and credible in portfolio review.

## Evidence on Hand

There are no customer claims, adoption metrics, proprietary assets, or verified performance benchmarks. Future work must label demonstration data as synthetic and replace metric placeholders only with measured results.

## Product Principles

- Human review is the boundary between AI output and published content.
- The server owns time, answers, state transitions, and scores.
- Recovery is a normal path, not an exceptional feature.
- Make the common live workflow fast and obvious before adding question formats.
- Prefer evidence and operational clarity over architecture theater.

## Accessibility & Inclusion

Target WCAG 2.2 AA. All creator workflows must support keyboard navigation; live participant controls require large targets, clear focus, non-color status cues, reduced-motion support, and screen-reader announcements that do not expose correct answers early.
