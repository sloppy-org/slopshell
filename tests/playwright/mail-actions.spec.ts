import { expect, test, type Page } from '@playwright/test';

type Header = {
  id: string;
  date: string;
  sender: string;
  subject: string;
};

function mailEvent(provider: string, headers: Header[]) {
  return {
    kind: 'text_artifact',
    event_id: `evt-${provider}-${headers.length}`,
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

async function renderMail(page: Page, provider: string, headers: Header[]) {
  await page.waitForFunction(() => typeof (window as any).renderHarnessArtifact === 'function');
  await page.evaluate((event) => {
    // @ts-expect-error injected by harness module
    window.renderHarnessArtifact(event);
  }, mailEvent(provider, headers));
}

async function swipeRow(page: Page, selector: string, deltaX: number) {
  const row = page.locator(selector);
  const box = await row.boundingBox();
  if (!box) throw new Error(`missing bounding box for ${selector}`);
  const startX = box.x + box.width / 2;
  const startY = box.y + box.height / 2;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + deltaX, startY, { steps: 10 });
  await page.mouse.up();
}

async function mockCapabilities(page: Page, provider = 'gmail') {
  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider,
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: true,
        },
      },
    });
  });
}

async function mockMicrophoneCapture(page: Page, chunkText = 'voice sample') {
  await page.evaluate((payload) => {
    const fakeStream = {
      getTracks() {
        return [{ stop() {} }];
      },
    };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        async getUserMedia() {
          return fakeStream;
        },
      },
    });

    class FakeMediaRecorder {
      static isTypeSupported() {
        return true;
      }

      constructor(stream, options = {}) {
        this.stream = stream;
        this.state = 'inactive';
        this.mimeType = options.mimeType || 'audio/webm';
        this._listeners = new Map();
      }

      addEventListener(type, cb) {
        if (!this._listeners.has(type)) {
          this._listeners.set(type, []);
        }
        this._listeners.get(type).push(cb);
      }

      removeEventListener(type, cb) {
        const list = this._listeners.get(type);
        if (!list) return;
        this._listeners.set(type, list.filter((fn) => fn !== cb));
      }

      _emit(type, ev) {
        const list = this._listeners.get(type) || [];
        for (const fn of list) {
          fn(ev);
        }
      }

      start() {
        this.state = 'recording';
      }

      stop() {
        if (this.state === 'inactive') return;
        this.state = 'inactive';
        queueMicrotask(() => {
          const blob = new Blob([payload], { type: this.mimeType });
          const dataEvent = { data: blob };
          this._emit('dataavailable', dataEvent);
          if (typeof this.ondataavailable === 'function') {
            this.ondataavailable(dataEvent);
          }
          this._emit('stop', {});
          if (typeof this.onstop === 'function') {
            this.onstop({});
          }
        });
      }
    }

    Object.defineProperty(window, 'MediaRecorder', {
      configurable: true,
      writable: true,
      value: FakeMediaRecorder,
    });
  }, chunkText);
}

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/playwright/harness.html');
});

test('gmail defer includes until_at and shows success state', async ({ page }) => {
  const actionCalls: Array<Record<string, unknown>> = [];

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

  await page.route('**/api/mail/action', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    actionCalls.push(body);
    await route.fulfill({
      json: {
        result: {
          action: body.action,
          message_id: body.message_id,
          status: 'ok',
          effective_provider_mode: 'native',
          deferred_until_at: body.until_at,
        },
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'One' },
  ]);

  await page.click('tr[data-message-id="m1"] button[data-mail-action="defer"]');
  await page.fill('tr[data-message-id="m1"] [data-mail-defer-input]', '2026-03-10T09:30');
  await page.click('tr[data-message-id="m1"] button[data-mail-action="defer-apply"]');

  await expect.poll(() => actionCalls.length).toBe(1);
  expect(actionCalls[0]?.action).toBe('defer');
  expect(String(actionCalls[0]?.until_at || '')).toContain('2026-03-10T');

  await expect(page.locator('tr[data-message-id="m1"] [data-mail-row-status]')).toContainText('Deferred until');
});

test('imap defer shows stub and sends no mutate call', async ({ page }) => {
  let mutateCalls = 0;

  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'imap',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: false,
        },
      },
    });
  });

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ status: 500, body: 'should not be called' });
  });

  await renderMail(page, 'imap', [
    { id: 'm9', date: '2026-02-20T09:00:00Z', sender: 'imap@example.com', subject: 'IMAP' },
  ]);

  await page.click('tr[data-message-id="m9"] button[data-mail-action="defer"]');

  await expect(page.locator('tr[data-message-id="m9"] [data-mail-row-status]')).toContainText('stub');
  await page.waitForTimeout(120);
  expect(mutateCalls).toBe(0);
});

