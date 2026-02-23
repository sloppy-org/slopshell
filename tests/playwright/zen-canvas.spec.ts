import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = { type: string; action?: string; text?: string; [key: string]: unknown };

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/zen-harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function injectCanvasModuleRef(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/canvas.js');
    (window as any).__canvasModule = mod;
  });
}

async function renderTestArtifact(page: Page, text = 'Line one\nLine two\nLine three\nLine four\nLine five') {
  await page.evaluate((content) => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'art-1',
      kind: 'text_artifact',
      title: 'test.txt',
      text: content,
    });
    // Show canvas pane (simulating what app.js showCanvasColumn does)
    const ct = document.getElementById('canvas-text');
    if (ct) {
      ct.style.display = '';
      ct.classList.add('is-active');
    }
    // Mark state as having artifact
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  }, text);
}

async function waitForLogEntry(page: Page, type: string, action?: string) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(e => e.type === type && (!action || e.action === action));
  }, { timeout: 5_000 }).toBe(true);
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    // Find the chat WS (not canvas)
    const chatWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/chat/'));
    if (chatWs) {
      chatWs.injectEvent(p);
    }
  }, payload);
}

test.describe('zen canvas - tabula rasa', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('starts as white screen with no visible chrome', async ({ page }) => {
    // No toolbar visible
    const toolbar = page.locator('#toolbar');
    await expect(toolbar).toBeHidden();

    // No prompt bar
    const promptBar = page.locator('#prompt-bar');
    await expect(promptBar).toHaveCount(0);

    // Canvas column fills viewport
    const canvasCol = page.locator('#canvas-column');
    await expect(canvasCol).toBeVisible();

    // No visible panes (rasa = blank)
    const activePanes = page.locator('.canvas-pane.is-active');
    await expect(activePanes).toHaveCount(0);
  });

  test('keyboard typing activates text input, Enter sends, text cleared', async ({ page }) => {
    // Type a character - should auto-open zen-input
    await page.keyboard.type('h');
    await page.waitForTimeout(100);

    const zenInput = page.locator('#zen-input');
    await expect(zenInput).toBeVisible();
    await expect(zenInput).toHaveValue('h');

    // Type more and send
    await page.keyboard.type('ello');
    await expect(zenInput).toHaveValue('hello');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // Text input should be hidden after send
    await expect(zenInput).toBeHidden();

    // Check message was sent
    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('hello');
  });

  test('click starts recording, indicator visible, click again stops', async ({ page }) => {
    await clearLog(page);

    // Click on canvas area
    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);

    // Recording indicator should be visible
    const indicator = page.locator('#zen-indicator');
    await expect(indicator).toBeVisible();
    await expect(indicator).toBeVisible();

    // Wait for recorder to start
    await waitForLogEntry(page, 'recorder', 'start');

    // Click again to stop
    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'stt', 'stop');

    // Indicator should disappear
    await expect(indicator).toBeHidden();
  });

  test('right-click opens text input at position', async ({ page }) => {
    await page.mouse.click(300, 300, { button: 'right' });
    await page.waitForTimeout(100);

    const zenInput = page.locator('#zen-input');
    await expect(zenInput).toBeVisible();
    await expect(zenInput).toBeFocused();
  });

  test('Escape dismisses text input', async ({ page }) => {
    await page.mouse.click(300, 300, { button: 'right' });
    await page.waitForTimeout(100);
    await expect(page.locator('#zen-input')).toBeVisible();

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);
    await expect(page.locator('#zen-input')).toBeHidden();
  });
});

