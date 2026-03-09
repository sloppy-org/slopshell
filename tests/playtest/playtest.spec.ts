import { expect, test, annotatePlaytest, openLiveApp, applySessionCookie } from '../e2e/live';
import { authenticate } from '../e2e/helpers';

async function openTopEdge(page: Parameters<typeof openLiveApp>[0]) {
  await page.mouse.move(160, 2);
  const edgeTop = page.locator('#edge-top');
  await expect(edgeTop).toHaveClass(/edge-active/);
  await expect(page.locator('#edge-top-projects')).toBeVisible();
  await expect(page.locator('#edge-top-models')).toBeVisible();
}

test.describe('live playtest smoke', () => {
  let sessionToken = '';

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('edge chrome opens and escape collapses the live shell', async ({ page }, testInfo) => {
    annotatePlaytest(testInfo, {
      tested: 'Live canvas shell navigation via top, right, and left edge controls.',
      expected: 'The edge panels should open on the real app, the file sidebar should toggle, and Escape should collapse transient UI.',
      steps: [
        './scripts/playtest.sh --grep "edge chrome opens and escape collapses the live shell"',
        'Open the live app at http://127.0.0.1:8420/ and trigger the top, right, and left edge controls.',
      ],
    });

    await openLiveApp(page, sessionToken);
    await expect(page.locator('#workspace')).toBeVisible();
    await expect(page.locator('#canvas-column')).toBeVisible();

    await openTopEdge(page);

    await page.locator('#edge-right-tap').click();
    await expect(page.locator('#edge-right')).toHaveClass(/edge-pinned/);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('body')).toHaveClass(/file-sidebar-open/);

    await page.keyboard.press('Escape');
    await expect(page.locator('body')).not.toHaveClass(/file-sidebar-open/);

    await page.keyboard.press('Escape');
    await expect(page.locator('#edge-right')).not.toHaveClass(/edge-pinned/);
    await expect(page.locator('#edge-top')).not.toHaveClass(/edge-active/);
  });

  test('yolo and silent toggles update runtime state and survive a refresh', async ({ page }, testInfo) => {
    annotatePlaytest(testInfo, {
      tested: 'Live runtime preference controls for yolo mode and silent mode.',
      expected: 'Toggling yolo and silent should update the live shell immediately and persist after a refresh.',
      steps: [
        './scripts/playtest.sh --grep "yolo and silent toggles update runtime state and survive a refresh"',
        'Open the top edge model controls, toggle yolo and silent, then refresh the page.',
      ],
    });

    await openLiveApp(page, sessionToken);
    await openTopEdge(page);

    const yoloButton = page.locator('.edge-yolo-btn');
    const silentButton = page.locator('.edge-silent-btn');

    const initialYolo = await yoloButton.getAttribute('aria-pressed');
    const initialSilent = await silentButton.getAttribute('aria-pressed');
    const nextYolo = initialYolo === 'true' ? 'false' : 'true';
    const nextSilent = initialSilent === 'true' ? 'false' : 'true';

    await yoloButton.click();
    await expect(yoloButton).toHaveAttribute('aria-pressed', nextYolo);

    await silentButton.click();
    await expect(silentButton).toHaveAttribute('aria-pressed', nextSilent);
    if (nextSilent === 'true') {
      await expect(page.locator('body')).toHaveClass(/silent-mode/);
    } else {
      await expect(page.locator('body')).not.toHaveClass(/silent-mode/);
    }

    await page.reload({ waitUntil: 'networkidle' });
    await openTopEdge(page);
    await expect(page.locator('.edge-yolo-btn')).toHaveAttribute('aria-pressed', nextYolo);
    await expect(page.locator('.edge-silent-btn')).toHaveAttribute('aria-pressed', nextSilent);
  });
});

test.describe('mobile capture route', () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });

  let sessionToken = '';

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('capture route loads without the full canvas shell', async ({ page }, testInfo) => {
    annotatePlaytest(testInfo, {
      tested: 'The dedicated mobile capture route on the live runtime.',
      expected: 'The capture UI should load on a mobile viewport while the full canvas shell stays absent.',
      steps: [
        './scripts/playtest.sh --grep "capture route loads without the full canvas shell"',
        'Open http://127.0.0.1:8420/capture on a mobile-sized viewport.',
      ],
    });

    await applySessionCookie(page, sessionToken);
    await page.goto('/capture');
    await page.waitForLoadState('networkidle');

    await expect(page.locator('#capture-page')).toBeVisible();
    await expect(page.locator('#capture-record')).toBeVisible();
    await expect(page.locator('#workspace')).toHaveCount(0);
    await expect(page.locator('#edge-left-tap')).toHaveCount(0);
  });
});