test('open switches to full detail view, marks read, and supports nav/back', async ({ page }) => {
  let actionCalls = 0;
  const readCalls: Array<Record<string, unknown>> = [];
  const markReadCalls: Array<Record<string, unknown>> = [];

  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'imap',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: false,
        },
      },
    });
  });

  await page.route('**/api/mail/action', async (route) => {
    actionCalls += 1;
    await route.fulfill({ status: 500, body: 'open should not call /api/mail/action' });
  });

  await page.route('**/api/mail/read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    readCalls.push(body);
    await route.fulfill({
      json: {
        message: {
          ID: body.message_id,
          Subject: `Subject ${body.message_id}`,
          Sender: 'Alice <alice@example.com>',
          Recipients: ['Bob <bob@example.com>'],
          Date: '2026-02-20T09:00:00Z',
          BodyText: `Full message body ${body.message_id}`,
        },
      },
    });
  });

  await page.route('**/api/mail/mark-read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    markReadCalls.push(body);
    await route.fulfill({
      json: {
        marked: 1,
      },
    });
  });

  await renderMail(page, 'imap', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'Alice <alice@example.com>', subject: 'Header subject 1' },
    { id: 'm2', date: '2026-02-20T08:00:00Z', sender: 'Bob <bob@example.com>', subject: 'Header subject 2' },
  ]);

  await page.click('tr[data-message-id="m1"] button[data-mail-action="open"]');

  await expect.poll(() => readCalls.length).toBe(1);
  await expect.poll(() => markReadCalls.length).toBe(1);
  expect(readCalls[0]?.message_id).toBe('m1');
  expect(markReadCalls[0]?.message_id).toBe('m1');
  expect(actionCalls).toBe(0);
  await expect(page.locator('[data-mail-detail-root]')).toBeVisible();
  await expect(page.locator('.mail-detail-subject')).toContainText('Subject m1');
  await expect(page.locator('[data-mail-detail-body]')).toContainText('Full message body m1');

  await page.click('button[data-mail-action="detail-next"]');
  await expect.poll(() => readCalls.length).toBe(2);
  await expect.poll(() => markReadCalls.length).toBe(2);
  expect(readCalls[1]?.message_id).toBe('m2');
  expect(markReadCalls[1]?.message_id).toBe('m2');
  await expect(page.locator('.mail-detail-subject')).toContainText('Subject m2');
  await expect(page.locator('button[data-mail-action="detail-next"]')).toBeDisabled();

  await page.keyboard.press('Escape');
  await expect(page.locator('.mail-triage-table')).toBeVisible();
  await expect(page.locator('tr[data-message-id="m2"]')).toBeVisible();
});

