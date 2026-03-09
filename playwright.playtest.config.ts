import { defineConfig } from '@playwright/test';

const audioFile = process.env.E2E_AUDIO_FILE;
if (!audioFile) {
  throw new Error('E2E_AUDIO_FILE env var required (path to speech WAV for fake mic input)');
}

export default defineConfig({
  testDir: 'tests',
  testMatch: /(?:e2e|playtest)\/.*\.spec\.ts$/,
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  expect: {
    timeout: 10_000,
  },
  outputDir: 'test-results/playtest',
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'playwright-report/playtest' }],
    ['./tests/playtest/github-reporter.cjs'],
  ],
  use: {
    baseURL: 'http://127.0.0.1:8420',
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        launchOptions: {
          args: [
            '--use-fake-device-for-media-stream',
            '--use-fake-ui-for-media-stream',
            `--use-file-audio-capture=${audioFile}`,
          ],
        },
      },
    },
  ],
});
