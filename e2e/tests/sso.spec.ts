import { test, expect, request as playwrightRequest } from '@playwright/test'
import { adminHeaders } from '../helpers/auth'

// SSO E2E walking the full authorize → Dex → callback → home flow.
//
// Requires the `dex` service from .github/workflows/ci.yml to be reachable
// at http://localhost:5556. Locally, run dex via:
//   docker run -d --name dex -p 5556:5556 \
//     -v $PWD/deploy/dex:/etc/dex/cfg dexidp/dex:v2.41.1 \
//     dex serve /etc/dex/cfg/config.yaml
//
// The test skips when Dex isn't reachable so non-CI dev runs stay green
// without the dependency.

const DEX_URL = 'http://localhost:5556'
const DEX_ISSUER = 'http://localhost:5556'
const DEX_CLIENT_ID = 'indelible-e2e'
const DEX_CLIENT_SECRET = 'indelible-e2e-secret'
const DEX_USER_EMAIL = 'alice@example.com'
const DEX_USER_PASSWORD = 'password'

async function isDexUp(): Promise<boolean> {
  try {
    const ctx = await playwrightRequest.newContext()
    const res = await ctx.get(`${DEX_URL}/.well-known/openid-configuration`)
    await ctx.dispose()
    return res.ok()
  } catch {
    return false
  }
}

// Tests run in file order. The no_account test MUST run before the
// auto-provision test — once auto-provision runs, alice is created in the
// local users table with external_id == her Dex sub. Any later OIDC sign-in
// (even on a different provider with auto_provision=false) will SCIM-correlate
// on external_id and silently log her in, defeating the no_account check.
//
// Retries are also disabled: a failed test would create a second provider
// row on retry, putting two same-named buttons on /login and breaking the
// strict locator. Better to surface the real error once than to mask it.
test.describe.configure({ retries: 0 })

test.describe('SSO end-to-end via Dex', () => {
  test.beforeAll(async () => {
    if (!(await isDexUp())) {
      test.skip(true, `Dex not reachable at ${DEX_URL} — start it via docker or use CI`)
    }
  })

  test('no_account error round-trips to /login?error=no_account when auto_provision is off', async ({ page, request }) => {
    // Fresh DB at this point (globalSetup only registered the admin). Create
    // a provider with auto_provision left at its default (false). alice has
    // no local users row and no external_id match, so the service should
    // refuse to invent an account.
    const createRes = await request.post('/api/v2/admin/oidc/providers', {
      headers: adminHeaders(),
      data: {
        name: 'dex-strict',
        display_name: 'Dex Strict',
        issuer_url: DEX_ISSUER,
        client_id: DEX_CLIENT_ID,
        client_secret: DEX_CLIENT_SECRET,
        scopes: 'openid,email,profile',
      },
    })
    expect(createRes.status(), `create dex-strict provider: ${await createRes.text()}`).toBe(201)

    await page.goto('/login')
    await page.getByRole('button', { name: 'Sign in with Dex Strict', exact: true }).click()
    await page.waitForURL((url) => url.host.startsWith('localhost:5556'), { timeout: 15000 })
    await page.locator('input[name="login"]').fill(DEX_USER_EMAIL)
    await page.locator('input[name="password"]').fill(DEX_USER_PASSWORD)
    await Promise.all([
      page.waitForURL(/error=no_account/, { timeout: 15000 }),
      page.locator('button[type="submit"]').click(),
    ])
    expect(page.url()).toContain('error=no_account')
  })

  test('admin configures provider, user signs in via Dex, lands on dashboard', async ({ page, request }) => {
    // Now turn on auto-provision via a second provider. alice still has no
    // local account at this point (the no_account test above didn't create
    // one), so this exercises the create-user path cleanly.
    const createRes = await request.post('/api/v2/admin/oidc/providers', {
      headers: adminHeaders(),
      data: {
        name: 'dex-open',
        display_name: 'Dex Open',
        issuer_url: DEX_ISSUER,
        client_id: DEX_CLIENT_ID,
        client_secret: DEX_CLIENT_SECRET,
        scopes: 'openid,email,profile',
      },
    })
    expect(createRes.status(), `create dex-open provider: ${await createRes.text()}`).toBe(201)
    const providerID = (await createRes.json()).id as number

    const apRes = await request.put(`/api/v2/admin/oidc/providers/${providerID}/auto-provision`, {
      headers: adminHeaders(),
      data: { auto_provision: true, default_group_id: 0 },
    })
    expect(apRes.status()).toBe(200)

    await page.goto('/login')
    const ssoButton = page.getByRole('button', { name: 'Sign in with Dex Open', exact: true })
    await expect(ssoButton).toBeVisible()

    await ssoButton.click()
    await page.waitForURL((url) => url.host.startsWith('localhost:5556'), { timeout: 15000 })
    await page.locator('input[name="login"]').fill(DEX_USER_EMAIL)
    await page.locator('input[name="password"]').fill(DEX_USER_PASSWORD)
    await Promise.all([
      page.waitForURL((url) => !url.host.startsWith('localhost:5556'), { timeout: 15000 }),
      page.locator('button[type="submit"]').click(),
    ])

    expect(page.url()).not.toMatch(/[?&]error=/)
    expect(page.url()).toContain('localhost:8080')

    const meRes = await page.request.get('/api/v2/me')
    expect(meRes.ok()).toBeTruthy()
    const me = await meRes.json()
    expect(me.email).toBe(DEX_USER_EMAIL)
  })
})
