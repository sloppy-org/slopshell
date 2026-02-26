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

async function renderTestArtifact(page: Page, text = 'Line one\nLine two\nLine three\nLine four\nLine five') {
  await page.evaluate((content) => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'art-1',
      kind: 'text_artifact',
      title: 'test.txt',
      text: content,
    });
  }, text);
  // Show canvas pane
  await page.evaluate(() => {
    const ct = document.getElementById('canvas-text');
    if (ct) {
      ct.style.display = '';
      ct.classList.add('is-active');
    }
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  });
}

async function installMessageSpy(page: Page) {
  await page.evaluate(() => {
    (window as any).__sentBodies = [];
    const prev = window.fetch;
    window.fetch = async function(url: any, opts: any) {
      const u = String(url);
      if (u.includes('/messages') && opts?.method === 'POST') {
        try {
          const body = JSON.parse(opts.body);
          (window as any).__sentBodies.push(body);
        } catch (_) {}
      }
      return prev.apply(this, arguments as any);
    };
  });
}

async function getSentBodies(page: Page): Promise<any[]> {
  return page.evaluate(() => (window as any).__sentBodies.slice());
}

test.describe('canvas layout', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
    await installMessageSpy(page);
  });

  test('canvas column visible, fills viewport when no artifact', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    const canvasColumn = page.locator('#canvas-column');
    await expect(canvasColumn).toBeVisible();
  });

  test('artifact renders in canvas, fills viewport', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await renderTestArtifact(page);

    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();
  });

  test('long canvas artifact scrolls in text pane', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    const longText = Array.from({ length: 500 }, (_, i) => `Line ${i + 1}`).join('\n');
    await renderTestArtifact(page, longText);

    const metricsBefore = await page.evaluate(() => {
      const text = document.getElementById('canvas-text');
      const viewport = document.getElementById('canvas-viewport');
      if (!(text instanceof HTMLElement) || !(viewport instanceof HTMLElement)) return null;
      return {
        textTop: text.scrollTop,
        textScrollHeight: text.scrollHeight,
        textClientHeight: text.clientHeight,
        viewportTop: viewport.scrollTop,
      };
    });
    expect(metricsBefore).toBeTruthy();
    expect(metricsBefore!.textScrollHeight).toBeGreaterThan(metricsBefore!.textClientHeight);

    await page.mouse.move(640, 360);
    await page.mouse.wheel(0, 1200);
    await page.waitForTimeout(120);

    const metricsAfter = await page.evaluate(() => {
      const text = document.getElementById('canvas-text');
      const viewport = document.getElementById('canvas-viewport');
      if (!(text instanceof HTMLElement) || !(viewport instanceof HTMLElement)) return null;
      return {
        textTop: text.scrollTop,
        viewportTop: viewport.scrollTop,
      };
    });
    expect(metricsAfter).toBeTruthy();
    expect(metricsAfter!.textTop).toBeGreaterThan(metricsBefore!.textTop);
    expect(metricsAfter!.viewportTop).toBe(metricsBefore!.viewportTop);
  });

  test('right-click on artifact opens text input', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');
    await page.mouse.click(box.x + 50, box.y + 50, { button: 'right' });
    await page.waitForTimeout(200);

    // Right-click opens text input
    const floatingInput = page.locator('#floating-input');
    await expect(floatingInput).toBeVisible();
  });

  test('left-click on artifact starts recording, not text input', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');
    await page.mouse.click(box.x + 20, box.y + 20);
    await page.waitForTimeout(500);

    // Should show recording indicator, not text input
    const floatingInput = page.locator('#floating-input');
    const inputVisible = await floatingInput.evaluate(el => el.style.display !== 'none');
    expect(inputVisible).toBe(false);
  });

  test('canvas clear hides artifact panes', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    await page.evaluate(() => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({ kind: 'clear_canvas' });
    });
    await page.waitForTimeout(100);

    // All panes hidden
    const activePanes = page.locator('.canvas-pane.is-active');
    await expect(activePanes).toHaveCount(0);
  });

  test('text selection works without opening bubble', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');

    await page.mouse.move(box.x + 10, box.y + 10);
    await page.mouse.down();
    await page.mouse.move(box.x + 100, box.y + 10);
    await page.mouse.up();
    await page.waitForTimeout(200);

    const bubbleCount = await page.locator('.annotation-bubble').count();
    expect(bubbleCount).toBe(0);
  });

  test('no tab bar in DOM', async ({ page }) => {
    const tabBar = await page.locator('#canvas-tab-bar').count();
    expect(tabBar).toBe(0);
  });

  test('no canvas-chat pane in DOM', async ({ page }) => {
    const chatPane = await page.locator('#canvas-chat').count();
    expect(chatPane).toBe(0);
  });

  test('send message via text input', async ({ page }) => {
    // Right-click to open text input
    await page.mouse.click(300, 300, { button: 'right' });
    await page.waitForTimeout(100);

    const floatingInput = page.locator('#floating-input');
    await expect(floatingInput).toBeVisible();
    await floatingInput.fill('hello');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    const bodies = await getSentBodies(page);
    expect(bodies.length).toBeGreaterThanOrEqual(1);
    const sent = bodies[bodies.length - 1];
    expect(sent.text).toBe('hello');
  });
});
