import { test, expect } from '@playwright/test'
import { ADMIN_USER } from '../helpers/fixtures'

async function registerAndLogin(page: any) {
  await page.goto('/register')
  await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
  await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
  await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
  await page.locator('input[placeholder="Password"]').fill(ADMIN_USER.password)
  await page.getByRole('button', { name: 'Create account' }).click()
  await page.waitForURL((url: URL) => !url.pathname.includes('/register'), { timeout: 10000 })
}

test.describe('Collections', () => {
  test('navigate to collections page after login', async ({ page }) => {
    await registerAndLogin(page)
    await page.goto('/collections')
    await expect(page.locator('body')).toContainText('Collection', { timeout: 10000 })
  })
})
