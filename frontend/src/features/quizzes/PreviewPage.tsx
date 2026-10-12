import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import type { Quiz } from './types'

// Local-only simulator: uses the persisted preview document, creates no
// join code, participant, answer, event, or analytics record.
export function PreviewPage() {
  const { id = '' } = useParams()
  const [role, setRole] = useState<'host' | 'participant'>('host')
  const [qi, setQi] = useState(0)
  const [picked, setPicked] = useState<number | null>(null)
  const preview = useQuery({
    queryKey: [...queryKeys.quiz(id), 'preview'],
    queryFn: () => api.get<{ data: { quiz: Quiz; persistedVersion: number; responsesSaved: boolean } }>(`/api/v1/quizzes/${id}/preview`),
  })

  if (preview.isPending) return <main className="p-8">Loading preview…</main>
  if (preview.isError) {
    return (
      <main className="p-8">
        <p role="alert">This draft cannot be simulated yet — add a complete question first.</p>
        <Link to={`/app/quizzes/${id}`} className="underline">Back to editor</Link>
      </main>
    )
  }
  const quiz = preview.data.data.quiz
  const current = quiz.questions[qi]
  return (
    <main className="mx-auto max-w-[1200px] px-4 py-6">
      <div className="flex items-center gap-3">
        <Link to={`/app/quizzes/${id}`} className="underline">← Editor</Link>
        <span role="status" className="rounded border border-line px-2 py-1 text-xs">Preview — responses aren’t saved</span>
        <div role="tablist" aria-label="Role" className="ml-auto flex gap-1 rounded-md border border-line p-1">
          {(['host', 'participant'] as const).map((r) => (
            <button key={r} role="tab" aria-selected={role === r} onClick={() => { setRole(r); setPicked(null) }} className={`min-h-[44px] rounded px-4 text-sm font-medium ${role === r ? 'bg-ink text-white' : ''}`}>
              {r === 'host' ? 'Presenter' : 'Participant'}
            </button>
          ))}
        </div>
      </div>
      {!current ? (
        <p className="mt-6 text-muted">No questions in this draft yet.</p>
      ) : role === 'host' ? (
        <section aria-label="Presenter preview" className="mt-6 rounded-lg border border-line bg-surface p-6">
          <h1 className="text-2xl font-bold">{current.prompt}</h1>
          <div className="mt-4 grid gap-2 sm:grid-cols-2">
            {current.options.map((o, i) => (
              <div key={o.id ?? i} className="rounded-md border border-line p-4">
                {o.text || `(option ${i + 1})`} {o.isCorrect && <span aria-label="correct">✓</span>}
              </div>
            ))}
          </div>
          <p className="mt-3 text-sm text-muted">Explanation: {current.explanation || '—'}</p>
          <div className="mt-4 flex gap-2">
            <button disabled={qi === 0} onClick={() => setQi(qi - 1)} className="min-h-[44px] rounded-md border border-line px-4 disabled:opacity-40">Previous</button>
            <button disabled={qi >= quiz.questions.length - 1} onClick={() => setQi(qi + 1)} className="min-h-[44px] rounded-md border border-line px-4 disabled:opacity-40">Next</button>
          </div>
        </section>
      ) : (
        <section aria-label="Participant preview" className="mx-auto mt-6 max-w-[420px] rounded-lg border border-line bg-surface p-6">
          <h1 className="text-xl font-bold">{current.prompt}</h1>
          <div className="mt-4 flex flex-col gap-2">
            {current.options.map((o, i) => (
              <button
                key={o.id ?? i}
                onClick={() => setPicked(i)}
                aria-pressed={picked === i}
                className={`min-h-[56px] rounded-lg border px-4 text-left ${picked === i ? 'border-brand bg-canvas font-semibold' : 'border-line'}`}
              >
                {o.text || `(option ${i + 1})`}
              </button>
            ))}
          </div>
          {picked !== null && <p role="status" className="mt-3 text-sm text-muted">Preview choice recorded locally only.</p>}
          <div className="mt-4 flex gap-2">
            <button disabled={qi === 0} onClick={() => { setQi(qi - 1); setPicked(null) }} className="min-h-[44px] rounded-md border border-line px-4 disabled:opacity-40">Previous</button>
            <button disabled={qi >= quiz.questions.length - 1} onClick={() => { setQi(qi + 1); setPicked(null) }} className="min-h-[44px] rounded-md border border-line px-4 disabled:opacity-40">Next</button>
          </div>
        </section>
      )}
    </main>
  )
}
