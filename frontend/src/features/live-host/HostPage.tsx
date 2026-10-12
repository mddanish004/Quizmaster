import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { useLiveSocket } from '../live/useLiveSocket'
import { Button } from '../../components/ui/primitives'

type HostSession = {
  id: string
  joinCode: string
  joinUrl: string
  status: string
  stateVersion: number
  participantCount: number
  questionCount: number
  currentQuestionPosition: number | null
  questionEndsAt: string | null
}

export function HostPage() {
  const { sessionId = '' } = useParams()
  const nav = useNavigate()
  const [session, setSession] = useState<HostSession | null>(null)
  const [loadError, setLoadError] = useState('')
  const { view, send } = useLiveSocket(sessionId)

  useEffect(() => {
    api.get<{ data: HostSession }>(`/api/v1/live-sessions/${sessionId}`).then(
      (r) => setSession(r.data),
      () => setLoadError('Could not load session.'),
    )
  }, [sessionId])

  useEffect(() => {
    if (view.snapshot) {
      setSession((s) => (s ? { ...s, status: view.snapshot!.state, stateVersion: view.snapshot!.stateVersion, participantCount: view.snapshot!.participantCount } : s))
    }
  }, [view.snapshot])

  if (loadError) {
    return (
      <main className="p-8">
        <p role="alert">{loadError}</p>
        <button onClick={() => nav('/app/sessions')} className="underline">Back to sessions</button>
      </main>
    )
  }
  if (!session) return <main className="p-8">Loading host console…</main>

  const version = view.snapshot?.stateVersion ?? session.stateVersion
  const state = view.snapshot?.state ?? session.status
  const count = view.snapshot?.participantCount ?? session.participantCount

  const command = (type: string, payload: Record<string, unknown> = {}) => {
    send(type, payload, version)
  }

  return (
    <main className="min-h-screen bg-ink text-white">
      <div className="mx-auto max-w-[1200px] px-6 py-8">
        <div className="flex items-center gap-3 text-sm">
          <span role="status" aria-label="connection">
            {view.connected ? '● Live' : view.retrying ? '● Reconnecting…' : '● Connecting…'}
          </span>
          <span className="ml-auto">v{version}</span>
        </div>

        {state === 'lobby' && (
          <section aria-label="Lobby" className="mt-8 text-center">
            <h1 className="text-4xl font-bold">Waiting for players</h1>
            <p className="mono mt-6 text-7xl font-bold tracking-widest" aria-label={`Join code ${session.joinCode}`}>
              {session.joinCode}
            </p>
            <div className="mt-4 flex justify-center gap-2">
              <button
                onClick={() => navigator.clipboard?.writeText(session.joinUrl)}
                className="inline-flex min-h-[44px] items-center rounded-md border border-white/30 px-4"
              >
                Copy join link
              </button>
            </div>
            <p className="mt-6 text-xl">{count} player{count === 1 ? '' : 's'} joined</p>
            <div className="mt-8 flex justify-center gap-2">
              <Button onClick={() => command('host.start_question', { questionPosition: 0 })}>Start question 1</Button>
              <button onClick={() => command('host.end_session')} className="inline-flex min-h-[44px] items-center rounded-md border border-white/30 px-4">
                End session
              </button>
            </div>
          </section>
        )}

        {state === 'question_open' && (
          <section aria-label="Live question" className="mt-8">
            <h1 className="text-3xl font-bold">{view.snapshot?.question?.prompt ?? 'Question running…'}</h1>
            <div className="mt-4 grid gap-2 sm:grid-cols-2">
              {(view.snapshot?.question?.options ?? []).map((o) => (
                <div key={o.id} className="rounded-lg border border-white/20 p-4 text-lg">{o.text}</div>
              ))}
            </div>
            <div className="mt-8 flex gap-2">
              <Button onClick={() => command('host.end_question')}>End question</Button>
              <button onClick={() => command('host.end_session')} className="inline-flex min-h-[44px] items-center rounded-md border border-white/30 px-4">
                End session
              </button>
            </div>
          </section>
        )}

        {(state === 'question_results' || state === 'leaderboard') && (
          <section aria-label="Results" className="mt-8">
            <h1 className="text-3xl font-bold">Results</h1>
            <QuestionResults payload={view.results} />
            <Leaderboard leaders={view.leaders} />
            <div className="mt-6 flex gap-2">
              <Button onClick={() => command('host.start_question', { questionPosition: nextPosition(view) })}>Next question</Button>
              <button onClick={() => command('host.show_leaderboard')} className="inline-flex min-h-[44px] items-center rounded-md border border-white/30 px-4">
                Leaderboard
              </button>
              <button onClick={() => command('host.end_session')} className="inline-flex min-h-[44px] items-center rounded-md border border-white/30 px-4">
                End session
              </button>
            </div>
          </section>
        )}

        {state === 'ended' && (
          <section aria-label="Ended" className="mt-8 text-center">
            <h1 className="text-4xl font-bold">Session ended</h1>
            <Leaderboard leaders={view.leaders} />
            <button onClick={() => nav('/app/sessions')} className="mt-6 underline">View history</button>
          </section>
        )}

        {view.error && <p role="alert" className="mt-4 rounded bg-danger p-3">{view.error}</p>}
      </div>
    </main>
  )
}

function nextPosition(view: ReturnType<typeof useLiveSocket>['view']): number {
  const q = view.snapshot?.question
  return (q?.position ?? -1) + 1
}

function QuestionResults({ payload }: { payload: Record<string, unknown> | null }) {
  if (!payload) return <p className="mt-4 text-white/70">Waiting for results…</p>
  const options = (payload['options'] ?? []) as { id: string; count: number; percentage: number }[]
  const correct = new Set((payload['correctOptionIds'] ?? []) as string[])
  const max = Math.max(1, ...options.map((o) => o.count))
  return (
    <div className="mt-4 flex flex-col gap-2">
      {options.map((o) => (
        <div key={o.id} className="flex items-center gap-3">
          <span aria-hidden>{correct.has(o.id) ? '✓' : ''}</span>
          <div className="h-6 flex-1 rounded bg-white/10">
            <div className="h-6 rounded bg-score" style={{ width: `${(o.count / max) * 100}%` }} />
          </div>
          <span className="mono w-24 text-right">{o.count} · {o.percentage.toFixed(0)}%</span>
          {correct.has(o.id) && <span className="sr-only">(correct)</span>}
        </div>
      ))}
      {payload['explanation'] ? <p className="mt-2 text-white/80">{String(payload['explanation'])}</p> : null}
    </div>
  )
}

function Leaderboard({ leaders }: { leaders: { displayName: string; rank: number; score: number }[] }) {
  if (leaders.length === 0) return null
  return (
    <ol className="mt-6 flex flex-col gap-1" aria-label="Leaderboard">
      {leaders.slice(0, 10).map((l) => (
        <li key={l.displayName + l.rank} className="flex items-center gap-3 rounded bg-white/5 px-4 py-2">
          <span className="mono w-8">#{l.rank}</span>
          <span className="flex-1 truncate" title={l.displayName}>{l.displayName}</span>
          <span className="mono">{l.score}</span>
        </li>
      ))}
    </ol>
  )
}
