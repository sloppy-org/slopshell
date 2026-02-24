import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = { type: string; action?: string; text?: string; [key: string]: unknown };

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
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

async function enableSilentMode(page: Page) {
  await page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app.getState();
    state.ttsSilent = true;
    document.body.classList.add('silent-mode');
    const edgeRight = document.getElementById('edge-right');
    if (edgeRight && window.matchMedia('(max-width: 767px)').matches) {
      edgeRight.classList.add('edge-pinned');
    }
  });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/chat/'));
    if (chatWs) {
      chatWs.injectEvent(p);
    }
  }, payload);
}

async function injectCanvasEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    const canvasWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/canvas/'));
    if (canvasWs) {
      canvasWs.injectEvent(p);
    }
  }, payload);
}

test.describe('silent mode mobile', () => {
  test.use({ viewport: { width: 375, height: 667 } });

  test.beforeEach(async ({ page }) => {
    page.on('console', (msg) => {
      if (msg.type() === 'error') console.log(`BROWSER [error]: ${msg.text()}`);
    });
    await waitReady(page);
    await enableSilentMode(page);
  });

  test('body has silent-mode class', async ({ page }) => {
    const hasSilent = await page.evaluate(() => document.body.classList.contains('silent-mode'));
    expect(hasSilent).toBe(true);
  });

  test('chat pane opens full-width, streams response, autoCanvas closes pane', async ({ page }) => {
    const edgeRight = page.locator('#edge-right');

    // Chat pane should be pinned open
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    // Full-width on mobile
    const width = await edgeRight.evaluate(el => getComputedStyle(el).width);
    expect(parseInt(width)).toBeGreaterThanOrEqual(370);

    // Send a message to create a pending row
    await page.keyboard.type('hello');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // Inject turn_started
    await injectChatEvent(page, {
      type: 'turn_started',
      turn_id: 'silent-t1',
    });
    await page.waitForTimeout(100);

    // Pending row should exist in chat
    const pendingRows = page.locator('#chat-history .chat-message.is-pending');
    await expect(pendingRows).toHaveCount(1);

    // Inject streaming message
    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'silent-t1',
      message: 'Streaming response text...',
    });
    await page.waitForTimeout(100);

    // Chat row should be updated with streaming text
    const chatBubble = page.locator('#chat-history .chat-message.chat-assistant .chat-bubble');
    await expect(chatBubble.first()).toContainText('Streaming response');

    // Inject canvas event (simulating autoCanvas file write)
    await injectCanvasEvent(page, {
      event_id: 'ac-1',
      kind: 'text_artifact',
      title: 'response.md',
      text: 'Full final response on canvas.',
    });
    await page.waitForTimeout(100);

    // Inject final assistant_output with auto_canvas
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'silent-t1',
      message: '',
      auto_canvas: true,
    });
    await page.waitForTimeout(200);

    // Chat pane should be closed (unpinned) after autoCanvas
    await expect(edgeRight).not.toHaveClass(/edge-pinned/);
  });

  test('non-autoCanvas response stays in chat pane', async ({ page }) => {
    const edgeRight = page.locator('#edge-right');
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    // Inject turn
    await injectChatEvent(page, {
      type: 'turn_started',
      turn_id: 'silent-t2',
    });
    await page.waitForTimeout(100);

    // Inject final output without auto_canvas (artifact already on canvas)
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'silent-t2',
      message: 'Short answer stays in chat.',
      auto_canvas: false,
    });
    await page.waitForTimeout(200);

    // Chat pane should remain open
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    // Chat should contain the response
    const chatBubble = page.locator('#chat-history .chat-message.chat-assistant .chat-bubble');
    await expect(chatBubble.first()).toContainText('Short answer stays in chat');
  });
});

test.describe('chat pane interactions', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test.beforeEach(async ({ page }) => {
    page.on('console', (msg) => {
      if (msg.type() === 'error') console.log(`BROWSER [error]: ${msg.text()}`);
    });
    await waitReady(page);
    // Open chat pane (pin it)
    await page.evaluate(() => {
      const edgeRight = document.getElementById('edge-right');
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    });
  });

  test('tap on chat history toggles recording', async ({ page }) => {
    // Click on the chat-history background area
    const chatHistory = page.locator('#chat-history');
    await chatHistory.click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(300);

    // zen-indicator should show is-recording
    const indicator = page.locator('#zen-indicator');
    await expect(indicator).toHaveClass(/is-recording/);

    // Click again to stop
    await chatHistory.click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(300);

    // Check harness log for stt stop
    const log = await getLog(page);
    const sttStop = log.find(e => e.type === 'stt' && e.action === 'stop');
    expect(sttStop).toBeTruthy();
  });

  test('chat-pane-input sends on Enter', async ({ page }) => {
    const input = page.locator('#chat-pane-input');
    await input.focus();
    await input.fill('test message from chat pane');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // Check harness log for message_sent
    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent' && typeof e.text === 'string' && e.text.includes('test message from chat pane'));
    expect(sent).toBeTruthy();

    // Input should be cleared
    await expect(input).toHaveValue('');
  });

  test('hyperlinks in chat history remain clickable', async ({ page }) => {
    // Inject a chat row containing an <a> tag
    await page.evaluate(() => {
      const ch = document.getElementById('chat-history');
      if (!ch) return;
      const row = document.createElement('div');
      row.className = 'chat-message chat-assistant';
      row.innerHTML = '<div class="chat-bubble"><a href="#test-link" id="test-anchor">Click me</a></div>';
      ch.appendChild(row);
    });

    // Clear harness log
    await page.evaluate(() => { (window as any).__harnessLog = []; });

    // Click the link
    await page.locator('#test-anchor').click();
    await page.waitForTimeout(300);

    // No recording should have started
    const log = await getLog(page);
    const sttStart = log.find(e => e.type === 'stt' && e.action === 'start');
    expect(sttStart).toBeFalsy();
  });
});

test.describe('silent mode desktop', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await enableSilentMode(page);
  });

  test('body has silent-mode class on desktop', async ({ page }) => {
    const hasSilent = await page.evaluate(() => document.body.classList.contains('silent-mode'));
    expect(hasSilent).toBe(true);
  });

  test('chat pane is not auto-pinned on desktop', async ({ page }) => {
    const edgeRight = page.locator('#edge-right');
    const isPinned = await edgeRight.evaluate(el => el.classList.contains('edge-pinned'));
    expect(isPinned).toBe(false);
  });
});
