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

async function openInbox(page: Page) {
  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
  await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
  await expect(page.locator('.sidebar-tab.is-active')).toContainText('Inbox');
}

const parserInboxItem = {
  id: 101,
  title: 'Review parser cleanup',
  state: 'inbox',
  artifact_id: 501,
  source: 'github',
  source_ref: 'owner/repo#177',
  artifact_title: 'Parser cleanup plan',
  artifact_kind: 'idea_note',
  actor_name: 'Alice',
  created_at: '2026-03-08 09:40:00',
  updated_at: '2026-03-08 09:58:00',
};

async function setInboxItems(page: Page, items: Array<Record<string, unknown>>) {
  await page.evaluate((nextItems) => {
    (window as any).__setItemSidebarData({ inbox: nextItems });
  }, items);
  await page.locator('.sidebar-tab', { hasText: 'Inbox' }).evaluate((el: HTMLElement) => {
    el.click();
  });
  if (items.length > 0) {
    await expect(page.locator('#pr-file-list')).toContainText(String(items[0].title || ''));
  } else {
    await expect(page.locator('#pr-file-list')).toContainText('No inbox items.');
  }
}

async function queueSidebarResponseDelay(
  page: Page,
  view: 'inbox' | 'waiting' | 'someday' | 'done' | 'counts',
  delayMs: number,
) {
  await page.evaluate(({ nextView, nextDelayMs }) => {
    (window as any).__queueItemSidebarResponseDelay(nextView, nextDelayMs);
  }, { nextView: view, nextDelayMs: delayMs });
}

async function touchPhase(page: Page, selector: string, phase: 'start' | 'move' | 'end', dx = 0, dy = 0) {
  await page.locator(selector).evaluate((el, payload) => {
    const rect = (el as HTMLElement).getBoundingClientRect();
    const startX = rect.left + 24;
    const startY = rect.top + rect.height / 2;
    const currentX = startX + Number(payload.dx || 0);
    const currentY = startY + Number(payload.dy || 0);
    const target = el as HTMLElement;
    const startTouch = new Touch({
      identifier: 1,
      target,
      clientX: startX,
      clientY: startY,
      pageX: startX,
      pageY: startY,
      screenX: startX,
      screenY: startY,
    });
    const moveTouch = new Touch({
      identifier: 1,
      target,
      clientX: currentX,
      clientY: currentY,
      pageX: currentX,
      pageY: currentY,
      screenX: currentX,
      screenY: currentY,
    });
    if (payload.phase === 'start') {
      target.dispatchEvent(new TouchEvent('touchstart', {
        bubbles: true,
        cancelable: true,
        touches: [startTouch],
        changedTouches: [startTouch],
        targetTouches: [startTouch],
      }));
      return;
    }
    if (payload.phase === 'move') {
      target.dispatchEvent(new TouchEvent('touchmove', {
        bubbles: true,
        cancelable: true,
        touches: [moveTouch],
        changedTouches: [moveTouch],
        targetTouches: [moveTouch],
      }));
      return;
    }
    target.dispatchEvent(new TouchEvent('touchend', {
      bubbles: true,
      cancelable: true,
      touches: [],
      changedTouches: [moveTouch],
      targetTouches: [],
    }));
  }, { phase, dx, dy });
}

