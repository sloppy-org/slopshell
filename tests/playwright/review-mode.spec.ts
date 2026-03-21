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

function plainTextEvent(eventID: string, text: string) {
  return {
    kind: 'text_artifact',
    event_id: eventID,
    title: 'Notes',
    text,
    meta: {},
  };
}

function imageEvent(eventID: string) {
  return {
    kind: 'image_artifact',
    event_id: eventID,
    title: 'Image',
    path: 'missing.png',
  };
}

function pdfEvent(eventID: string, path = 'missing.pdf') {
  return {
    kind: 'pdf_artifact',
    event_id: eventID,
    title: 'PDF',
    path,
    page: 0,
  };
}

async function renderArtifact(page: Page, event: Record<string, unknown>) {
  await page.evaluate((payload) => {
    const mod = (window as any).__canvasModule;
    if (!mod?.renderCanvas) {
      throw new Error('canvas module unavailable');
    }
    mod.renderCanvas(payload);
  }, event);
}

async function clearHarnessMessages(page: Page) {
  await page.evaluate(() => {
    (window as any).__harnessLog?.splice?.(0);
  });
}

test.beforeEach(async ({ page }) => {
  await waitReady(page);
  await injectCanvasModuleRef(page);
  await clearHarnessMessages(page);
});

test('text artifact renders markdown into canvas-text', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-text-1', '# Header\nAlpha Beta'));
  await expect(page.locator('#canvas-text')).toBeVisible();
  const html = await page.locator('#canvas-text').innerHTML();
  expect(html).toContain('Header');
});

test('switching artifacts clears text pane content and classes', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-text-2', '# Switch Test'));
  await expect(page.locator('#canvas-text')).toBeVisible();
  await expect(page.locator('#canvas-text')).toContainText('Switch Test');

  await renderArtifact(page, imageEvent('evt-image-1'));
  await expect(page.locator('#canvas-image')).toBeVisible();
  await expect(page.locator('#canvas-text')).toBeHidden();
  const textClasses = await page.locator('#canvas-text').evaluate((el) => Array.from(el.classList));
  expect(textClasses).not.toContain('is-active');
});

test('pdf artifacts render in custom canvas viewer without native embed/object chrome', async ({ page }) => {
  await renderArtifact(page, pdfEvent('evt-pdf-render', 'docs/reports/missing.pdf'));

  await expect(page.locator('#canvas-pdf iframe')).toHaveCount(0);
  await expect(page.locator('#canvas-pdf object')).toHaveCount(0);
  await expect(page.locator('#canvas-pdf embed')).toHaveCount(0);
  await expect(page.locator('#canvas-pdf .canvas-pdf-pages')).toHaveCount(1);
  await expect(page.locator('#canvas-pdf .canvas-pdf-fallback a')).toHaveCount(1);

  const fallbackHref = await page.locator('#canvas-pdf .canvas-pdf-fallback a').evaluate((el) => {
    return (el as HTMLAnchorElement).href || '';
  });
  expect(fallbackHref).toContain('/api/files/');
  expect(fallbackHref).toContain('docs%2Freports%2Fmissing.pdf');
});

test('clearCanvas hides all artifact panes', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-clear', '# Visible'));
  await expect(page.locator('#canvas-text')).toBeVisible();

  await page.evaluate(() => {
    const mod = (window as any).__canvasModule;
    if (!mod?.clearCanvas) {
      throw new Error('canvas module unavailable');
    }
    mod.clearCanvas();
  });

  await expect(page.locator('#canvas-text')).toBeHidden();
  await expect(page.locator('#canvas-image')).toBeHidden();
  await expect(page.locator('#canvas-pdf')).toBeHidden();
});
