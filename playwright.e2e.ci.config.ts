import { defineConfig } from '@playwright/test';

const DEFAULT_SERVER_URL = 'http://127.0.0.1:8420';
const baseURL = String(process.env.E2E_BASE_URL || process.env.TABURA_TEST_SERVER_URL || DEFAULT_SERVER_URL).trim() || DEFAULT_SERVER_URL;
const managedServerURL = String(process.env.E2E_MANAGED_SERVER_URL || DEFAULT_SERVER_URL).trim() || DEFAULT_SERVER_URL;
const useManagedServer = process.env.E2E_MANAGED_SERVER === '1'
  || (!process.env.E2E_BASE_URL && !process.env.TABURA_TEST_SERVER_URL);
const grepInvert = process.env.PLAYWRIGHT_GREP_INVERT
  ? new RegExp(process.env.PLAYWRIGHT_GREP_INVERT)
  : /@local-only/;

export default defineConfig({
  testDir: 'tests/e2e',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  grepInvert,
  expect: {
    timeout: 10_000,
  },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL,
    headless: true,
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
      },
    },
  ],
  webServer: useManagedServer
    ? {
        command: './scripts/e2e-ci-server.sh',
        url: `${managedServerURL}/api/setup`,
        reuseExistingServer: false,
        timeout: 120_000,
      }
    : undefined,
});
