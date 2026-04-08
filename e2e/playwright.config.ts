import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  timeout: 30000,
  retries: 1,
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
