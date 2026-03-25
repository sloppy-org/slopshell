import { expect, openLiveApp, test } from './live';
import { authenticate } from './helpers';
import { setInteractionTool, setLiveSession, waitForLiveAppReady } from './live-ui';

test.describe('full browser voice flow @local-only', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('dialogue click -> real browser capture -> real STT -> transcript in chat', async ({ page }) => {
    await openLiveApp(page, sessionToken);

    await waitForLiveAppReady(page);
    await setLiveSession(page, 'dialogue', true);
    await setInteractionTool(page, 'prompt');
    await page.waitForTimeout(1000);

    // In the default app contract, Meeting is the boot mode and canvas clicks
    // do not start capture there. This flow explicitly switches to Dialogue so
    // the click-to-record browser path is exercised directly.
    await page.locator('#canvas-viewport').click({
      position: { x: 420, y: 320 },
      timeout: 10_000,
    });

    await expect.poll(async () => {
      return page.evaluate(() => {
        const app = (window as any)._taburaApp;
        const state = app?.getState?.();
        return String(state?.voiceLifecycle || '');
      });
    }, { timeout: 8_000 }).toBe('recording');

    // Let the fake device feed the speech segment into the real browser
    // capture path. If VAD has not auto-stopped yet, stop manually so this
    // E2E stays focused on browser capture -> STT -> chat delivery.
    await page.waitForTimeout(3_000);
    const stillRecording = await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      const state = app?.getState?.();
      return String(state?.voiceLifecycle || '') === 'recording';
    });
    if (stillRecording) {
      await page.locator('#canvas-viewport').click({
        position: { x: 420, y: 320 },
        timeout: 10_000,
      });
    }

    // Wait for STT to process the captured audio and for the transcript to
    // appear in the UI.
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
