import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function switchToTestProject(page: Page) {
  await page.evaluate(() => {
    const buttons = Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'));
    const button = buttons.find((node) => node.textContent?.trim().toLowerCase() === 'test');
    if (button instanceof HTMLButtonElement) button.click();
  });
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    if (String(state?.activeWorkspaceId || '') !== 'test') return 'switching';
    return state?.chatWs?.readyState === (window as any).WebSocket.OPEN ? 'ready' : 'waiting';
  })).toBe('ready');
}

async function openCircle(page: Page) {
  await page.evaluate(() => {
    const button = document.getElementById('tabura-circle-dot');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('tabura circle dot not found');
    }
    button.click();
  });
  await expect(page.locator('#tabura-circle')).toHaveAttribute('data-state', 'expanded');
}

async function clickSegment(page: Page, segment: string) {
  await page.evaluate((name) => {
    const button = document.getElementById(`tabura-circle-segment-${name}`);
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error(`circle segment not found: ${name}`);
    }
    button.click();
  }, segment);
}

test.beforeEach(async ({ page }) => {
  await waitReady(page);
  await switchToTestProject(page);
});

test('top panel keeps summary only while Tabura Circle owns live controls', async ({ page }) => {
  await expect(page.locator('#tabura-circle-dot')).toBeVisible();
  await expect(page.locator('#edge-top-models')).toHaveAttribute('aria-label', 'Workspace runtime summary');
  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Manual');
  await expect(page.locator('#edge-top-models button')).toHaveCount(0);
});

test('circle segments switch tools without using the top panel', async ({ page }) => {
  await openCircle(page);
  await clickSegment(page, 'ink');
  await expect(page.locator('#tabura-circle-segment-ink')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'ink');
});

test('silent stays independent from tool selection', async ({ page }) => {
  await openCircle(page);
  await clickSegment(page, 'silent');
  await expect(page.locator('#tabura-circle-segment-silent')).toHaveAttribute('aria-pressed', 'true');

  await clickSegment(page, 'pointer');
  await expect(page.locator('#tabura-circle-segment-silent')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#tabura-circle-segment-pointer')).toHaveAttribute('aria-pressed', 'true');
});
