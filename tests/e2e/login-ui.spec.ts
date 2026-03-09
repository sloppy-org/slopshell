import { expect, test } from './live';
import { requireTestPassword } from './helpers';

test.describe('login UI flow', () => {
  test('password submit reveals the main app', async ({ page }) => {
    const pageErrors: string[] = [];
    const consoleErrors: string[] = [];
    page.on('pageerror', (err) => pageErrors.push(err.message));
    page.on('console', (message) => {
      if (message.type() === 'error') {
        consoleErrors.push(message.text());
      }
    });

    const setupResp = await page.request.get('/api/setup');
    const setup = await setupResp.json() as Record<string, unknown>;
    test.skip(!setup.has_password, 'auth disabled');

    const password = requireTestPassword();

    await page.goto('/');
    await expect(page.locator('#view-login')).toBeVisible();
    await expect(page.locator('#view-main')).toBeHidden();

    await page.fill('#login-password', password);
    await page.click('#btn-login');
    await page.waitForLoadState('networkidle');

    await expect(page.locator('#view-login')).toBeHidden();
    await expect(page.locator('#view-main')).toBeVisible();
    await expect(page.locator('#workspace')).toBeVisible();
    await expect(page.locator('#login-error')).toHaveText('');
    await expect(page.locator('body')).not.toContainText(/invalid json/i);
    await expect.poll(async () => {
      const cookies = await page.context().cookies();
      return cookies.some((cookie) => cookie.name === 'tabura_session');
    }).toBe(true);
    expect(pageErrors).toEqual([]);
    expect(consoleErrors).toEqual([]);
  });
});
