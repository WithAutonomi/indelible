import { request } from '@playwright/test'
import fs from 'fs'
import path from 'path'

// Playwright runs the webServer (bin/indelible-test) once for the whole test
// invocation, then runs every test file against that single server instance.
// "First user becomes admin" only fires for the literal first /register call,
// so individual test files can't each create their own admin — whoever wins
// the race becomes admin and everyone else gets the read-only default.
//
// This globalSetup registers a single known admin before any tests run and
// writes the resulting bearer token to e2e/.auth/admin.json. Tests load that
// file via helpers/auth.ts when they need to drive admin-protected endpoints.

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

  let token: string
  const reg = await ctx.post('/api/v2/auth/register', { data: ADMIN_CREDS })
  if (reg.status() === 201) {
    token = (await reg.json()).token
  } else if (reg.status() === 409) {
    // Server was reused (reuseExistingServer or a flaky DB-on-disk path).
    // Fall back to login so re-runs stay green.
    const login = await ctx.post('/api/v2/auth/login', {
      data: { email: ADMIN_CREDS.email, password: ADMIN_CREDS.password },
    })
    if (login.status() !== 200) {
      throw new Error(`E2E admin login failed: ${login.status()} ${await login.text()}`)
    }
    token = (await login.json()).token
  } else {
    throw new Error(`E2E admin register failed: ${reg.status()} ${await reg.text()}`)
  }

  fs.writeFileSync(ADMIN_FILE, JSON.stringify({ token, ...ADMIN_CREDS }, null, 2))
  await ctx.dispose()
}
