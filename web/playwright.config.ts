import { defineConfig, devices } from '@playwright/test'

export const authState = 'e2e/.auth/state.json'

// Runs against an already running server (the Docker image in CI, see
// `task e2e`). Set E2E_BASE_URL to point elsewhere.
export default defineConfig({
  testDir: './e2e',
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
  },
  projects: [
    // Log in once and share the session: logins are rate limited.
    { name: 'setup', testMatch: /auth\.setup\.ts/ },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], storageState: authState },
      dependencies: ['setup'],
    },
  ],
})
