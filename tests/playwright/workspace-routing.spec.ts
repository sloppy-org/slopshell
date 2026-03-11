import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  action?: string;
  payload?: Record<string, unknown>;
};

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    const activeChatWs = app?.getState?.().chatWs;
    if (activeChatWs && typeof activeChatWs.injectEvent === 'function') {
      activeChatWs.injectEvent(eventPayload);
      return;
    }
    const sessions = (window as any).__mockWsSessions || [];
    const candidates = sessions.filter((ws: any) => String(ws?.url || '').includes('/ws/chat/'));
    const chatWs = candidates[candidates.length - 1];
    if (chatWs && typeof chatWs.injectEvent === 'function') {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
}

async function seedTwoProjects(page: Page) {
  await page.evaluate(() => {
    (window as any).__setProjects([
      {
        id: 'test',
        name: 'Test',
        kind: 'managed',
        sphere: 'private',
        project_key: '/tmp/test',
        root_path: '/tmp/test',
        chat_session_id: 'chat-1',
        canvas_session_id: 'local',
        chat_mode: 'chat',
        chat_model: 'spark',
        chat_model_reasoning_effort: 'low',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
      {
        id: 'notes',
        name: 'Notes',
        kind: 'managed',
        sphere: 'private',
        project_key: '/tmp/notes',
        root_path: '/tmp/notes',
        chat_session_id: 'chat-2',
        canvas_session_id: 'notes',
        chat_mode: 'chat',
        chat_model: 'spark',
        chat_model_reasoning_effort: 'low',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
    ], 'test');
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
}

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(150);
  await clearLog(page);
});

test('project buttons only show real projects', async ({ page }) => {
  await seedTwoProjects(page);
  await expect(page.locator('#edge-top-projects .edge-project-btn')).toHaveCount(2);
  await expect(page.locator('#edge-top-projects')).toContainText('Test');
  await expect(page.locator('#edge-top-projects')).toContainText('Notes');
  const labels = await page.locator('#edge-top-projects .edge-project-btn').allTextContents();
  expect(labels.map((label) => label.trim().toLowerCase())).toEqual(['test', 'notes']);
});

test('clicking another project activates it', async ({ page }) => {
  await seedTwoProjects(page);
  await page.getByRole('button', { name: /Notes: idle/ }).click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'notes',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect(page.getByRole('button', { name: /Notes: idle/ })).toHaveClass(/is-active/);
});

test('inactive projects show run state and unread state clears on activation', async ({ page }) => {
  await seedTwoProjects(page);
  const notesButton = page.getByRole('button', { name: /Notes:/ });

  await page.evaluate(() => {
    (window as any).__setProjectRunStates({
      notes: { active_turns: 1, queued_turns: 2, is_working: true, status: 'running', active_turn_id: 'turn-1' },
    });
    (window as any).__setProjectActivity({
      notes: { unread: true, review_pending: false, chat_mode: 'chat' },
    });
  });

  await expect.poll(async () => notesButton.getAttribute('title'), { timeout: 5_000 }).toContain('1 active, 2 queued');
  await expect(notesButton).toHaveClass(/is-working/);
  await expect(notesButton).toHaveClass(/is-unread/);

  await notesButton.click();
  await expect(notesButton).not.toHaveClass(/is-unread/);
  await expect(notesButton).toHaveClass(/is-active/);
});

test('system actions route through ordinary projects', async ({ page }) => {
  await seedTwoProjects(page);
  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'switch_project',
      project_id: 'notes',
    },
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'notes',
    );
  }, { timeout: 5_000 }).toBe(true);
});

test('temporary tasks discard back to the remaining active project', async ({ page }) => {
  await seedTwoProjects(page);
  await expect(page.locator('#edge-top-models .edge-temp-task-btn')).toBeVisible();

  await page.locator('#edge-top-models .edge-temp-task-btn').click();
  await expect(page.locator('#edge-top-projects .edge-project-btn.is-active')).toContainText('Task 1');

  await page.locator('#edge-top-models .edge-temp-discard-btn').click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_discard'
        && String(entry.payload?.project_id || '') === 'task-1',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect(page.locator('#edge-top-projects .edge-project-btn.is-active')).toContainText('Test');
});
