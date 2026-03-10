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

async function refreshItemSidebarCounts(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/app-item-sidebar-utils.js');
    await mod.refreshItemSidebarCounts();
  });
}

async function clickSphereButton(page: Page, sphere: 'work' | 'private') {
  await page.evaluate((nextSphere) => {
    const button = document.querySelector(`#edge-top-models .edge-sphere-btn[data-sphere="${nextSphere}"]`);
    if (button instanceof HTMLButtonElement) button.click();
  }, sphere);
}

async function refreshProjects(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/app-projects.js');
    await mod.fetchProjects();
  });
}

async function projectButtonTexts(page: Page) {
  return page.evaluate(() => {
    return Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'))
      .map((button) => String(button.textContent || '').trim())
      .filter(Boolean);
  });
}

async function seedSphereScenario(page: Page) {
  await page.evaluate(() => {
    (window as any).__setProjects([
      {
        id: 'private-notes',
        name: 'Private Notes',
        kind: 'managed',
        sphere: 'private',
        project_key: '/tmp/private-notes',
        root_path: '/tmp/private-notes',
        chat_session_id: 'chat-private',
        canvas_session_id: 'local',
      },
      {
        id: 'work-tracker',
        name: 'Work Tracker',
        kind: 'managed',
        sphere: 'work',
        project_key: '/tmp/work-tracker',
        root_path: '/tmp/work-tracker',
        chat_session_id: 'chat-work',
        canvas_session_id: 'local',
      },
      {
        id: 'hub',
        name: 'Hub',
        kind: 'hub',
        sphere: '',
        project_key: '__hub__',
        root_path: '/tmp/hub',
        chat_session_id: 'chat-hub',
        canvas_session_id: 'local',
      },
    ], 'private-notes');
    (window as any).__setRuntimeState({ active_sphere: 'private' });
    (window as any).__setItemSidebarWorkspaces([
      { id: 1, name: 'Private Desk', dir_path: '/tmp/private', sphere: 'private', is_active: true },
      { id: 2, name: 'Work Desk', dir_path: '/tmp/work', sphere: 'work', is_active: false },
    ]);
    (window as any).__setItemSidebarData({
      inbox: [
        {
          id: 101,
          title: 'Private inbox item',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 501,
          artifact_title: 'Parser cleanup plan',
          artifact_kind: 'idea_note',
          actor_name: 'Alice',
          created_at: '2026-03-08 09:40:00',
          updated_at: '2026-03-08 09:58:00',
        },
        {
          id: 102,
          title: 'Work inbox item',
          state: 'inbox',
          sphere: 'work',
          artifact_id: 502,
          artifact_title: 'Re: triage follow-up',
          artifact_kind: 'email',
          actor_name: 'Bob',
          created_at: '2026-03-08 09:10:00',
          updated_at: '2026-03-08 09:12:00',
        },
      ],
      waiting: [],
      someday: [],
      done: [],
    });
    localStorage.removeItem('tabura.activeSphere');
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  await refreshProjects(page);
  await refreshItemSidebarCounts(page);
}

const parserInboxItem = {
  id: 101,
  title: 'Review parser cleanup',
  state: 'inbox',
  sphere: 'private',
  artifact_id: 501,
  source: 'github',
  source_ref: 'owner/repo#177',
  artifact_title: 'Parser cleanup plan',
  artifact_kind: 'idea_note',
  actor_name: 'Alice',
  created_at: '2026-03-08 09:40:00',
  updated_at: '2026-03-08 09:58:00',
};

const privateEmailInboxItem = {
  id: 102,
  title: 'Answer triage email',
  state: 'inbox',
  sphere: 'private',
  artifact_id: 502,
  source: 'exchange',
  source_ref: 'msg-102',
  artifact_title: 'Re: triage follow-up',
  artifact_kind: 'email',
  actor_name: 'Bob',
  created_at: '2026-03-08 09:10:00',
  updated_at: '2026-03-08 09:12:00',
};

const reopenedEmailItem = {
  ...privateEmailInboxItem,
  state: 'done',
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

async function swipeCanvas(page: Page, dx: number, dy = 0) {
  await page.locator('#canvas-viewport').evaluate((el, payload) => {
    const target = el as HTMLElement;
    const rect = target.getBoundingClientRect();
    const startX = rect.left + rect.width / 2;
    const startY = rect.top + rect.height / 2;
    const endX = startX + Number(payload.dx || 0);
    const endY = startY + Number(payload.dy || 0);
    const makeTouch = (clientX: number, clientY: number) => new Touch({
      identifier: 1,
      target,
      clientX,
      clientY,
      pageX: clientX,
      pageY: clientY,
      screenX: clientX,
      screenY: clientY,
    });
    const startTouch = makeTouch(startX, startY);
    const endTouch = makeTouch(endX, endY);
    target.dispatchEvent(new TouchEvent('touchstart', {
      bubbles: true,
      cancelable: true,
      touches: [startTouch],
      changedTouches: [startTouch],
      targetTouches: [startTouch],
    }));
    target.dispatchEvent(new TouchEvent('touchmove', {
      bubbles: true,
      cancelable: true,
      touches: [endTouch],
      changedTouches: [endTouch],
      targetTouches: [endTouch],
    }));
    target.dispatchEvent(new TouchEvent('touchend', {
      bubbles: true,
      cancelable: true,
      touches: [],
      changedTouches: [endTouch],
      targetTouches: [],
    }));
  }, { dx, dy });
}

async function touchPhase(page: Page, selector: string, phase: 'start' | 'move' | 'end', dx = 0, dy = 0) {
  await page.locator(selector).evaluate((el, payload) => {
    const target = el as HTMLElement;
    const key = String(payload.selector || '');
    const touchState = ((window as any).__playwrightTouchState ||= {});
    const readOrigin = () => {
      const existing = touchState[key];
      if (existing && Number.isFinite(existing.startX) && Number.isFinite(existing.startY)) {
        return existing;
      }
      const rect = target.getBoundingClientRect();
      const origin = {
        startX: rect.left + 24,
        startY: rect.top + rect.height / 2,
      };
      touchState[key] = origin;
      return origin;
    };
    const makeTouch = (clientX: number, clientY: number) => new Touch({
      identifier: 1,
      target,
      clientX,
      clientY,
      pageX: clientX,
      pageY: clientY,
      screenX: clientX,
      screenY: clientY,
    });
    if (payload.phase === 'start') {
      const rect = target.getBoundingClientRect();
      const origin = {
        startX: rect.left + 24,
        startY: rect.top + rect.height / 2,
      };
      touchState[key] = origin;
      const startTouch = makeTouch(origin.startX, origin.startY);
      target.dispatchEvent(new TouchEvent('touchstart', {
        bubbles: true,
        cancelable: true,
        touches: [startTouch],
        changedTouches: [startTouch],
        targetTouches: [startTouch],
      }));
      return;
    }
    const origin = readOrigin();
    const currentX = origin.startX + Number(payload.dx || 0);
    const currentY = origin.startY + Number(payload.dy || 0);
    const moveTouch = makeTouch(currentX, currentY);
    const startTouch = new Touch({
      identifier: 1,
      target,
      clientX: origin.startX,
      clientY: origin.startY,
      pageX: origin.startX,
      pageY: origin.startY,
      screenX: origin.startX,
      screenY: origin.startY,
    });
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
    delete touchState[key];
  }, { selector, phase, dx, dy });
}

test.describe('inbox triage interactions', () => {
  test('sphere toggle filters projects and inbox items', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await seedSphereScenario(page);

    await expect.poll(() => projectButtonTexts(page)).toEqual(['Private Notes', 'Hub']);
    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '1');

    await clickSphereButton(page, 'work');
    await expect.poll(async () => {
      const log = await page.evaluate(() => (window as any).__harnessLog || []);
      return log.some((entry: any) => entry?.action === 'runtime_preferences' && entry?.payload?.active_sphere === 'work');
    }).toBe(true);

    await expect.poll(() => projectButtonTexts(page)).toEqual(['Work Tracker', 'Hub']);
    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '1');
    await openInbox(page);
    await expect(page.locator('.sidebar-tab', { hasText: 'Inbox' }).first()).toContainText('1');
    await expect(page.locator('#pr-file-list')).toContainText('Work inbox item');
    await expect(page.locator('#pr-file-list')).not.toContainText('Private inbox item');
    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__getRuntimeState?.().active_sphere || '');
    }).toBe('work');
  });

  test('moving a standalone item to the other sphere hides it until the sphere changes', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 101,
          title: 'Private inbox item',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 501,
          artifact_title: 'Parser cleanup plan',
          artifact_kind: 'idea_note',
          actor_name: 'Alice',
          created_at: '2026-03-08 09:40:00',
          updated_at: '2026-03-08 09:58:00',
        }],
        waiting: [],
        someday: [],
        done: [],
      });
      (window as any).__setRuntimeState({ active_sphere: 'private' });
      document.getElementById('edge-top')?.classList.add('edge-pinned');
      localStorage.removeItem('tabura.activeSphere');
    });

    await openInbox(page);
    const row = page.locator('#pr-file-list .pr-file-item[data-item-id="101"]');
    await row.click({ button: 'right' });
    await expect(page.locator('#item-sidebar-menu')).toContainText('Move to Work');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Move to Work' }).click();

    await expect(page.locator('#pr-file-list')).not.toContainText('Private inbox item');
    await expect.poll(async () => {
      const log = await page.evaluate(() => (window as any).__harnessLog || []);
      return log.some((entry: any) => entry?.action === 'item_update' && entry?.payload?.sphere === 'work');
    }).toBe(true);

    await clickSphereButton(page, 'work');
    await expect(page.locator('#pr-file-list')).toContainText('Private inbox item');
  });

  test('switching directly to a project in another sphere syncs the active sphere', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await seedSphereScenario(page);

    await page.evaluate(async () => {
      const mod = await import('../../internal/web/static/app-chat-transport.js');
      await mod.switchProject('work-tracker');
    });

    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__getRuntimeState?.().active_sphere || '');
    }).toBe('work');
    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().activeSphere || '');
    }).toBe('work');
  });

  test('tap opens the linked artifact on canvas', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    await page.locator('#pr-file-list .pr-file-item').first().click();

    await expect(page.locator('#canvas-text')).toContainText('Parser cleanup plan');
    await expect(page.locator('#canvas-text')).toContainText('Break parser cleanup into a small refactor');
  });

  test('bare items stay in the sidebar without synthetic canvas output', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);
    await setInboxItems(page, [{
      id: 201,
      title: 'Bare sidebar task',
      state: 'inbox',
      sphere: 'private',
      artifact_id: null,
      artifact_title: '',
      artifact_kind: '',
      actor_name: 'Alice',
      created_at: '2026-03-08 10:00:00',
      updated_at: '2026-03-08 10:05:00',
    }]);

    await page.locator('#pr-file-list .pr-file-item[data-item-id="201"]').click();

    await expect(page.locator('#pr-file-list .pr-file-item.is-active[data-item-id="201"]')).toHaveCount(1);
    await expect(page.locator('#canvas-viewport .canvas-pane.is-active')).toHaveCount(0);
    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().itemSidebarActiveItemID || 0);
    }).toBe(201);
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

  test('idea notes render promotion review details on canvas', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await page.evaluate(() => {
      (window as any).__setItemSidebarArtifacts({
        501: {
          id: 501,
          kind: 'idea_note',
          title: 'Parser cleanup plan',
          meta_json: JSON.stringify({
            title: 'Parser cleanup plan',
            transcript: 'Break parser cleanup into a small refactor, a test pass, and one cleanup issue.',
            capture_mode: 'voice',
            captured_at: '2026-03-08T09:40:00Z',
            workspace: 'Default',
            notes: [
              'Draft the rollout checklist',
              'Add regression coverage',
            ],
            promotion_preview: {
              target: 'items',
              candidates: [
                { index: 1, title: 'Draft the rollout checklist', details: 'Draft the rollout checklist' },
                { index: 2, title: 'Add regression coverage', details: 'Add regression coverage' },
              ],
            },
          }),
        },
      });
    });
    await openInbox(page);

    await page.locator('#pr-file-list .pr-file-item[data-item-id="101"]').click();

    await expect(page.locator('#canvas-text')).toContainText('Promotion Review');
    await expect(page.locator('#canvas-text')).toContainText('create these idea items');
    await expect(page.locator('#canvas-text')).toContainText('Draft the rollout checklist');
    await expect(page.locator('#canvas-text')).toContainText('Add regression coverage');
  });

  test('mobile canvas swipe flips between inbox items', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);

    await page.locator('#pr-file-list .pr-file-item[data-item-id="101"]').click();
    await expect(page.locator('#canvas-text')).toContainText('Break parser cleanup into a small refactor');
    await expect(page.locator('#pr-file-list .pr-file-item.is-active[data-item-id="101"]')).toHaveCount(1);

    await swipeCanvas(page, -160, 0);
    await expect(page.locator('#canvas-text')).toContainText('Need a response before tomorrow morning');
    await expect(page.locator('#pr-file-list .pr-file-item.is-active[data-item-id="102"]')).toHaveCount(1);

    await swipeCanvas(page, 160, 0);
    await expect(page.locator('#canvas-text')).toContainText('Break parser cleanup into a small refactor');
    await expect(page.locator('#pr-file-list .pr-file-item.is-active[data-item-id="101"]')).toHaveCount(1);
  });

  test('touch gestures expose feedback and commit done, delete, delegate, and later', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);
    await setInboxItems(page, [parserInboxItem]);

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
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('#item-sidebar-menu')).toBeVisible();
    await expect(page.locator('#item-sidebar-menu')).toContainText('Alice');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Codex' }).click();
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    await setInboxItems(page, [parserInboxItem]);
    await touchPhase(page, row, 'start');
    await touchPhase(page, row, 'move', -185, 0);
    await expect(page.locator(row)).toHaveAttribute('data-triage-action', 'later');
    await touchPhase(page, row, 'end', -185, 0);
    await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
    await expect(page.locator('#pr-file-list')).not.toContainText('Review parser cleanup');

    const log = await page.evaluate(() => (window as any).__harnessLog || []);
    const triageCalls = log.filter((entry: any) => entry?.action === 'item_triage');
    expect(triageCalls.map((entry: any) => entry?.payload?.action)).toEqual(['done', 'delete', 'delegate', 'later']);
  });

  test('desktop context menu workspace and context pickers reassign the active item', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await page.evaluate(() => {
      (window as any).__setItemSidebarWorkspaces([
        { id: 1, name: 'Alpha', dir_path: '/tmp/alpha', sphere: 'private', is_active: true },
        { id: 2, name: 'Beta', dir_path: '/tmp/beta', sphere: 'private', is_active: false },
      ]);
    });
    await openInbox(page);
    await setInboxItems(page, [{
      ...parserInboxItem,
      workspace_id: 1,
      project_id: '',
    }]);

    const row = page.locator('#pr-file-list .pr-file-item[data-item-id="101"]');

    await row.click({ button: 'right' });
    await expect(page.locator('#item-sidebar-menu')).toContainText('Workspace...');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Workspace...' }).click();
    await expect(page.locator('#item-sidebar-menu')).toContainText('Beta');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Beta' }).click();
    await expect.poll(async () => {
      return page.evaluate(() => {
        const log = (window as any).__harnessLog || [];
        return log.some((entry: any) => entry?.action === 'item_workspace');
      });
    }).toBe(true);

    await row.click({ button: 'right' });
    await expect(page.locator('#item-sidebar-menu')).toContainText('Context...');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Context...' }).click();
    await expect(page.locator('#item-sidebar-menu')).toContainText('Test');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Test' }).click();
    await expect.poll(async () => {
      return page.evaluate(() => {
        const log = (window as any).__harnessLog || [];
        return log.some((entry: any) => entry?.action === 'item_project');
      });
    }).toBe(true);

    const itemState = await page.evaluate(() => {
      const data = (window as any).__itemSidebarData || {};
      return Array.isArray(data.inbox) ? data.inbox[0] : null;
    });
    expect(itemState?.workspace_id).toBe(2);
    expect(itemState?.project_id).toBe('test');
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
    await setInboxItems(page, [parserInboxItem, privateEmailInboxItem]);

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

  test('non-inbox items support context menus and can move back to inbox', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await openInbox(page);
    await page.evaluate((doneItem) => {
      (window as any).__setItemSidebarData({
        inbox: [],
        waiting: [{
          ...doneItem,
          id: 101,
          title: 'Follow up with Bob',
          state: 'waiting',
          artifact_id: 501,
          artifact_kind: 'idea_note',
          artifact_title: 'Parser cleanup plan',
          source: 'github',
          source_ref: 'owner/repo#177',
        }],
        someday: [],
        done: [doneItem],
      });
    }, reopenedEmailItem);

    await page.locator('.sidebar-tab', { hasText: 'Waiting' }).click();
    const waitingRow = page.locator('#pr-file-list .pr-file-item[data-item-id="101"]');
    await waitingRow.click({ button: 'right' });
    await expect(page.locator('#item-sidebar-menu')).toContainText('Back to Inbox');
    await page.mouse.click(10, 10);

    await page.locator('.sidebar-tab', { hasText: 'Done' }).click();
    const doneRow = page.locator('#pr-file-list .pr-file-item[data-item-id="102"]');
    await doneRow.click({ button: 'right' });
    await expect(page.locator('#item-sidebar-menu')).toContainText('Back to Inbox');
    await page.locator('#item-sidebar-menu .item-sidebar-menu-item', { hasText: 'Back to Inbox' }).click();

    await expect(page.locator('#pr-file-list')).toContainText('No done items.');
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await expect(page.locator('#pr-file-list')).toContainText('Answer triage email');
    await expect.poll(async () => {
      const log = await page.evaluate(() => (window as any).__harnessLog || []);
      return log.some((entry: any) => entry?.action === 'item_state' && entry?.payload?.state === 'inbox');
    }).toBe(true);
  });

  test('partial swipe cancels cleanly, rapid swipes remain stable, and empty inbox stays inert', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await waitReady(page);
    await openInbox(page);
    await setInboxItems(page, [parserInboxItem, privateEmailInboxItem]);

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
