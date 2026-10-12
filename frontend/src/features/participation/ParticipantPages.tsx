import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { api } from '../../lib/api'
import { Button, Field, Input } from '../../components/ui/primitives'
import { useLiveSocket } from '../live/useLiveSocket'
import { uuid } from '../live/protocol'

const codeSchema = z.object({ code: z.string().trim().length(6).regex(/^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]+$/i) })

export function JoinPage() {
  const nav = useNavigate()
  const [step, setStep] = useState<'code' | 'name'>('code')
  const [code, setCode] = useState('')
  const [serverError, setServerError] = useState('')
  const codeForm = useForm<z.infer<typeof codeSchema>>({ resolver: zodResolver(codeSchema) })
  const nameForm = useForm<z.infer<typeof nameSchema>>({ resolver: zodResolver(nameSchema) })

  const checkCode = codeForm.handleSubmit(async (v) => {
    setServerError('')
    const upper = v.code.toUpperCase()
    try {
      const meta = await api.get<{ data: { sessionId: string; joinable: boolean } }>(`/api/v1/join/${upper}`)
      if (!meta.data.joinable) {
        setServerError('That session is not joinable right now.')
        return
      }
      setCode(upper)
      setStep('name')
    } catch {
      setServerError('Code not found or expired. Check the six characters and try again.')
    }
  })

  const join = nameForm.handleSubmit(async (v) => {
    setServerError('')
    try {
      const res = await api.post<{ data: { sessionId: string } }>(`/api/v1/join/${code}`, v)
      nav(`/play/${res.data.sessionId}`)
    } catch (e) {
      const err = e as Error & { status?: number }
      if (err.status === 409) {
        setServerError('That name is taken in this session. Try another.')
      } else {
        setServerError('Could not join. Check the code and try again.')
      }
    }
  })

  if (step === 'name') {
    return (
      <main className="mx-auto w-full max-w-[440px] px-6 py-16">
        <p className="font-bold">QuizForge AI</p>
        <h1 className="mt-2 text-2xl font-bold">Pick a display name</h1>
        <form onSubmit={join} className="mt-6 flex flex-col gap-4" noValidate>
          <Field label="Display name" htmlFor="display-name" error={nameForm.formState.errors.displayName?.message}>
            <Input id="display-name" {...nameForm.register('displayName')} maxLength={30} autoComplete="nickname" />
          </Field>
          {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
          <Button type="submit" disabled={nameForm.formState.isSubmitting}>
            {nameForm.formState.isSubmitting ? 'Joining…' : 'Join quiz'}
          </Button>
        </form>
      </main>
    )
  }

  return (
    <main className="mx-auto w-full max-w-[440px] px-6 py-16">
      <p className="font-bold">QuizForge AI</p>
      <h1 className="mt-2 text-2xl font-bold">Join a quiz</h1>
      <form onSubmit={checkCode} className="mt-6 flex flex-col gap-4" noValidate>
        <Field label="Join code" htmlFor="code" error={codeForm.formState.errors.code?.message}>
          <Input
            id="code"
            {...codeForm.register('code')}
            placeholder="ABC123"
            autoComplete="off"
            autoCapitalize="characters"
            maxLength={6}
            className="mono text-center text-2xl uppercase tracking-widest"
          />
        </Field>
        {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
        <Button type="submit" disabled={codeForm.formState.isSubmitting}>
          {codeForm.formState.isSubmitting ? 'Checking…' : 'Continue'}
        </Button>
      </form>
    </main>
  )
}

const nameSchema = z.object({ displayName: z.string().trim().min(1).max(30) })

export function ParticipantPage() {
  const { sessionId = '' } = useParams()
  const [snapshotFailed, setSnapshotFailed] = useState(false)
  useEffect(() => {
    // The socket snapshot is authoritative; this REST call only decides
    // whether to show the join hint early.
    api.get(`/api/v1/play/${sessionId}/snapshot`).then(
      () => {},
      (e: Error & { status?: number }) => {
        if (e.status === 401) setSnapshotFailed(true)
      },
    )
  }, [sessionId])
  if (snapshotFailed) {
    return (
      <main className="mx-auto w-full max-w-[440px] px-6 py-16">
        <p role="alert">You haven’t joined this session yet.</p>
        <Link to="/join" className="underline">Join with a code</Link>
      </main>
    )
  }
  return <PlayRoom sessionId={sessionId} />
}

function PlayRoom({ sessionId }: { sessionId: string }) {
  const { view, send } = useLiveSocket(sessionId)
  const [sending, setSending] = useState(false)
  const [picked, setPicked] = useState<string | null>(null)
  const [submissionId] = useState(uuid())

  const snap = view.snapshot
  const endsAt = snap?.questionEndsAt ? Date.parse(snap.questionEndsAt) : null
  const now = Date.now() + view.serverOffsetMs
  const remaining = endsAt ? Math.max(0, Math.ceil((endsAt - now) / 1000)) : null

  const answer = (optionId: string) => {
    if (!snap?.question || sending || view.ack) return
    setSending(true)
    setPicked(optionId)
    send('participant.submit_answer', {
      submissionId, questionId: snap.question.id, optionIds: [optionId],
    })
    setSending(false)
  }

  return (
    <main className="mx-auto w-full max-w-[480px] px-4 py-8">
      <div className="flex items-center gap-2 text-sm text-muted" role="status" aria-label="connection">
        <span>{view.connected ? 'Connected' : view.retrying ? 'Reconnecting…' : 'Connecting…'}</span>
        {snap?.participant && <span className="ml-auto">Playing as {snap.participant.displayName}</span>}
      </div>

      {!snap && <p className="mt-8">Joining session…</p>}

      {snap?.state === 'lobby' && (
        <section aria-label="Waiting room" className="mt-8 text-center">
          <h1 className="text-2xl font-bold">You’re in!</h1>
          <p className="mt-2 text-muted">Waiting for the host to start. ({snap.participantCount} players)</p>
        </section>
      )}

      {snap?.state === 'question_open' && snap.question && (
        <section aria-label="Question" className="mt-6">
          <div className="flex items-center justify-between">
            <h1 className="text-xl font-bold">{snap.question.prompt}</h1>
            {remaining !== null && <span className="mono text-2xl" aria-label={`${remaining} seconds left`}>{remaining}s</span>}
          </div>
          <div className="mt-4 flex flex-col gap-2">
            {snap.question.options.map((o, i) => (
              <button
                key={o.id}
                onClick={() => answer(o.id)}
                disabled={sending || !!view.ack}
                aria-pressed={picked === o.id}
                aria-label={`Answer ${i + 1}: ${o.text}`}
                className={`min-h-[56px] rounded-lg border px-4 text-left text-lg ${picked === o.id ? 'border-brand bg-canvas font-semibold' : 'border-line bg-surface'}`}
              >
                {o.text}
              </button>
            ))}
          </div>
          {sending && <p role="status">Sending…</p>}
          {view.ack && <p role="status" className="mt-3 font-medium">Answer received ✓</p>}
        </section>
      )}

      {(snap?.state === 'question_results' || snap?.state === 'leaderboard') && (
        <section aria-label="Round result" className="mt-6">
          <ResultView results={view.results} ack={view.ack} />
        </section>
      )}

      {snap?.state === 'ended' && (
        <section aria-label="Session ended" className="mt-8 text-center">
          <h1 className="text-2xl font-bold">Thanks for playing!</h1>
          <LeaderboardMini leaders={view.leaders} />
        </section>
      )}

      {view.error && <p role="alert" className="mt-4 rounded border border-danger p-3 text-sm">{view.error}</p>}
    </main>
  )
}

function ResultView({ results, ack }: { results: Record<string, unknown> | null; ack: { submissionId: string; outcome: string } | null }) {
  if (!results) return <p>Waiting for results…</p>
  const own = results['ownResult'] as { isCorrect?: boolean; points?: number; noAnswer?: boolean } | undefined
  return (
    <div>
      {own?.noAnswer ? (
        <h1 className="text-2xl font-bold">No answer recorded</h1>
      ) : own?.isCorrect ? (
        <h1 className="text-2xl font-bold">✓ Correct! +{own?.points ?? 0}</h1>
      ) : (
        <h1 className="text-2xl font-bold">✗ Not quite{typeof own?.points === 'number' && own.points > 0 ? ` +${own.points}` : ''}</h1>
      )}
      {typeof results['explanation'] === 'string' && results['explanation'] ? (
        <p className="mt-2 text-muted">{results['explanation'] as string}</p>
      ) : null}
      {!ack && <p className="mt-2 text-sm text-muted">Waiting for the host…</p>}
    </div>
  )
}

function LeaderboardMini({ leaders }: { leaders: { displayName: string; rank: number; score: number }[] }) {
  if (leaders.length === 0) return null
  return (
    <ol className="mt-4 flex flex-col gap-1 text-left" aria-label="Leaderboard">
      {leaders.slice(0, 10).map((l) => (
        <li key={l.displayName + l.rank} className="flex gap-3 rounded border border-line bg-surface px-3 py-2">
          <span className="mono">#{l.rank}</span>
          <span className="flex-1 truncate">{l.displayName}</span>
          <span className="mono">{l.score}</span>
        </li>
      ))}
    </ol>
  )
}
