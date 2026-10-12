import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { Button, Field, Input } from '../../components/ui/primitives'
import type { Quiz } from '../quizzes/types'

const schema = z.object({
  topic: z.string().trim().min(3, 'Topic must be 3-200 characters.').max(200, 'Topic must be 3-200 characters.'),
  difficulty: z.enum(['easy', 'medium', 'hard']),
  questionCount: z.number().int().min(1).max(10),
  targetAudience: z.string().max(200, 'Audience must be at most 200 characters.'),
  additionalInstructions: z.string().max(1000, 'Instructions must be at most 1000 characters.'),
  language: z.string().regex(/^[a-z]{2}$/, 'Choose a supported language.'),
})
type Form = z.infer<typeof schema>

type Stage = 'idle' | 'preparing' | 'generating' | 'checking'

export function GeneratorPage() {
  const nav = useNavigate()
  const [stage, setStage] = useState<Stage>('idle')
  const [serverError, setServerError] = useState('')
  const [isMock, setIsMock] = useState<boolean | null>(null)
  const { register, handleSubmit, watch, setValue, formState } = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: { difficulty: 'medium', questionCount: 5, targetAudience: '', additionalInstructions: '', language: 'en', topic: '' },
  })
  const count = watch('questionCount')
  const difficulty = watch('difficulty')

  const onSubmit = handleSubmit(async (v) => {
    setServerError('')
    setStage('preparing')
    try {
      setStage('generating')
      const res = await api.post<{ data: { generation: { id: string }; quiz: Quiz; mock: boolean } }>(
        '/api/v1/ai/generations',
        { ...v, targetAudience: v.targetAudience || 'general audience' },
        { 'Idempotency-Key': crypto.randomUUID() },
      )
      setStage('checking')
      setIsMock(res.data.mock)
      nav(`/app/quizzes/${res.data.quiz.id}?generated=1${res.data.mock ? '&mock=1' : ''}`)
    } catch (e) {
      setStage('idle')
      setServerError(e instanceof Error ? e.message : 'Generation failed. Your brief is preserved — retry or start a manual draft.')
    }
  })

  const busy = stage !== 'idle'

  return (
    <main className="mx-auto max-w-[1000px] px-6 py-8">
      <h1 className="text-2xl font-bold">Generate with AI</h1>
      <p className="mt-1 text-sm text-muted">
        Daily quota: 5 per user. {isMock === true && 'Demo provider (mock) — clearly labeled, no external call.'}
      </p>
      <div className="mt-6 grid gap-6 lg:grid-cols-2">
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <Field label="Topic" htmlFor="topic" error={formState.errors.topic?.message}>
            <Input id="topic" {...register('topic')} placeholder="Go concurrency fundamentals" />
          </Field>
          <fieldset>
            <legend className="text-sm font-medium">Difficulty</legend>
            <div className="mt-1 flex gap-1 rounded-md border border-line p-1" role="group" aria-label="Difficulty">
              {(['easy', 'medium', 'hard'] as const).map((d) => (
                <button
                  key={d}
                  type="button"
                  aria-pressed={difficulty === d}
                  onClick={() => setValue('difficulty', d, { shouldValidate: true })}
                  className={`min-h-[44px] flex-1 rounded px-3 text-sm font-medium capitalize ${difficulty === d ? 'bg-ink text-white' : ''}`}
                >
                  {d}
                </button>
              ))}
            </div>
          </fieldset>
          <Field label={`Questions: ${count}`} htmlFor="question-count" error={formState.errors.questionCount?.message}>
            <input
              type="range"
              min={1}
              max={10}
              id="question-count"
              aria-label="Question count"
              {...register('questionCount', { valueAsNumber: true })}
              className="w-full"
            />
          </Field>
          <Field label="Target audience" htmlFor="target-audience" error={formState.errors.targetAudience?.message}>
            <Input id="target-audience" {...register('targetAudience')} placeholder="Developers with six months of Go" />
          </Field>
          <Field label="Language" htmlFor="language" error={formState.errors.language?.message}>
            <select id="language" {...register('language')} className="min-h-[44px] rounded-md border border-line bg-surface px-2">
              <option value="en">English</option>
            </select>
          </Field>
          <Field label="Additional instructions (optional)" htmlFor="additional-instructions" error={formState.errors.additionalInstructions?.message}>
            <textarea id="additional-instructions" {...register('additionalInstructions')} rows={3} maxLength={1000} className="w-full rounded-md border border-line px-3 py-2" />
          </Field>
          {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
          <Button type="submit" disabled={busy}>
            {busy ? 'Generating…' : 'Generate draft'}
          </Button>
        </form>
        <aside aria-label="Progress" className="rounded-lg border border-line bg-surface p-6">
          {!busy && !serverError && <p className="text-muted">Your brief stays here while the draft is generated. Nothing is saved until the response passes validation.</p>}
          {busy && (
            <ol className="flex flex-col gap-2">
              {(['preparing', 'generating', 'checking'] as Stage[]).map((s) => (
                <li key={s} aria-current={stage === s} className={stage === s ? 'font-semibold' : 'text-muted'}>
                  {s === 'preparing' && 'Preparing brief'}
                  {s === 'generating' && 'Generating'}
                  {s === 'checking' && 'Checking structure'}
                  {stage === s ? '…' : ''}
                </li>
              ))}
            </ol>
          )}
          {serverError && (
            <div>
              <p className="font-medium">Generation failed</p>
              <p className="mt-1 text-sm text-muted">Your inputs are preserved. You can retry, or start with a blank draft titled by your topic.</p>
              <Button onClick={() => nav('/app/quizzes')}>Go to quizzes</Button>
            </div>
          )}
        </aside>
      </div>
    </main>
  )
}
