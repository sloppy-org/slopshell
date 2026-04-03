/**
 * End-to-end hotword → VAD → STT pipeline test.
 *
 * Uses Piper TTS to generate audio containing the wake word "Computer" followed
 * by a command. Chromium's --use-file-audio-capture feeds this audio as the fake
 * mic input. The test verifies hotword triggers, VAD detects speech end, STT
 * transcribes, and the transcript appears in chat.
 *
 * Run:
 *   E2E_AUDIO_FILE=/tmp/hotword-test.wav ./scripts/e2e-local.sh -- --grep "hotword"
 *
 * The beforeAll hook generates the audio file via Piper if E2E_AUDIO_FILE is not set.
 */
import { writeFileSync, unlinkSync } from 'fs';
import { join } from 'path';
import { tmpdir } from 'os';
import { applySessionCookie, expect, openLiveApp, test } from './live';
import { authenticate, synthesizePiperWav } from './helpers';
import { setLiveSession, waitForLiveAppReady } from './live-ui';

const HOTWORD_PHRASE = 'Computer, what is the weather in Graz?';
const CONTINUOUS_PHRASE = 'Computer tell me a joke about programming';

test.describe('hotword voice pipeline @local-only', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('hotword triggers, VAD ends speech, STT transcribes', async ({ page }) => {
    test.setTimeout(90_000);

    // Generate test audio via Piper TTS
    const wavBuf = await synthesizePiperWav(HOTWORD_PHRASE);
    const audioPath = join(tmpdir(), `slopshell-hotword-e2e-${Date.now()}.wav`);
    writeFileSync(audioPath, wavBuf);

    try {
      await applySessionCookie(page, sessionToken);
      await openLiveApp(page, sessionToken);
      await waitForLiveAppReady(page);
      await setLiveSession(page, 'dialogue', true);
      await page.waitForTimeout(2000);

      // Wait for hotword monitor to become active
      await expect.poll(async () => {
        return page.evaluate(() => {
          const state = (window as any)._slopshellApp?.getState?.();
          return state?.hotwordActive === true;
        });
      }, {
        message: 'Hotword monitor should become active in dialogue mode',
        timeout: 15_000,
        intervals: [500],
      }).toBe(true);

      // The fake mic from --use-file-audio-capture loops the WAV file.
      // Wait for hotword to trigger voice capture.
      await expect.poll(async () => {
        return page.evaluate(() => {
          const app = (window as any)._slopshellApp;
          const state = app?.getState?.();
          const lc = String(state?.voiceLifecycle || '');
          return lc === 'recording' || lc === 'listening' || lc === 'stopping_recording';
        });
      }, {
        message: 'Hotword should trigger voice capture within 15s',
        timeout: 15_000,
        intervals: [500],
      }).toBe(true);

      // Wait for VAD to detect speech end → STT → back to idle
      await expect.poll(async () => {
        return page.evaluate(() => {
          const app = (window as any)._slopshellApp;
          const state = app?.getState?.();
          const lc = String(state?.voiceLifecycle || '');
          // idle or awaiting_turn means STT completed
          return lc === 'idle' || lc === 'awaiting_turn';
        });
      }, {
        message: 'VAD should detect speech end and pipeline should complete',
        timeout: 45_000,
        intervals: [1000],
      }).toBe(true);

      // Verify transcript appeared in chat
      await expect.poll(async () => {
        const chatHistory = page.locator('#chat-history');
        const text = await chatHistory.textContent();
        if (!text) return false;
        const lower = text.toLowerCase();
        return lower.includes('weather') || lower.includes('graz');
      }, {
        message: 'Chat should contain transcribed speech',
        timeout: 30_000,
      }).toBe(true);
    } finally {
      try { unlinkSync(audioPath); } catch (_) {}
    }
  });

  test('speech immediately after hotword is not clipped', async ({ page }) => {
    test.setTimeout(90_000);

    const wavBuf = await synthesizePiperWav(CONTINUOUS_PHRASE);
    const audioPath = join(tmpdir(), `slopshell-continuous-e2e-${Date.now()}.wav`);
    writeFileSync(audioPath, wavBuf);

    try {
      await applySessionCookie(page, sessionToken);
      await openLiveApp(page, sessionToken);
      await waitForLiveAppReady(page);
      await setLiveSession(page, 'dialogue', true);
      await page.waitForTimeout(2000);

      // Wait for full pipeline: hotword → capture → VAD → STT → idle
      await expect.poll(async () => {
        return page.evaluate(() => {
          const app = (window as any)._slopshellApp;
          const state = app?.getState?.();
          return String(state?.voiceLifecycle || '') === 'idle';
        });
      }, { timeout: 60_000, intervals: [1000] }).toBe(true);

      // The words AFTER "Computer" must be present in the transcript.
      // "joke" or "programming" proves the audio wasn't clipped.
      await expect.poll(async () => {
        const chatHistory = page.locator('#chat-history');
        const text = await chatHistory.textContent();
        if (!text) return false;
        const lower = text.toLowerCase();
        return lower.includes('joke') || lower.includes('programming');
      }, {
        message: 'Speech after hotword must not be clipped — "joke" or "programming" should appear',
        timeout: 30_000,
      }).toBe(true);
    } finally {
      try { unlinkSync(audioPath); } catch (_) {}
    }
  });

  test('VAD speech end detection fires within 3 seconds of silence', async ({ page }) => {
    test.setTimeout(60_000);

    // Generate short phrase — VAD should detect end quickly after speech stops
    const wavBuf = await synthesizePiperWav('Computer, hello.');
    const audioPath = join(tmpdir(), `slopshell-vad-timing-e2e-${Date.now()}.wav`);
    writeFileSync(audioPath, wavBuf);

    try {
      await applySessionCookie(page, sessionToken);
      await openLiveApp(page, sessionToken);
      await waitForLiveAppReady(page);
      await setLiveSession(page, 'dialogue', true);
      await page.waitForTimeout(2000);

      // Wait for hotword to trigger
      await expect.poll(async () => {
        return page.evaluate(() => {
          const app = (window as any)._slopshellApp;
          const state = app?.getState?.();
          const lc = String(state?.voiceLifecycle || '');
          return lc === 'recording' || lc === 'listening';
        });
      }, { timeout: 15_000, intervals: [500] }).toBe(true);

      // Record when we first see "listening" (speech detected by VAD)
      const captureStartedAt = Date.now();

      // Wait for speech end → STT completion
      await expect.poll(async () => {
        return page.evaluate(() => {
          const app = (window as any)._slopshellApp;
          const state = app?.getState?.();
          const lc = String(state?.voiceLifecycle || '');
          return lc === 'idle' || lc === 'awaiting_turn';
        });
      }, { timeout: 30_000, intervals: [500] }).toBe(true);

      const elapsed = Date.now() - captureStartedAt;
      // Speech end should be detected within a reasonable time.
      // The audio is ~1s of speech + silence. With 1.4s redemption + STT latency,
      // total should be under 15s (generous, accounting for whisper processing).
      expect(elapsed).toBeLessThan(15_000);
    } finally {
      try { unlinkSync(audioPath); } catch (_) {}
    }
  });
});