test.describe('zen canvas - response overlay', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('response overlay streams in and click outside dismisses', async ({ page }) => {
    // Send a message to trigger turn
    await page.keyboard.type('hello');
    await page.waitForTimeout(50);
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    // Inject turn_started
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'turn-1' });
    await page.waitForTimeout(100);

    // Overlay should appear
    const overlay = page.locator('#zen-overlay');
    await expect(overlay).toBeVisible();

    // Inject assistant message
    await injectChatEvent(page, { type: 'assistant_message', turn_id: 'turn-1', message: 'Hello back!' });
    await page.waitForTimeout(100);

    const content = page.locator('.zen-overlay-content');
    const text = await content.textContent();
    expect(text).toContain('Hello back!');

    // Inject message_persisted to finalize
    await injectChatEvent(page, { type: 'message_persisted', role: 'assistant', turn_id: 'turn-1', message: 'Hello back!' });
    await page.waitForTimeout(100);

    // Click outside overlay to dismiss
    await page.mouse.click(10, 10);
    await page.waitForTimeout(100);
    await expect(overlay).toBeHidden();
  });

  test('error shows in overlay and auto-dismisses', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'turn-err' });
    await page.waitForTimeout(50);
    await injectChatEvent(page, { type: 'error', turn_id: 'turn-err', error: 'something broke' });
    await page.waitForTimeout(100);

    const overlay = page.locator('#zen-overlay');
    await expect(overlay).toBeVisible();
    const content = await page.locator('.zen-overlay-content').textContent();
    expect(content).toContain('something broke');

    // Auto-dismisses after ~2s
    await page.waitForTimeout(2200);
    await expect(overlay).toBeHidden();
  });
});

test.describe('zen canvas - artifact mode', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
    await renderTestArtifact(page);
  });

  test('artifact fills viewport, text visible', async ({ page }) => {
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();
    const text = await canvasText.textContent();
    expect(text).toContain('Line one');
    expect(text).toContain('Line five');
  });

  test('Escape clears artifact back to tabula rasa', async ({ page }) => {
    await expect(page.locator('#canvas-text')).toBeVisible();

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);

    // No active pane
    const activePanes = page.locator('.canvas-pane.is-active');
    await expect(activePanes).toHaveCount(0);
  });

  test('document edit applies diff highlight', async ({ page }) => {
    // First render with multi-paragraph markdown (blank line = separate <p> blocks)
    const original = '# Title\n\nFirst paragraph.\n\nSecond paragraph.\n\nThird paragraph.';
    await page.evaluate((content) => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({
        event_id: 'art-diff-1',
        kind: 'text_artifact',
        title: 'test.txt',
        text: content,
      });
      const ct = document.getElementById('canvas-text');
      if (ct) { ct.style.display = ''; ct.classList.add('is-active'); }
    }, original);
    await page.waitForTimeout(50);

    // Update with changed content (second paragraph changed)
    const updated = '# Title\n\nFirst paragraph.\n\nSecond paragraph CHANGED.\n\nThird paragraph.';
    await page.evaluate((content) => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({
        event_id: 'art-diff-2',
        kind: 'text_artifact',
        title: 'test.txt',
        text: content,
      });
    }, updated);
    await page.waitForTimeout(100);

    // Check for diff-highlight class on changed blocks
    const highlighted = page.locator('.diff-highlight');
    const count = await highlighted.count();
    expect(count).toBeGreaterThan(0);
  });
});

test.describe('zen canvas - edge panels', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('mouse near right edge reveals diagnostics panel', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });

    const edgeRight = page.locator('#edge-right');
    // Initially not active
    const initialClasses = await edgeRight.getAttribute('class');
    expect(initialClasses).not.toContain('edge-active');

    // Move mouse to right edge
    await page.mouse.move(1275, 360);
    await page.waitForTimeout(100);

    await expect(edgeRight).toHaveClass(/edge-active/);
  });

  test('mouse near top edge reveals projects panel', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });

    const edgeTop = page.locator('#edge-top');
    // Move mouse to top edge
    await page.mouse.move(640, 5);
    await page.waitForTimeout(100);

    await expect(edgeTop).toHaveClass(/edge-active/);
  });

  test('Escape closes edge panels', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });

    // Open right panel
    await page.mouse.move(1275, 360);
    await page.waitForTimeout(100);
    const edgeRight = page.locator('#edge-right');
    await expect(edgeRight).toHaveClass(/edge-active/);

    // Click to pin
    await page.mouse.click(1275, 360);
    await page.waitForTimeout(100);
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    // Escape closes
    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);
    const classes = await edgeRight.getAttribute('class');
    expect(classes).not.toContain('edge-pinned');
    expect(classes).not.toContain('edge-active');
  });

  test('chat log appears in right edge panel', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await injectCanvasModuleRef(page);

    // Send a message
    await page.keyboard.type('test msg');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // Open right panel
    await page.mouse.move(1275, 360);
    await page.waitForTimeout(200);

    const chatHistory = page.locator('#chat-history');
    const chatText = await chatHistory.textContent();
    expect(chatText).toContain('test msg');
  });
});
