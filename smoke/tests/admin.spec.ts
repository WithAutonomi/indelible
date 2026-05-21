import { test, expect } from '@playwright/test'
import { adminHeaders } from '../helpers/auth'

test.describe('Admin', () => {
  test('register via API works for a new non-admin user', async ({ request }) => {
    // The shared admin (admin@e2e-test.com) was registered by globalSetup, so
    // this test exercises the register path for a second, non-admin user.
    const response = await request.post('/api/v2/auth/register', {
      data: {
        email: `api-test-${Date.now()}@e2e.com`,
        password: 'TestPassword123!',
        first_name: 'API',
        last_name: 'Test',
      },
    })
    expect(response.status()).toBe(201)
  })

  test('shared admin token reaches admin-protected endpoint', async ({ request }) => {
    // Smoke test for the globalSetup admin bootstrap — any admin endpoint will
    // do. /admin/settings is the most stable target (always exists, no
    // creation side-effects). If this fails, every other admin-driven E2E
    // (SSO, SCIM, quotas) will also fail.
    const res = await request.get('/api/v2/admin/settings', { headers: adminHeaders() })
    expect(res.status()).toBe(200)
    const body = await res.json()
    expect(body.settings).toBeDefined()
  })
})