test('swipe thresholds map to archive/delete exactly once', async ({ page }) => {
  const actionCalls: string[] = [];

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

  await page.route('**/api/mail/action', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    actionCalls.push(String(body.action || ''));
    await route.fulfill({
      json: {
        result: {
          action: body.action,
          message_id: body.message_id,
          status: 'ok',
          effective_provider_mode: 'native',
        },
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'One' },
    { id: 'm2', date: '2026-02-20T08:00:00Z', sender: 'b@example.com', subject: 'Two' },
  ]);

  await swipeRow(page, 'tr[data-message-id="m1"]', -160);
  await expect.poll(() => actionCalls.filter((a) => a === 'archive').length).toBe(1);

  await swipeRow(page, 'tr[data-message-id="m2"]', -320);
  await expect.poll(() => actionCalls.filter((a) => a === 'delete').length).toBe(1);

  expect(actionCalls.filter((a) => a === 'archive')).toHaveLength(1);
  expect(actionCalls.filter((a) => a === 'delete')).toHaveLength(1);
});

test('draft reply assist uses shared action_id handler with state transitions in list/detail', async ({ page }) => {
  let mutateCalls = 0;
  const draftCalls: Array<Record<string, unknown>> = [];

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

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ json: { result: { status: 'ok' } } });
  });

  await page.route('**/api/mail/read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    await route.fulfill({
      json: {
        message: {
          ID: body.message_id,
          Subject: `Subject ${body.message_id}`,
          Sender: 'Alice <alice@example.com>',
          Recipients: ['Bob <bob@example.com>'],
          Date: '2026-02-20T09:00:00Z',
          BodyText: `Full message body ${body.message_id}`,
        },
      },
    });
  });

  await page.route('**/api/mail/mark-read', async (route) => {
    await route.fulfill({ json: { marked: 1 } });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    draftCalls.push(body);
    await new Promise((resolve) => setTimeout(resolve, 30));
    await route.fulfill({
      json: {
        source: 'llm',
        draft_text: `Draft for ${body.message_id}`,
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm3', date: '2026-02-20T07:00:00Z', sender: 'Alice <alice@example.com>', subject: 'Status' },
    { id: 'm4', date: '2026-02-20T06:00:00Z', sender: 'Bob <bob@example.com>', subject: 'Follow-up' },
  ]);

  await expect(page.locator('tr[data-message-id="m3"] button[data-mail-action="draft-reply"]')).toHaveAttribute('data-mail-action-id', 'mail.draft_reply');
  await page.click('tr[data-message-id="m3"] button[data-mail-action="draft-reply"]');
  const promptInput = page.locator('[data-mail-draft-panel] [data-mail-draft-prompt]');
  await expect(promptInput).toBeFocused();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('capturing');
  await promptInput.fill('Keep this short and ask for Friday confirmation.');
  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-generate"]');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-history')).toContain('capturing>generating>ready');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('ready');
  const draftText = page.locator('[data-mail-draft-panel] [data-mail-draft-text]');
  await expect(draftText).toHaveValue(/Draft for m3/);
  await expect(page.locator('tr[data-message-id="m3"] [data-mail-row-status]')).toContainText('Draft ready');

  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-cancel"]');
  await expect(page.locator('[data-mail-draft-panel]')).toBeHidden();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('idle');

  await page.click('tr[data-message-id="m4"] button[data-mail-action="open"]');
  await expect(page.locator('[data-mail-detail-root]')).toBeVisible();
  await expect(page.locator('.mail-detail-actions button[data-mail-action="draft-reply"]')).toHaveAttribute('data-mail-action-id', 'mail.draft_reply');
  await page.click('.mail-detail-actions button[data-mail-action="draft-reply"]');
  await expect(promptInput).toBeFocused();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('capturing');
  await promptInput.fill('Reply with a polite acknowledgement and next step.');
  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-generate"]');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('ready');
  await expect(draftText).toHaveValue(/Draft for m4/);
  await expect(page.locator('[data-mail-detail-status]')).toContainText('Draft ready');

  expect(draftCalls).toHaveLength(2);
  expect(draftCalls.map((c) => c.message_id)).toEqual(['m3', 'm4']);
  expect(draftCalls.map((c) => c.selection_text)).toEqual([
    'Keep this short and ask for Friday confirmation.',
    'Reply with a polite acknowledgement and next step.',
  ]);
  expect(Object.keys(draftCalls[0] || {}).sort()).toEqual(Object.keys(draftCalls[1] || {}).sort());
  expect(mutateCalls).toBe(0);
});

test('draft reply assist shows consistent backend errors in list/detail', async ({ page }) => {
  let mutateCalls = 0;
  const readCalls: string[] = [];
  const markReadCalls: string[] = [];
  const draftCalls: Array<Record<string, unknown>> = [];

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

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ json: { result: { status: 'ok' } } });
  });

  await page.route('**/api/mail/read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    readCalls.push(String(body.message_id || ''));
    await route.fulfill({
      json: {
        message: {
          ID: body.message_id,
          Subject: `Subject ${body.message_id}`,
          Sender: 'Alice <alice@example.com>',
          Recipients: ['Bob <bob@example.com>'],
          Date: '2026-02-20T09:00:00Z',
          BodyText: `Full message body ${body.message_id}`,
        },
      },
    });
  });

  await page.route('**/api/mail/mark-read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    markReadCalls.push(String(body.message_id || ''));
    await route.fulfill({ json: { marked: 1 } });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    draftCalls.push(body);
    await route.fulfill({
      status: 502,
      json: {
        error: 'draft backend unavailable',
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm12', date: '2026-02-20T07:00:00Z', sender: 'Alice <alice@example.com>', subject: 'First' },
    { id: 'm13', date: '2026-02-20T06:00:00Z', sender: 'Bob <bob@example.com>', subject: 'Second' },
  ]);

  await page.click('tr[data-message-id="m12"] button[data-mail-action="draft-reply"]');
  const promptInput = page.locator('[data-mail-draft-panel] [data-mail-draft-prompt]');
  await expect(promptInput).toBeFocused();
  await promptInput.fill('List prompt should fail.');
  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-generate"]');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('error');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-error')).toBe('draft backend unavailable');
  await expect(page.locator('[data-mail-draft-panel]')).toBeHidden();
  await expect(page.locator('tr[data-message-id="m12"] [data-mail-row-status]')).toContainText('draft backend unavailable');

  await page.click('tr[data-message-id="m13"] button[data-mail-action="open"]');
  await expect.poll(() => readCalls.length).toBe(1);
  await expect.poll(() => markReadCalls.length).toBe(1);
  await expect(page.locator('[data-mail-detail-root]')).toHaveAttribute('data-message-id', 'm13');
  await page.click('.mail-detail-actions button[data-mail-action="draft-reply"]');
  await expect(promptInput).toBeFocused();
  await promptInput.fill('Detail prompt should also fail.');
  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-generate"]');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('error');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-error')).toBe('draft backend unavailable');
  await expect(page.locator('[data-mail-draft-panel]')).toBeHidden();
  await expect(page.locator('[data-mail-detail-status]')).toContainText('draft backend unavailable');

  expect(draftCalls).toHaveLength(2);
  expect(draftCalls.map((c) => c.message_id)).toEqual(['m12', 'm13']);
  expect(draftCalls.map((c) => c.selection_text)).toEqual([
    'List prompt should fail.',
    'Detail prompt should also fail.',
  ]);
  expect(Object.keys(draftCalls[0] || {}).sort()).toEqual(Object.keys(draftCalls[1] || {}).sort());
  expect(readCalls).toEqual(['m13']);
  expect(markReadCalls).toEqual(['m13']);
  expect(mutateCalls).toBe(0);
});

