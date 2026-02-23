import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = { type: string; action: string; [key: string]: unknown };

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

async function waitForLogEntry(page: Page, type: string, action: string) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(e => e.type === type && e.action === action);
  }, { timeout: 5_000 }).toBe(true);
}

async function waitForSTTAction(page: Page, action: string) {
  await waitForLogEntry(page, 'stt', action);
}

test.beforeEach(async ({ page }) => {
  page.on('console', (msg) => {
    if (msg.type() === 'error') console.log(`BROWSER [error]: ${msg.text()}`);
  });
  page.on('pageerror', (err) => console.log(`PAGE ERROR: ${err.message}`));
  await page.goto('/tests/playwright/zen-harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
});

test('click on canvas starts voice recording', async ({ page }) => {
  await clearLog(page);

  // Click on canvas area to start recording (zen mode: tap = voice)
  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);

  await waitForLogEntry(page, 'recorder', 'start');

  // Recording indicator should be visible
  const indicator = page.locator('#zen-indicator');
  await expect(indicator).toBeVisible();

  // Click again to stop recording
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'start');
  await waitForSTTAction(page, 'stop');

  const log = await getLog(page);
  const sttActions = log.filter(e => e.type === 'stt').map(e => e.action);
  expect(sttActions).toContain('start');
  expect(sttActions).toContain('stop');
});

test('silence auto-stop sends transcript without manual stop click', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      -80, -80, -80, -80, -80, -80, -80, -80,
      -12, -12, -12, -12, -12, -12, -12, -12, -12, -12,
      -80, -80, -80, -80, -80, -80, -80, -80, -80, -80,
      -80, -80, -80, -80, -80, -80, -80, -80, -80, -80,
    ]);
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(200);

  const log = await getLog(page);
  const sent = log.find(e => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');
  expect(log.some(e => e.type === 'stt' && e.action === 'cancel')).toBe(false);
});

test('silence auto-stop works with low-level speech near ambient floor', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    // Simulate hardware like a quiet webcam mic:
    // ambient ~ -41 dB, speech ~ -35 dB, then silence.
    (window as any).__setVadDbFrames([
      -41, -41, -41, -41, -41, -41, -41, -41,
      -35, -35, -35, -35, -35, -35, -35, -35,
      -44, -44, -44, -44, -44, -44, -44, -44, -44, -44,
      -44, -44, -44, -44, -44, -44, -44, -44, -44, -44,
    ]);
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(200);

  const log = await getLog(page);
  const sent = log.find(e => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');
  expect(log.some(e => e.type === 'stt' && e.action === 'cancel')).toBe(false);
});

test('Control long-press starts voice recording (desktop PTT)', async ({ page }) => {
  await clearLog(page);

  // Press and hold Control
  await page.keyboard.down('Control');
  await page.waitForTimeout(300);
  await waitForLogEntry(page, 'recorder', 'start');

  // Release Control
  await page.keyboard.up('Control');
  await waitForSTTAction(page, 'stop');

  const log = await getLog(page);
  const sttActions = log.filter(e => e.type === 'stt').map(e => e.action);
  expect(sttActions).toContain('start');
  expect(sttActions).toContain('stop');
});

test('short Control press does not start voice recording', async ({ page }) => {
  await clearLog(page);
  await page.keyboard.down('Control');
  await page.waitForTimeout(50);
  await page.keyboard.up('Control');
  await page.waitForTimeout(500);

  const log = await getLog(page);
  const sttActions = log.filter(e => e.type === 'stt');
  expect(sttActions).toHaveLength(0);
});

test('Enter stops active recording', async ({ page }) => {
  await clearLog(page);

  // Start recording by clicking
  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  // Press Enter to stop
  await page.keyboard.press('Enter');
  await waitForSTTAction(page, 'stop');
});

test('voice transcription result gets sent as message', async ({ page }) => {
  await clearLog(page);

  // Start recording
  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  // Stop recording (will auto-send via zen voice capture)
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(500);

  // Check that message was sent (MockWebSocket returns 'hello world')
  const log = await getLog(page);
  const sent = log.find(e => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');
});

test('recording indicator shows symbol', async ({ page }) => {
  await clearLog(page);

  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  const indicator = page.locator('#zen-indicator');
  await expect(indicator).toBeVisible();
  await expect(page.locator('.zen-record-dot')).toBeVisible();
  await expect(page.locator('.zen-stop-square')).toBeHidden();

  // Stop recording and transition to stop square
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(200);
  await expect(indicator).toBeVisible();
  await expect(page.locator('.zen-stop-square')).toBeVisible();
  await expect(page.locator('.zen-record-dot')).toBeHidden();
});
