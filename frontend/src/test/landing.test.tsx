import { render } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { queryClient } from '../lib/query-client'
import { LandingPage } from '../features/landing/LandingPage'
import { describe, it, expect } from 'vitest'

describe('landing', () => {
  it('renders product offer', () => {
    const { getByText } = render(
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <LandingPage />
        </BrowserRouter>
      </QueryClientProvider>,
    )
    expect(getByText(/AI-built quizzes, played live/)).toBeTruthy()
  })
})
