import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = Record<string, unknown>;

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return s.chatWs?.readyState === wsOpen && s.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 8_000 });
}

async function openInbox(page: Page) {
  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
  await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
  await expect(page.locator('.sidebar-tab.is-active')).toContainText('Inbox');
}

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => {
    const log = (window as any).__harnessLog;
    return Array.isArray(log) ? log : [];
  });
}

async function clearLog(page: Page) {
  await page.evaluate(() => {
    (window as any).__harnessLog = [];
  });
}

async function waitForLogEntry(page: Page, type: string, action?: string) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => {
      if (String(entry?.type || '') !== type) return false;
      if (!action) return true;
      return String(entry?.action || '') === action;
    });
  }).toBe(true);
}

async function setInteractionTool(page: Page, tool: 'pointer' | 'highlight' | 'ink' | 'text_note' | 'prompt') {
  await page.evaluate((mode) => {
    (window as any).__setRuntimeState?.({ tool: mode });
    const app = (window as any)._taburaApp;
    if (app?.getState) {
      const interaction = app.getState().interaction;
      interaction.tool = mode;
      interaction.toolPinned = true;
    }
  }, tool);
}

test.describe('mail drafts', () => {
  test('new mail remains available in an empty inbox and supports save, reopen, suggestions, and send', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarActors([
        { id: 1, name: 'Ada', kind: 'human', email: 'ada@example.com' },
        { id: 2, name: 'Bob', kind: 'human', email: 'bob@example.com' },
      ]);
    });

    await openInbox(page);
    await expect(page.locator('#pr-file-list')).toContainText('No inbox items.');
    await expect(page.locator('#new-mail-trigger')).toBeVisible();

    await clearLog(page);
    await page.locator('#new-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_create');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('.mail-draft-title')).toContainText('Draft email');
    await expect(page.locator('.mail-draft-envelope .mail-draft-section-label')).toHaveText('Envelope');
    await expect(page.locator('.mail-draft-letter .mail-draft-section-label')).toHaveText('Message');
    await expect(page.locator('.mail-draft-envelope .mail-draft-field-line')).toHaveCount(4);
    const composerLayout = await page.locator('.mail-draft-envelope .mail-draft-field-line').first().evaluate((node) => {
      const style = window.getComputedStyle(node as HTMLElement);
      return {
        display: style.display,
        columns: style.gridTemplateColumns,
      };
    });
    const paperLayout = await page.locator('.mail-draft-paper').evaluate((node) => {
      const style = window.getComputedStyle(node as HTMLElement);
      return {
        columns: style.gridTemplateColumns,
      };
    });
    expect(composerLayout.display).toBe('grid');
    expect(composerLayout.columns).not.toBe('none');
    expect(paperLayout.columns.split(' ').length).toBeGreaterThan(1);
    await expect.poll(async () => page.locator('#mail-draft-recipient-suggestions option').count()).toBe(2);
    await expect(page.locator('#mail-draft-recipient-suggestions option').nth(0)).toHaveAttribute('value', 'ada@example.com');
    await expect(page.locator('#mail-draft-recipient-suggestions option').nth(1)).toHaveAttribute('value', 'bob@example.com');

    await clearLog(page);
    await page.locator('[name="to"]').fill('ada@example.com');
    await page.locator('[name="cc"]').fill('bob@example.com');
    await page.locator('[name="subject"]').fill('Quarterly update');
    await page.locator('[name="body"]').fill('Ship the revised agenda today.');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_update');
    await expect(page.locator('#mail-draft-status')).toContainText('Draft saved');

    await page.locator('.sidebar-tab', { hasText: 'Done' }).click();
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Done');
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await expect(page.locator('#pr-file-list')).toContainText('Quarterly update');

    await clearLog(page);
    await page.locator('#pr-file-list .pr-file-item', { hasText: 'Quarterly update' }).click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_get');
    await expect(page.locator('[name="to"]')).toHaveValue('ada@example.com');
    await expect(page.locator('[name="cc"]')).toHaveValue('bob@example.com');
    await expect(page.locator('[name="subject"]')).toHaveValue('Quarterly update');
    await expect(page.locator('[name="body"]')).toHaveValue('Ship the revised agenda today.');

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);

    await clearLog(page);
    await page.locator('[name="body"]').fill('Ship the revised agenda before lunch.');
    await page.locator('#mail-draft-send').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_update');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_send');
    await expect(page.locator('#mail-draft-status')).toContainText('Sent');

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await expect(page.locator('#pr-file-list')).not.toContainText('Quarterly update');

    await page.locator('.sidebar-tab', { hasText: 'Done' }).click();
    await expect(page.locator('#pr-file-list')).toContainText('Quarterly update');
  });

  test('mail drafts keep focus inside the form instead of reopening the floating composer', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [],
        waiting: [],
        someday: [],
        done: [],
      });
    });

    await openInbox(page);
    await page.locator('#new-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_create');

    await setInteractionTool(page, 'text_note');
    await page.locator('.mail-draft-envelope .mail-draft-field-line').first().click();
    await expect(page.locator('#floating-input')).toBeHidden();
    await expect(page.locator('[name="to"]')).toBeFocused();

    await page.locator('.mail-draft-body-row').click();
    await expect(page.locator('#floating-input')).toBeHidden();
    await expect(page.locator('[name="body"]')).toBeFocused();
  });

  test('reply drafts seed recipient and subject from the selected email item', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Reply to client',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await expect(page.locator('#canvas-text')).toContainText('Can you send the revised proposal?');
    await expect(page.locator('#reply-mail-trigger')).toBeVisible();

    await clearLog(page);
    await page.locator('#reply-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('.mail-draft-account')).toContainText('Work Exchange');
    await expect(page.locator('.mail-draft-account')).toContainText('exchange');
    await expect(page.locator('[name="to"]')).toHaveValue('client@example.com');
    await expect(page.locator('[name="subject"]')).toHaveValue('Re: Client question');
  });

  test('email canvas keeps mail actions available after the sidebar closes', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Reply to client',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await expect(page.locator('#canvas-text')).toContainText('Can you send the revised proposal?');

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);
    await expect(page.locator('#canvas-new-mail-trigger')).toBeVisible();
    await expect(page.locator('#canvas-reply-mail-trigger')).toBeVisible();

    await clearLog(page);
    await page.locator('#canvas-reply-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('[name="to"]')).toHaveValue('client@example.com');
    await expect(page.locator('[name="subject"]')).toHaveValue('Re: Client question');
  });

  test('new mail starts voice mail authoring when the prompt tool is active', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [],
        waiting: [],
        someday: [],
        done: [],
      });
    });
    await setInteractionTool(page, 'prompt');
    await openInbox(page);

    await clearLog(page);
    await page.locator('#new-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_create');
    await waitForLogEntry(page, 'media', 'get_user_media');
    await waitForLogEntry(page, 'recorder', 'start');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('.mail-draft-title')).toContainText('Draft email');
    await expect(page.locator('#dictation-indicator')).toBeHidden();
    await expect.poll(async () => page.evaluate(() => {
      const app = (window as any)._taburaApp;
      return String(app?.getState?.().voiceLifecycle || '');
    })).toBe('recording');
  });

  test('reply starts voice mail authoring when the prompt tool is active', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Reply to client',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });
    await setInteractionTool(page, 'prompt');
    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await clearLog(page);
    await page.locator('#reply-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply');
    await waitForLogEntry(page, 'media', 'get_user_media');
    await waitForLogEntry(page, 'recorder', 'start');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('.mail-draft-title')).toContainText('Re: Client question');
    await expect(page.locator('[name="to"]')).toHaveValue('client@example.com');
    await expect(page.locator('[name="subject"]')).toHaveValue('Re: Client question');
    await expect(page.locator('#dictation-indicator')).toBeHidden();
    await expect.poll(async () => page.evaluate(() => {
      const app = (window as any)._taburaApp;
      return String(app?.getState?.().voiceLifecycle || '');
    })).toBe('recording');
  });

  test('command center can launch reply from the current sidebar selection', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Reply to client',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await clearLog(page);
    await page.keyboard.press('Control+k');
    await expect(page.locator('#command-center')).toBeVisible();
    await expect(page.locator('#command-center-input')).toBeFocused();
    await page.locator('#command-center-input').fill('reply');
    await page.keyboard.press('Enter');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply');

    await expect(page.locator('#command-center')).toBeHidden();
    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('[name="to"]')).toHaveValue('client@example.com');
    await expect(page.locator('[name="subject"]')).toHaveValue('Re: Client question');
  });

  test('compose and reply hotkeys work from the sidebar without clicking action buttons', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Reply to client',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await clearLog(page);
    await page.keyboard.press('r');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply');
    await expect(page.locator('[name="subject"]')).toHaveValue('Re: Client question');

    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await clearLog(page);
    await page.keyboard.press('c');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_create');
    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('.mail-draft-title')).toContainText('Draft email');
  });

  test('forward button visible for email items and triggers forward API', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Forward test',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await expect(page.locator('#forward-mail-trigger')).toBeVisible();

    await clearLog(page);
    await page.locator('#forward-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_forward');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
  });

  test('forward hotkey f works from sidebar', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Forward hotkey test',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await clearLog(page);
    await page.keyboard.press('f');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_forward');
    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
  });

  test('canvas forward button works after sidebar closes', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Canvas forward',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Client question',
          artifact_kind: 'email',
          actor_name: 'Client',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Client question',
          meta_json: JSON.stringify({
            subject: 'Client question',
            sender: 'Client <client@example.com>',
            thread_id: 'thread-812',
            body: 'Can you send the revised proposal?',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);

    await expect(page.locator('#canvas-forward-mail-trigger')).toBeVisible();

    await clearLog(page);
    await page.locator('#canvas-forward-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_forward');
    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
  });

  test('reply all includes all recipients in cc', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Team update',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Team update',
          artifact_kind: 'email',
          actor_name: 'Boss',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Team update',
          meta_json: JSON.stringify({
            subject: 'Team update',
            sender: 'boss@example.com',
            thread_id: 'thread-812',
            recipients: ['alice@example.com', 'bob@example.com', 'carol@example.com'],
            body: 'Please review the proposal.',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await expect(page.locator('#reply-all-mail-trigger')).toBeVisible();

    await clearLog(page);
    await page.locator('#reply-all-mail-trigger').click();
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply_all');

    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
    await expect(page.locator('[name="to"]')).toHaveValue('boss@example.com');
    const ccValue = await page.locator('[name="cc"]').inputValue();
    expect(ccValue).toContain('bob@example.com');
    expect(ccValue).toContain('carol@example.com');
  });

  test('reply all hotkey a works from sidebar', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 812,
          title: 'Team update',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 612,
          source: 'exchange',
          source_ref: 'msg-812',
          artifact_title: 'Team update',
          artifact_kind: 'email',
          actor_name: 'Boss',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        612: {
          id: 612,
          kind: 'email',
          title: 'Team update',
          meta_json: JSON.stringify({
            subject: 'Team update',
            sender: 'boss@example.com',
            thread_id: 'thread-812',
            recipients: ['alice@example.com', 'bob@example.com'],
            body: 'Please review.',
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await clearLog(page);
    await page.keyboard.press('a');
    await waitForLogEntry(page, 'api_fetch', 'mail_draft_reply_all');
    await expect(page.locator('#canvas-text')).toHaveClass(/mail-draft-canvas/);
  });

  test('thread view shows foldable messages with last expanded', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 900,
          title: 'Thread test',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 700,
          source: 'gmail',
          artifact_title: 'Project discussion',
          artifact_kind: 'email_thread',
          created_at: '2026-03-10 10:00:00',
          updated_at: '2026-03-10 10:05:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setItemSidebarArtifacts({
        700: {
          id: 700,
          kind: 'email_thread',
          title: 'Project discussion',
          meta_json: JSON.stringify({
            subject: 'Project discussion',
            thread_id: 'thread-900',
            message_count: 3,
            participants: ['alice@example.com', 'bob@example.com'],
            messages: [
              { id: 'msg-1', sender: 'alice@example.com', date: 'Mar 8', body: 'Let us start the project', recipients: ['bob@example.com'] },
              { id: 'msg-2', sender: 'bob@example.com', date: 'Mar 9', body: 'Sounds good, I will prepare the docs', recipients: ['alice@example.com'] },
              { id: 'msg-3', sender: 'alice@example.com', date: 'Mar 10', body: 'Great, please share by end of day', recipients: ['bob@example.com'] },
            ],
          }),
        },
      });
    });

    await openInbox(page);
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await expect(page.locator('#canvas-text')).toContainText('Project discussion');
    await expect(page.locator('#canvas-text')).toContainText('alice@example.com');
    await expect(page.locator('#canvas-text')).toContainText('bob@example.com');
    await expect(page.locator('#canvas-text')).toContainText('Great, please share by end of day');
  });

});