test.describe('inbox triage interactions', () => {
  test('tap opens the linked artifact on canvas', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    await page.locator('#pr-file-list .pr-file-item').first().click();

    await expect(page.locator('#canvas-text')).toContainText('Parser cleanup plan');
    await expect(page.locator('#canvas-text')).toContainText('Break parser cleanup into a small refactor');
  });

  test('idea items open as structured idea notes on canvas', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    await page.locator('#pr-file-list .pr-file-item[data-item-id="101"]').click();

    await expect(page.locator('#canvas-text')).toContainText('Parser cleanup plan');
    await expect(page.locator('#canvas-text')).toContainText('Notes');
    await expect(page.locator('#canvas-text')).toContainText('Context');
    await expect(page.locator('#canvas-text')).toContainText('Workspace: Default');
  });

  test('touch gestures expose feedback and commit done, delete, delegate, and later', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    const row = '#pr-file-list .pr-file-item[data-item-id="101"]';

    await touchPhase(page, row, 'start');
    await touchPhase(page, row, 'move', 90, 0);
    await expect(page.locator(row)).toHaveAttribute('data-triage-action', 'done');
    await expect(page.locator(row)).toHaveAttribute('data-triage-label', 'Done');
    await touchPhase(page, row, 'end', 90, 0);
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await setInboxItems(page, [parserInboxItem]);
    await touchPhase(page, row, 'start');
    await touchPhase(page, row, 'move', 180, 0);
    await expect(page.locator(row)).toHaveAttribute('data-triage-action', 'delete');
    await touchPhase(page, row, 'end', 180, 0);
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await setInboxItems(page, [parserInboxItem]);
    await touchPhase(page, row, 'start');
    await touchPhase(page, row, 'move', -100, 0);
    await expect(page.locator(row)).toHaveAttribute('data-triage-action', 'delegate');
    await touchPhase(page, row, 'end', -100, 0);
    await expect(page.locator('#item-sidebar-menu')).toBeVisible();
    await expect(page.locator('#item-sidebar-menu')).toContainText('Alice');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Codex' }).click();
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await setInboxItems(page, [parserInboxItem]);
    await touchPhase(page, row, 'start');
    await touchPhase(page, row, 'move', -185, 0);
    await expect(page.locator(row)).toHaveAttribute('data-triage-action', 'later');
    await touchPhase(page, row, 'end', -185, 0);
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    const log = await page.evaluate(() => (window as any).__harnessLog || []);
    const triageCalls = log.filter((entry: any) => entry?.action === 'item_triage');
    expect(triageCalls.map((entry: any) => entry?.payload?.action)).toEqual(['done', 'delete', 'delegate', 'later']);
  });

  test('stale inbox reload does not resurrect a triaged item', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    const row = '#pr-file-list .pr-file-item[data-item-id="101"]';
    await queueSidebarResponseDelay(page, 'inbox', 250);
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await page.waitForTimeout(50);

    await touchPhase(page, row, 'start');
    await touchPhase(page, row, 'move', -185, 0);
    await expect(page.locator(row)).toHaveAttribute('data-triage-action', 'later');
    await touchPhase(page, row, 'end', -185, 0);
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await page.waitForTimeout(300);
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');
  });

  test('desktop context menu and keyboard shortcuts cover archive, delete, later, and delegate', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await openInbox(page);

    const emailRow = page.locator('#pr-file-list .pr-file-item[data-item-id="102"]');
    await emailRow.click();
    await emailRow.click({ button: 'right' });
    await expect(page.locator('#item-sidebar-menu')).toBeVisible();
    await expect(page.locator('#item-sidebar-menu .item-sidebar-menu-item').first()).toContainText('Archive');
    await expect(page.locator('#item-sidebar-menu')).toContainText('Delete');
    await page.mouse.click(10, 10);

    await page.keyboard.press('KeyD');
    await expect(page.locator('#pr-file-list')).not.toContainText('Answer triage email');

    await setInboxItems(page, [parserInboxItem]);
    await page.keyboard.press('KeyL');
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await setInboxItems(page, [parserInboxItem]);
    await page.locator('#pr-file-list .pr-file-item[data-item-id="101"]').click();
    await page.keyboard.press('KeyG');
    await expect(page.locator('#item-sidebar-menu')).toBeVisible();
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Bob' }).click();
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await setInboxItems(page, [parserInboxItem]);
    await page.keyboard.press('Backspace');
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');
  });

  test('partial swipe cancels cleanly, rapid swipes remain stable, and empty inbox stays inert', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    const rowA = '#pr-file-list .pr-file-item[data-item-id="101"]';
    await touchPhase(page, rowA, 'start');
    await touchPhase(page, rowA, 'move', 20, 0);
    await touchPhase(page, rowA, 'end', 20, 0);
    await expect(page.locator(rowA)).toBeVisible();

    await touchPhase(page, rowA, 'start');
    await touchPhase(page, rowA, 'move', 90, 0);
    await touchPhase(page, rowA, 'end', 90, 0);
    const rowB = '#pr-file-list .pr-file-item[data-item-id="102"]';
    await touchPhase(page, rowB, 'start');
    await touchPhase(page, rowB, 'move', 180, 0);
    await touchPhase(page, rowB, 'end', 180, 0);

    await setInboxItems(page, []);
    await expect(page.locator('#item-sidebar-menu')).toBeHidden();
  });
});
