import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function injectCanvasModuleRef(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/canvas.js');
    (window as any).__canvasModule = mod;
  });
}

async function renderTestArtifact(page: Page, text: string) {
  await page.evaluate((content) => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'art-1',
      kind: 'text_artifact',
      title: 'main.go',
      text: content,
    });
    const ct = document.getElementById('canvas-text');
    if (ct) {
      ct.style.display = '';
      ct.classList.add('is-active');
    }
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  }, text);
}

test.describe('canvas auto-refresh', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('desktop: canvas updates when new artifact event arrives', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    const original = 'package main\n\nfunc main() {\n\tprintln("hello")\n}';
    await renderTestArtifact(page, original);

    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();
    const beforeText = await canvasText.textContent();
    expect(beforeText).toContain('hello');

    const updated = 'package main\n\nfunc main() {\n\tprintln("updated")\n}';
    await page.evaluate((content) => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({
        event_id: 'art-2',
        kind: 'text_artifact',
        title: 'main.go',
        text: content,
      });
    }, updated);

    await page.waitForTimeout(100);
    const afterText = await canvasText.textContent();
    expect(afterText).toContain('updated');
    expect(afterText).not.toContain('"hello"');
  });

  test('mobile: canvas shows updated content', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    const original = 'Line 1\nLine 2\nLine 3';
    await renderTestArtifact(page, original);

    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const updated = 'Line 1\nLine 2 MODIFIED\nLine 3';
    await page.evaluate((content) => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({
        event_id: 'art-3',
        kind: 'text_artifact',
        title: 'main.go',
        text: content,
      });
    }, updated);

    await page.waitForTimeout(100);
    const afterText = await canvasText.textContent();
    expect(afterText).toContain('MODIFIED');
  });

  test('canvas viewport visible after canvas refresh', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await renderTestArtifact(page, 'initial content');

    await page.evaluate(() => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({
        event_id: 'art-4',
        kind: 'text_artifact',
        title: 'main.go',
        text: 'refreshed content',
      });
    });

    await page.waitForTimeout(100);
    const canvasColumn = page.locator('#canvas-column');
    await expect(canvasColumn).toBeVisible();
  });
});
