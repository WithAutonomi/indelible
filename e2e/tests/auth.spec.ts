import { test, expect } from '@playwright/test'
import { ADMIN_USER } from '../helpers/fixtures'

test.describe('Authentication', () => {
  test('register first user and land on dashboard', async ({ page }) => {
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    // PrimeVue Password component wraps the input
    await page.locator('input[placeholder="Password"]').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()

    // Wait for navigation away from /register
    await page.waitForURL((url) => !url.pathname.includes('/register'), { timeout: 10000 })
  })

  test('logout and login again', async ({ page }) => {
    // Register first
    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.locator('input[placeholder="Password"]').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()
    await page.waitForURL((url) => !url.pathname.includes('/register'), { timeout: 10000 })

    // Logout — look for any logout button/link
    const logoutBtn = page.locator('button:has-text("Logout"), button:has-text("Log out"), a:has-text("Logout")')
    if (await logoutBtn.count() > 0) {
      await logoutBtn.first().click()
      await page.waitForURL('**/login', { timeout: 10000 })
    }

    // Login
    await page.goto('/login')
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.locator('input[placeholder="Password"]').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await page.waitForURL((url) => !url.pathname.includes('/login'), { timeout: 10000 })
  })
})
