import { test, expect } from '@playwright/test'
import { ADMIN_USER } from '../helpers/fixtures'

test.describe('Authentication', () => {
  test('register first user and land on dashboard', async ({ page }) => {
    await page.goto('/register')

    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()

    // Should redirect to dashboard
    await expect(page).toHaveURL('/')
    await expect(page.locator('body')).toContainText('Dashboard')
  })

  test('logout and login again', async ({ page }) => {
    // Register first
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')

    // Logout
    await page.getByRole('button', { name: /logout/i }).click()
    await expect(page).toHaveURL('/login')

    // Login
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.getByPlaceholder('Password').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/')
  })
})
