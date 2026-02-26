import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = { type: string; action?: string; text?: string; [key: string]: unknown };

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

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

async function injectCanvasModuleRef(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/canvas.js');
    (window as any).__canvasModule = mod;
    const ui = await import('../../internal/web/static/ui.js');
    (window as any).__uiModule = ui;
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

async function injectCanvasEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    const canvasWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/canvas/'));
    if (canvasWs) {
      canvasWs.injectEvent(p);
    }
  }, payload);
}

async function dispatchTouchTap(page: Page, x: number, y: number) {
  await page.evaluate(({ x, y }) => {
    if (typeof Touch === 'undefined') return;
    const target = document.elementFromPoint(x, y) || document.body;
    const touchInit = { clientX: x, clientY: y, pageX: x, pageY: y, identifier: 0, target };
    const touch = new Touch(touchInit);
    target.dispatchEvent(new TouchEvent('touchstart', { touches: [touch], changedTouches: [touch], bubbles: true }));
    target.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [touch], bubbles: true, cancelable: true }));
  }, { x, y });
}

async function dispatchTouchSwipe(page: Page, startX: number, startY: number, endX: number, endY: number) {
  await page.evaluate(({ startX, startY, endX, endY }) => {
    if (typeof Touch === 'undefined') return;
    const target = document.elementFromPoint(startX, startY) || document.body;
    const start = new Touch({
      clientX: startX,
      clientY: startY,
      pageX: startX,
      pageY: startY,
      identifier: 0,
      target,
    });
    const end = new Touch({
      clientX: endX,
      clientY: endY,
      pageX: endX,
      pageY: endY,
      identifier: 0,
      target,
    });
    target.dispatchEvent(new TouchEvent('touchstart', { touches: [start], changedTouches: [start], bubbles: true }));
    target.dispatchEvent(new TouchEvent('touchmove', { touches: [end], changedTouches: [end], bubbles: true, cancelable: true }));
    target.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [end], bubbles: true, cancelable: true }));
  }, { startX, startY, endX, endY });
}

test.describe('canvas - tabula rasa', () => {
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
    // Type a character - should auto-open floating-input
    await page.keyboard.type('h');
    await page.waitForTimeout(100);

    const floatingInput = page.locator('#floating-input');
    await expect(floatingInput).toBeVisible();
    await expect(floatingInput).toHaveValue('h');

    // Type more and send
    await page.keyboard.type('ello');
    await expect(floatingInput).toHaveValue('hello');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    // Text input should be hidden after send
    await expect(floatingInput).toBeHidden();

    // Check message was sent
    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('hello');
  });

  test('click starts recording, stop indicator stays visible after stop', async ({ page }) => {
    await clearLog(page);

    // Click on canvas area
    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);

    // Recording indicator should be visible
    const indicator = page.locator('#indicator');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.record-dot')).toBeVisible();
    await expect(page.locator('.stop-square')).toBeHidden();

    // Wait for recorder to start
    await waitForLogEntry(page, 'recorder', 'start');

    // Click again to stop recording and start transcription
    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'stt', 'stop');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.stop-square')).toBeVisible();
    await expect(page.locator('.record-dot')).toBeHidden();
  });

  test('right-click opens text input at position', async ({ page }) => {
    await page.mouse.click(300, 300, { button: 'right' });
    await page.waitForTimeout(100);

    const floatingInput = page.locator('#floating-input');
    await expect(floatingInput).toBeVisible();
    await expect(floatingInput).toBeFocused();
  });

  test('Escape dismisses text input', async ({ page }) => {
    await page.mouse.click(300, 300, { button: 'right' });
    await page.waitForTimeout(100);
    await expect(page.locator('#floating-input')).toBeVisible();

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);
    await expect(page.locator('#floating-input')).toBeHidden();
  });
});

