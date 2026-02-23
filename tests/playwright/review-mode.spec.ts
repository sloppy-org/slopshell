import { expect, test, type Page } from '@playwright/test';

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
  await page.waitForFunction(() => typeof (window as any).renderHarnessArtifact === 'function');
  await page.evaluate((payload) => {
    // @ts-expect-error injected by harness module
    window.renderHarnessArtifact(payload);
  }, event);
}

async function clearHarnessMessages(page: Page) {
  await page.waitForFunction(() => typeof (window as any).clearHarnessMessages === 'function');
  await page.evaluate(() => {
    // @ts-expect-error injected by harness module
    window.clearHarnessMessages();
  });
}

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/playwright/harness.html');
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

test('pdf artifacts render without iframe using object surface', async ({ page }) => {
  await renderArtifact(page, pdfEvent('evt-pdf-render'));

  await expect(page.locator('#canvas-pdf iframe')).toHaveCount(0);
  await expect(page.locator('#canvas-pdf .canvas-pdf-object')).toHaveCount(1);
  await expect(page.locator('#canvas-pdf .canvas-pdf-hit-layer')).toHaveCount(1);

  const dataAttr = await page.locator('#canvas-pdf .canvas-pdf-object').evaluate((el) => {
    return (el as HTMLObjectElement).data || '';
  });
  expect(dataAttr).toContain('/api/files/');
  expect(dataAttr).toContain('missing.pdf');
});

test('clearCanvas hides all artifact panes', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-clear', '# Visible'));
  await expect(page.locator('#canvas-text')).toBeVisible();

  await page.evaluate(() => {
    // @ts-expect-error injected by harness module
    window.clearHarnessCanvas();
  });

  await expect(page.locator('#canvas-text')).toBeHidden();
  await expect(page.locator('#canvas-image')).toBeHidden();
  await expect(page.locator('#canvas-pdf')).toBeHidden();
});
