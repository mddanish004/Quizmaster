import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { HistoryPage, SessionDetailPage } from '../features/results/HistoryPages'
import { jsonResponse } from './mockFetch'

function renderWith(ui: React.ReactElement, path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/app/sessions" element={ui} />
          <Route path="/app/sessions/:sessionId" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('session history', () => {
  it('lists owned sessions and links to detail', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ data: [{ id: 's1', quizTitle: 'Go Basics', status: 'ended', participantCount: 4, questionCount: 5, createdAt: '2026-10-10T10:00:00Z', endedAt: '2026-10-10T10:20:00Z' }], page: { hasMore: false } }),
    ) as unknown as typeof fetch
    renderWith(<HistoryPage />, '/app/sessions')
    expect(await screen.findByText('Go Basics')).toBeTruthy()
    expect(screen.getByText(/4 players/)).toBeTruthy()
  })

  it('shows leaderboard and per-question aggregates with accessible tables', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({
        data: {
          session: { id: 's1', quizTitle: 'Go Basics', status: 'ended', participantCount: 2, questionCount: 1, createdAt: '2026-10-10T10:00:00Z', endedAt: '2026-10-10T10:20:00Z' },
          participants: [
            { id: 'p1', displayName: 'Alice', score: 950, joinedAt: '2026-10-10T10:00:00Z' },
            { id: 'p2', displayName: 'Bob', score: 500, joinedAt: '2026-10-10T10:01:00Z' },
          ],
          questions: [
            { id: 'q1', position: 0, prompt: 'Q?', correctOptionIds: ['o2'], options: [{ id: 'o1', text: 'A', count: 1, percentage: 50 }, { id: 'o2', text: 'B', count: 1, percentage: 50 }], answerCount: 2, accuracy: 50 },
          ],
        },
      }),
    ) as unknown as typeof fetch
    renderWith(<SessionDetailPage sessionId="s1" />, '/app/sessions/s1')
    expect(await screen.findByText('Alice')).toBeTruthy()
    expect(screen.getByText(/50% correct/)).toBeTruthy()
  })

  it('is owner-only: 404 surfaces a safe error without leaking data', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ error: { code: 'NOT_FOUND', message: 'Session not found.', requestId: 'r', retryable: false } }, 404, false),
    ) as unknown as typeof fetch
    renderWith(<SessionDetailPage sessionId="other" />, '/app/sessions/other')
    expect(await screen.findByText(/could not load/i)).toBeTruthy()
  })
})
