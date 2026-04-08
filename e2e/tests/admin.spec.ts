import { test, expect } from '@playwright/test'

test.describe('Admin', () => {
  test('register via API works', async ({ request }) => {
    const response = await request.post('/api/v2/auth/register', {
      data: {
        email: 'api-test@e2e.com',
        password: 'TestPassword123!',
        first_name: 'API',
        last_name: 'Test',
      },
    })
    console.log('Register API status:', response.status())
    console.log('Register API body:', await response.text())
    expect(response.status()).toBe(201)
  })
})
