import { test, expect } from '@playwright/test'
import { adminHeaders } from '../helpers/auth'
import { TEST_WALLET_KEY } from '../helpers/fixtures'

// API-driven admin E2E (V2-281 item 5). Goes through the real HTTP stack
// (admin middleware, validators, DB) without taking on Vue-form brittleness.
// The UI gets surface-level coverage from individual view smoke tests; these
// validate the contract a customer's integration would target.
//
// Admin token comes from globalSetup (single shared admin registered once
// per playwright invocation). See e2e/global-setup.ts for the reasoning —
// "first user becomes admin" can't be done per-test against a shared server.

test.describe('Admin', () => {
  test('register via API works for a new non-admin user', async ({ request }) => {
    // globalSetup already owns admin@e2e-test.com. This exercises the register
    // path for a second, non-admin user (a regression canary if the server
    // doesn't come up). Registration is anti-enumeration (V2-430): a neutral
    // 202, no token.
    const response = await request.post('/api/v2/auth/register', {
      data: {
        email: `api-test-${Date.now()}@e2e.com`,
        password: 'TestPassword123!',
        first_name: 'API',
        last_name: 'Test',
      },
    })
    expect(response.status()).toBe(202)
  })

  test('settings PATCH happy path + GET reflects', async ({ request }) => {
    const headers = adminHeaders()

    const patchRes = await request.patch('/api/v2/admin/settings', {
      headers,
      data: { environment_name: 'staging', timezone: 'America/New_York' },
    })
    expect(patchRes.status()).toBe(200)

    const getRes = await request.get('/api/v2/admin/settings', { headers })
    expect(getRes.status()).toBe(200)
    const settings = (await getRes.json()).settings
    expect(settings.environment_name).toBe('staging')
    expect(settings.timezone).toBe('America/New_York')
  })

  test('user CRUD: create service account, list shows it, delete removes it', async ({ request }) => {
    const headers = adminHeaders()
    const svcEmail = `svc-${Date.now()}@e2e.com`

    const createRes = await request.post('/api/v2/admin/users/service-accounts', {
      headers,
      data: { email: svcEmail, first_name: 'Service', last_name: 'Account', permissions: 'write' },
    })
    expect(createRes.status(), `create service account: ${await createRes.text()}`).toBe(201)
    const svcID = (await createRes.json()).id as number
    expect(svcID).toBeGreaterThan(0)

    const listRes = await request.get('/api/v2/admin/users', { headers })
    expect(listRes.status()).toBe(200)
    const users = (await listRes.json()).users as any[]
    expect(users.find(u => u.id === svcID)?.is_service_account).toBe(true)

    const delRes = await request.delete(`/api/v2/admin/users/${svcID}`, { headers })
    expect(delRes.status()).toBe(200)

    // List again — soft-delete should hide it from the active list.
    const listRes2 = await request.get('/api/v2/admin/users', { headers })
    const users2 = (await listRes2.json()).users as any[]
    expect(users2.find(u => u.id === svcID)).toBeUndefined()
  })

  test('group CRUD: create, list, update name, delete', async ({ request }) => {
    const headers = adminHeaders()
    const groupName = `engineering-${Date.now()}`

    const createRes = await request.post('/api/v2/admin/groups', {
      headers,
      data: { name: groupName, description: 'eng dept', permission_level: 'write' },
    })
    expect(createRes.status(), `create group: ${await createRes.text()}`).toBe(201)
    const groupID = (await createRes.json()).id as number

    const listRes = await request.get('/api/v2/admin/groups', { headers })
    expect(listRes.status()).toBe(200)
    const groups = (await listRes.json()).groups as any[]
    expect(groups.find(g => g.id === groupID)?.name).toBe(groupName)

    const renamed = `${groupName}-renamed`
    const updateRes = await request.put(`/api/v2/admin/groups/${groupID}`, {
      headers,
      data: { name: renamed, description: 'renamed', permission_level: 'write' },
    })
    expect(updateRes.status()).toBe(200)

    const listRes2 = await request.get('/api/v2/admin/groups', { headers })
    expect((await listRes2.json()).groups.find((g: any) => g.id === groupID)?.name).toBe(renamed)

    const delRes = await request.delete(`/api/v2/admin/groups/${groupID}`, { headers })
    expect(delRes.status()).toBe(200)
  })

  test('quota create + list returns the row with usage', async ({ request }) => {
    const headers = adminHeaders()

    // System-wide quota (no entity_id needed).
    const createRes = await request.post('/api/v2/admin/quotas', {
      headers,
      data: { entity_type: 'system', max_bytes: 10737418240 }, // 10 GB
    })
    expect(createRes.status(), `create quota: ${await createRes.text()}`).toBe(201)
    const quotaID = (await createRes.json()).id as number

    const listRes = await request.get('/api/v2/admin/quotas', { headers })
    expect(listRes.status()).toBe(200)
    const quotas = (await listRes.json()).quotas as any[]
    const found = quotas.find(q => q.id === quotaID)
    expect(found).toBeDefined()
    expect(found.entity_type).toBe('system')
    expect(found.max_bytes).toBe(10737418240)
    expect(typeof found.used_bytes).toBe('number') // 0 initially, but the field must be present
  })

  test('wallet create makes it default + GET wallet-status returns the configured address', async ({ request }) => {
    const headers = adminHeaders()

    const createRes = await request.post('/api/v2/admin/wallets', {
      headers,
      data: { name: `primary-${Date.now()}`, private_key: TEST_WALLET_KEY },
    })
    expect(createRes.status(), `create wallet: ${await createRes.text()}`).toBe(201)
    // Handler wraps the wallet in {message, wallet}; tests read from .wallet.
    const created = (await createRes.json()).wallet
    expect(created.is_default).toBe(true)
    expect(created.address).toMatch(/^0x[a-fA-F0-9]{40}$/)

    // wallet-status endpoint (authenticated, available to any user) should
    // now report a configured default wallet.
    const statusRes = await request.get('/api/v2/system/wallet-status', { headers })
    expect(statusRes.status()).toBe(200)
    expect((await statusRes.json()).has_default_wallet).toBe(true)

    // The admin wallets list should also include this wallet.
    const listRes = await request.get('/api/v2/admin/wallets', { headers })
    expect(listRes.status()).toBe(200)
    const wallets = (await listRes.json()).wallets as any[]
    const found = wallets.find(w => w.id === created.id)
    expect(found?.address?.toLowerCase()).toBe(created.address.toLowerCase())
  })

  test('non-admin user is rejected from admin endpoints', async ({ request }) => {
    // globalSetup already owns the admin; register a fresh non-admin, then log
    // in for a token (registration no longer auto-logs-in — V2-430) and confirm
    // they get 403 on an admin route.
    const email = `plain-${Date.now()}@e2e-test.com`
    const regRes = await request.post('/api/v2/auth/register', {
      data: { email, password: 'TestPassword123!', first_name: 'Plain', last_name: 'User' },
    })
    expect(regRes.status()).toBe(202)

    const loginRes = await request.post('/api/v2/auth/login', {
      data: { email, password: 'TestPassword123!' },
    })
    expect(loginRes.ok()).toBeTruthy()
    const plainToken: string = (await loginRes.json()).token

    const res = await request.get('/api/v2/admin/users', {
      headers: { Authorization: `Bearer ${plainToken}` },
    })
    expect(res.status()).toBe(403)
  })
})
