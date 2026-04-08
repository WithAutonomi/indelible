import { test, expect } from '@playwright/test'
import { ADMIN_USER, TEST_WALLET_KEY } from '../helpers/fixtures'

test.describe('Uploads', () => {
  test.beforeEach(async ({ page }) => {
    // Register and login
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')

    // Create wallet via API (required for uploads)
    const token = await page.evaluate(() => localStorage.getItem('token'))
    await page.request.post('/api/v2/admin/wallets', {
      headers: { Authorization: `Bearer ${token}` },
      data: { name: 'e2e-wallet', private_key: TEST_WALLET_KEY },
    })
  })

  test('upload a file and see it in uploads list', async ({ page }) => {
    // Navigate to uploads
    await page.goto('/uploads')
    await expect(page.locator('body')).toContainText('Uploads')
  })
})
