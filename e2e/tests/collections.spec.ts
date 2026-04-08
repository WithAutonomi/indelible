import { test, expect } from '@playwright/test'

test.describe('Collections', () => {
  test('register page loads', async ({ page }) => {
    await page.goto('/register')
    await expect(page.locator('body')).toContainText('Create your account', { timeout: 10000 })
  })
})
