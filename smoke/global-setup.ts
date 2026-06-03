import { request } from '@playwright/test'
import fs from 'fs'
import path from 'path'

// Playwright runs the webServer (bin/indelible-test) once for the whole test
// invocation, then runs every test file against that single server instance.
//
// The bootstrap admin is seeded from INDELIBLE_ADMIN_EMAIL / INDELIBLE_ADMIN_PASSWORD
// (set to ADMIN_CREDS in CI) — self-registration is disabled by default and no
// longer grants admin. So this globalSetup logs in as the seeded admin, then
// enables self-registration so the tests that exercise the register flow work
// (those users get the read-only default). It writes the admin bearer token to
// .auth/admin.json; tests load it via helpers/auth.ts for admin-protected calls.

const AUTH_DIR = path.join(__dirname, '.auth')
const ADMIN_FILE = path.join(AUTH_DIR, 'admin.json')

export const ADMIN_CREDS = {
  email: 'admin@e2e-test.com',
  password: 'TestPassword123!',
  first_name: 'E2E',
  last_name: 'Admin',
}

export default async function globalSetup() {
  fs.mkdirSync(AUTH_DIR, { recursive: true })

  const ctx = await request.newContext({ baseURL: 'http://localhost:8080' })

  // The Playwright webServer config waits for /health to respond, but races
  // can still happen on first start. Give it a short retry window.
  for (let i = 0; i < 30; i++) {
    try {
      const h = await ctx.get('/health')
      if (h.ok()) break
    } catch {}
    await new Promise((r) => setTimeout(r, 500))
  }

  // Log in as the seeded bootstrap admin (registration is disabled by default,
  // so there's nothing to register).
  const login = await ctx.post('/api/v2/auth/login', {
    data: { email: ADMIN_CREDS.email, password: ADMIN_CREDS.password },
  })
  if (login.status() !== 200) {
    throw new Error(`E2E admin login failed: ${login.status()} ${await login.text()}`)
  }
  const token = (await login.json()).token

  // Enable self-registration so tests exercising the register flow work; those
  // users get the read-only default. Bearer callers are CSRF-exempt.
  const patch = await ctx.patch('/api/v2/admin/settings', {
    headers: { Authorization: `Bearer ${token}` },
    data: { registration_enabled: 'true' },
  })
  if (!patch.ok()) {
    throw new Error(`E2E enable registration failed: ${patch.status()} ${await patch.text()}`)
  }

  fs.writeFileSync(ADMIN_FILE, JSON.stringify({ token, ...ADMIN_CREDS }, null, 2))
  await ctx.dispose()
}
