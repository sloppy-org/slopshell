import { defineConfig } from '@playwright/test';

const audioFile = String(process.env.E2E_AUDIO_FILE || '').trim();
const grepInvert = process.env.PLAYWRIGHT_GREP_INVERT
  ? new RegExp(process.env.PLAYWRIGHT_GREP_INVERT)
  : undefined;

const launchArgs = [
  '--use-fake-device-for-media-stream',
  '--use-fake-ui-for-media-stream',
];
if (audioFile) {
  launchArgs.push(`--use-file-audio-capture=${audioFile}`);
}

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
    baseURL: process.env.E2E_BASE_URL || process.env.TABURA_TEST_SERVER_URL || 'http://127.0.0.1:8420',
    headless: true,
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        launchOptions: {
          args: launchArgs,
        },
      },
    },
  ],
});
