import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { SettingsPage } from '../features/settings/SettingsPage'
import { jsonResponse } from './mockFetch'

const mePayload = {
  data: {
    user: { id: 'u1', email: 'ada@example.com', username: 'ada_l', displayName: 'Ada', emailVerified: true, loginMethods: ['password'], createdAt: '' },
    csrfToken: 'csrf',
  },
}

function renderSettings() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/app/settings']}>
        <Routes>
          <Route path="/app/settings" element={<SettingsPage />} />
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('settings page', () => {
  it('saves profile and shows login methods', async () => {
    const user = userEvent.setup()
    const calls: string[] = []
    globalThis.fetch = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`)
      return jsonResponse(mePayload)
    }) as unknown as typeof fetch
    renderSettings()
    expect(await screen.findByText('Password')).toBeTruthy()
    await user.clear(screen.getByLabelText('Display name'))
    await user.type(screen.getByLabelText('Display name'), 'Ada L')
    await user.click(screen.getByRole('button', { name: /save profile/i }))
    expect(await screen.findByText('Saved.')).toBeTruthy()
    expect(calls.join('\n')).toContain('PATCH /api/v1/me')
  })

  it('signs out everywhere and returns to login', async () => {
    const user = userEvent.setup()
    globalThis.fetch = vi.fn().mockImplementation(async () => jsonResponse(mePayload)) as unknown as typeof fetch
    renderSettings()
    await screen.findByText('Password')
    await user.click(screen.getByRole('button', { name: /sign out everywhere/i }))
    expect(await screen.findByText('login page')).toBeTruthy()
  })
})
