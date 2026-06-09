import { test, expect } from '@playwright/test'

test.describe('Authentication', () => {
  test('register a fresh non-admin user, then sign in', async ({ page }) => {
    // globalSetup seeds the shared admin (admin@e2e-test.com), so this test
    // uses a distinct email and verifies the register-form path. Registration
    // is anti-enumeration (V2-430): it shows a neutral confirmation and does
    // NOT auto-log-in, so the user signs in afterward. The new account ends up
    // with default "read" permissions.
    const email = `register-${Date.now()}@e2e-test.com`

    await page.goto('/register')
    await page.getByPlaceholder('First name').fill('Register')
    await page.getByPlaceholder('Last name').fill('Test')
    await page.getByPlaceholder('Email').fill(email)
    await page.locator('input[placeholder="Password"]').fill('TestPassword123!')
    await page.getByRole('button', { name: 'Create account' }).click()

    // Neutral confirmation appears instead of a redirect into the app.
    await expect(page.getByRole('button', { name: 'Go to sign in' })).toBeVisible({ timeout: 10000 })

    // Sign in to confirm the account exists with read-only permissions. Logging
    // in via the page context sets the session cookie so /me resolves.
    const loginRes = await page.request.post('/api/v2/auth/login', {
      data: { email, password: 'TestPassword123!' },
    })
    expect(loginRes.ok()).toBeTruthy()

    const meRes = await page.request.get('/api/v2/me')
    expect(meRes.ok()).toBeTruthy()
    const me = await meRes.json()
    expect(me.email).toBe(email)
    expect(me.permissions).toBe('read')
  })
})
