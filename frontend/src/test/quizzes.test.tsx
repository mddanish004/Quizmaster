import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi } from 'vitest'
import { renumberQuestions, blankQuestion } from '../features/quizzes/types'
import { PreviewPage } from '../features/quizzes/PreviewPage'
import { jsonResponse } from './mockFetch'

describe('quiz helpers', () => {
  it('renumbers questions and options contiguously', () => {
    const qs = [blankQuestion(5), blankQuestion(9)]
    const next = renumberQuestions(qs)
    expect(next[0].position).toBe(0)
    expect(next[1].position).toBe(1)
    expect(next[0].options.map((o) => o.position)).toEqual([0, 1])
  })
})

describe('preview page', () => {
  it('switches roles without writes', async () => {
    const user = userEvent.setup()
    const quiz = {
      id: 'q1', title: 'T', description: '', status: 'draft', questionCount: 1, version: 1,
      createdAt: '', updatedAt: '', publishedAt: null,
      questions: [
        {
          id: 'qu1', type: 'multiple_choice', position: 0, prompt: 'Q?', explanation: 'E',
          difficulty: 'easy', timeLimitSeconds: 30, scoringMode: 'speed', basePoints: 1000,
          options: [
            { id: 'o1', position: 0, text: 'A', isCorrect: false },
            { id: 'o2', position: 1, text: 'B', isCorrect: true },
          ],
        },
      ],
    }
    let posts = 0
    globalThis.fetch = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method && init.method !== 'GET') posts++
      return jsonResponse({ data: { quiz, persistedVersion: 1, responsesSaved: false } })
    }) as unknown as typeof fetch
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/app/quizzes/q1/preview']}>
          <Routes>
            <Route path="/app/quizzes/:id/preview" element={<PreviewPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Q?')).toBeTruthy()
    await user.click(screen.getByRole('tab', { name: 'Participant' }))
    await user.click(screen.getByRole('button', { name: 'A' }))
    expect(await screen.findByText(/locally only/i)).toBeTruthy()
    expect(posts).toBe(0)
  })
})
