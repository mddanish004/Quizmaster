import { QueryClient } from '@tanstack/react-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 30_000, retry: 1, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
})

export const queryKeys = {
  me: ['me'] as const,
  quizzes: (filters: string) => ['quizzes', filters] as const,
  quiz: (id: string) => ['quiz', id] as const,
  sessions: (filters: string) => ['sessions', filters] as const,
  sessionResults: (id: string) => ['session', id, 'results'] as const,
}
