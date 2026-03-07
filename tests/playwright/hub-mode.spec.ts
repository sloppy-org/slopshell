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
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => String(ws?.url || '').includes('/ws/chat/'));
    if (chatWs && typeof chatWs.injectEvent === 'function') {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
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

test('hub button is visible among project buttons', async ({ page }) => {
  const hubButton = page.locator('#edge-top-projects .edge-hub-btn');
  await expect(hubButton).toHaveCount(1);
  await expect(hubButton).toHaveText('Hub');
  await expect(hubButton).toHaveClass(/is-active/);

  await expect(page.locator('#edge-top-projects .edge-project-btn')).toHaveCount(2);
});

test('clicking hub button activates hub project', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  await page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)').click();
  const hubButton = page.locator('#edge-top-projects .edge-hub-btn');
  await hubButton.click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'hub',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect(hubButton).toHaveClass(/is-active/);
});

test('switching from hub back to project keeps normal project switching', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  const hubButton = page.locator('#edge-top-projects .edge-hub-btn');
  await page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)').click();
  await hubButton.click();
  await page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)').click();

  await expect.poll(async () => {
    const log = await getLog(page);
    const seenHub = log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'hub',
    );
    const seenProject = log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'test',
    );
    return seenHub && seenProject;
  }, { timeout: 5_000 }).toBe(true);
});

test('hub monitors project run state without activating the project', async ({ page }) => {
  const projectButton = page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)');

  await page.evaluate(() => {
    (window as any).__setProjectRunStates({
      test: { active_turns: 1, queued_turns: 2, is_working: true, status: 'running', active_turn_id: 'turn-1' },
    });
  });

  await expect.poll(async () => projectButton.getAttribute('title'), { timeout: 5_000 }).toContain('1 active, 2 queued');
  await expect(projectButton).toHaveClass(/is-working/);
  await expect(projectButton).not.toHaveClass(/is-active/);
});

test('hub shows unread project state and activation clears it', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  const hubButton = page.locator('#edge-top-projects .edge-hub-btn');
  const projectButton = page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)');

  await hubButton.click();
  await expect(hubButton).toHaveClass(/is-active/);

  await page.evaluate(() => {
    (window as any).__setProjectActivity({
      test: { unread: true, review_pending: false, chat_mode: 'chat' },
    });
  });

  await expect.poll(async () => projectButton.getAttribute('class'), { timeout: 5_000 }).toContain('is-unread');

  await projectButton.click();
  await expect(projectButton).not.toHaveClass(/is-unread/);

  await hubButton.click();
  await expect(hubButton).toHaveClass(/is-active/);

  await page.evaluate(() => {
    (window as any).__setProjectActivity({
      test: { unread: true, review_pending: false, chat_mode: 'chat' },
    });
  });

  await expect.poll(async () => projectButton.getAttribute('class'), { timeout: 5_000 }).toContain('is-unread');
});

test('system switch_model action updates project model state', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  const projectButton = page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)');
  await projectButton.click();
  await expect(projectButton).toHaveClass(/is-active/);
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return String(s.activeProjectId || '') === 'test'
      && s.chatWs?.readyState === wsOpen;
  });
  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'switch_model',
      project_id: 'test',
      alias: 'gpt',
      effort: 'high',
    },
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.action === 'project_chat_model');
  }).toBe(false);
});

test('system toggle actions update ui state', async ({ page }) => {
  const silentButton = page.locator('#edge-top-models .edge-silent-btn');
  const dialogueButton = page.locator('#edge-top-models .edge-live-dialogue-btn');

  await expect(silentButton).not.toHaveClass(/is-active/);
  await expect(dialogueButton).toBeVisible();

  await injectChatEvent(page, { type: 'system_action', action: { type: 'toggle_silent' } });
  await injectChatEvent(page, { type: 'system_action', action: { type: 'toggle_conversation' } });

  await expect(silentButton).toHaveClass(/is-active/);
  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
});

test('system switch_project action routes through project activation', async ({ page }) => {
  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'switch_project',
      project_id: 'test',
    },
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
        (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'test',
    );
  }, { timeout: 5_000 }).toBe(true);
});

test('temporary meeting can be spawned from the current project and persisted', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  await page.locator('#edge-top-projects .edge-project-btn:not(.edge-hub-btn)').click();
  await expect(page.locator('#edge-top-models .edge-temp-meeting-btn')).toBeVisible();

  await page.locator('#edge-top-models .edge-temp-meeting-btn').click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.kind || '') === 'meeting'
        && String(entry.payload?.source_project_id || '') === 'test',
    );
  }, { timeout: 5_000 }).toBe(true);

  const meetingButton = page.locator('#edge-top-projects .edge-project-btn.is-active').filter({ hasText: 'Meeting 1' });
  await expect(meetingButton).toHaveCount(1);
  await expect(page.locator('#edge-top-models .edge-temp-persist-btn')).toBeVisible();
  await expect(page.locator('#edge-top-models .edge-temp-discard-btn')).toBeVisible();

  await page.locator('#edge-top-models .edge-temp-persist-btn').click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_persist'
        && String(entry.payload?.project_id || '') === 'meeting-1',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect(page.locator('#edge-top-models .edge-temp-persist-btn')).toHaveCount(0);
  await expect(page.locator('#edge-top-models .edge-temp-discard-btn')).toHaveCount(0);
  await expect(page.locator('#edge-top-models .edge-temp-meeting-btn')).toBeVisible();
});

test('temporary task can be spawned from hub and discarded', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  await expect(page.locator('#edge-top-models .edge-temp-task-btn')).toBeVisible();

  await page.locator('#edge-top-models .edge-temp-task-btn').click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_create'
        && String(entry.payload?.kind || '') === 'task'
        && String(entry.payload?.source_project_id || '') === '',
    );
  }, { timeout: 5_000 }).toBe(true);

  const taskButton = page.locator('#edge-top-projects .edge-project-btn.is-active').filter({ hasText: 'Task 1' });
  await expect(taskButton).toHaveCount(1);
  await page.locator('#edge-top-models .edge-temp-discard-btn').click();

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_discard'
        && String(entry.payload?.project_id || '') === 'task-1',
    );
  }, { timeout: 5_000 }).toBe(true);

  await expect(page.locator('#edge-top-projects .edge-hub-btn')).toHaveClass(/is-active/);
});
