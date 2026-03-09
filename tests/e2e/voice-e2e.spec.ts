import { expect, test } from './live';
import { authenticate } from './helpers';

test.describe('full browser voice flow', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('mic click -> real VAD -> real STT -> transcript in chat', async ({ page }) => {
    if (sessionToken) {
      await page.context().addCookies([{
        name: 'tabura_session',
        value: sessionToken,
        domain: '127.0.0.1',
        path: '/',
      }]);
    }

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Wait for app to initialize and WS to connect
    await page.waitForFunction(() => {
      const indicator = document.getElementById('indicator');
      return indicator !== null;
    }, null, { timeout: 10_000 });
    await page.waitForTimeout(1000);

    // Click to start recording (the fake audio device streams our Piper WAV)
    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);

    // Verify recording indicator appears
    const indicator = page.locator('#indicator');
    await expect(indicator).toBeVisible({ timeout: 5_000 });

    // Wait for VAD to detect speech end from the silence padding in the WAV
    // and for STT to process the audio. This can take up to 30s total.
    // The flow is: VAD auto-stop -> audio sent to server -> STT transcription -> result in UI
    await expect.poll(async () => {
      const chatHistory = page.locator('#chat-history');
      const text = await chatHistory.textContent();
      return text && text.trim().length > 0;
    }, {
      message: 'Chat history should contain transcript from voice input',
      timeout: 45_000,
      intervals: [1000],
    }).toBeTruthy();

    // Verify transcript is non-empty and plausible (came from "Hello, this is a test of voice recording")
    const chatText = await page.locator('#chat-history').textContent();
    expect(chatText!.trim().length).toBeGreaterThan(3);
  });
});
