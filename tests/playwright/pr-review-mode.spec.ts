import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/zen-harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return s.chatWs?.readyState === wsOpen && s.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 8_000 });
}

async function injectCanvasEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') {
      throw new Error('tabura app state unavailable');
    }
    const canvasWs = app.getState().canvasWs;
    if (!canvasWs || typeof canvasWs.injectEvent !== 'function') {
      throw new Error('canvas websocket not available in harness');
    }
    canvasWs.injectEvent(eventPayload);
  }, payload);
}

async function horizontalFlip(page: Page, deltaX: number) {
  await page.locator('#canvas-viewport').evaluate((el, dX) => {
    const ev = new WheelEvent('wheel', {
      deltaX: Number(dX),
      deltaY: 0,
      bubbles: true,
      cancelable: true,
    });
    el.dispatchEvent(ev);
  }, deltaX);
}

function twoFileDiff(): string {
  return [
    'diff --git a/docs/one.md b/docs/one.md',
    'index 1111111..2222222 100644',
    '--- a/docs/one.md',
    '+++ b/docs/one.md',
    '@@ -1 +1 @@',
    '-old',
    '+new',
    'diff --git a/src/two.js b/src/two.js',
    'index 3333333..4444444 100644',
    '--- a/src/two.js',
    '+++ b/src/two.js',
    '@@ -1 +1 @@',
    '-console.log("before");',
    '+console.log("after");',
  ].join('\n');
}

test.describe('pr review canvas mode', () => {
  test('shows changed file list and switches file diff on click', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'evt-pr-1',
      title: '.tabura/artifacts/pr/pr-17.diff',
      text: twoFileDiff(),
    });

    await expect(page.locator('body')).toHaveClass(/pr-review-mode/);
    await expect(page.locator('#pr-file-list .pr-file-item')).toHaveCount(2);
    await expect(page.locator('#canvas-text')).toContainText('docs/one.md');

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await page.locator('#pr-file-list .pr-file-item').nth(1).click();
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('src/two.js');
    await expect(page.locator('#canvas-text')).toContainText('src/two.js');
  });

  test('supports keyboard file navigation', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'evt-pr-2',
      title: '.tabura/artifacts/pr/pr-18.diff',
      text: twoFileDiff(),
    });
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('docs/one.md');

    await page.keyboard.press('ArrowRight');
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('src/two.js');

    await page.keyboard.press('ArrowLeft');
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('docs/one.md');
  });

  test('supports horizontal canvas flip in pr mode', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'evt-pr-2b',
      title: '.tabura/artifacts/pr/pr-18.diff',
      text: twoFileDiff(),
    });
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('docs/one.md');

    await horizontalFlip(page, 140);
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('src/two.js');

    await horizontalFlip(page, -140);
    await expect(page.locator('#pr-file-list .pr-file-item.is-active .pr-file-name')).toContainText('docs/one.md');
  });

  test('uses drawer-style file pane on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);

    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'evt-pr-3',
      title: '.tabura/artifacts/pr/pr-19.diff',
      text: twoFileDiff(),
    });

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    const paneWidth = await page.locator('#pr-file-pane').evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return Math.round(rect.width);
    });
    expect(paneWidth).toBe(390);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);
  });

  test('workspace chooser supports parent row navigation', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('#pr-file-list .pr-file-item .pr-file-name', { hasText: 'docs' })).toHaveCount(1);

    await page.locator('#pr-file-list .pr-file-item', { hasText: 'docs' }).click();
    await expect(page.locator('#pr-file-list .pr-file-item .pr-file-name', { hasText: '..' })).toHaveCount(1);
    await expect(page.locator('#pr-file-list .pr-file-item .pr-file-name', { hasText: 'guide.md' })).toHaveCount(1);

    await page.locator('#pr-file-list .pr-file-item', { hasText: '..' }).click();
    await expect(page.locator('#pr-file-list .pr-file-item .pr-file-name', { hasText: 'README.md' })).toHaveCount(1);
    await expect(page.locator('#pr-file-list .pr-file-item .pr-file-name', { hasText: '..' })).toHaveCount(0);
  });

  test('supports horizontal canvas flip in workspace folder files', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await page.locator('#pr-file-list .pr-file-item', { hasText: 'README.md' }).click();
    await page.waitForFunction(() => {
      const app = (window as any)._taburaApp;
      return app?.getState?.().workspaceOpenFilePath === 'README.md';
    });

    await horizontalFlip(page, 140);
    await page.waitForFunction(() => {
      const app = (window as any)._taburaApp;
      return app?.getState?.().workspaceOpenFilePath === 'NOTES.md';
    });
    await expect(page.locator('#canvas-text')).toContainText('NOTES.md');

    await horizontalFlip(page, -140);
    await page.waitForFunction(() => {
      const app = (window as any)._taburaApp;
      return app?.getState?.().workspaceOpenFilePath === 'README.md';
    });
    await expect(page.locator('#canvas-text')).toContainText('README.md');
  });
});
