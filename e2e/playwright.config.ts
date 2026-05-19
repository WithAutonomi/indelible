import { defineConfig } from '@playwright/test'

// Real E2E suite — full user journeys (upload → list → download, admin
// provisions group + quota → user upload blocked, etc). Unlike smoke/,
// tests here may rely on a running antd binary, a configured wallet, and
// real worker processing. Expect minutes per run, not seconds.
//
// Not run in CI — scripts/ci-dev1.sh runs this against dev1 on demand.
// Tests should self-skip when their preconditions (antd, wallet, etc.)
// aren't met so partial environments still get useful signal.

export default defineConfig({
  testDir: './tests',
  // Long timeout — real uploads through antd can take 30-60s for tiny files.
  timeout: 120_000,
  // No retries: e2e tests are stateful (real network, real worker), and a
  // retry that re-uses provider/wallet/file state usually masks the real
  // failure with a duplicate-row error.
  retries: 0,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report', open: 'never' }],
  ],
  globalSetup: './global-setup.ts',
  use: {
    baseURL: 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  webServer: {
    command: '../bin/indelible-test',
    port: 8080,
    reuseExistingServer: false,
    // Generous because indelible startup spawns + waits for antd.
    timeout: 60_000,
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
})
