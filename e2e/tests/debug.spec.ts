import { test, expect } from '@playwright/test'

test('debug: check what register page renders', async ({ page }) => {
  const errors: string[] = []
  const logs: string[] = []

  page.on('pageerror', (err) => errors.push(err.message))
  page.on('console', (msg) => {
    if (msg.type() === 'error') logs.push(`[console.error] ${msg.text()}`)
  })

  const response = await page.goto('/register')
  console.log(`Status: ${response?.status()}`)
  console.log(`URL: ${page.url()}`)

  // Wait for network to settle
  await page.waitForLoadState('networkidle')

  // Wait a bit more for Vue to mount
  await page.waitForTimeout(3000)

  // Dump errors
  if (errors.length) console.log('Page errors:', errors)
  if (logs.length) console.log('Console errors:', logs)

  // Dump rendered HTML
  const html = await page.content()
  console.log('=== Page HTML (first 3000 chars) ===')
  console.log(html.substring(0, 3000))

  // Check if #app has any content
  const appContent = await page.locator('#app').innerHTML()
  console.log('=== #app innerHTML (first 1000 chars) ===')
  console.log(appContent.substring(0, 1000))

  // Take screenshot
  await page.screenshot({ path: 'test-results/debug-register.png', fullPage: true })

  expect(errors).toEqual([])
})
