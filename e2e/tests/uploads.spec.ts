import { test, expect } from '@playwright/test'

test.describe('Uploads', () => {
  test('login page loads', async ({ page }) => {
    await page.goto('/login')
    await expect(page.locator('body')).toContainText('Sign in', { timeout: 10000 })
  })
})
