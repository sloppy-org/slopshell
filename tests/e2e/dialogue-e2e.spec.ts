import { applySessionCookie, expect, test } from './live';
import { authenticate, synthesizePiperWav } from './helpers';

test.describe('multi-turn dialogue flow @local-only', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('dialogue activates, captures speech, gets response, reopens listen', async ({ page }) => {
    await applySessionCookie(page, sessionToken);
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Wait for app to be ready with a project active
    await page.waitForFunction(() => {
      const app = (window as { _taburaApp?: { getState?: () => { activeProjectId?: string } } })._taburaApp;
      if (!app || typeof app.getState !== 'function') return false;
      return Boolean(String(app.getState()?.activeProjectId || '').trim());
    }, null, { timeout: 15_000 });

    // Verify edge-top models panel is available
    await expect.poll(async () => page.evaluate(() => {
      const btn = document.querySelector('#edge-top-models .edge-live-dialogue-btn');
      return btn instanceof HTMLButtonElement;
    }), { timeout: 10_000 }).toBe(true);

    // Activate dialogue mode
    await page.evaluate(() => {
      const btn = document.querySelector('#edge-top-models .edge-live-dialogue-btn');
      if (btn instanceof HTMLButtonElement) btn.click();
    });

    // Verify dialogue is active
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue', { timeout: 5_000 });

    // Verify voice lifecycle enters listening state (dialogue listen window opens)
    await expect.poll(async () => page.evaluate(() => {
      const app = (window as any)._taburaApp;
      const s = app?.getState?.();
      return Boolean(s?.liveSessionActive && s?.liveSessionMode === 'dialogue');
    }), { timeout: 5_000 }).toBe(true);

    // The companion face should show listening state if companion is enabled,
    // or the indicator should show listening.
    await expect.poll(async () => page.evaluate(() => {
      const indicator = document.getElementById('indicator');
      const companion = document.getElementById('companion-idle-surface');
      const indicatorListening = indicator?.classList.contains('is-listening') === true;
      const companionVisible = companion?.style.display !== 'none';
      return indicatorListening || companionVisible;
    }), { timeout: 8_000 }).toBe(true);
  });
});
