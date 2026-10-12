import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import { EmptyState, Skeleton } from '../../components/ui/primitives'

type SessionSummary = {
  id: string
  quizTitle: string
  status: string
  participantCount: number
  questionCount: number
  createdAt: string
  endedAt: string | null
}

export function HistoryPage() {
  const history = useQuery({
    queryKey: queryKeys.sessions('all'),
    queryFn: () => api.get<{ data: SessionSummary[]; page: { hasMore: boolean } }>('/api/v1/sessions'),
  })
  return (
    <main className="mx-auto max-w-[1280px] px-6 py-8">
      <h1 className="text-2xl font-bold">Sessions</h1>
      <section aria-label="Session history" className="mt-6">
        {history.isPending && <Skeleton className="h-32" />}
        {history.isError && (
          <div role="alert" className="rounded-md border border-danger p-4">
            Could not load history. <button onClick={() => history.refetch()} className="underline">Retry</button>
          </div>
        )}
        {history.data && history.data.data.length === 0 && (
          <EmptyState title="No sessions yet — publish a quiz and start a live session." />
        )}
        {history.data && history.data.data.length > 0 && (
          <ul className="divide-y divide-line rounded-lg border border-line bg-surface">
            {history.data.data.map((s) => (
              <li key={s.id} className="flex items-center justify-between px-4 py-3">
                <div>
                  <Link to={`/app/sessions/${s.id}`} className="font-medium underline-offset-2 hover:underline">
                    {s.quizTitle}
                  </Link>
                  <p className="text-sm text-muted">
                    {s.status} · {s.participantCount} players · {s.questionCount} questions · {new Date(s.createdAt).toLocaleString()}
                  </p>
                </div>
                <span className="text-sm text-muted">{s.endedAt ? 'ended' : 'active'}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  )
}

type SessionDetail = {
  session: SessionSummary
  participants: { id: string; displayName: string; score: number; joinedAt: string }[]
  questions: {
    id: string
    position: number
    prompt: string
    correctOptionIds: string[]
    options: { id: string; text: string; count: number; percentage: number }[]
    answerCount: number
    accuracy: number
  }[]
}

export function SessionDetailPage({ sessionId }: { sessionId: string }) {
  const detail = useQuery({
    queryKey: queryKeys.sessionResults(sessionId),
    queryFn: () => api.get<{ data: SessionDetail }>(`/api/v1/sessions/${sessionId}`),
    staleTime: 15_000,
  })
  if (detail.isPending) return <main className="p-8">Loading results…</main>
  if (detail.isError) {
    return (
      <main className="p-8">
        <p role="alert">Could not load session results.</p>
        <Link to="/app/sessions" className="underline">Back to sessions</Link>
      </main>
    )
  }
  const d = detail.data.data
  const ranked = [...d.participants].sort((a, b) => b.score - a.score)
  return (
    <main className="mx-auto max-w-[1000px] px-6 py-8">
      <Link to="/app/sessions" className="underline">← Sessions</Link>
      <h1 className="mt-2 text-2xl font-bold">{d.session.quizTitle}</h1>
      <p className="text-sm text-muted">
        {d.session.participantCount} players · {d.session.questionCount} questions · {d.session.status}
      </p>
      <section aria-label="Participants" className="mt-6">
        <h2 className="text-lg font-semibold">Leaderboard</h2>
        <ol className="mt-2 divide-y divide-line rounded-lg border border-line bg-surface">
          {ranked.map((p, i) => (
            <li key={p.id} className="flex gap-3 px-4 py-2">
              <span className="mono w-8">#{i + 1}</span>
              <span className="flex-1 truncate" title={p.displayName}>{p.displayName}</span>
              <span className="mono">{p.score}</span>
            </li>
          ))}
        </ol>
      </section>
      {d.questions.map((q) => {
        const max = Math.max(1, ...q.options.map((o) => o.count))
        return (
          <section key={q.id} aria-label={`Question ${q.position + 1}`} className="mt-6 rounded-lg border border-line bg-surface p-4">
            <h2 className="font-semibold">Q{q.position + 1}: {q.prompt}</h2>
            <p className="text-sm text-muted">{q.answerCount} answers · {q.accuracy.toFixed(0)}% correct</p>
            <div className="mt-2 flex flex-col gap-1" role="img" aria-label={`Answer distribution for question ${q.position + 1}`}>
              {q.options.map((o) => (
                <div key={o.id} className="flex items-center gap-2 text-sm">
                  <span className="w-40 truncate" title={o.text}>{o.text}{q.correctOptionIds.includes(o.id) ? ' ✓' : ''}</span>
                  <div className="h-4 flex-1 rounded bg-canvas">
                    <div className="h-4 rounded bg-brand" style={{ width: `${(o.count / max) * 100}%` }} />
                  </div>
                  <span className="mono w-20 text-right">{o.count} · {o.percentage.toFixed(0)}%</span>
                </div>
              ))}
            </div>
            <table className="sr-only">
              <caption>Answer counts for question {q.position + 1}</caption>
              <tbody>
                {q.options.map((o) => (
                  <tr key={o.id}><td>{o.text}</td><td>{o.count}</td><td>{o.percentage.toFixed(1)} percent</td></tr>
                ))}
              </tbody>
            </table>
          </section>
        )
      })}
    </main>
  )
}
