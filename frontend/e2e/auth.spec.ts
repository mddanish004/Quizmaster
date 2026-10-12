import { test, expect } from '@playwright/test'
import { registerUser, logout, loginUser, tag } from './helpers'

test('register → logout → login → guards', async ({ page }) => {
  const t = tag()
  const user = await registerUser(page, t)
  await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible()
  await logout(page)
  // Protected route redirects when anonymous.
  await page.goto('/app/quizzes')
  await expect(page).toHaveURL(/\/login/)
  await loginUser(page, user.email, user.password)
  await expect(page).toHaveURL(/\/app/)
})

test('registration validation + duplicate identity', async ({ page }) => {
  const t = tag()
  await registerUser(page, t)
  await logout(page)
  await page.goto('/register')
  await page.getByLabel('Email').fill(`${t}2@example.com`)
  await page.getByLabel('Username').fill(`user_${t}2`.slice(0, 28))
  await page.getByLabel('Display name').fill('Dup')
  await page.getByLabel('Password', { exact: true }).fill('short')
  await page.getByRole('button', { name: /create account/i }).click()
  // Client-side error, no navigation.
  await expect(page).toHaveURL(/\/register/)
  // Duplicate email → server conflict stays on form with values preserved.
  await page.getByLabel('Password', { exact: true }).fill('correct horse battery staple extended!')
  await page.getByLabel('Email').fill(`${t}@example.com`)
  await page.getByRole('button', { name: /create account/i }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByLabel('Email')).toHaveValue(`${t}@example.com`)
})

test('login rejects bad credentials generically; forgot/reset flows', async ({ page }) => {
  const t = tag()
  const user = await registerUser(page, t)
  await logout(page)
  await page.goto('/login')
  await page.getByLabel('Identifier').fill(user.email)
  await page.getByLabel('Password', { exact: true }).fill('wrong passphrase entirely 12345')
  await page.getByRole('button', { name: /^sign in$/i }).click()
  await expect(page.getByText(/invalid credentials/i)).toBeVisible()
  // Forgot password is enumeration-safe.
  await page.goto('/forgot-password')
  await page.getByLabel('Email').fill(user.email)
  await page.getByRole('button', { name: /send reset link/i }).click()
  await expect(page.getByText(/reset link is on its way/i)).toBeVisible()
  // Bad token surfaces an error, not a crash.
  await page.goto('/reset-password?token=invalid-token-value-xyz')
  await page.getByLabel('Password', { exact: true }).fill('another long passphrase 12345')
  await page.getByRole('button', { name: /set new password/i }).click()
  await expect(page.getByRole('alert')).toBeVisible()
})
