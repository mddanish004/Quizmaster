import { expect, type Page, type APIRequestContext } from '@playwright/test'

export const baseUrl = () => process.env.E2E_BASE_URL ?? 'http://localhost:5173'

export const tag = () => `e${Date.now().toString(36)}${Math.floor(Math.random() * 1e6).toString(36)}`.replace(/[^a-z0-9]/g, '').slice(0, 12)

export async function registerUser(page: Page, t: string) {
  const user = { email: `${t}@example.com`, username: `user_${t}`.slice(0, 28), displayName: `User ${t}`.slice(0, 30), password: 'correct horse battery staple extended!' }
  await page.goto('/register')
  await page.getByLabel('Email').fill(user.email)
  await page.getByLabel('Username').fill(user.username)
  await page.getByLabel('Display name').fill(user.displayName)
  await page.getByLabel('Password', { exact: true }).fill(user.password)
  await page.getByRole('button', { name: /create account/i }).click()
  await expect(page).toHaveURL(/\/app/, { timeout: 15_000 })
  return user
}

export async function logout(page: Page) {
  await page.getByRole('button', { name: /sign out/i }).first().click()
  await expect(page).toHaveURL(/\/login/)
}

export async function loginUser(page: Page, email: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Identifier').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: /^sign in$/i }).click()
  await expect(page).toHaveURL(/\/app/, { timeout: 15_000 })
}

// createPublishedQuiz uses the API for deterministic setup; UI creation is covered in quiz.spec.ts.
export async function createPublishedQuiz(request: APIRequestContext, cookies: { name: string; value: string }[], title: string) {
  const headers = { 'Content-Type': 'application/json' }
  const cookie = cookies.map((c) => `${c.name}=${c.value}`).join('; ')
  const mk = async (method: string, path: string, body?: unknown, extra?: Record<string, string>) => {
    const res = await request.fetch(`${baseUrl()}${path}`, {
      method, headers: { ...headers, Cookie: cookie, ...(extra ?? {}) },
      data: body === undefined ? undefined : JSON.stringify(body),
    })
    if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()} ${await res.text()}`)
    return res.json()
  }
  const csrf = async () => (await (await request.fetch(`${baseUrl()}/api/v1/me`, { headers: { Cookie: cookie } })).json()).data.csrfToken as string
  const created = await mk('POST', '/api/v1/quizzes', { title, description: 'e2e' }, { 'X-CSRF-Token': await csrf() })
  const quizId = created.data.id as string
  const token = await csrf()
  const updated = await mk('PATCH', `/api/v1/quizzes/${quizId}`, {
    title, description: 'e2e',
    questions: [
      { type: 'multiple_choice', position: 0, prompt: 'Which keyword declares a variable?', explanation: 'var declares.', difficulty: 'easy', timeLimitSeconds: 60, scoringMode: 'speed', basePoints: 1000, options: [{ position: 0, text: 'let', isCorrect: false }, { position: 1, text: 'var', isCorrect: true }] },
      { type: 'multiple_choice', position: 1, prompt: 'Which is idiomatic?', explanation: 'for.', difficulty: 'easy', timeLimitSeconds: 60, scoringMode: 'fixed', basePoints: 500, options: [{ position: 0, text: 'while', isCorrect: false }, { position: 1, text: 'for', isCorrect: true }] },
    ],
  }, { 'X-CSRF-Token': token, 'If-Match': '"1"' })
  await mk('POST', `/api/v1/quizzes/${quizId}/publish`, undefined, { 'X-CSRF-Token': await csrf(), 'If-Match': `"${updated.data.version}"` })
  return quizId
}

export async function browserCookies(page: Page) {
  const cookies = await page.context().cookies()
  return cookies.filter((c) => c.name.startsWith('__Host-'))
}
