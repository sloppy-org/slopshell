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

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/playwright/zen-harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(150);
  await clearLog(page);
});

test('hub button is visible and distinct from project buttons', async ({ page }) => {
  const hubButton = page.locator('#edge-top-models .edge-hub-btn');
  await expect(hubButton).toHaveCount(1);
  await expect(hubButton).toHaveText('Hub');

  await expect(page.locator('#edge-top-projects .edge-project-btn')).toHaveCount(1);
  await expect(page.locator('#edge-top-projects .edge-project-btn')).toHaveText('Test');
});

test('clicking hub button activates hub project', async ({ page }) => {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
  const hubButton = page.locator('#edge-top-models .edge-hub-btn');
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
  const hubButton = page.locator('#edge-top-models .edge-hub-btn');
  await hubButton.click();
  await page.locator('#edge-top-projects .edge-project-btn').click();

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
