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
            { ID: 'm0', Subject: 'Already reviewed', Sender: 'done@example.com', Labels: ['Posteingang'], Date: '2026-03-16T11:00:00Z' },
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

  await page.route('**/api/external-accounts/2/mail-triage/manual/reviews*', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          data: {
            reviews: [],
            count: 0,
            reviewed_message_ids: ['m0'],
            distilled: { review_count: 0, policy_summary: [], examples: [] },
          },
        }),
      });
      return;
    }
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
  await expect(page.locator('#canvas-text')).toContainText('up cc');
  await page.keyboard.press('ArrowUp');

  await expect.poll(() => reviewBodies.length).toBe(1);
  expect(reviewBodies[0]).toMatchObject({
    message_id: 'm1',
    folder: 'Posteingang',
    action: 'cc',
  });

  await expect(page.locator('#canvas-text')).toContainText('Second inbox mail');
  await page.keyboard.press('ArrowLeft');

  await expect.poll(() => reviewBodies.length).toBe(2);
  expect(reviewBodies[1]).toMatchObject({
    message_id: 'm2',
    folder: 'Posteingang',
    action: 'inbox',
  });

  await expect(page.locator('#canvas-text')).toContainText('Manual triage complete for this batch.');
});

test('manual mail triage continues past a fully reviewed first page', async ({ page }) => {
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
    const url = new URL(route.request().url());
    const pageToken = url.searchParams.get('page_token') || '';
    const payload = pageToken === 'next-1'
      ? {
          ok: true,
          data: {
            messages: [
              { ID: 'm2', Subject: 'Unreviewed second page mail', Sender: 'alice@example.com', Labels: ['Posteingang'], Date: '2026-03-16T10:00:00Z' },
            ],
            next_page_token: '',
          },
        }
      : {
          ok: true,
          data: {
            messages: [
              { ID: 'm0', Subject: 'Reviewed first page mail', Sender: 'done@example.com', Labels: ['Posteingang'], Date: '2026-03-16T11:00:00Z' },
            ],
            next_page_token: 'next-1',
          },
        };
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify(payload),
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
            Subject: 'Unreviewed second page mail',
            Sender: 'alice@example.com',
            Labels: ['Posteingang'],
            Snippet: 'Second page snippet',
            BodyText: 'Second page body',
            Date: '2026-03-16T10:00:00Z',
          },
        },
      }),
    });
  });

  await page.route('**/api/external-accounts/2/mail-triage/manual/reviews*', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          data: {
            reviews: [],
            count: 0,
            reviewed_message_ids: ['m0'],
            distilled: { review_count: 0, policy_summary: [], examples: [] },
          },
        }),
      });
      return;
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          review: { id: 1 },
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

  await expect(page.locator('#canvas-text')).toContainText('Unreviewed second page mail');
  await expect(page.locator('#canvas-text')).toContainText('Second page body');
});

test('junk audit continues past a fully reviewed first page', async ({ page }) => {
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
    const url = new URL(route.request().url());
    const pageToken = url.searchParams.get('page_token') || '';
    const text = url.searchParams.get('text') || '';
    const folder = url.searchParams.get('folder') || '';
    if (folder !== 'Junk-E-Mail' || text !== '[SUSPICIOUS MESSAGE]') {
      throw new Error(`unexpected junk query: folder=${folder} text=${text}`);
    }
    const payload = pageToken === 'next-1'
      ? {
          ok: true,
          data: {
            messages: [
              { ID: 'j2', Subject: 'Possible false positive', Sender: 'research@example.com', Labels: ['Junk-E-Mail'], Date: '2026-03-16T10:00:00Z' },
            ],
            next_page_token: '',
          },
        }
      : {
          ok: true,
          data: {
            messages: [
              { ID: 'j0', Subject: 'Reviewed suspicious mail', Sender: 'done@example.com', Labels: ['Junk-E-Mail'], Date: '2026-03-16T11:00:00Z' },
            ],
            next_page_token: 'next-1',
          },
        };
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify(payload),
    });
  });

  await page.route('**/api/external-accounts/2/mail/messages/j2', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          message: {
            ID: 'j2',
            Subject: 'Possible false positive',
            Sender: 'research@example.com',
            Labels: ['Junk-E-Mail'],
            Snippet: 'Junk second page snippet',
            BodyText: 'Junk second page body',
            Date: '2026-03-16T10:00:00Z',
          },
        },
      }),
    });
  });

  await page.route('**/api/external-accounts/2/mail-triage/manual/reviews*', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          data: {
            reviews: [],
            count: 0,
            reviewed_message_ids: ['j0'],
            distilled: { review_count: 0, policy_summary: [], examples: [] },
          },
        }),
      });
      return;
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          review: { id: 1 },
          succeeded: 1,
        },
      }),
    });
  });

  await waitReady(page);

  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/app-mail-triage.js');
    await mod.openJunkMailTriage();
  });

  await expect(page.locator('#canvas-text')).toContainText('Possible false positive');
  await expect(page.locator('#canvas-text')).toContainText('Junk second page body');
  await expect(page.locator('#canvas-text')).toContainText('Junk Audit');
});
