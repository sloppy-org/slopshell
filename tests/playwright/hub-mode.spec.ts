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

  await expect(page.locator('#edge-top-projects .edge-project-btn')).toHaveCount(2);
});

test('clicking hub button activates hub project', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
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

test('system switch_model action updates project model state', async ({ page }) => {
  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'switch_model',
      project_id: 'test',
      alias: 'gpt',
      effort: 'high',
    },
  });

  await expect(page.locator('#edge-top-models .edge-model-btn', { hasText: 'gpt' })).toHaveClass(/is-active/);
  await expect(page.locator('#edge-top-models .edge-reasoning-effort-select')).toHaveValue('high');
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.action === 'project_chat_model');
  }).toBe(false);
});

test('system toggle actions update ui state', async ({ page }) => {
  const silentButton = page.locator('#edge-top-models .edge-silent-btn');
  const convButton = page.locator('#edge-top-models .edge-conv-btn');

  await expect(silentButton).not.toHaveClass(/is-active/);
  await expect(convButton).not.toHaveClass(/is-active/);

  await injectChatEvent(page, { type: 'system_action', action: { type: 'toggle_silent' } });
  await injectChatEvent(page, { type: 'system_action', action: { type: 'toggle_conversation' } });

  await expect(silentButton).toHaveClass(/is-active/);
  await expect(convButton).toHaveClass(/is-active/);
});

test('system switch_project action routes through project activation', async ({ page }) => {
  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'switch_project',
      project_id: 'hub',
    },
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(
      (entry) => entry.type === 'api_fetch'
        && entry.action === 'project_activate'
        && String(entry.payload?.project_id || '') === 'hub',
    );
  }, { timeout: 5_000 }).toBe(true);
});
