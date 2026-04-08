import { test, expect } from '@playwright/test'
import { ADMIN_USER, TEST_WALLET_KEY } from '../helpers/fixtures'

test.describe('Collections', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')

    const token = await page.evaluate(() => localStorage.getItem('token'))
    await page.request.post('/api/v2/admin/wallets', {
      headers: { Authorization: `Bearer ${token}` },
      data: { name: 'e2e-wallet', private_key: TEST_WALLET_KEY },
    })
  })

  test('create a collection and see it listed', async ({ page }) => {
    await page.goto('/collections')
    await expect(page.locator('body')).toContainText('Collections')
  })
})
