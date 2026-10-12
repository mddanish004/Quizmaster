import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import { useToast } from '../../components/ui/toast'
import { EmptyState, Skeleton } from '../../components/ui/primitives'
import type { Quiz } from './types'

export function QuizzesPage() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const toast = useToast()
  const quizzes = useQuery({
    queryKey: queryKeys.quizzes('all'),
    queryFn: () => api.get<{ data: Quiz[]; page: { hasMore: boolean } }>('/api/v1/quizzes'),
  })

  const createBlank = async () => {
    try {
      const res = await api.post<{ data: Quiz }>('/api/v1/quizzes', { title: 'Untitled quiz', description: '' })
      await qc.invalidateQueries({ queryKey: ['quizzes'] })
      nav(`/app/quizzes/${res.data.id}`)
    } catch {
      toast('Could not create quiz. Try again.')
    }
  }

  const remove = async (id: string, title: string) => {
    if (!window.confirm(`Delete "${title}"? This moves it to trash.`)) return
    try {
      await api.del(`/api/v1/quizzes/${id}`)
      await qc.invalidateQueries({ queryKey: ['quizzes'] })
      toast('Quiz deleted.')
    } catch {
      toast('Could not delete quiz.')
    }
  }

  return (
    <main className="mx-auto max-w-[1280px] px-6 py-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Quizzes</h1>
        <button onClick={createBlank} className="inline-flex min-h-[44px] items-center rounded-md bg-brand px-4 font-semibold text-white">
          New quiz
        </button>
      </div>
      <section aria-label="Quizzes" className="mt-6">
        {quizzes.isPending && <Skeleton className="h-32" />}
        {quizzes.isError && (
          <div role="alert" className="rounded-md border border-danger p-4">
            Could not load quizzes. <button onClick={() => quizzes.refetch()} className="underline">Retry</button>
          </div>
        )}
        {quizzes.data && quizzes.data.data.length === 0 && (
          <EmptyState
            title="No quizzes yet — create a blank quiz or generate one with AI."
            action={
              <button onClick={createBlank} className="inline-flex min-h-[44px] items-center rounded-md bg-brand px-4 font-semibold text-white">
                Create blank quiz
              </button>
            }
          />
        )}
        {quizzes.data && quizzes.data.data.length > 0 && (
          <ul className="divide-y divide-line rounded-lg border border-line bg-surface">
            {quizzes.data.data.map((q) => (
              <li key={q.id} className="flex items-center justify-between gap-3 px-4 py-3">
                <div>
                  <Link to={`/app/quizzes/${q.id}`} className="font-medium underline-offset-2 hover:underline">
                    {q.title}
                  </Link>
                  <p className="text-sm text-muted">{q.status} · {q.questionCount} questions · v{q.version}</p>
                </div>
                <div className="flex gap-2">
                  <Link to={`/app/quizzes/${q.id}/preview`} className="inline-flex min-h-[44px] items-center rounded-md border border-line px-3 text-sm">
                    Preview
                  </Link>
                  <button onClick={() => remove(q.id, q.title)} aria-label={`Delete ${q.title}`} className="inline-flex min-h-[44px] items-center rounded-md border border-line px-3 text-sm text-danger">
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  )
}
