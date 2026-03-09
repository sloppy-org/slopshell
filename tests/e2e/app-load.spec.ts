import { applySessionCookie, expect, test } from './live';
import { SERVER_URL, authenticate, authFetch } from './helpers';

test.describe('app load smoke test', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('page loads without console errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await applySessionCookie(page, sessionToken);

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    expect(errors).toEqual([]);
  });

  test('key DOM elements exist', async ({ page }) => {
    await applySessionCookie(page, sessionToken);

    await page.goto('/');
    await page.waitForLoadState('networkidle');

    for (const sel of ['#workspace', '#canvas-column', '#indicator']) {
      await expect(page.locator(sel)).toBeAttached();
    }
  });

  test('/api/setup returns valid JSON', async () => {
    const resp = await authFetch(`${SERVER_URL}/api/setup`, sessionToken);
    expect(resp.ok).toBe(true);
    const body = await resp.json();
    expect(body).toBeDefined();
    expect(typeof body).toBe('object');
  });

  test('/api/runtime returns valid JSON', async () => {
    const resp = await authFetch(`${SERVER_URL}/api/runtime`, sessionToken);
    expect(resp.ok).toBe(true);
    const body = await resp.json();
    expect(body).toBeDefined();
    expect(typeof body).toBe('object');
  });
});