test('detail navigation cancels pending draft capture and keeps draft context on current message', async ({ page }) => {
  let mutateCalls = 0;
  const readCalls: string[] = [];
  const markReadCalls: string[] = [];
  const draftCalls: Array<Record<string, unknown>> = [];

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

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ json: { result: { status: 'ok' } } });
  });

  await page.route('**/api/mail/read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    readCalls.push(String(body.message_id || ''));
    await route.fulfill({
      json: {
        message: {
          ID: body.message_id,
          Subject: `Subject ${body.message_id}`,
          Sender: 'Alice <alice@example.com>',
          Recipients: ['Bob <bob@example.com>'],
          Date: '2026-02-20T09:00:00Z',
          BodyText: `Full message body ${body.message_id}`,
        },
      },
    });
  });

  await page.route('**/api/mail/mark-read', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    markReadCalls.push(String(body.message_id || ''));
    await route.fulfill({ json: { marked: 1 } });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    draftCalls.push(body);
    await route.fulfill({
      json: {
        source: 'llm',
        draft_text: `Draft for ${body.message_id}`,
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm10', date: '2026-02-20T07:00:00Z', sender: 'Alice <alice@example.com>', subject: 'First' },
    { id: 'm11', date: '2026-02-20T06:00:00Z', sender: 'Bob <bob@example.com>', subject: 'Second' },
  ]);

  await page.click('tr[data-message-id="m10"] button[data-mail-action="open"]');
  await expect.poll(() => readCalls.length).toBe(1);
  await expect.poll(() => markReadCalls.length).toBe(1);
  await expect(page.locator('[data-mail-detail-root]')).toHaveAttribute('data-message-id', 'm10');

  await page.click('.mail-detail-actions button[data-mail-action="draft-reply"]');
  const promptInput = page.locator('[data-mail-draft-panel] [data-mail-draft-prompt]');
  await expect(promptInput).toBeFocused();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('capturing');
  await promptInput.fill('This prompt should be canceled by navigation.');

  await page.click('button[data-mail-action="detail-next"]');
  await expect.poll(() => readCalls.length).toBe(2);
  await expect.poll(() => markReadCalls.length).toBe(2);
  await expect(page.locator('[data-mail-detail-root]')).toHaveAttribute('data-message-id', 'm11');
  await expect(page.locator('[data-mail-draft-panel]')).toBeHidden();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('idle');

  await page.click('.mail-detail-actions button[data-mail-action="draft-reply"]');
  await expect(promptInput).toBeFocused();
  await promptInput.fill('Reply with confirmation for m11.');
  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-generate"]');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('ready');
  await expect(page.locator('[data-mail-draft-panel] [data-mail-draft-text]')).toHaveValue(/Draft for m11/);
  await expect(page.locator('[data-mail-detail-status]')).toContainText('Draft ready');

  expect(draftCalls).toHaveLength(1);
  expect(draftCalls[0]?.message_id).toBe('m11');
  expect(draftCalls[0]?.selection_text).toBe('Reply with confirmation for m11.');
  expect(readCalls).toEqual(['m10', 'm11']);
  expect(markReadCalls).toEqual(['m10', 'm11']);
  expect(mutateCalls).toBe(0);
});

