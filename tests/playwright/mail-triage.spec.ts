import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    return typeof app?.getState === 'function';
  }, null, { timeout: 8_000 });
}

test('manual mail triage advances through messages and records review actions', async ({ page }) => {
  const reviewBodies: Array<Record<string, unknown>> = [];

  await page.route('**/api/mail/accounts', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          accounts: [
            { id: 2, sphere: 'work', provider: 'exchange_ews', label: 'TU Graz Exchange', account_name: 'TU Graz Exchange' },
          ],
        },
      }),
    });
  });

  await page.route('**/api/external-accounts/2/mail/messages?*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          messages: [
            { ID: 'm1', Subject: 'First inbox mail', Sender: 'alice@example.com', Labels: ['Posteingang'], Date: '2026-03-16T10:00:00Z' },
            { ID: 'm2', Subject: 'Second inbox mail', Sender: 'bob@example.com', Labels: ['Posteingang'], Date: '2026-03-16T09:00:00Z' },
          ],
        },
      }),
    });
  });

  await page.route('**/api/external-accounts/2/mail/messages/m1', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          message: {
            ID: 'm1',
            Subject: 'First inbox mail',
            Sender: 'alice@example.com',
            Labels: ['Posteingang'],
            Snippet: 'First snippet',
            BodyText: 'First body',
            Date: '2026-03-16T10:00:00Z',
          },
        },
      }),
    });
  });

  await page.route('**/api/external-accounts/2/mail/messages/m2', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          message: {
            ID: 'm2',
            Subject: 'Second inbox mail',
            Sender: 'bob@example.com',
            Labels: ['Posteingang'],
            Snippet: 'Second snippet',
            BodyText: 'Second body',
            Date: '2026-03-16T09:00:00Z',
          },
        },
      }),
    });
  });

  await page.route('**/api/external-accounts/2/mail-triage/manual/reviews', async (route) => {
    if (route.request().method() === 'POST') {
      reviewBodies.push(JSON.parse(route.request().postData() || '{}'));
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          review: { id: reviewBodies.length || 1 },
          succeeded: 1,
        },
      }),
    });
  });

  await waitReady(page);

  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/app-mail-triage.js');
    await mod.openInboxMailTriage();
  });

  await expect(page.locator('#canvas-text')).toContainText('First inbox mail');
  await expect(page.locator('#canvas-text')).toContainText('First body');
  await page.locator('#canvas-text .mail-triage-action-archive').evaluate((node) => {
    (node as HTMLButtonElement).click();
  });

  await expect.poll(() => reviewBodies.length).toBe(1);
  expect(reviewBodies[0]).toMatchObject({
    message_id: 'm1',
    folder: 'Posteingang',
    action: 'archive',
  });

  await expect(page.locator('#canvas-text')).toContainText('Second inbox mail');
  await page.locator('#canvas-text .mail-triage-action-keep').evaluate((node) => {
    (node as HTMLButtonElement).click();
  });

  await expect.poll(() => reviewBodies.length).toBe(2);
  expect(reviewBodies[1]).toMatchObject({
    message_id: 'm2',
    folder: 'Posteingang',
    action: 'keep',
  });

  await expect(page.locator('#canvas-text')).toContainText('Manual triage complete for this batch.');
});
