export function LandingPage() {
  return (
    <main className="mx-auto max-w-[1200px] px-6 py-16">
      <p className="text-sm font-semibold uppercase tracking-wide text-brand">QuizForge AI</p>
      <h1 className="mt-3 max-w-2xl text-5xl font-bold leading-tight">AI-built quizzes, played live.</h1>
      <p className="mt-4 max-w-xl text-lg text-muted">
        Turn a topic into an editable quiz, host it with server-timed questions, and let guests join from
        their phones with a six-character code.
      </p>
      <div className="mt-8 flex gap-3">
        <a href="/app" className="inline-flex min-h-[44px] items-center rounded-md bg-brand px-5 py-2 font-semibold text-white">
          Create a quiz
        </a>
        <a href="/join" className="inline-flex min-h-[44px] items-center rounded-md border border-line bg-surface px-5 py-2 font-semibold">
          Join quiz
        </a>
      </div>
      <section aria-label="How it works" className="mt-16 grid gap-4 md:grid-cols-3">
        {[
          ['Generate', 'Describe a topic and get a structured, editable draft.'],
          ['Review', 'Check every answer and explanation before publishing.'],
          ['Host', 'Run timed questions with live scores and recovery.'],
        ].map(([t, d]) => (
          <div key={t} className="rounded-lg border border-line bg-surface p-6">
            <h2 className="text-lg font-semibold">{t}</h2>
            <p className="mt-2 text-muted">{d}</p>
          </div>
        ))}
      </section>
    </main>
  )
}