test.describe('canvas - response overlay', () => {
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
    const overlay = page.locator('#overlay');
    await expect(overlay).toBeVisible();

    // Inject assistant message
    await injectChatEvent(page, { type: 'assistant_message', turn_id: 'turn-1', message: 'Hello back!' });
    await page.waitForTimeout(100);

    const content = page.locator('.overlay-content');
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

  test('item progress stays in the active assistant row through final output', async ({ page }) => {
    await page.keyboard.type('run checks');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(120);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'turn-progress-1' });
    await page.waitForTimeout(80);

    await injectChatEvent(page, {
      type: 'item_completed',
      turn_id: 'turn-progress-1',
      item_type: 'exec_command',
      detail: 'go test ./internal/web -run TestStop',
    });
    await injectChatEvent(page, {
      type: 'item_completed',
      turn_id: 'turn-progress-1',
      item_type: 'reasoning',
      detail: 'Validating stop handling and cancellation paths',
    });
    await page.waitForTimeout(80);

    const assistantRow = page.locator('#chat-history .chat-message.chat-assistant').first();
    await expect(assistantRow).toContainText('exec command');
    await expect(assistantRow).toContainText('go test ./internal/web -run TestStop');
    await expect(assistantRow).toContainText('reasoning');

    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'turn-progress-1',
      message: 'Stop flow is now stable.',
      auto_canvas: false,
    });
    await page.waitForTimeout(100);

    await expect(assistantRow).toContainText('Stop flow is now stable.');
    await expect(assistantRow).toContainText('exec command');
    await expect(page.locator('#chat-history .chat-message.chat-system')).toHaveCount(0);
    await expect(page.locator('#chat-history .chat-message.chat-assistant.is-pending')).toHaveCount(0);
  });

  test('empty canvas switches from text overlay to symbol on first artifact event', async ({ page }) => {
    await page.evaluate(() => {
      const uiMod = (window as any).__uiModule;
      if (uiMod?.getUiState) {
        const zs = uiMod.getUiState();
        zs.lastInputX = 400;
        zs.lastInputY = 300;
      }
    });

    await page.keyboard.type('draw');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'draw-1' });
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeVisible();
    await expect(page.locator('#indicator')).toBeVisible();

    // No event_id on purpose: empty->drawn transition must still flip to artifact symbol mode.
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      title: 'drawn.txt',
      text: 'Drawn content',
    });
    await page.waitForTimeout(120);

    await expect(page.locator('#canvas-text')).toBeVisible();
    await expect(page.locator('#canvas-text')).toContainText('Drawn content');
    await expect(page.locator('#overlay')).toBeHidden();
    await expect(page.locator('#indicator')).toBeHidden();

    const hasArtifact = await page.evaluate(() => Boolean((window as any)._taburaApp?.getState?.().hasArtifact));
    expect(hasArtifact).toBe(true);
  });

  test('error shows in overlay and auto-dismisses', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'turn-err' });
    await page.waitForTimeout(50);
    await injectChatEvent(page, { type: 'error', turn_id: 'turn-err', error: 'something broke' });
    await page.waitForTimeout(100);

    const overlay = page.locator('#overlay');
    await expect(overlay).toBeVisible();
    const content = await page.locator('.overlay-content').textContent();
    expect(content).toContain('something broke');

    // Auto-dismisses after ~2s
    await page.waitForTimeout(2200);
    await expect(overlay).toBeHidden();
  });
});

