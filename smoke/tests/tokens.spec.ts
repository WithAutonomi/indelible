import { test, expect } from '@playwright/test'

test.describe('Tokens', () => {
  test('health endpoint works', async ({ request }) => {
    const response = await request.get('/health')
    expect(response.status()).toBe(200)
  })
})
