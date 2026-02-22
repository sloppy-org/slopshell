import { expect, test, type Page } from '@playwright/test';

type Header = {
  id: string;
  date: string;
  sender: string;
  subject: string;
};

function plainTextEvent(eventID: string, text: string) {
  return {
    kind: 'text_artifact',
    event_id: eventID,
    title: 'Notes',
    text,
    meta: {},
  };
}

function mailEvent(eventID: string, provider: string, headers: Header[]) {
  return {
    kind: 'text_artifact',
    event_id: eventID,
    title: 'Mail Headers',
    text: '# Mail Headers',
    meta: {
      producer_mcp_url: 'http://127.0.0.1:8090/mcp',
      message_triage_v1: {
        provider,
        folder: 'INBOX',
        count: headers.length,
        headers,
      },
    },
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

async function getHarnessMessages(page: Page): Promise<Record<string, unknown>[]> {
  await page.waitForFunction(() => typeof (window as any).getHarnessMessages === 'function');
  return page.evaluate(() => {
    // @ts-expect-error injected by harness module
    return window.getHarnessMessages();
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

test('switching artifacts tears down stale mail handlers', async ({ page }) => {
  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'gmail',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: true,
        },
      },
    });
  });

  await renderArtifact(page, mailEvent('evt-mail-2', 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'Switch Test' },
  ]));

  const before = await page.evaluate(() => {
    const root = document.getElementById('canvas-text') as any;
    return {
      hasMailClickHandler: Boolean(root?._mailClickHandler),
      hasMailPointerDownHandler: Boolean(root?._mailPointerDownHandler),
      hasMailClass: root?.classList.contains('mail-artifact') || false,
    };
  });
  expect(before.hasMailClickHandler).toBe(true);
  expect(before.hasMailPointerDownHandler).toBe(true);
  expect(before.hasMailClass).toBe(true);

  await clearHarnessMessages(page);
  await renderArtifact(page, imageEvent('evt-image-1'));

  const after = await page.evaluate(() => {
    const root = document.getElementById('canvas-text') as any;
    return {
      hasMailClickHandler: Boolean(root?._mailClickHandler),
      hasMailPointerDownHandler: Boolean(root?._mailPointerDownHandler),
      hasMailDetailKeyDownHandler: Boolean(root?._mailDetailKeyDownHandler),
      hasMailClass: root?.classList.contains('mail-artifact') || false,
    };
  });
  expect(after.hasMailClickHandler).toBe(false);
  expect(after.hasMailPointerDownHandler).toBe(false);
  expect(after.hasMailDetailKeyDownHandler).toBe(false);
  expect(after.hasMailClass).toBe(false);
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
