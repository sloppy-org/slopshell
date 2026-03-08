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
});
