import { test, expect } from '@playwright/test'
import { registerUser, tag } from './helpers'

test('settings: profile save, login methods, sign out everywhere', async ({ page }) => {
  const t = tag()
  const user = await registerUser(page, t)
  await page.goto('/app/settings')
  await expect(page.getByText(user.email)).toBeVisible()
  await expect(page.getByText('Password')).toBeVisible()
  await page.getByLabel('Display name').fill(`Renamed ${t}`.slice(0, 30))
  await page.getByRole('button', { name: /save profile/i }).click()
  await expect(page.getByText('Saved.')).toBeVisible()
  await page.getByRole('button', { name: /sign out everywhere/i }).click()
  await expect(page).toHaveURL(/\/login/)
})