test('draft reply prompt capture focuses input and cancel keeps state idle without mutations', async ({ page }) => {
  let draftCalls = 0;
  let mutateCalls = 0;

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

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ json: { result: { status: 'ok' } } });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    draftCalls += 1;
    await route.fulfill({ json: { source: 'llm', draft_text: 'unexpected call' } });
  });

  await renderMail(page, 'gmail', [
    { id: 'm6', date: '2026-02-20T04:00:00Z', sender: 'Frank <frank@example.com>', subject: 'Update' },
  ]);

  await page.click('tr[data-message-id="m6"] button[data-mail-action="draft-reply"]');
  const promptInput = page.locator('[data-mail-draft-panel] [data-mail-draft-prompt]');
  await expect(promptInput).toBeFocused();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('capturing');

  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-cancel"]');
  await expect(page.locator('[data-mail-draft-panel]')).toBeHidden();
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('idle');
  expect(draftCalls).toBe(0);
  expect(mutateCalls).toBe(0);
});

test('global recording control supports hold mode press/release transitions', async ({ page }) => {
  await mockCapabilities(page);

  await renderMail(page, 'gmail', [
    { id: 'm20', date: '2026-02-20T11:00:00Z', sender: 'hold@example.com', subject: 'Hold test' },
  ]);

  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  const canvasText = page.locator('#canvas-text');

  await expect(canvasText).toHaveAttribute('data-mail-recording-mode', 'hold');
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'idle');
  await trigger.dispatchEvent('pointerdown', { button: 0, pointerId: 17 });
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'recording');
  await expect(page.locator('[data-mail-record-indicator]')).toContainText('Recording (hold mode)');

  await trigger.dispatchEvent('pointerup', { button: 0, pointerId: 17 });
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'idle');
  await expect(canvasText).toHaveAttribute('data-mail-recording-last-stop', 'release');
  await expect(page.locator('[data-mail-record-indicator]')).toContainText('Ready (hold mode)');
  await expect.poll(async () => canvasText.getAttribute('data-mail-recording-history')).toContain('mode:hold>state:idle>state:recording>stop:release>state:idle');
});

test('global recording stop semantics support click and space in hold/toggle modes', async ({ page }) => {
  await mockCapabilities(page);

  await renderMail(page, 'gmail', [
    { id: 'm21', date: '2026-02-20T11:30:00Z', sender: 'stop@example.com', subject: 'Stop test' },
  ]);

  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  const stopButton = page.locator('button[data-mail-record-action="stop"]');
  const toggleMode = page.locator('button[data-mail-record-mode="toggle"]');
  const canvasText = page.locator('#canvas-text');

  await trigger.dispatchEvent('pointerdown', { button: 0, pointerId: 29 });
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'recording');
  await stopButton.click();
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'idle');
  await expect(canvasText).toHaveAttribute('data-mail-recording-last-stop', 'click');

  await toggleMode.click();
  await expect(canvasText).toHaveAttribute('data-mail-recording-mode', 'toggle');
  await trigger.click();
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'recording');
  await page.keyboard.press('Space');
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'idle');
  await expect(canvasText).toHaveAttribute('data-mail-recording-last-stop', 'space');
  await expect.poll(async () => canvasText.getAttribute('data-mail-recording-history')).toContain('mode:toggle');
});

