import { useEffect, useRef, useState } from 'react'
import { useParams, Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import { useToast } from '../../components/ui/toast'
import { Button } from '../../components/ui/primitives'
import { blankQuestion, renumberQuestions, type Question, type Quiz } from './types'

type SaveState = 'saved' | 'saving' | 'conflict' | 'error' | 'offline'

export function EditorPage() {
  const { id = '' } = useParams()
  const [search] = useSearchParams()
  const generated = search.get('generated') === '1'
  const mockGen = search.get('mock') === '1'
  const nav = useNavigate()
  const qc = useQueryClient()
  const toast = useToast()
  const quizQuery = useQuery({
    queryKey: queryKeys.quiz(id),
    queryFn: () => api.get<{ data: Quiz }>(`/api/v1/quizzes/${id}`),
    staleTime: 5_000,
  })

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [questions, setQuestions] = useState<Question[]>([])
  const [version, setVersion] = useState(1)
  const [status, setStatus] = useState('draft')
  const [active, setActive] = useState(0)
  const [saveState, setSaveState] = useState<SaveState>('saved')
  const [serverVersion, setServerVersion] = useState(1)
  const [initialized, setInitialized] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const snapshot = useRef('')

  useEffect(() => {
    if (quizQuery.data && !initialized) {
      const q = quizQuery.data.data
      setTitle(q.title)
      setDescription(q.description)
      setQuestions(q.questions)
      setVersion(q.version)
      setServerVersion(q.version)
      setStatus(q.status)
      snapshot.current = JSON.stringify([q.title, q.description, q.questions])
      setInitialized(true)
    }
  }, [quizQuery.data, initialized])

  const dirty = snapshot.current !== '' && snapshot.current !== JSON.stringify([title, description, questions])

  useEffect(() => {
    if (!initialized || !dirty || saveState === 'conflict') return
    setSaveState('saving')
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(async () => {
      try {
        const res = await api.patch<{ data: Quiz }>(
          `/api/v1/quizzes/${id}`,
          { title, description, questions },
          { 'If-Match': `"${version}"` },
        )
        setVersion(res.data.version)
        setServerVersion(res.data.version)
        setStatus(res.data.status)
        snapshot.current = JSON.stringify([title, description, questions])
        setSaveState('saved')
        qc.invalidateQueries({ queryKey: ['quizzes'] })
      } catch (e) {
        const err = e as Error & { status?: number; body?: { error?: { code?: string; details?: { currentVersion?: number } } } }
        if (err.status === 409) {
          setServerVersion(err.body?.error?.details?.currentVersion ?? version)
          setSaveState('conflict')
        } else {
          setSaveState(navigator.onLine === false ? 'offline' : 'error')
        }
      }
    }, 600)
    return () => {
      if (timer.current) clearTimeout(timer.current)
    }
  }, [title, description, questions, initialized, dirty, saveState, id, version, qc])

  const reloadServer = async () => {
    const res = await api.get<{ data: Quiz }>(`/api/v1/quizzes/${id}`)
    const q = res.data
    setTitle(q.title)
    setDescription(q.description)
    setQuestions(q.questions)
    setVersion(q.version)
    setServerVersion(q.version)
    setStatus(q.status)
    snapshot.current = JSON.stringify([q.title, q.description, q.questions])
    setSaveState('saved')
    setInitialized(true)
  }

  const publish = async () => {
    try {
      const res = await api.post<{ data: Quiz }>(`/api/v1/quizzes/${id}/publish`, undefined, { 'If-Match': `"${version}"` })
      setVersion(res.data.version)
      setStatus(res.data.status)
      toast('Quiz published.')
    } catch {
      toast('Publish failed — every question needs 2-6 options and exactly one correct answer.')
    }
  }

  const startLive = async () => {
    try {
      const res = await api.post<{ data: { id: string } }>(
        `/api/v1/quizzes/${id}/live-sessions`,
        { allowLateJoin: true, showLeaderboardAfterEachQuestion: true },
        { 'Idempotency-Key': crypto.randomUUID() },
      )
      nav(`/app/host/${res.data.id}`)
    } catch {
      toast('Could not start a live session. Publish the quiz first.')
    }
  }

  const unpublish = async () => {
    try {
      const res = await api.post<{ data: Quiz }>(`/api/v1/quizzes/${id}/unpublish`, undefined, { 'If-Match': `"${version}"` })
      setVersion(res.data.version)
      setStatus(res.data.status)
    } catch {
      toast('Could not unpublish.')
    }
  }

  if (quizQuery.isPending) return <main className="p-8">Loading editor…</main>
  if (quizQuery.isError) {
    return (
      <main className="p-8">
        <p role="alert">Could not load quiz.</p>
        <Link to="/app/quizzes" className="underline">Back to quizzes</Link>
      </main>
    )
  }

  const q = questions[active]

  const updateActive = (fn: (q: Question) => Question) => {
    setQuestions((qs) => qs.map((item, i) => (i === active ? fn(item) : item)))
  }

  const move = (from: number, dir: -1 | 1) => {
    const to = from + dir
    if (to < 0 || to >= questions.length) return
    const next = [...questions]
    const [item] = next.splice(from, 1)
    next.splice(to, 0, item)
    setQuestions(renumberQuestions(next))
    setActive(to)
  }

  const onKeyReorder = (e: React.KeyboardEvent, index: number) => {
    if (e.altKey && e.key === 'ArrowUp') {
      e.preventDefault()
      move(index, -1)
    }
    if (e.altKey && e.key === 'ArrowDown') {
      e.preventDefault()
      move(index, 1)
    }
  }

  return (
    <main className="mx-auto max-w-[1400px] px-4 py-6">
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/app/quizzes" className="underline">← Quizzes</Link>
        <span aria-label="status" className="rounded border border-line px-2 py-1 text-xs uppercase">{status}</span>
        <span role="status" className="text-sm text-muted">
          {saveState === 'saved' && 'Saved'}
          {saveState === 'saving' && 'Saving…'}
          {saveState === 'error' && 'Save failed — retrying on next edit'}
          {saveState === 'offline' && 'Offline changes — will save when reconnected'}
          {saveState === 'conflict' && `Conflict — server is at v${serverVersion}`}
        </span>
        <div className="ml-auto flex gap-2">
          <Link to={`/app/quizzes/${id}/preview`} className="inline-flex min-h-[44px] items-center rounded-md border border-line px-3 text-sm">
            Preview
          </Link>
          {status === 'published' ? (
            <Button onClick={unpublish}>Unpublish</Button>
          ) : (
            <Button onClick={publish}>Publish</Button>
          )}
          <Button onClick={startLive}>Start live</Button>
        </div>
      </div>

      {saveState === 'conflict' && (
        <div role="alert" className="mt-4 rounded-md border border-danger p-4">
          <p>This quiz changed elsewhere (server v{serverVersion}). Reloading discards your unsaved edits.</p>
          <button onClick={reloadServer} className="mt-2 inline-flex min-h-[44px] items-center rounded-md bg-brand px-4 font-semibold text-white">
            Reload server version
          </button>
        </div>
      )}

      {generated && (
        <div role="status" className="mt-4 rounded-md border border-score bg-surface p-4">
          <p className="font-medium">
            {mockGen ? 'Mock-generated draft — demo output, not from the AI provider.' : 'AI-generated draft — review before presenting.'}
          </p>
          <p className="text-sm text-muted">Check every answer and explanation, then publish.</p>
        </div>
      )}

      <div className="mt-4">
        <label htmlFor="quiz-title" className="text-sm font-medium">Title</label>
        <input
          id="quiz-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          maxLength={120}
          className="mt-1 w-full rounded-md border border-line bg-surface px-3 py-2 text-lg font-semibold"
        />
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-[240px_minmax(0,1fr)_300px]">
        <section aria-label="Questions" className="rounded-lg border border-line bg-surface p-2">
          <ol className="flex flex-col gap-1">
            {questions.map((item, i) => (
              <li key={item.id ?? `new-${i}`}>
                <button
                  onClick={() => setActive(i)}
                  onKeyDown={(e) => onKeyReorder(e, i)}
                  aria-current={i === active}
                  className={`flex min-h-[44px] w-full items-center gap-2 rounded px-2 text-left text-sm ${i === active ? 'bg-brand text-white' : 'hover:bg-canvas'}`}
                >
                  <span className="mono w-6">{String(i + 1).padStart(2, '0')}</span>
                  <span className="truncate">{item.prompt || '(untitled)'}</span>
                </button>
              </li>
            ))}
          </ol>
          <button
            onClick={() => {
              const next = renumberQuestions([...questions, blankQuestion(questions.length)])
              setQuestions(next)
              setActive(next.length - 1)
            }}
            className="mt-2 inline-flex min-h-[44px] w-full items-center justify-center rounded-md border border-line text-sm"
          >
            + Add question
          </button>
          <p className="mt-2 text-xs text-muted">Tip: Alt+↑/↓ reorders the focused question.</p>
        </section>

        <section aria-label="Question canvas" className="rounded-lg border border-line bg-surface p-4">
          {q ? (
            <div className="flex flex-col gap-3">
              <div>
                <label htmlFor="q-prompt" className="text-sm font-medium">Prompt</label>
                <textarea
                  id="q-prompt"
                  value={q.prompt}
                  onChange={(e) => updateActive((old) => ({ ...old, prompt: e.target.value }))}
                  maxLength={500}
                  rows={2}
                  className="mt-1 w-full rounded-md border border-line px-3 py-2"
                />
              </div>
              <fieldset>
                <legend className="text-sm font-medium">Options (select the correct one)</legend>
                <div className="mt-1 flex flex-col gap-2">
                  {q.options.map((o, oi) => (
                    <div key={o.id ?? `opt-${oi}`} className="grid grid-cols-[auto_1fr_auto] items-center gap-2">
                      <input
                        type="radio"
                        name={`correct-${active}`}
                        checked={o.isCorrect}
                        onChange={() =>
                          updateActive((old) => ({
                            ...old,
                            options: old.options.map((x, xi) => ({ ...x, isCorrect: xi === oi })),
                          }))
                        }
                        aria-label={`Mark option ${oi + 1} correct`}
                        className="h-5 w-5"
                      />
                      <input
                        value={o.text}
                        onChange={(e) =>
                          updateActive((old) => ({
                            ...old,
                            options: old.options.map((x, xi) => (xi === oi ? { ...x, text: e.target.value } : x)),
                          }))
                        }
                        maxLength={300}
                        placeholder={`Option ${oi + 1}`}
                        aria-label={`Option ${oi + 1} text`}
                        className="min-h-[44px] rounded-md border border-line px-3"
                      />
                      <button
                        onClick={() => {
                          if (q.options.length <= 2) return
                          updateActive((old) => ({ ...old, options: old.options.filter((_, xi) => xi !== oi) }))
                        }}
                        disabled={q.options.length <= 2}
                        aria-label={`Remove option ${oi + 1}`}
                        className="inline-flex min-h-[44px] min-w-[44px] items-center justify-center rounded-md border border-line disabled:opacity-40"
                      >
                        ×
                      </button>
                    </div>
                  ))}
                </div>
                <button
                  onClick={() => {
                    if (q.options.length >= 6) return
                    updateActive((old) => ({
                      ...old,
                      options: [...old.options, { position: old.options.length, text: '', isCorrect: false }],
                    }))
                  }}
                  disabled={q.options.length >= 6}
                  className="mt-2 inline-flex min-h-[44px] items-center rounded-md border border-line px-3 text-sm disabled:opacity-40"
                >
                  + Add option
                </button>
              </fieldset>
              <div>
                <label htmlFor="q-expl" className="text-sm font-medium">Explanation</label>
                <textarea
                  id="q-expl"
                  value={q.explanation}
                  onChange={(e) => updateActive((old) => ({ ...old, explanation: e.target.value }))}
                  maxLength={1000}
                  rows={2}
                  className="mt-1 w-full rounded-md border border-line px-3 py-2"
                />
              </div>
              <button
                onClick={() => {
                  const next = renumberQuestions(questions.filter((_, i) => i !== active))
                  setQuestions(next)
                  setActive(Math.max(0, active - 1))
                  toast('Question removed.')
                }}
                className="self-start text-sm text-danger underline"
              >
                Remove this question
              </button>
            </div>
          ) : (
            <p className="text-muted">No questions yet — add one to begin.</p>
          )}
        </section>

        <aside aria-label="Settings" className="rounded-lg border border-line bg-surface p-4">
          {q && (
            <div className="flex flex-col gap-3">
              <div>
                <label htmlFor="q-diff" className="text-sm font-medium">Difficulty</label>
                <select id="q-diff" value={q.difficulty} onChange={(e) => updateActive((old) => ({ ...old, difficulty: e.target.value as Question['difficulty'] }))} className="mt-1 w-full min-h-[44px] rounded-md border border-line bg-surface px-2">
                  <option value="easy">Easy</option>
                  <option value="medium">Medium</option>
                  <option value="hard">Hard</option>
                </select>
              </div>
              <div>
                <label htmlFor="q-timer" className="text-sm font-medium">Timer (10-180s)</label>
                <input id="q-timer" type="number" min={10} max={180} value={q.timeLimitSeconds} onChange={(e) => updateActive((old) => ({ ...old, timeLimitSeconds: Number(e.target.value) }))} className="mt-1 w-full min-h-[44px] rounded-md border border-line px-3" />
              </div>
              <div>
                <label htmlFor="q-scoring" className="text-sm font-medium">Scoring</label>
                <select id="q-scoring" value={q.scoringMode} onChange={(e) => updateActive((old) => ({ ...old, scoringMode: e.target.value as Question['scoringMode'] }))} className="mt-1 w-full min-h-[44px] rounded-md border border-line bg-surface px-2">
                  <option value="speed">Speed bonus</option>
                  <option value="fixed">Fixed points</option>
                </select>
              </div>
              <div>
                <label htmlFor="q-points" className="text-sm font-medium">Base points (100-10000)</label>
                <input id="q-points" type="number" min={100} max={10000} step={100} value={q.basePoints} onChange={(e) => updateActive((old) => ({ ...old, basePoints: Number(e.target.value) }))} className="mt-1 w-full min-h-[44px] rounded-md border border-line px-3" />
              </div>
              <div>
                <label htmlFor="quiz-desc" className="text-sm font-medium">Quiz description</label>
                <textarea id="quiz-desc" value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} rows={3} className="mt-1 w-full rounded-md border border-line px-3 py-2" />
              </div>
            </div>
          )}
        </aside>
      </div>
    </main>
  )
}
