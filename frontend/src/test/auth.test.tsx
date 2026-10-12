import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { RequireAuth } from '../features/auth/AppLayout'
import { jsonResponse, emptyResponse } from './mockFetch'
import { ForgotPage } from '../features/auth/ForgotPage'
import { ResetPage } from '../features/auth/ResetPage'
import { LoginPage } from '../features/auth/LoginPage'

function renderWithClient(ui: React.ReactElement, path = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('auth guards', () => {
  it('redirects anonymous users to login', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ error: { code: 'AUTH_REQUIRED', message: 'Sign in', requestId: 'x', retryable: false } }, 401, false),
    ) as unknown as typeof fetch
    renderWithClient(
      <Routes>
        <Route path="/" element={<RequireAuth><div>secret</div></RequireAuth>} />
        <Route path="/login" element={<div>login page</div>} />
      </Routes>,
      '/',
    )
    expect(await screen.findByText('login page')).toBeTruthy()
  })
})

describe('forgot password', () => {
  it('shows confirmation after submit', async () => {
    const user = userEvent.setup()
    globalThis.fetch = vi.fn().mockResolvedValue(emptyResponse(202)) as unknown as typeof fetch
    renderWithClient(<ForgotPage />)
    await user.type(screen.getByLabelText('Email'), 'ada@example.com')
    await user.click(screen.getByRole('button', { name: /send reset link/i }))
    expect(await screen.findByText(/a reset link is on its way/i)).toBeTruthy()
  })
})

describe('reset password', () => {
  it('warns when token missing', () => {
    renderWithClient(
      <Routes>
        <Route path="/" element={<ResetPage />} />
      </Routes>,
      '/',
    )
    expect(screen.getByText(/link is incomplete/i)).toBeTruthy()
  })
})

describe('login page', () => {
  it('shows oauth failure banner from query', () => {
    renderWithClient(
      <Routes>
        <Route path="/" element={<LoginPage />} />
      </Routes>,
      '/?oauth=failed',
    )
    expect(screen.getByText(/did not complete/i)).toBeTruthy()
  })
})
