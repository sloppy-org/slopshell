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
    const zen = await import('../../internal/web/static/zen.js');
    (window as any).__zenModule = zen;
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

  test('click starts recording, stop indicator stays visible after stop', async ({ page }) => {
    await clearLog(page);

    // Click on canvas area
    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);

    // Recording indicator should be visible
    const indicator = page.locator('#zen-indicator');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.zen-record-dot')).toBeVisible();
    await expect(page.locator('.zen-play-icon')).toBeHidden();

    // Wait for recorder to start
    await waitForLogEntry(page, 'recorder', 'start');

    // Click again to stop recording and start transcription
    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'stt', 'stop');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.zen-play-icon')).toBeVisible();
    await expect(page.locator('.zen-record-dot')).toBeHidden();
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

  test('empty canvas switches from text overlay to symbol on first artifact event', async ({ page }) => {
    await page.evaluate(() => {
      const zenMod = (window as any).__zenModule;
      if (zenMod?.getZenState) {
        const zs = zenMod.getZenState();
        zs.lastInputX = 400;
        zs.lastInputY = 300;
      }
    });

    await page.keyboard.type('draw');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'draw-1' });
    await page.waitForTimeout(100);
    await expect(page.locator('#zen-overlay')).toBeVisible();
    await expect(page.locator('#zen-indicator')).toBeVisible();

    // No event_id on purpose: empty->drawn transition must still flip to artifact symbol mode.
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      title: 'drawn.txt',
      text: 'Drawn content',
    });
    await page.waitForTimeout(120);

    await expect(page.locator('#canvas-text')).toBeVisible();
    await expect(page.locator('#canvas-text')).toContainText('Drawn content');
    await expect(page.locator('#zen-overlay')).toBeHidden();
    await expect(page.locator('#zen-indicator')).toBeHidden();

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

test.describe('zen canvas - TTS voice output', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  async function setVoiceOrigin(page: Page) {
    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      if (app?.getState) app.getState().lastInputOrigin = 'voice';
      // Set a valid position so indicators are visible in viewport
      const zenMod = (window as any).__zenModule;
      if (zenMod?.getZenState) {
        const zs = zenMod.getZenState();
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
    const indicator = page.locator('#zen-indicator');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.zen-play-icon')).toBeVisible();

    const overlay = page.locator('#zen-overlay');
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

    const overlay = page.locator('#zen-overlay');
    await expect(overlay).toBeVisible();
    const content = await page.locator('.zen-overlay-content').textContent();
    expect(content).toContain('Thinking');

    // Indicator stays visible while work is active
    const indicator = page.locator('#zen-indicator');
    await expect(indicator).toBeVisible();
    await expect(page.locator('.zen-play-icon')).toBeVisible();
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
    const overlay = page.locator('#zen-overlay');
    await expect(overlay).toBeHidden();
  });

  test('auto canvas stream events do not trigger TTS before final output', async ({ page }) => {
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
    await page.waitForTimeout(80);

    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'tts-canvas-keep',
      auto_canvas: true,
      message: '',
      delta: '',
    });
    await expect(page.locator('#zen-indicator')).toBeHidden();
    await page.waitForTimeout(250);

    let log = await getLog(page);
    let spoken = log.filter((e) => e.type === 'tts').map((e) => String(e.text || '').toLowerCase());
    expect(spoken.length).toBe(0);

    await injectChatEvent(page, {
      type: 'message_persisted',
      role: 'assistant',
      turn_id: 'tts-canvas-keep',
      message: 'I will open readme and place it there',
    });
    await page.waitForTimeout(450);

    log = await getLog(page);
    spoken = log.filter((e) => e.type === 'tts').map((e) => String(e.text || '').toLowerCase());
    expect(spoken.some((t) => t.includes('open readme') || t.includes('place it there'))).toBe(true);
  });

  test('voice TTS speaks only the finalized snapshot', async ({ page }) => {
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
    await page.waitForTimeout(250);

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

    const log = await getLog(page);
    const spoken = log
      .filter((e) => e.type === 'tts')
      .map((e) => String(e.text || ''));
    expect(spoken.some((t) => t.includes('cleaned tree snapshot'))).toBe(false);
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
    const overlay = page.locator('#zen-overlay');
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

  test('bottom tap opens floating input without opening chat panel', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });

    const edgeRight = page.locator('#edge-right');
    const initialClasses = await edgeRight.getAttribute('class');
    expect(initialClasses).not.toContain('edge-pinned');

    // Click the bottom tap area
    await page.click('#edge-bottom-tap');
    await page.waitForTimeout(200);

    // Panel should NOT open in canvas/voice mode
    await expect(edgeRight).not.toHaveClass(/edge-pinned/);
    // Floating input bar should be visible and focused
    const bar = page.locator('#chat-bottom-bar');
    await expect(bar).toHaveClass(/is-active/);
    const cpInput = page.locator('#chat-pane-input');
    await expect(cpInput).toBeFocused();
  });
});
