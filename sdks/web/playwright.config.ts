import { defineConfig, devices } from '@playwright/test';

const PORT = 5173;
const BASE_URL = `http://localhost:${PORT}`;

const RECEIVER_PORT = 4010;
const RECEIVER_URL = `http://localhost:${RECEIVER_PORT}`;

export default defineConfig({
  testDir: './tests/e2e',
  // All e2e tests share ONE mock receiver process (single global state: stored
  // reports + mode), so they must not run concurrently. Serialize the run.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: [['list']],
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
  webServer: [
    {
      command: 'npm run dev',
      url: BASE_URL,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      // Local mock receiver (ASSUMED contract). Own port => real cross-origin.
      command: 'node tools/mock-receiver/server.mjs',
      url: `${RECEIVER_URL}/__health`,
      reuseExistingServer: !process.env.CI,
      timeout: 30_000,
      env: {
        ALLOWED_ORIGIN: BASE_URL,
      },
    },
  ],
});
