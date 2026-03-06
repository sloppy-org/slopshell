import { defineConfig, devices } from '@playwright/test';

const grepInvert = process.env.PLAYWRIGHT_GREP_INVERT
  ? new RegExp(process.env.PLAYWRIGHT_GREP_INVERT)
  : undefined;

export default defineConfig({
  testDir: 'tests/playwright',
  timeout: 30_000,
  fullyParallel: false,
  workers: process.env.CI ? 1 : 2,
  grepInvert,
  expect: {
    timeout: 5_000,
  },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: 'http://127.0.0.1:4173',
    headless: true,
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
      grep: /safari-recorder/,
    },
  ],
  webServer: {
    command: 'python3 -m http.server 4173 --bind 127.0.0.1',
    port: 4173,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
