import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  action?: string;
  text?: string;
  payload?: Record<string, unknown>;
};

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

test('dictation mode accumulates draft on canvas and dispatches only on explicit send', async ({ page }) => {
  await page.goto('/tests/playwright/harness.html');

  const input = page.locator('#chat-pane-input');
  await expect(input).toBeVisible();
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });

  await input.fill('take a letter');
  await input.press('Enter');

  await expect(page.locator('#dictation-indicator')).toBeVisible();
  await expect(page.locator('#dictation-indicator')).toContainText('Email Reply');

  let log = await getLog(page);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'dictation_start')).toBe(true);
  expect(log.some((entry) => entry.type === 'message_sent')).toBe(false);

  await page.evaluate(async () => {
    await (window as any)._taburaApp.appendDictationTranscript('Thanks for the update. I can send the revision tomorrow.');
  });

  await expect(page.locator('#canvas-text')).toContainText('Email Reply Draft');
  await expect(page.locator('#canvas-text')).toContainText('Thanks for the update. I can send the revision tomorrow.');

  log = await getLog(page);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'dictation_append')).toBe(true);
  expect(log.some((entry) => entry.type === 'message_sent')).toBe(false);

  await page.locator('#dictation-send').click();

  await expect.poll(async () => {
    const current = await getLog(page);
    return current.filter((entry) => entry.type === 'message_sent').length;
  }).toBe(1);

  log = await getLog(page);
  const message = log.find((entry) => entry.type === 'message_sent');
  expect(String(message?.text || '')).toContain('Use this dictated email reply draft');
  expect(String(message?.text || '')).toContain('Thanks for the update. I can send the revision tomorrow.');
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'dictation_draft')).toBe(true);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'dictation_stop')).toBe(true);
});
