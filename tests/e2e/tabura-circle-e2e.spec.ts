import { applySessionCookie, expect, openLiveApp, test } from './live';
import {
  assertCircleNoOverlap,
  circleSegment,
  clickCircleSegment,
  openCircle,
  resetCircleRuntimeState,
  setCircleToggle,
  setInteractionTool,
  setLiveSession,
  waitForLiveAppReady,
} from './live-ui';
import { authenticate } from './helpers';

test.describe('Tabura Circle live runtime @local-only', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('page opens in meeting mode with silent and fast off', async ({ page }) => {
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await expect.poll(async () => {
      return String(await page.locator('#edge-top-models .edge-live-status').textContent() || '').trim();
    }, { timeout: 15_000 }).toContain('Meeting');
    await expect(circleSegment(page, 'meeting')).toHaveAttribute('aria-pressed', 'true');
    await expect(circleSegment(page, 'silent')).toHaveAttribute('aria-pressed', 'false');
    await expect(circleSegment(page, 'fast')).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('body')).not.toHaveClass(/silent-mode/);
  });

  test('desktop clicks reach every segment state and persist across refresh', async ({ page }) => {
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await assertCircleNoOverlap(page);

    await setLiveSession(page, 'dialogue', true);
    await setLiveSession(page, 'meeting', true);

    await setCircleToggle(page, 'silent', true);
    await setCircleToggle(page, 'fast', true);
    await expect(page.locator('body')).toHaveClass(/silent-mode/);

    await setInteractionTool(page, 'prompt');
    await setInteractionTool(page, 'text_note');
    await setInteractionTool(page, 'highlight');
    await setInteractionTool(page, 'ink');
    await setInteractionTool(page, 'pointer');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'pointer');

    await page.reload({ waitUntil: 'networkidle' });
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);

    await expect(circleSegment(page, 'meeting')).toHaveAttribute('aria-pressed', 'true');
    await expect(circleSegment(page, 'silent')).toHaveAttribute('aria-pressed', 'false');
    await expect(circleSegment(page, 'fast')).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'pointer');
  });
});

test.describe('Tabura Circle mobile tap runtime @local-only', () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });

  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('mobile taps hit live mode, toggle, and tool segments cleanly', async ({ page }) => {
    await applySessionCookie(page, sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page, 'touch');

    await openCircle(page, 'touch');
    await assertCircleNoOverlap(page);

    await clickCircleSegment(page, 'dialogue', 'touch');
    await expect(circleSegment(page, 'dialogue')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'meeting', 'touch');
    await expect(circleSegment(page, 'meeting')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'silent', 'touch');
    await expect(circleSegment(page, 'silent')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'fast', 'touch');
    await expect(circleSegment(page, 'fast')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'prompt', 'touch');
    await expect(circleSegment(page, 'prompt')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'text_note', 'touch');
    await expect(circleSegment(page, 'text_note')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'highlight', 'touch');
    await expect(circleSegment(page, 'highlight')).toHaveAttribute('aria-pressed', 'true');

    await clickCircleSegment(page, 'ink', 'touch');
    await expect(circleSegment(page, 'ink')).toHaveAttribute('aria-pressed', 'true');
  });
});
