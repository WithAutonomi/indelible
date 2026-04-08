import { test, expect } from '@playwright/test'
import { ADMIN_USER } from '../helpers/fixtures'

test.describe('Admin Panel', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')
  })

  test('admin can see admin navigation items', async ({ page }) => {
    // First user is admin — verify admin nav items are visible
    await expect(page.locator('nav')).toContainText('Users')
    await expect(page.locator('nav')).toContainText('Wallets')
  })

  test('admin can navigate to users page', async ({ page }) => {
    await page.goto('/admin/users')
    await expect(page.locator('body')).toContainText('Users')
  })
})
