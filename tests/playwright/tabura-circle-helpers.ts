import { expect, type Locator, type Page } from '@playwright/test';

export type TaburaCircleSegment =
  | 'dialogue'
  | 'meeting'
  | 'silent'
  | 'prompt'
  | 'text_note'
  | 'pointer'
  | 'highlight'
  | 'ink';

const LIVE_STATUS_SELECTOR = '#edge-top-models .edge-live-status';

export function circleSegment(page: Page, segment: TaburaCircleSegment): Locator {
  return page.locator(`#tabura-circle-menu .tabura-circle-segment[data-segment="${segment}"]`);
}

export async function waitForCircleControls(page: Page) {
  await expect(circleSegment(page, 'dialogue')).toBeAttached();
  await expect(circleSegment(page, 'meeting')).toBeAttached();
  await expect(circleSegment(page, 'silent')).toBeAttached();
}

export async function switchToWorkspace(page: Page, workspaceId: string) {
  await page.evaluate((targetWorkspaceId) => {
    const buttons = Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'));
    const button = buttons.find((node) => node.textContent?.trim().toLowerCase() === targetWorkspaceId);
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error(`project button not found: ${targetWorkspaceId}`);
    }
    button.click();
  }, workspaceId);
  await expect.poll(async () => page.evaluate((targetWorkspaceId) => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    const wsOpen = (window as any).WebSocket.OPEN;
    if (String(state?.activeWorkspaceId || '') !== targetWorkspaceId) return '';
    return state?.chatWs?.readyState === wsOpen ? 'ready' : 'waiting';
  }, workspaceId)).toBe('ready');
}

export async function openCircle(page: Page) {
  if (await page.locator('#tabura-circle').getAttribute('data-state') === 'expanded') {
    return;
  }
  await page.evaluate(() => {
    const button = document.getElementById('tabura-circle-dot');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('tabura circle dot not found');
    }
    button.click();
  });
  await expect(page.locator('#tabura-circle')).toHaveAttribute('data-state', 'expanded');
}

export async function closeCircle(page: Page) {
  if (await page.locator('#tabura-circle').getAttribute('data-state') !== 'expanded') {
    return;
  }
  await page.evaluate(() => {
    const button = document.getElementById('tabura-circle-dot');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('tabura circle dot not found');
    }
    button.click();
  });
  await expect(page.locator('#tabura-circle')).toHaveAttribute('data-state', 'collapsed');
}

export async function clickCircleSegment(page: Page, segment: TaburaCircleSegment) {
  await openCircle(page);
  await page.evaluate((targetSegment) => {
    const button = document.querySelector(`#tabura-circle-menu .tabura-circle-segment[data-segment="${targetSegment}"]`);
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error(`tabura circle segment not found: ${targetSegment}`);
    }
    button.click();
  }, segment);
}

export async function setLiveMode(page: Page, mode: 'dialogue' | 'meeting') {
  await switchToWorkspace(page, 'test');
  await waitForCircleControls(page);
  const segment = circleSegment(page, mode);
  if (await segment.getAttribute('aria-pressed') !== 'true') {
    await clickCircleSegment(page, mode);
  }
  await expect(page.locator(LIVE_STATUS_SELECTOR)).toContainText(mode === 'meeting' ? 'Meeting' : 'Dialogue');
  await expect(segment).toHaveAttribute('aria-pressed', 'true');
}

export async function stopLiveMode(page: Page, mode: 'dialogue' | 'meeting') {
  await switchToWorkspace(page, 'test');
  await waitForCircleControls(page);
  const segment = circleSegment(page, mode);
  if (await segment.getAttribute('aria-pressed') === 'true') {
    await clickCircleSegment(page, mode);
  }
  await expect(page.locator(LIVE_STATUS_SELECTOR)).toContainText('Manual');
  await expect(segment).toHaveAttribute('aria-pressed', 'false');
}

export async function setSilentMode(page: Page, enabled: boolean) {
  await waitForCircleControls(page);
  const segment = circleSegment(page, 'silent');
  const current = await segment.getAttribute('aria-pressed');
  const target = enabled ? 'true' : 'false';
  if (current !== target) {
    await clickCircleSegment(page, 'silent');
  }
  await expect(segment).toHaveAttribute('aria-pressed', target);
}
