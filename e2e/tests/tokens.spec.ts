import { test, expect } from '@playwright/test'
import { ADMIN_USER } from '../helpers/fixtures'

test.describe('API Tokens', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')
  })

  test('navigate to tokens page', async ({ page }) => {
    await page.goto('/tokens')
    await expect(page.locator('body')).toContainText('API Tokens')
  })
})
