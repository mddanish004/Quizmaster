import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import { EmptyState, Skeleton } from '../../components/ui/primitives'

type QuizSummary = {
  id: string
  title: string
  status: string
  questionCount: number
  version: number
  updatedAt: string
}

export function DashboardPage() {
  const quizzes = useQuery({
    queryKey: queryKeys.quizzes('all'),
    queryFn: () => api.get<{ data: QuizSummary[]; page: { hasMore: boolean } }>('/api/v1/quizzes'),
  })
  return (
    <main className="mx-auto max-w-[1280px] px-6 py-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="flex gap-2">
          <Link to="/app/quizzes/new" className="inline-flex min-h-[44px] items-center rounded-md bg-brand px-4 font-semibold text-white">
            New quiz
          </Link>
          <Link to="/app/generate" className="inline-flex min-h-[44px] items-center rounded-md border border-line bg-surface px-4 font-semibold">
            Generate with AI
          </Link>
        </div>
      </div>
      <section aria-label="Quizzes" className="mt-6">
        {quizzes.isPending && <Skeleton className="h-32" />}
        {quizzes.isError && (
          <div role="alert" className="rounded-md border border-danger p-4">
            Could not load quizzes. <button onClick={() => quizzes.refetch()} className="underline">Retry</button>
          </div>
        )}
        {quizzes.data && quizzes.data.data.length === 0 && (
          <EmptyState title="No quizzes yet — create a blank quiz or generate one with AI." />
        )}
        {quizzes.data && quizzes.data.data.length > 0 && (
          <ul className="divide-y divide-line rounded-lg border border-line bg-surface">
            {quizzes.data.data.map((q) => (
              <li key={q.id} className="flex items-center justify-between px-4 py-3">
                <Link to={`/app/quizzes/${q.id}`} className="font-medium underline-offset-2 hover:underline">
                  {q.title}
                </Link>
                <span className="text-sm text-muted">
                  {q.status} · {q.questionCount} questions
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  )
}
