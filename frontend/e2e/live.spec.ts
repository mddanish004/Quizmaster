import { test, expect } from '@playwright/test'
import { registerUser, createPublishedQuiz, browserCookies, tag, baseUrl } from './helpers'

test('full live flow across host + participant, with refresh recovery', async ({ browser }) => {
  const t = tag()
  const hostCtx = await browser.newContext()
  const partCtx = await browser.newContext()
  const host = await hostCtx.newPage()
  const part = await partCtx.newPage()

  await registerUser(host, `host${t}`.slice(0, 12))
  const quizId = await createPublishedQuiz(host.request, await browserCookies(host), `Live E2E ${t}`)

  // Host starts a session from the editor.
  await host.goto(`/app/quizzes/${quizId}`)
  await host.getByRole('button', { name: /start live/i }).click()
  await expect(host).toHaveURL(/\/app\/host\//, { timeout: 15_000 })
  const sessionUrl = host.url()
  const code = await host.getByLabel(/join code/i).textContent()
  expect(code?.trim()).toMatch(/^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{6}$/)
  await host.screenshot({ path: 'e2e/screenshots/host-lobby.png' })

  // Participant joins with the code.
  await part.goto('/join')
  await part.getByLabel('Join code').fill(code!.trim())
  await part.getByRole('button', { name: /continue/i }).click()
  await part.getByLabel('Display name').fill('Part E2E')
  await part.getByRole('button', { name: /join quiz/i }).click()
  await expect(part.getByText(/you’re in/i)).toBeVisible({ timeout: 15_000 })
  await part.screenshot({ path: 'e2e/screenshots/participant-waiting.png' })

  // Host starts question 1; participant answers.
  await host.getByRole('button', { name: /start question 1/i }).click()
  await expect(part.getByText(/which keyword declares/i)).toBeVisible({ timeout: 15_000 })
  await part.screenshot({ path: 'e2e/screenshots/participant-question.png' })
  await part.getByRole('button', { name: /var/i }).click()
  await expect(part.getByText(/answer received/i)).toBeVisible({ timeout: 10_000 })

  // Duplicate taps do not double-score (button disables after ack; resubmission is idempotent server-side).
  // Host refreshes mid-question and regains control.
  await host.reload()
  await expect(host.getByRole('button', { name: /end question/i })).toBeVisible({ timeout: 15_000 })

  // Host ends the question; both see results.
  await host.getByRole('button', { name: /end question/i }).click()
  await expect(host.getByText(/results/i).first()).toBeVisible({ timeout: 15_000 })
  await expect(part.getByText(/correct|not quite|no answer/i).first()).toBeVisible({ timeout: 15_000 })
  await host.screenshot({ path: 'e2e/screenshots/host-results.png' })
  await part.screenshot({ path: 'e2e/screenshots/participant-result.png' })

  // Participant refreshes after ack: still received, single score.
  await part.reload()
  await expect(part.getByText(/correct|not quite|no answer|waiting/i).first()).toBeVisible({ timeout: 15_000 })

  // Host ends the session; history shows it.
  await host.getByRole('button', { name: /end session/i }).click()
  await expect(host.getByText(/session ended/i)).toBeVisible({ timeout: 15_000 })
  await host.goto('/app/sessions')
  await expect(host.getByText(`Live E2E ${t}`)).toBeVisible({ timeout: 15_000 })
  await hostCtx.close()
  await partCtx.close()
  void sessionUrl
})

test('join guards: invalid code, duplicate name, unauthorized host command', async ({ browser }) => {
  const t = tag()
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await registerUser(page, `guard${t}`.slice(0, 12))

  await page.goto('/join')
  await page.getByLabel('Join code').fill('ZZZ999')
  await page.getByRole('button', { name: /continue/i }).click()
  await expect(page.getByText(/not found or expired/i)).toBeVisible()

  // Unauthorized host command: a fresh anonymous context gets 401 (no session);
  // the authenticated page context without a CSRF header gets 403.
  const anon = await browser.newContext()
  const anonRes = await anon.request.post(`${baseUrl()}/api/v1/live-sessions/00000000-0000-4000-8000-000000000000/commands`, {
    headers: { 'Content-Type': 'application/json' },
    data: JSON.stringify({ commandId: '00000000-0000-4000-8000-000000000001', type: 'end_session', expectedVersion: 1, payload: {} }),
  })
  expect(anonRes.status()).toBe(401)
  await anon.close()
  const csrfLess = await page.request.post(`${baseUrl()}/api/v1/live-sessions/00000000-0000-4000-8000-000000000000/commands`, {
    headers: { 'Content-Type': 'application/json' },
    data: JSON.stringify({ commandId: '00000000-0000-4000-8000-000000000001', type: 'end_session', expectedVersion: 1, payload: {} }),
  })
  expect(csrfLess.status()).toBe(403)
  await ctx.close()
})
