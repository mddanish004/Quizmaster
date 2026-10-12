import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { JoinPage } from '../features/participation/ParticipantPages'
import { jsonResponse } from './mockFetch'

function renderJoin(path = '/join') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/join" element={<JoinPage />} />
          <Route path="/play/:sessionId" element={<div>play room</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('join flow', () => {
  it('advances from code to name to play room', async () => {
    const user = userEvent.setup()
    const calls: string[] = []
    globalThis.fetch = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`)
      if (String(url).includes('/api/v1/join/ABC234')) {
        if ((init?.method ?? 'GET') === 'GET') {
          return jsonResponse({ data: { sessionId: 's1', joinable: true } })
        }
        return jsonResponse({ data: { sessionId: 's1' } }, 201)
      }
      return jsonResponse({ error: { code: 'NOT_FOUND', message: 'x', requestId: 'r', retryable: false } }, 404, false)
    }) as unknown as typeof fetch
    renderJoin()
    await user.type(screen.getByLabelText('Join code'), 'ABC234')
    await user.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText('Pick a display name')).toBeTruthy()
    await user.type(screen.getByLabelText('Display name'), 'Zoe')
    await user.click(screen.getByRole('button', { name: /join quiz/i }))
    expect(await screen.findByText('play room')).toBeTruthy()
    expect(calls.join('\n')).toContain('POST /api/v1/join/ABC234')
  })

  it('shows distinct states for bad codes', async () => {
    const user = userEvent.setup()
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ error: { code: 'NOT_FOUND', message: 'gone', requestId: 'r', retryable: false } }, 404, false),
    ) as unknown as typeof fetch
    renderJoin()
    await user.type(screen.getByLabelText('Join code'), 'ZZZ999')
    await user.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/not found or expired/i)).toBeTruthy()
  })
})
