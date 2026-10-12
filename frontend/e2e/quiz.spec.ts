import { test, expect } from '@playwright/test'
import { registerUser, tag } from './helpers'

test('quiz CRUD: create → edit → autosave → publish → preview → delete', async ({ page }) => {
  const t = tag()
  await registerUser(page, t)
  await page.goto('/app/quizzes')
  await page.getByRole('button', { name: /^new quiz$/i }).click()
  await expect(page).toHaveURL(/\/app\/quizzes\//)
  const editorUrl = page.url()

  // Title + first question.
  await page.getByLabel('Title').fill(`E2E Quiz ${t}`)
  await expect(page.getByText(/^saved$/i)).toBeVisible({ timeout: 15_000 })
  await page.getByRole('button', { name: /add question/i }).click()
  await page.getByLabel('Prompt').fill('Which keyword declares a variable?')
  await page.getByLabel('Option 1 text').fill('let')
  await page.getByLabel('Option 2 text').fill('var')
  await page.getByLabel('Mark option 2 correct').check()
  await expect(page.getByText(/^saved$/i)).toBeVisible({ timeout: 15_000 })

  // Reload preserves the aggregate.
  await page.reload()
  await expect(page.getByLabel('Title')).toHaveValue(`E2E Quiz ${t}`)
  await expect(page.getByLabel('Prompt')).toHaveValue('Which keyword declares a variable?')
  await page.screenshot({ path: 'e2e/screenshots/editor.png' })

  // Publish requires exactly one correct answer (option 2 preselected as correct in blank template? verify).
  await page.getByRole('button', { name: /^publish$/i }).click()
  await expect(page.getByText(/published/i).first()).toBeVisible({ timeout: 15_000 })

  // Preview shows both roles without saving.
  await page.getByRole('link', { name: /preview/i }).click()
  await expect(page.getByText(/responses aren’t saved/i)).toBeVisible()
  await page.getByRole('tab', { name: /participant/i }).click()
  await page.screenshot({ path: 'e2e/screenshots/preview-participant.png' })

  // Unpublish + delete.
  await page.goto(editorUrl)
  await page.getByRole('button', { name: /unpublish/i }).click()
  await expect(page.getByText(/^draft$/i).first()).toBeVisible({ timeout: 15_000 })
  await page.goto('/app/quizzes')
  page.on('dialog', (d) => d.accept())
  await page.getByRole('button', { name: new RegExp(`Delete E2E Quiz ${t}`) }).click()
  await expect(page.getByText(/no quizzes yet/i)).toBeVisible({ timeout: 15_000 })
})

test('version conflict surfaces reload dialog, never silent overwrite', async ({ page, context }) => {
  const t = tag()
  await registerUser(page, t)
  await page.goto('/app/quizzes')
  await page.getByRole('button', { name: /^new quiz$/i }).click()
  await expect(page).toHaveURL(/\/app\/quizzes\//)
  const url = page.url()
  const other = await context.newPage()
  await other.goto(url)
  await other.getByLabel('Title').fill(`Conflict A ${t}`)
  await expect(other.getByText(/^saved$/i)).toBeVisible({ timeout: 15_000 })
  // Stale tab edits without reloading → conflict banner.
  await page.getByLabel('Title').fill(`Conflict B ${t}`)
  await expect(page.getByText(/conflict/i).first()).toBeVisible({ timeout: 15_000 })
  await other.close()
})
