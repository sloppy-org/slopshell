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
    if (app?.getState) app.getState().interaction.tool = mode;
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

  test('new mail starts dictation authoring when the prompt tool is active', async ({ page }) => {
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
    await waitForLogEntry(page, 'api_fetch', 'dictation_start');
    await expect(page.locator('#dictation-indicator')).toContainText('Email Draft');

    await page.evaluate(async () => {
      await (window as any)._taburaApp.appendDictationTranscript('Hello team. Sending the updated status shortly.');
    });
    await expect(page.locator('#canvas-text')).toContainText('Email Draft');
    await expect(page.locator('#canvas-text')).toContainText('Hello team. Sending the updated status shortly.');
  });

  test('reply starts dictation authoring when the prompt tool is active', async ({ page }) => {
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
    await waitForLogEntry(page, 'api_fetch', 'dictation_start');
    await expect(page.locator('#dictation-indicator')).toContainText('Email Reply');

    await page.evaluate(async () => {
      await (window as any)._taburaApp.appendDictationTranscript('I can send the revised proposal this afternoon.');
    });
    await expect(page.locator('#canvas-text')).toContainText('Email Reply Draft');
    await expect(page.locator('#canvas-text')).toContainText('Thread: Client question');
    await expect(page.locator('#canvas-text')).toContainText('I can send the revised proposal this afternoon.');
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
