import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  timeout: 30000,
  retries: 1,
  // HTML reporter writes playwright-report/ which CI uploads as an artifact.
  // List reporter keeps the console output usable.
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report', open: 'never' }],
  ],
  // Registers a single shared admin before tests run; see global-setup.ts for
  // why the "first user becomes admin" bootstrap can't be done per-test.
  globalSetup: './global-setup.ts',
  use: {
    baseURL: 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  webServer: {
    command: '../bin/indelible-test',
    port: 8080,
    reuseExistingServer: false,
    timeout: 15000,
    // Env vars are set externally (CI step env or shell env for local dev).
    // Do NOT set webServer.env here — it replaces process.env entirely,
    // stripping PATH and other essentials needed to run the binary.
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
})
