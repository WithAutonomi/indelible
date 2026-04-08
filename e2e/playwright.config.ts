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
    env: {
      INDELIBLE_DB_URL: 'sqlite://:memory:',
      INDELIBLE_JWT_SECRET: 'e2e-test-secret-minimum-32-characters-long',
      INDELIBLE_WALLET_ENCRYPTION_KEY: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab',
      INDELIBLE_DATA_DIR: '/tmp/indelible-e2e',
    },
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
})