test('keyboardless flow can complete full recording cycle via global button', async ({ page }) => {
  await mockCapabilities(page);

  await renderMail(page, 'gmail', [
    { id: 'm22', date: '2026-02-20T12:00:00Z', sender: 'button@example.com', subject: 'Button only' },
  ]);

  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  const toggleMode = page.locator('button[data-mail-record-mode="toggle"]');
  const canvasText = page.locator('#canvas-text');

  await toggleMode.click();
  await trigger.click();
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'recording');
  await trigger.click();
  await expect(canvasText).toHaveAttribute('data-mail-recording-state', 'idle');
  await expect(page.locator('[data-mail-record-indicator]')).toContainText('Ready (toggle mode)');
});

test('recording stop sends STT transcript into draft reply pipeline', async ({ page }) => {
  const sttCalls: Array<Record<string, unknown>> = [];
  const intentCalls: Array<Record<string, unknown>> = [];
  const draftCalls: Array<Record<string, unknown>> = [];
  const transcript = 'Please confirm delivery by Friday.';

  await mockCapabilities(page);
  await mockMicrophoneCapture(page, 'voice-bytes');

  await page.route('**/api/mail/stt', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    sttCalls.push(body);
    await route.fulfill({
      json: {
        text: transcript,
        language: 'en',
        language_probability: 0.98,
        source: 'helpy_stt',
        attempts: 1,
      },
    });
  });

  await page.route('**/api/mail/draft-intent', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    intentCalls.push(body);
    await route.fulfill({
      json: {
        intent: 'prompt',
        reason: 'instruction_signals',
        fallback_applied: false,
        fallback_policy: 'prompt',
      },
    });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    draftCalls.push(body);
    await route.fulfill({
      json: {
        source: 'llm',
        draft_text: `Voice draft for ${body.message_id}`,
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm23', date: '2026-02-20T13:00:00Z', sender: 'voice@example.com', subject: 'Voice path' },
  ]);

  await page.click('tr[data-message-id="m23"] button[data-mail-action="draft-reply"]');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('capturing');

  await page.click('button[data-mail-record-mode="toggle"]');
  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  await trigger.click();
  await expect(page.locator('#canvas-text')).toHaveAttribute('data-mail-recording-state', 'recording');
  await trigger.click();
  await expect(page.locator('#canvas-text')).toHaveAttribute('data-mail-recording-state', 'idle');

  await expect.poll(() => sttCalls.length).toBe(1);
  await expect.poll(() => intentCalls.length).toBe(1);
  await expect.poll(() => draftCalls.length).toBe(1);
  expect(String(sttCalls[0]?.audio_base64 || '')).not.toBe('');
  expect(intentCalls[0]?.transcript).toBe(transcript);
  expect(draftCalls[0]?.selection_text).toBe(transcript);
  await expect(page.locator('[data-mail-draft-panel] [data-mail-draft-text]')).toHaveValue(/Voice draft for m23/);
  await expect(page.locator('tr[data-message-id="m23"] [data-mail-row-status]')).toContainText('Draft ready');
});

test('voice dictation intent uses transcript as editable draft without generator call', async ({ page }) => {
  let draftCalls = 0;
  const transcript = 'Hi Bob,\n\nThanks for the update. I can send details tomorrow.\n\nBest,\nAlice';

  await mockCapabilities(page);
  await mockMicrophoneCapture(page, 'voice-bytes');

  await page.route('**/api/mail/stt', async (route) => {
    await route.fulfill({
      json: {
        text: transcript,
        language: 'en',
        language_probability: 0.95,
        source: 'helpy_stt',
        attempts: 1,
      },
    });
  });

  await page.route('**/api/mail/draft-intent', async (route) => {
    await route.fulfill({
      json: {
        intent: 'dictation',
        reason: 'dictation_signals',
        fallback_applied: false,
        fallback_policy: 'prompt',
      },
    });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    draftCalls += 1;
    await route.fulfill({ json: { source: 'llm', draft_text: 'unexpected generator call' } });
  });

  await renderMail(page, 'gmail', [
    { id: 'm25', date: '2026-02-20T14:00:00Z', sender: 'dictation@example.com', subject: 'Dictation path' },
  ]);

  await page.click('tr[data-message-id="m25"] button[data-mail-action="draft-reply"]');
  await page.click('button[data-mail-record-mode="toggle"]');
  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  await trigger.click();
  await trigger.click();

  await expect.poll(() => draftCalls).toBe(0);
  await expect(page.locator('[data-mail-draft-panel] [data-mail-draft-text]')).toHaveValue(transcript);
  await expect(page.locator('[data-mail-draft-panel] [data-mail-draft-source]')).toContainText('Captured dictation draft');
});

test('ambiguous voice intent uses deterministic prompt fallback policy', async ({ page }) => {
  const draftCalls: Array<Record<string, unknown>> = [];
  const transcript = 'Tomorrow should work.';

  await mockCapabilities(page);
  await mockMicrophoneCapture(page, 'voice-bytes');

  await page.route('**/api/mail/stt', async (route) => {
    await route.fulfill({
      json: {
        text: transcript,
        language: 'en',
        language_probability: 0.6,
        source: 'helpy_stt',
        attempts: 1,
      },
    });
  });

  await page.route('**/api/mail/draft-intent', async (route) => {
    await route.fulfill({
      json: {
        intent: 'prompt',
        reason: 'ambiguous_signals',
        fallback_applied: true,
        fallback_policy: 'prompt',
      },
    });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    draftCalls.push(body);
    await route.fulfill({
      json: {
        source: 'llm',
        draft_text: `Fallback generated for ${body.message_id}`,
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm26', date: '2026-02-20T14:30:00Z', sender: 'fallback@example.com', subject: 'Fallback path' },
  ]);

  await page.click('tr[data-message-id="m26"] button[data-mail-action="draft-reply"]');
  await page.click('button[data-mail-record-mode="toggle"]');
  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  await trigger.click();
  await trigger.click();

  await expect.poll(() => draftCalls.length).toBe(1);
  expect(draftCalls[0]?.selection_text).toBe(transcript);
  await expect(page.locator('[data-mail-draft-panel] [data-mail-draft-source]')).toContainText('ambiguous intent fallback');
});

test('recording STT failure remains recoverable via manual prompt retry', async ({ page }) => {
  let draftCalls = 0;

  await mockCapabilities(page);
  await mockMicrophoneCapture(page, 'voice-bytes');

  await page.route('**/api/mail/stt', async (route) => {
    await route.fulfill({
      status: 502,
      body: 'upstream unavailable',
    });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    draftCalls += 1;
    const body = JSON.parse(route.request().postData() || '{}');
    await route.fulfill({
      json: {
        source: 'llm',
        draft_text: `Manual fallback for ${body.message_id}`,
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm24', date: '2026-02-20T13:30:00Z', sender: 'retry@example.com', subject: 'Retry path' },
  ]);

  await page.click('tr[data-message-id="m24"] button[data-mail-action="draft-reply"]');
  await page.click('button[data-mail-record-mode="toggle"]');
  const trigger = page.locator('button[data-mail-record-action="trigger"]');
  await trigger.click();
  await trigger.click();

  await expect(page.locator('tr[data-message-id="m24"] [data-mail-row-status]')).toContainText('Transcription failed');
  expect(draftCalls).toBe(0);

  await page.fill('[data-mail-draft-panel] [data-mail-draft-prompt]', 'Manual retry prompt.');
  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-generate"]');

  await expect.poll(() => draftCalls).toBe(1);
  await expect(page.locator('[data-mail-draft-panel] [data-mail-draft-text]')).toHaveValue(/Manual fallback for m24/);
});

test('unregistered assist action_id returns deterministic error without network call', async ({ page }) => {
  let draftCalls = 0;
  let mutateCalls = 0;

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

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ json: { result: { status: 'ok' } } });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    draftCalls += 1;
    await route.fulfill({ json: { source: 'llm', draft_text: 'unexpected call' } });
  });

  await renderMail(page, 'gmail', [
    { id: 'm5', date: '2026-02-20T05:00:00Z', sender: 'Eve <eve@example.com>', subject: 'Question' },
  ]);

  await page.evaluate(() => {
    const btn = document.querySelector('tr[data-message-id="m5"] button[data-mail-action="draft-reply"]');
    if (btn) {
      btn.setAttribute('data-mail-action-id', 'mail.unknown');
    }
  });

  await page.click('tr[data-message-id="m5"] button[data-mail-action="draft-reply"]');
  await expect(page.locator('tr[data-message-id="m5"] [data-mail-row-status]')).toContainText('Unsupported assist action_id: mail.unknown');
  await expect.poll(async () => page.locator('#canvas-text').getAttribute('data-mail-assist-state')).toBe('error');
  expect(draftCalls).toBe(0);
  expect(mutateCalls).toBe(0);
});
