import { expect, test, type Page } from '@playwright/test';

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

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const app = (window as any)._taburaApp;
    const activeChatWs = app?.getState?.().chatWs;
    if (activeChatWs && typeof activeChatWs.injectEvent === 'function') {
      activeChatWs.injectEvent(p);
      return;
    }
    const sessions = (window as any).__mockWsSessions || [];
    const candidates = sessions.filter((ws: any) => ws.url && ws.url.includes('/ws/chat/'));
    const chatWs = candidates[candidates.length - 1];
    if (chatWs && typeof chatWs.injectEvent === 'function') chatWs.injectEvent(p);
  }, payload);
}

test.describe('item inbox sidebar', () => {
  test('renders inbox metadata and exposes inbox count on the trigger', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '2');

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Inbox');
    await expect(page.getByRole('button', { name: /Review parser cleanup/i })).toBeVisible();
    await expect(page.locator('#pr-file-list')).toContainText('Parser cleanup plan');
    await expect(page.locator('#pr-file-list')).toContainText('idea');
    await expect(page.locator('#pr-file-list')).toContainText('github');
    await expect(page.locator('#pr-file-list')).toContainText('email');
  });

  test('desktop sidebar stays docked beside the canvas instead of covering it', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    const before = await page.locator('#canvas-column').evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return {
        left: Math.round(rect.left),
        width: Math.round(rect.width),
      };
    });
    expect(before.left).toBe(0);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Inbox');

    const readLayout = () => page.evaluate(() => {
      const pane = document.getElementById('pr-file-pane');
      const canvas = document.getElementById('canvas-column');
      const backdrop = document.getElementById('pr-file-drawer-backdrop');
      const edgeTop = document.getElementById('edge-top');
      if (
        !(pane instanceof HTMLElement)
        || !(canvas instanceof HTMLElement)
        || !(backdrop instanceof HTMLElement)
        || !(edgeTop instanceof HTMLElement)
      ) {
        return null;
      }
      const paneRect = pane.getBoundingClientRect();
      const canvasRect = canvas.getBoundingClientRect();
      const edgeTopRect = edgeTop.getBoundingClientRect();
      return {
        paneLeft: Math.round(paneRect.left),
        paneRight: Math.round(paneRect.right),
        paneTop: Math.round(paneRect.top),
        paneWidth: Math.round(paneRect.width),
        canvasLeft: Math.round(canvasRect.left),
        canvasWidth: Math.round(canvasRect.width),
        backdropDisplay: getComputedStyle(backdrop).display,
        edgeTopLeft: Math.round(edgeTopRect.left),
        edgeTopRight: Math.round(edgeTopRect.right),
      };
    });

    await expect.poll(readLayout).toMatchObject({
      paneLeft: 0,
      paneTop: 0,
      canvasLeft: 340,
      paneRight: 340,
      backdropDisplay: 'none',
    });

    const layout = await readLayout();
    expect(layout).not.toBeNull();
    expect(layout?.paneLeft).toBe(0);
    expect(layout?.paneTop).toBe(0);
    expect(layout?.paneWidth ?? 0).toBeGreaterThanOrEqual(320);
    expect(layout?.canvasLeft).toBe(layout?.paneRight);
    expect(layout?.canvasWidth ?? 0).toBeLessThan(before.width);
    expect(layout?.canvasWidth ?? 0).toBeGreaterThan(600);
    expect(layout?.backdropDisplay).toBe('none');

    await page.evaluate(() => {
      document.getElementById('edge-top')?.classList.add('edge-pinned');
    });

    await expect.poll(readLayout).toMatchObject({
      canvasLeft: 340,
      edgeTopLeft: 340,
    });

    const pinnedLayout = await readLayout();
    expect(pinnedLayout).not.toBeNull();
    expect(pinnedLayout?.edgeTopLeft).toBe(pinnedLayout?.canvasLeft);
    expect(pinnedLayout?.edgeTopRight).toBeGreaterThan(pinnedLayout?.edgeTopLeft ?? 0);

    await page.locator('.sidebar-tab', { hasText: 'Waiting' }).click();
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Waiting');
  });

  test('switches between waiting, someday, done, and files tabs', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);

    await page.locator('.sidebar-tab', { hasText: 'Waiting' }).click();
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Waiting');
    await expect(page.locator('#pr-file-list .pr-file-item')).toContainText('Await review feedback');
    await expect(page.locator('#pr-file-list')).toContainText('review');

    await page.locator('.sidebar-tab', { hasText: 'Someday' }).click();
    await expect(page.locator('#pr-file-list .pr-file-item')).toContainText('Sketch mobile inbox gestures');

    await page.locator('.sidebar-tab', { hasText: 'Done' }).click();
    await expect(page.locator('#pr-file-list .pr-file-item')).toContainText('Ship capture flow');

    await page.locator('.sidebar-tab', { hasText: 'Files' }).click();
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Files');
    await expect(page.locator('#pr-file-list .pr-file-item .pr-file-name', { hasText: 'docs' })).toHaveCount(1);
  });

  test('touch taps switch sidebar categories on mobile', async ({ browser }) => {
    const context = await browser.newContext({
      hasTouch: true,
      viewport: { width: 390, height: 844 },
    });
    const page = await context.newPage();
    await waitReady(page);

    try {
      await page.locator('#edge-left-tap').tap();
      await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);

      await page.locator('.sidebar-tab', { hasText: 'Waiting' }).tap();
      await expect(page.locator('.sidebar-tab.is-active')).toContainText('Waiting');
      await expect(page.locator('#pr-file-list .pr-file-item')).toContainText('Await review feedback');

      await page.locator('.sidebar-tab', { hasText: 'Someday' }).tap();
      await expect(page.locator('.sidebar-tab.is-active')).toContainText('Someday');
      await expect(page.locator('#pr-file-list .pr-file-item')).toContainText('Sketch mobile inbox gestures');

      await page.locator('.sidebar-tab', { hasText: 'Done' }).tap();
      await expect(page.locator('.sidebar-tab.is-active')).toContainText('Done');
      await expect(page.locator('#pr-file-list .pr-file-item')).toContainText('Ship capture flow');
    } finally {
      await context.close();
    }
  });

  test('opening a PR review item enters PR review mode', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 144,
          title: 'Review sidebar PR mapping',
          state: 'inbox',
          source: 'github',
          source_ref: 'owner/repo#PR-144',
          artifact_title: 'PR #144',
          artifact_kind: 'github_pr',
          actor_name: 'Alice',
          created_at: '2026-03-08 10:00:00',
          updated_at: '2026-03-08 10:05:00',
        }],
        waiting: [],
      });
    });

    await page.locator('#edge-left-tap').click();
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await expect(page.locator('#pr-file-list')).toContainText('Review sidebar PR mapping');
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await expect(page.locator('body')).toHaveClass(/pr-review-mode/);
    await expect(page.locator('#canvas-text')).toContainText('src/review.js');
    await expect(page.locator('#pr-file-list .pr-file-item')).toHaveCount(1);

    const log = await page.evaluate(() => (window as any).__harnessLog || []);
    expect(log.some((entry: any) => entry?.type === 'command_sent' && entry?.command === '/pr 144')).toBe(true);
  });

  test('system actions can open provider-filtered and unassigned inbox views', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [
          {
            id: 301,
            title: 'Todoist follow-up',
            state: 'inbox',
            sphere: 'private',
            source: 'todoist',
            workspace_id: null,
            artifact_kind: 'plan_note',
            updated_at: '2026-03-08 10:05:00',
          },
          {
            id: 302,
            title: 'Exchange triage',
            state: 'inbox',
            sphere: 'private',
            source: 'exchange',
            workspace_id: 9,
            artifact_kind: 'email',
            updated_at: '2026-03-08 10:06:00',
          },
        ],
      });
    });

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'show_item_sidebar_view',
        view: 'inbox',
        filters: { source: 'todoist' },
      },
    });
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('#pr-file-list')).toContainText('Todoist follow-up');
    await expect(page.locator('#pr-file-list')).not.toContainText('Exchange triage');
    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '1');

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'show_item_sidebar_view',
        view: 'inbox',
        filters: { workspace_id: 'null' },
      },
    });
    await expect(page.locator('#pr-file-list')).toContainText('Todoist follow-up');
    await expect(page.locator('#pr-file-list')).not.toContainText('Exchange triage');
  });

  test('system actions can opt into all-spheres inbox views', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setRuntimeState({ active_sphere: 'private' });
      (window as any).__setItemSidebarData({
        inbox: [
          {
            id: 401,
            title: 'Private inbox item',
            state: 'inbox',
            sphere: 'private',
            artifact_kind: 'plan_note',
            updated_at: '2026-03-08 10:05:00',
          },
          {
            id: 402,
            title: 'Work inbox item',
            state: 'inbox',
            sphere: 'work',
            artifact_kind: 'email',
            updated_at: '2026-03-08 10:06:00',
          },
        ],
      });
    });

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'show_item_sidebar_view',
        view: 'inbox',
        clear_filters: true,
        filters: { all_spheres: true },
      },
    });
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('#pr-file-list')).toContainText('Private inbox item');
    await expect(page.locator('#pr-file-list')).toContainText('Work inbox item');

    const log = await page.evaluate(() => (window as any).__harnessLog || []);
    const itemListFetch = [...log].reverse().find((entry: any) => entry?.action === 'item_list');
    expect(String(itemListFetch?.url || '')).not.toContain('sphere=private');
  });

  test('context filter narrows the inbox and parent contexts include child items', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__setItemSidebarContexts?.([
        { id: 10, name: 'Work', parent_id: null },
        { id: 11, name: 'W7x', parent_id: 10 },
        { id: 12, name: 'Private', parent_id: null },
      ]);
      (window as any).__setItemSidebarData({
        inbox: [
          {
            id: 901,
            title: 'Review W7x backlog',
            state: 'inbox',
            sphere: 'private',
            context_ids: [11],
            artifact_title: 'W7x backlog',
            artifact_kind: 'plan_note',
            created_at: '2026-03-08 09:40:00',
            updated_at: '2026-03-08 09:58:00',
          },
          {
            id: 902,
            title: 'Reply to family email',
            state: 'inbox',
            sphere: 'private',
            context_ids: [12],
            artifact_title: 'Family',
            artifact_kind: 'email',
            created_at: '2026-03-08 09:10:00',
            updated_at: '2026-03-08 09:12:00',
          },
        ],
        waiting: [],
        someday: [],
        done: [],
      });
    });

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'show_item_sidebar_view',
        view: 'inbox',
      },
    });
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('#pr-file-list')).toContainText('Review W7x backlog');
    await expect(page.locator('#pr-file-list')).toContainText('Reply to family email');
  });
});