test.describe('canvas - artifact mode', () => {
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

test.describe('canvas - TTS voice output', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  async function setVoiceOrigin(page: Page) {
    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      if (app?.getState) app.getState().lastInputOrigin = 'voice';
      // Set a valid position so indicators are visible in viewport
      const uiMod = (window as any).__uiModule;
      if (uiMod?.getUiState) {
        const zs = uiMod.getUiState();
        zs.lastInputX = 400;
        zs.lastInputY = 300;
      }
    });
  }

  test('voice turn shows stop indicator, no overlay', async ({ page }) => {
    await clearLog(page);
    await setVoiceOrigin(page);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'tts-dots' });
    await page.waitForTimeout(100);

    // Stop indicator visible, overlay hidden
    const indicator = page.locator('#indicator');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.stop-square')).toBeVisible();

    const overlay = page.locator('#overlay');
    await expect(overlay).toBeHidden();
  });

  test('text turn shows overlay with Thinking and stop indicator', async ({ page }) => {
    await clearLog(page);
    // Default is text origin; send via keyboard to confirm
    await page.keyboard.type('hello');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'text-t1' });
    await page.waitForTimeout(100);

    const overlay = page.locator('#overlay');
    await expect(overlay).toBeVisible();
    const content = await page.locator('.overlay-content').textContent();
    expect(content).toContain('Thinking');

    // Indicator stays visible while work is active
    const indicator = page.locator('#indicator');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.stop-square')).toBeVisible();
  });

  test('voice response triggers TTS, no overlay', async ({ page }) => {
    await clearLog(page);
    await setVoiceOrigin(page);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'tts-1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-1',
      message: 'Hello, how can I help you today?',
    });
    await page.waitForTimeout(500);

    await injectChatEvent(page, {
      type: 'message_persisted',
      role: 'assistant',
      turn_id: 'tts-1',
      message: 'Hello, how can I help you today?',
    });
    await page.waitForTimeout(500);

    // TTS fetch should have been called
    const log = await getLog(page);
    const ttsCalls = log.filter(e => e.type === 'tts');
    expect(ttsCalls.length).toBeGreaterThan(0);
    expect(ttsCalls[0].text).toBeTruthy();

    // Overlay should NOT be visible for voice turns
    const overlay = page.locator('#overlay');
    await expect(overlay).toBeHidden();
  });

  test('first voice response speaks immediately, auto_canvas does not add extra TTS', async ({ page }) => {
    await clearLog(page);
    await setVoiceOrigin(page);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'tts-canvas-keep' });
    await page.waitForTimeout(80);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-canvas-keep',
      message: 'I will open readme and place it there',
      delta: 'I will open readme and place it there',
    });
    await page.waitForTimeout(500);

    // First response should be spoken immediately
    let log = await getLog(page);
    let spoken = log.filter((e) => e.type === 'tts').map((e) => String(e.text || '').toLowerCase());
    expect(spoken.some((t) => t.includes('open readme') || t.includes('place it there'))).toBe(true);

    await clearLog(page);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-canvas-keep',
      auto_canvas: true,
      message: '',
      delta: '',
    });
    await expect(page.locator('#indicator')).toBeHidden();
    await page.waitForTimeout(250);

    // auto_canvas empty message should not add extra TTS
    log = await getLog(page);
    spoken = log.filter((e) => e.type === 'tts');
    expect(spoken.length).toBe(0);
  });

  test('voice TTS speaks first response immediately and final supersedes', async ({ page }) => {
    await clearLog(page);
    await setVoiceOrigin(page);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'tts-rewrite' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-rewrite',
      message: 'I have a cleaned tree snapshot and will share it as canvas content.',
      delta: 'I have a cleaned tree snapshot and will share it as canvas content.',
    });
    await page.waitForTimeout(500);

    // First response should be spoken immediately
    let log = await getLog(page);
    let spoken = log.filter((e) => e.type === 'tts').map((e) => String(e.text || ''));
    expect(spoken.some((t) => t.includes('cleaned tree snapshot'))).toBe(true);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-rewrite',
      message: 'Here is the current repository snapshot so the structure stays readable.',
      delta: 'Here is the current repository snapshot so the structure stays readable.',
    });
    await page.waitForTimeout(250);

    await injectChatEvent(page, {
      type: 'message_persisted',
      role: 'assistant',
      turn_id: 'tts-rewrite',
      message: 'Here is the current repository snapshot so the structure stays readable.',
    });
    await page.waitForTimeout(500);

    log = await getLog(page);
    spoken = log.filter((e) => e.type === 'tts').map((e) => String(e.text || ''));
    // Final output should also be spoken
    expect(spoken.some((t) => t.includes('current repository snapshot'))).toBe(true);
  });

  test('text turn does not trigger TTS and still shows overlay', async ({ page }) => {
    await clearLog(page);
    // Text origin (default)
    await page.keyboard.type('analyze');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'tts-text' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-text',
      message: 'Here is the analysis with some visual content.',
    });
    await page.waitForTimeout(500);

    // Overlay shows markdown
    const overlay = page.locator('#overlay');
    await expect(overlay).toBeVisible();

    await injectChatEvent(page, {
      type: 'message_persisted',
      role: 'assistant',
      turn_id: 'tts-text',
      message: 'Here is the analysis with some visual content.',
    });
    await page.waitForTimeout(500);

    // Text-entry turns should not be spoken.
    const log = await getLog(page);
    const ttsCalls = log.filter(e => e.type === 'tts');
    expect(ttsCalls.length).toBe(0);
  });

  test('lang tag sends lang=de for German text', async ({ page }) => {
    await clearLog(page);
    await setVoiceOrigin(page);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'tts-de' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-de',
      message: '[lang:de] Hallo, ich bin Tabura und kann dir helfen.',
    });
    await page.waitForTimeout(300);

    await injectChatEvent(page, {
      type: 'message_persisted',
      role: 'assistant',
      turn_id: 'tts-de',
      message: '[lang:de] Hallo, ich bin Tabura und kann dir helfen.',
    });
    await page.waitForTimeout(500);

    const log = await getLog(page);
    const ttsCalls = log.filter(e => e.type === 'tts');
    expect(ttsCalls.length).toBeGreaterThan(0);
    const deCalls = ttsCalls.filter(e => e.lang === 'de');
    expect(deCalls.length).toBeGreaterThan(0);
  });
});

