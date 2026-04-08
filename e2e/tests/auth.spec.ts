import { test, expect } from '@playwright/test'
import { ADMIN_USER } from '../helpers/fixtures'

test.describe('Authentication', () => {
  test('register first user and land on dashboard', async ({ page }) => {
    // Capture API responses for debugging
    const responses: string[] = []
    page.on('response', async (response) => {
      if (response.url().includes('/api/')) {
        const status = response.status()
        let body = ''
        try { body = await response.text() } catch {}
        responses.push(`${response.request().method()} ${response.url()} → ${status}: ${body.substring(0, 200)}`)
      }
    })

    await page.goto('/register')
    await page.getByPlaceholder('First name').fill(ADMIN_USER.firstName)
    await page.getByPlaceholder('Last name').fill(ADMIN_USER.lastName)
    await page.getByPlaceholder('Email').fill(ADMIN_USER.email)
    await page.locator('input[placeholder="Password"]').fill(ADMIN_USER.password)
    await page.getByRole('button', { name: 'Create account' }).click()

    // Wait a moment for the API call to complete
    await page.waitForTimeout(3000)

    // Log API responses for debugging
    console.log('API responses:', JSON.stringify(responses, null, 2))

    // Check if there's an error message on the page
    const pageText = await page.locator('body').innerText()
    if (pageText.includes('failed') || pageText.includes('Error') || pageText.includes('error')) {
      console.log('Page contains error text:', pageText.substring(0, 500))
    }

    // Check current URL
    console.log('Current URL after submit:', page.url())

    // The test: we should have navigated away from /register
    await page.waitForURL((url) => !url.pathname.includes('/register'), { timeout: 10000 })
  })
})
