import { test, expect } from '@playwright/test'

test.describe('Authentication', () => {
  test('register a fresh non-admin user and land on dashboard', async ({ page }) => {
    // globalSetup already registered the shared admin (admin@e2e-test.com),
    // so this test uses a distinct email and only verifies the register-form
    // → POST /auth/register → redirect-to-dashboard path. The new account
    // ends up with default "read" permissions, which is the same path most
    // SSO/SCIM-provisioned users go through.
    const email = `register-${Date.now()}@e2e-test.com`

    await page.goto('/register')
    await page.getByPlaceholder('First name').fill('Register')
    await page.getByPlaceholder('Last name').fill('Test')
    await page.getByPlaceholder('Email').fill(email)
    await page.locator('input[placeholder="Password"]').fill('TestPassword123!')
    await page.getByRole('button', { name: 'Create account' }).click()

    await page.waitForURL((url) => !url.pathname.includes('/register'), { timeout: 10000 })

    // /me should reflect the freshly-registered, non-admin user.
    const meRes = await page.request.get('/api/v2/me')
    expect(meRes.ok()).toBeTruthy()
    const me = await meRes.json()
    expect(me.email).toBe(email)
    expect(me.permissions).toBe('read')
  })
})