test.describe('canvas - edge panels', () => {
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

  test('right edge opens chat panel with input visible', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });

    const edgeRight = page.locator('#edge-right');
    const initialClasses = await edgeRight.getAttribute('class');
    expect(initialClasses).not.toContain('edge-pinned');

    // Pin the panel via the right-edge tap button (dispatch click directly
    // because the hover-triggered panel overlaps the button at z-index 200)
    await page.evaluate(() => {
      document.getElementById('edge-right-tap')?.click();
    });
    await page.waitForTimeout(200);

    // Panel should be pinned
    await expect(edgeRight).toHaveClass(/edge-pinned/);
    // Chat pane input should be visible inside the panel
    const cpInput = page.locator('#chat-pane-input');
    await expect(cpInput).toBeVisible();
  });

  test('touch tap on right edge opens chat panel without recording', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await clearLog(page);

    const edgeRight = page.locator('#edge-right');
    const initialClasses = await edgeRight.getAttribute('class');
    expect(initialClasses).not.toContain('edge-pinned');

    // Dispatch synthetic touch events at right edge (x=372, well inside 30px zone)
    await page.evaluate(() => {
      const x = window.innerWidth - 3;
      const y = Math.floor(window.innerHeight / 2);
      const target = document.elementFromPoint(x, y) || document.body;
      const touchInit = { clientX: x, clientY: y, pageX: x, pageY: y, identifier: 0, target };
      const touch = new Touch(touchInit);
      target.dispatchEvent(new TouchEvent('touchstart', { touches: [touch], changedTouches: [touch], bubbles: true }));
      target.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [touch], bubbles: true, cancelable: true }));
    });
    await page.waitForTimeout(300);

    // Panel should be pinned
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    // No recording should have started
    const log = await getLog(page);
    const sttStart = log.find(e => e.type === 'stt' && e.action === 'start');
    expect(sttStart).toBeFalsy();
  });

  test('touch tap inside pinned chat panel does not cancel default focus flow', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    const edgeRight = page.locator('#edge-right');
    await dispatchTouchTap(page, 372, 333);
    await page.waitForTimeout(200);
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    const result = await page.evaluate(() => {
      const input = document.getElementById('chat-pane-input');
      if (!(input instanceof HTMLTextAreaElement)) return null;
      if (typeof Touch === 'undefined') return null;
      const rect = input.getBoundingClientRect();
      const x = Math.floor(rect.left + Math.max(8, Math.min(40, rect.width / 3)));
      const y = Math.floor(rect.top + Math.max(8, Math.min(24, rect.height / 2)));
      const target = document.elementFromPoint(x, y) || input;
      const touch = new Touch({
        clientX: x,
        clientY: y,
        pageX: x,
        pageY: y,
        identifier: 1,
        target,
      });
      target.dispatchEvent(new TouchEvent('touchstart', {
        touches: [touch],
        changedTouches: [touch],
        bubbles: true,
        cancelable: true,
      }));
      const touchEndAllowed = target.dispatchEvent(new TouchEvent('touchend', {
        touches: [],
        changedTouches: [touch],
        bubbles: true,
        cancelable: true,
      }));
      return { touchEndAllowed };
    });

    expect(result).not.toBeNull();
    expect(result?.touchEndAllowed).toBe(true);
  });

  test('touch swipe right hides pinned chat panel', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    const edgeRight = page.locator('#edge-right');
    await dispatchTouchTap(page, 372, 333);
    await page.waitForTimeout(200);
    await expect(edgeRight).toHaveClass(/edge-pinned/);

    await dispatchTouchSwipe(page, 90, 333, 230, 333);
    await page.waitForTimeout(150);

    const classes = await edgeRight.getAttribute('class');
    expect(classes).not.toContain('edge-pinned');
    expect(classes).not.toContain('edge-active');
  });

  test('touch swipe up hides pinned top panel', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    const edgeTop = page.locator('#edge-top');
    await dispatchTouchTap(page, 187, 3);
    await page.waitForTimeout(200);
    await expect(edgeTop).toHaveClass(/edge-pinned/);

    await dispatchTouchSwipe(page, 187, 140, 187, 24);
    await page.waitForTimeout(150);

    const classes = await edgeTop.getAttribute('class');
    expect(classes).not.toContain('edge-pinned');
    expect(classes).not.toContain('edge-active');
  });

  test('touch swipe left hides file sidebar drawer', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    const pane = page.locator('#pr-file-pane');
    await dispatchTouchTap(page, 3, 333);
    await page.waitForTimeout(200);
    await expect(pane).toHaveClass(/is-open/);

    await dispatchTouchSwipe(page, 220, 333, 80, 333);
    await page.waitForTimeout(150);

    const paneClasses = await pane.getAttribute('class');
    expect(paneClasses).not.toContain('is-open');
    const bodyClass = await page.locator('body').getAttribute('class');
    expect(bodyClass || '').not.toContain('file-sidebar-open');
  });
});
