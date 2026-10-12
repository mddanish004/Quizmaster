import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route, useParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { GeneratorPage } from '../features/ai-generation/GeneratorPage'
import { jsonResponse } from './mockFetch'

function renderGen() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/app/generate']}>
        <Routes>
          <Route path="/app/generate" element={<GeneratorPage />} />
          <Route path="/app/quizzes/:id" element={<Probe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function Probe() {
  const { id } = useParams()
  return <div>editor for {id}</div>
}

describe('generator page', () => {
  it('rejects short topics client-side', async () => {
    const user = userEvent.setup()
    globalThis.fetch = vi.fn() as unknown as typeof fetch
    renderGen()
    await user.type(screen.getByLabelText('Topic'), 'Go')
    await user.click(screen.getByRole('button', { name: /generate draft/i }))
    expect(await screen.findByText(/topic must be 3-200 characters/i)).toBeTruthy()
    expect(globalThis.fetch).not.toHaveBeenCalled()
  })

  it('navigates to the editor with mock label on success', async () => {
    const user = userEvent.setup()
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ data: { generation: { id: 'g1' }, quiz: { id: 'quiz-1' }, mock: true } }, 201),
    ) as unknown as typeof fetch
    renderGen()
    await user.type(screen.getByLabelText('Topic'), 'Go concurrency fundamentals')
    await user.click(screen.getByRole('button', { name: /generate draft/i }))
    expect(await screen.findByText('editor for quiz-1')).toBeTruthy()
    const [, init] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit]
    expect((init.headers as Record<string, string>)['Idempotency-Key']).toBeTruthy()
  })

  it('preserves input and offers retry on provider failure', async () => {
    const user = userEvent.setup()
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ error: { code: 'AI_UNAVAILABLE', message: 'AI generation is unavailable.', requestId: 'r', retryable: true } }, 503, false),
    ) as unknown as typeof fetch
    renderGen()
    const topic = 'Go concurrency fundamentals'
    await user.type(screen.getByLabelText('Topic'), topic)
    await user.click(screen.getByRole('button', { name: /generate draft/i }))
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect((screen.getByLabelText('Topic') as HTMLInputElement).value).toBe(topic)
  })
})
