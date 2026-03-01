import { expect, test, type Page } from '@playwright/test';
import { execSync } from 'child_process';

type HarnessLogEntry = { type: string; action?: string; text?: string; [key: string]: unknown };

// Fetch a WAV from Piper TTS, decode PCM, compute per-frame dB levels for VAD mock.
// Returns null if Piper is not running.
function piperVadFrames(text: string, frameMs = 40): number[] | null {
  let wav: Buffer;
  try {
    wav = execSync(
      `curl -fsS --max-time 3 -X POST http://127.0.0.1:8424/v1/audio/speech ` +
      `-H 'Content-Type: application/json' -d '${JSON.stringify({ input: text })}'`,
      { stdio: ['ignore', 'pipe', 'ignore'] },
    );
  } catch {
    return null;
  }
  if (wav.length < 44) return null;
  const sampleRate = wav.readUInt32LE(24);
  const bitsPerSample = wav.readUInt16LE(34);
  const bytesPerSample = bitsPerSample / 8;
  const dataOffset = 44;
  const samplesPerFrame = Math.floor((sampleRate * frameMs) / 1000);
  const frames: number[] = [];
  let pos = dataOffset;
  while (pos + samplesPerFrame * bytesPerSample <= wav.length) {
    let sumSq = 0;
    for (let i = 0; i < samplesPerFrame; i++) {
      const sample = bytesPerSample === 2
        ? wav.readInt16LE(pos + i * bytesPerSample) / 32768
        : (wav[pos + i * bytesPerSample]! - 128) / 128;
      sumSq += sample * sample;
    }
    const rms = Math.sqrt(sumSq / samplesPerFrame);
    const db = rms > 0 ? 20 * Math.log10(rms) : -96;
    frames.push(Math.round(db * 10) / 10);
    pos += samplesPerFrame * bytesPerSample;
  }
  return frames;
}

let _cachedPiperFrames: number[] | null | undefined;
function getPiperVadFrames(): number[] | null {
  if (_cachedPiperFrames === undefined) {
    const raw = piperVadFrames('Hello, this is a test of voice recording.');
    if (raw) {
      // Use peak speech dB from Piper to create frames that VAD will always
      // classify as active speech (well above any noise floor + offset).
      const peakDb = Math.max(...raw);
      const target = Math.ceil(5000 / 40);
      _cachedPiperFrames = Array.from({ length: target }, () => peakDb);
    } else {
      _cachedPiperFrames = null;
    }
  }
  return _cachedPiperFrames;
}

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

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/chat/'));
    if (chatWs) chatWs.injectEvent(p);
  }, payload);
}

async function injectCanvasEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    const canvasWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/canvas/'));
    if (canvasWs) canvasWs.injectEvent(p);
  }, payload);
}

async function waitForLogEntry(page: Page, type: string, action?: string) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(e => e.type === type && (!action || e.action === action));
  }, { timeout: 5_000 }).toBe(true);
}

async function setVoiceOrigin(page: Page) {
  await page.evaluate(() => {
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().lastInputOrigin = 'voice';
    const uiMod = (window as any).__uiModule;
    if (uiMod?.getUiState) {
      const zs = uiMod.getUiState();
      zs.lastInputX = 400;
      zs.lastInputY = 300;
    }
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
    const ct = document.getElementById('canvas-text');
    if (ct) { ct.style.display = ''; ct.classList.add('is-active'); }
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  }, text);
}

async function dispatchTouchTap(page: Page, x: number, y: number) {
  await page.evaluate(({ x, y }) => {
    if (typeof Touch === 'undefined') return;
    const target = document.elementFromPoint(x, y) || document.body;
    const touchInit = { clientX: x, clientY: y, pageX: x, pageY: y, identifier: 0, target };
    const touch = new Touch(touchInit);
    target.dispatchEvent(new TouchEvent('touchstart', { touches: [touch], changedTouches: [touch], bubbles: true }));
    target.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [touch], bubbles: true, cancelable: true }));
    // Simulate the delayed click that iOS fires ~300ms after touchend.
    // The app must suppress this via touchTapSuppressClick to prevent double-action.
    setTimeout(() => {
      target.dispatchEvent(new MouseEvent('click', { clientX: x, clientY: y, bubbles: true, button: 0 }));
    }, 300);
  }, { x, y });
}

async function dispatchTouchLongPress(page: Page, x: number, y: number, holdMs = 560) {
  await page.evaluate(async ({ x, y, holdMs }) => {
    if (typeof Touch === 'undefined') return;
    const target = document.elementFromPoint(x, y) || document.body;
    const touchInit = { clientX: x, clientY: y, pageX: x, pageY: y, identifier: 0, target };
    const touch = new Touch(touchInit);
    target.dispatchEvent(new TouchEvent('touchstart', { touches: [touch], changedTouches: [touch], bubbles: true }));
    await new Promise((resolve) => setTimeout(resolve, holdMs));
    const endTouch = new Touch(touchInit);
    target.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [endTouch], bubbles: true, cancelable: true }));
  }, { x, y, holdMs });
}


// =============================================================================
// Tabula Rasa button
// =============================================================================

test.describe('tabula rasa button', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('rasa button clears canvas and hides all panes', async ({ page }) => {
    await renderTestArtifact(page);
    await expect(page.locator('#canvas-text')).toBeVisible();

    // Open top edge and click rasa button
    await page.evaluate(() => {
      document.getElementById('edge-top')?.classList.add('edge-pinned');
    });
    await page.waitForTimeout(50);

    await page.evaluate(() => {
      document.getElementById('btn-edge-rasa')?.click();
    });
    await page.waitForTimeout(200);

    // All panes should be hidden
    const activePanes = page.locator('.canvas-pane.is-active');
    await expect(activePanes).toHaveCount(0);

    // Top panel should be unpinned
    const topClasses = await page.locator('#edge-top').getAttribute('class');
    expect(topClasses).not.toContain('edge-pinned');
    expect(topClasses).not.toContain('edge-active');

    // hasArtifact should be false
    const hasArtifact = await page.evaluate(() => (window as any)._taburaApp?.getState?.().hasArtifact);
    expect(hasArtifact).toBe(false);
  });

  test('rasa button resets to blank state from image artifact', async ({ page }) => {
    // Render an image artifact
    await page.evaluate(() => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({
        event_id: 'img-1',
        kind: 'image_artifact',
        title: 'photo.png',
        path: '/api/files/local/photo.png',
      });
      const ci = document.getElementById('canvas-image');
      if (ci) { ci.style.display = ''; ci.classList.add('is-active'); }
      (window as any)._taburaApp.getState().hasArtifact = true;
    });
    await expect(page.locator('#canvas-image')).toBeVisible();

    await page.evaluate(() => {
      document.getElementById('btn-edge-rasa')?.click();
    });
    await page.waitForTimeout(200);

    await expect(page.locator('.canvas-pane.is-active')).toHaveCount(0);
  });
});


// =============================================================================
// Image artifact rendering
// =============================================================================

test.describe('image artifact rendering', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('image_artifact renders in canvas-image pane', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'image_artifact',
      event_id: 'img-2',
      title: 'screenshot.png',
      path: '/api/files/local/screenshot.png',
    });
    await page.waitForTimeout(200);

    const canvasImage = page.locator('#canvas-image');
    await expect(canvasImage).toHaveClass(/is-active/);
    const img = page.locator('#canvas-img');
    const src = await img.getAttribute('src');
    expect(src).toContain('screenshot.png');
  });

  test('switching from text to image artifact hides text pane', async ({ page }) => {
    await renderTestArtifact(page);
    await expect(page.locator('#canvas-text')).toBeVisible();

    await injectCanvasEvent(page, {
      kind: 'image_artifact',
      event_id: 'img-3',
      title: 'pic.jpg',
      path: '/api/files/local/pic.jpg',
    });
    await page.waitForTimeout(200);

    await expect(page.locator('#canvas-image')).toHaveClass(/is-active/);
    await expect(page.locator('#canvas-text')).not.toHaveClass(/is-active/);
  });
});


// =============================================================================
// mode_changed WS event
// =============================================================================

test.describe('mode_changed event', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('mode_changed updates chat-mode-pill to plan', async ({ page }) => {
    const pill = page.locator('#chat-mode-pill');
    await expect(pill).toHaveText('chat');

    await injectChatEvent(page, { type: 'mode_changed', mode: 'plan' });
    await page.waitForTimeout(100);

    await expect(pill).toHaveText('plan');
    await expect(pill).toHaveClass(/review/);
  });

  test('mode_changed back to chat removes review class', async ({ page }) => {
    await injectChatEvent(page, { type: 'mode_changed', mode: 'plan' });
    await page.waitForTimeout(100);
    await expect(page.locator('#chat-mode-pill')).toHaveText('plan');

    await injectChatEvent(page, { type: 'mode_changed', mode: 'chat' });
    await page.waitForTimeout(100);

    const pill = page.locator('#chat-mode-pill');
    await expect(pill).toHaveText('chat');
    const classes = await pill.getAttribute('class');
    expect(classes).not.toContain('review');
  });

  test('mode_changed with message appends system message', async ({ page }) => {
    await injectChatEvent(page, { type: 'mode_changed', mode: 'plan', message: 'Entering plan mode.' });
    await page.waitForTimeout(200);

    const chatHistory = page.locator('#chat-history');
    const text = await chatHistory.textContent();
    expect(text).toContain('Entering plan mode.');
  });
});


// =============================================================================
// action: open_canvas event
// =============================================================================

test.describe('action events', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('open_canvas action shows canvas column', async ({ page }) => {
    await injectChatEvent(page, { type: 'action', action: 'open_canvas' });
    await page.waitForTimeout(200);

    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toHaveClass(/is-active/);
  });
});


// =============================================================================
// Chat pane input: Shift+Enter, Escape, Enter send
// =============================================================================

test.describe('chat pane input interactions', () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await waitReady(page);
    // Pin right panel to get chat pane visible
    await page.evaluate(() => {
      document.getElementById('edge-right-tap')?.click();
    });
    await page.waitForTimeout(200);
  });

  test('Shift+Enter inserts newline instead of sending', async ({ page }) => {
    const cpInput = page.locator('#chat-pane-input');
    await cpInput.focus();
    await cpInput.fill('line1');
    await page.keyboard.press('Shift+Enter');
    await page.keyboard.type('line2');
    await page.waitForTimeout(100);

    const value = await cpInput.inputValue();
    expect(value).toContain('line1');
    expect(value).toContain('line2');

    // Should NOT have sent anything
    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeFalsy();
  });

  test('Escape clears chat pane input and blurs', async ({ page }) => {
    const cpInput = page.locator('#chat-pane-input');
    await cpInput.focus();
    await cpInput.fill('some text');
    await page.waitForTimeout(50);

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);

    await expect(cpInput).toHaveValue('');
    await expect(cpInput).not.toBeFocused();
  });

  test('Enter sends message and clears input', async ({ page }) => {
    const cpInput = page.locator('#chat-pane-input');
    await cpInput.focus();
    await cpInput.fill('test message');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(300);

    await expect(cpInput).toHaveValue('');

    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('test message');
  });

  test('Enter with empty input does not send', async ({ page }) => {
    const cpInput = page.locator('#chat-pane-input');
    await cpInput.focus();
    await cpInput.fill('');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeFalsy();
  });
});


// =============================================================================
// Turn lifecycle: cancelled, queue cleared, error recovery
// =============================================================================

test.describe('turn lifecycle events', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('turn_cancelled shows Stopped in chat', async ({ page }) => {
    await page.keyboard.type('do thing');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'cancel-1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, { type: 'turn_cancelled', turn_id: 'cancel-1' });
    await page.waitForTimeout(200);

    const chatHistory = page.locator('#chat-history');
    const text = await chatHistory.textContent();
    expect(text).toContain('Stopped');
  });

  test('turn_cancelled hides overlay', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'cancel-2' });
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeVisible();

    await injectChatEvent(page, { type: 'turn_cancelled', turn_id: 'cancel-2' });
    await page.waitForTimeout(300);

    await expect(page.locator('#overlay')).toBeHidden();
  });

  test('turn_queue_cleared marks pending rows as stopped', async ({ page }) => {
    // Send two messages to create pending rows
    await page.keyboard.type('first');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'q1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, { type: 'turn_queue_cleared', count: 1 });
    await page.waitForTimeout(200);

    const statusText = await page.locator('#status-text').textContent();
    expect(statusText).toContain('queue cleared');
  });

  test('error event shows in overlay and auto-dismisses', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'err-1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, { type: 'error', turn_id: 'err-1', error: 'backend failed' });
    await page.waitForTimeout(100);

    const overlay = page.locator('#overlay');
    await expect(overlay).toBeVisible();
    await expect(page.locator('.overlay-content')).toContainText('backend failed');

    // Auto-dismisses
    await page.waitForTimeout(2200);
    await expect(overlay).toBeHidden();
  });
});


// =============================================================================
// Conversation mode: multi-turn loop
// =============================================================================

test.describe('conversation mode multi-turn', () => {
  async function waitForEdgeButtons(page: Page) {
    await expect.poll(async () => page.evaluate(() => {
      const conv = document.querySelector('#edge-top-models .edge-conv-btn');
      const silent = document.querySelector('#edge-top-models .edge-silent-btn');
      return Boolean(conv && silent);
    })).toBe(true);
  }

  async function enableConversationMode(page: Page) {
    await waitForEdgeButtons(page);
    await page.evaluate(() => {
      const button = document.querySelector('#edge-top-models .edge-conv-btn');
      if (!(button instanceof HTMLButtonElement)) throw new Error('conversation button not found');
      if (button.getAttribute('aria-pressed') !== 'true') button.click();
    });
    await expect.poll(async () => page.evaluate(() => {
      const button = document.querySelector('#edge-top-models .edge-conv-btn');
      return button instanceof HTMLButtonElement ? button.getAttribute('aria-pressed') : 'false';
    })).toBe('true');
  }

  async function triggerVoiceAssistantTTS(page: Page, turnID: string, text = 'Hello there.') {
    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      const s = app.getState();
      s.lastInputOrigin = 'voice';
      s.voiceAwaitingTurn = true;
    });
    await injectChatEvent(page, { type: 'turn_started', turn_id: turnID });
    await injectChatEvent(page, { type: 'assistant_message', turn_id: turnID, message: text });
    await injectChatEvent(page, { type: 'assistant_output', role: 'assistant', turn_id: turnID, message: text });
  }

  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
    await page.evaluate(() => {
      window.localStorage.removeItem('tabura.conversationMode');
      (window as any).__taburaConversationListenMs = 1200;
    });
  });

  test('TTS playback completion triggers listen window in conversation mode', async ({ page }) => {
    await enableConversationMode(page);
    await clearLog(page);

    await triggerVoiceAssistantTTS(page, 'conv-1');

    // TTS should have been queued
    await expect.poll(async () => {
      const log = await getLog(page);
      return log.some(e => e.type === 'tts');
    }).toBe(true);

    // Wait for mock TTS playback to complete and listen indicator to appear
    await expect.poll(async () => page.evaluate(() => {
      const indicator = document.getElementById('indicator');
      return Boolean(indicator?.classList.contains('is-listening'));
    }), { timeout: 5_000 }).toBe(true);
  });

  test('conversation stall recovery when no TTS queued', async ({ page }) => {
    await enableConversationMode(page);
    await clearLog(page);
    await setVoiceOrigin(page);

    // Assistant output with empty message (no TTS will be queued)
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'conv-stall' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'conv-stall',
      message: '',
      auto_canvas: true,
    });
    await page.waitForTimeout(500);

    // Even with empty message, conversation mode should not stall
    // (onTTSPlaybackComplete should have been called as recovery)
    // Verify no unhandled rejections
    const log = await getLog(page);
    const rejections = log.filter(e => e.type === 'unhandled_rejection');
    expect(rejections.length).toBe(0);
  });
});


// =============================================================================
// Keyboard auto-routing to floating input
// =============================================================================

test.describe('keyboard auto-routing', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('typing on blank canvas opens floating input', async ({ page }) => {
    await page.keyboard.type('a');
    await page.waitForTimeout(100);

    const input = page.locator('#floating-input');
    await expect(input).toBeVisible();
    await expect(input).toHaveValue('a');
  });

  test('typing when chat input focused does not open floating input', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.evaluate(() => {
      document.getElementById('edge-right-tap')?.click();
    });
    await page.waitForTimeout(200);

    const cpInput = page.locator('#chat-pane-input');
    await cpInput.focus();
    await page.keyboard.type('test');
    await page.waitForTimeout(100);

    // Floating input should NOT be visible since chat pane input is focused
    const floating = page.locator('#floating-input');
    await expect(floating).toBeHidden();
    await expect(cpInput).toHaveValue('test');
  });
});


// =============================================================================
// Project active state persistence
// =============================================================================

test.describe('project state persistence', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('silent mode persists in localStorage', async ({ page }) => {
    // Toggle silent mode via system_action
    await injectChatEvent(page, {
      type: 'system_action',
      action: { type: 'toggle_silent' },
    });
    await page.waitForTimeout(200);

    const silentState = await page.evaluate(() => {
      return (window as any)._taburaApp?.getState?.().ttsSilent;
    });
    expect(silentState).toBe(true);

    // localStorage should persist
    const stored = await page.evaluate(() => {
      return localStorage.getItem('tabura.ttsSilent');
    });
    expect(stored).toBe('true');
  });

  test('conversation mode persists in localStorage', async ({ page }) => {
    await injectChatEvent(page, {
      type: 'system_action',
      action: { type: 'toggle_conversation' },
    });
    await page.waitForTimeout(200);

    const stored = await page.evaluate(() => {
      return localStorage.getItem('tabura.conversationMode');
    });
    expect(stored).toBe('true');
  });
});


// =============================================================================
// System action: switch_model updates UI without extra API call
// =============================================================================

test.describe('system_action model and project switching', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('switch_model updates model button and effort select', async ({ page }) => {
    await clearLog(page);

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'switch_model',
        project_id: 'test',
        alias: 'gpt',
        effort: 'high',
      },
    });
    await page.waitForTimeout(300);

    // Model should be updated in state
    const model = await page.evaluate(() => {
      const s = (window as any)._taburaApp?.getState?.();
      const p = s.projects?.find((p: any) => p.id === 'test');
      return { alias: p?.chat_model, effort: p?.chat_model_reasoning_effort };
    });
    expect(model.alias).toBe('gpt');
    expect(model.effort).toBe('high');

    // No extra API call for chat-model
    const log = await getLog(page);
    const modelApiCalls = log.filter(e => e.type === 'api_fetch' && e.action === 'project_chat_model');
    expect(modelApiCalls.length).toBe(0);
  });

  test('switch_project triggers project activate API', async ({ page }) => {
    await clearLog(page);

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'switch_project',
        project_id: 'hub',
      },
    });
    await page.waitForTimeout(500);

    const log = await getLog(page);
    const activateCalls = log.filter(e => e.type === 'api_fetch' && e.action === 'project_activate');
    expect(activateCalls.length).toBeGreaterThan(0);
  });
});


// =============================================================================
// Mic stream caching and invalidation
// =============================================================================

test.describe('mic stream management', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('devicechange invalidates cached mic stream', async ({ page }) => {
    await clearLog(page);

    // Acquire stream to cache it
    await page.evaluate(async () => {
      await (window as any)._taburaApp.acquireMicStream();
    });
    await clearLog(page);

    // Trigger device change
    await page.evaluate(() => { (window as any).__triggerMicDeviceChange(); });
    await page.waitForTimeout(100);

    // Re-acquire should call getUserMedia again
    await page.evaluate(async () => {
      await (window as any)._taburaApp.acquireMicStream();
    });

    const log = await getLog(page);
    const mediaCalls = log.filter(e => e.type === 'media' && e.action === 'get_user_media');
    expect(mediaCalls.length).toBe(1);
  });

  test('track ended invalidates cached mic stream', async ({ page }) => {
    await clearLog(page);

    // Acquire stream to cache it
    await page.evaluate(async () => {
      await (window as any)._taburaApp.acquireMicStream();
    });
    await clearLog(page);

    // End the mic track
    await page.evaluate(() => { (window as any).__triggerMicTrackEnded(); });
    await page.waitForTimeout(100);

    // Re-acquire should call getUserMedia again
    await page.evaluate(async () => {
      await (window as any)._taburaApp.acquireMicStream();
    });

    const log = await getLog(page);
    const mediaCalls = log.filter(e => e.type === 'media' && e.action === 'get_user_media');
    expect(mediaCalls.length).toBe(1);
  });
});


// =============================================================================
// Escape key: context-dependent behavior
// =============================================================================

test.describe('escape key behavior', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('Escape dismisses floating input first', async ({ page }) => {
    await page.mouse.click(300, 300, { button: 'right' });
    await page.waitForTimeout(100);
    await expect(page.locator('#floating-input')).toBeVisible();

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);
    await expect(page.locator('#floating-input')).toBeHidden();
  });

  test('Escape dismisses overlay', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'esc-overlay' });
    await page.waitForTimeout(100);
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'esc-overlay',
      message: 'Done.',
      auto_canvas: false,
    });
    await page.waitForTimeout(200);
    await expect(page.locator('#overlay')).toBeVisible();

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeHidden();
  });

  test('Escape clears artifact when nothing else is open', async ({ page }) => {
    await renderTestArtifact(page);
    await expect(page.locator('#canvas-text')).toBeVisible();

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);

    await expect(page.locator('.canvas-pane.is-active')).toHaveCount(0);
    const hasArtifact = await page.evaluate(() => (window as any)._taburaApp?.getState?.().hasArtifact);
    expect(hasArtifact).toBe(false);
  });

  test('Escape unpins edge panel', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.evaluate(() => {
      document.getElementById('edge-right')?.classList.add('edge-pinned');
    });
    await page.waitForTimeout(50);
    await expect(page.locator('#edge-right')).toHaveClass(/edge-pinned/);

    await page.keyboard.press('Escape');
    await page.waitForTimeout(100);
    const classes = await page.locator('#edge-right').getAttribute('class');
    expect(classes).not.toContain('edge-pinned');
  });
});


// =============================================================================
// Mobile viewport tests
// =============================================================================

test.describe('mobile viewport', () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('canvas fills mobile viewport', async ({ page }) => {
    const canvasCol = page.locator('#canvas-column');
    await expect(canvasCol).toBeVisible();
    const box = await canvasCol.boundingBox();
    expect(box).toBeTruthy();
    expect(box!.width).toBeGreaterThan(350);
  });

  test('touch tap on canvas starts recording on mobile', async ({ page }) => {
    await clearLog(page);

    await dispatchTouchTap(page, 187, 333);
    await page.waitForTimeout(500);

    await waitForLogEntry(page, 'recorder', 'start');
    const indicator = page.locator('#indicator');
    await expect(indicator).toBeVisible();
  });

  test('touch tap recording stays active despite delayed click', async ({ page }) => {
    const frames = getPiperVadFrames();
    test.skip(!frames, 'Piper TTS not running on :8424');
    await clearLog(page);
    await page.evaluate((f) => { (window as any).__setVadDbFrames(f); }, frames!);

    await dispatchTouchTap(page, 187, 333);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');

    // Wait past the 300ms delayed click — recording must survive it
    await page.waitForTimeout(1000);
    const isStillRecording = await page.evaluate(() => {
      return Boolean((window as any)._taburaApp?.getState?.().chatVoiceCapture?.active);
    });
    expect(isStillRecording).toBe(true);

    const log = await getLog(page);
    const sttStops = log.filter(e => e.type === 'stt' && e.action === 'stop');
    expect(sttStops.length).toBe(0);
  });

  test('touch tap on artifact does not stop recording via delayed click', async ({ page }) => {
    const frames = getPiperVadFrames();
    test.skip(!frames, 'Piper TTS not running on :8424');
    await renderTestArtifact(page);
    await clearLog(page);
    await page.evaluate((f) => { (window as any).__setVadDbFrames(f); }, frames!);

    await dispatchTouchTap(page, 187, 333);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');

    await page.waitForTimeout(500);
    const isStillRecording = await page.evaluate(() => {
      return Boolean((window as any)._taburaApp?.getState?.().chatVoiceCapture?.active);
    });
    expect(isStillRecording).toBe(true);
  });

  test('touch tap start then tap stop sends message and gets chat response', async ({ page }) => {
    await clearLog(page);

    // Tap to start recording
    await dispatchTouchTap(page, 187, 333);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');

    // Tap again to stop (second touch tap)
    await dispatchTouchTap(page, 187, 333);
    await waitForLogEntry(page, 'stt', 'stop');
    await waitForLogEntry(page, 'message_sent');

    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('hello world');

    // Inject assistant response to verify it appears in chat
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'touch-turn-1' });
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'touch-turn-1',
      message: 'Response to your voice message.',
      auto_canvas: false,
    });
    await page.waitForTimeout(300);

    const chatHistory = page.locator('#chat-history');
    const chatText = await chatHistory.textContent();
    expect(chatText).toContain('hello world');
    expect(chatText).toContain('Response to your voice message');
  });

  test('touch tap start then tap stop with TTS response plays audio', async ({ page }) => {
    await clearLog(page);

    // Tap to start
    await dispatchTouchTap(page, 187, 333);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');

    // Tap to stop
    await dispatchTouchTap(page, 187, 333);
    await waitForLogEntry(page, 'stt', 'stop');
    await waitForLogEntry(page, 'message_sent');

    // Assistant response with voice origin triggers TTS
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'touch-tts-1' });
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'touch-tts-1',
      message: 'Here is your answer.',
      auto_canvas: false,
    });
    await page.waitForTimeout(500);

    const log = await getLog(page);
    const ttsCalls = log.filter(e => e.type === 'tts');
    expect(ttsCalls.length).toBeGreaterThan(0);
    expect(ttsCalls[0]!.text).toContain('Here is your answer');
  });

  test('touch tap stop during working mode cancels turn', async ({ page }) => {
    await clearLog(page);

    // Submit text message first
    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      app.getState().lastInputOrigin = 'text';
    });
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    // Simulate assistant working
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'cancel-turn-1' });
    await page.waitForTimeout(200);

    // Tap should trigger stop/cancel action
    await dispatchTouchTap(page, 187, 333);
    await page.waitForTimeout(500);

    const log = await getLog(page);
    const cancelCalls = log.filter(e => e.type === 'api_fetch' && e.action === 'cancel');
    expect(cancelCalls.length).toBeGreaterThan(0);
  });

  test('artifact renders on mobile and fills viewport', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();
    const text = await canvasText.textContent();
    expect(text).toContain('Line one');
  });

  test('long-press on artifact opens artifact editor and does not start recording', async ({ page }) => {
    await renderTestArtifact(page);
    await clearLog(page);

    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();
    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');

    const x = Math.floor(box.x + box.width * 0.45);
    const y = Math.floor(box.y + box.height * 0.4);
    await dispatchTouchLongPress(page, x, y);
    await page.waitForTimeout(200);

    const artifactEditor = page.locator('#artifact-editor');
    await expect(artifactEditor).toBeVisible();

    const log = await getLog(page);
    const recorderStarts = log.filter(e => e.type === 'recorder' && e.action === 'start');
    expect(recorderStarts.length).toBe(0);

    await page.keyboard.press('Escape');
    await page.waitForTimeout(120);
    await expect(artifactEditor).toBeHidden();
  });

  test('right-click opens floating input on mobile', async ({ page }) => {
    // Simulate contextmenu via evaluate since mobile doesn't have right-click
    await page.evaluate(() => {
      const ev = new MouseEvent('contextmenu', { clientX: 187, clientY: 333, bubbles: true });
      document.elementFromPoint(187, 333)?.dispatchEvent(ev);
    });
    await page.waitForTimeout(100);
    await expect(page.locator('#floating-input')).toBeVisible();
  });
});


// =============================================================================
// Voice lifecycle: STT result -> message submission
// =============================================================================

test.describe('voice-to-message flow', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('voice capture -> STT result -> message sent', async ({ page }) => {
    await clearLog(page);

    // Start recording
    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');

    // Stop recording (triggers STT)
    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'stt', 'stop');

    // Wait for message to be sent (STT returns "hello world" after 5ms)
    await waitForLogEntry(page, 'message_sent');

    const log = await getLog(page);
    const sent = log.find(e => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('hello world');
  });

  test('lastInputOrigin set to voice after voice capture', async ({ page }) => {
    await clearLog(page);

    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');

    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'stt', 'stop');
    await page.waitForTimeout(200);

    const origin = await page.evaluate(() => (window as any)._taburaApp?.getState?.().lastInputOrigin);
    expect(origin).toBe('voice');
  });

  test('lastInputOrigin set to text after text submit', async ({ page }) => {
    await page.keyboard.type('hello');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    const origin = await page.evaluate(() => (window as any)._taburaApp?.getState?.().lastInputOrigin);
    expect(origin).toBe('text');
  });
});


// =============================================================================
// Full assistant turn flow: text input -> turn -> response -> overlay -> dismiss
// =============================================================================

test.describe('full assistant turn flow', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('text input -> turn started -> streaming -> final output -> dismiss', async ({ page }) => {
    // Submit message
    await page.keyboard.type('explain this');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    // Turn started -> overlay with Thinking
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'full-1' });
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeVisible();
    await expect(page.locator('.overlay-content')).toContainText('Thinking');

    // Streaming response
    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'full-1',
      message: 'Here is the explanation.',
      delta: 'Here is the explanation.',
    });
    await page.waitForTimeout(100);
    await expect(page.locator('.overlay-content')).toContainText('explanation');

    // Item completed progress
    await injectChatEvent(page, {
      type: 'item_completed',
      turn_id: 'full-1',
      item_type: 'reasoning',
      detail: 'Analyzed the code structure',
    });
    await page.waitForTimeout(100);

    // Final output
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'full-1',
      message: 'Here is the explanation.',
      auto_canvas: false,
    });
    await page.waitForTimeout(200);

    // Click to dismiss overlay
    await page.mouse.click(10, 10);
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeHidden();

    // Chat history should contain user message and assistant response
    const chatHistory = page.locator('#chat-history');
    const chatText = await chatHistory.textContent();
    expect(chatText).toContain('explain this');
    expect(chatText).toContain('explanation');
  });

  test('voice input -> turn -> TTS -> no overlay shown', async ({ page }) => {
    await clearLog(page);

    // Simulate voice submit
    await page.mouse.click(400, 400);
    await page.waitForTimeout(500);
    await waitForLogEntry(page, 'recorder', 'start');
    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'stt', 'stop');
    await waitForLogEntry(page, 'message_sent');
    await page.waitForTimeout(200);

    // Turn started (voice origin)
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'voice-full-1' });
    await page.waitForTimeout(100);

    // Overlay should NOT appear for voice turns
    await expect(page.locator('#overlay')).toBeHidden();

    // Assistant response triggers TTS
    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'voice-full-1',
      message: 'Sure, I can help with that.',
      auto_canvas: false,
    });
    await page.waitForTimeout(500);

    const log = await getLog(page);
    const ttsCalls = log.filter(e => e.type === 'tts');
    expect(ttsCalls.length).toBeGreaterThan(0);
    await expect(page.locator('#overlay')).toBeHidden();
  });
});


// =============================================================================
// Canvas artifact with response: artifact suppresses overlay
// =============================================================================

test.describe('canvas artifact during turn', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('canvas artifact event hides overlay and indicator during turn', async ({ page }) => {
    await page.keyboard.type('generate');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'art-turn-1' });
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeVisible();

    // Canvas artifact arrives
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'gen-1',
      title: 'generated.txt',
      text: 'Generated content here.',
    });
    await page.waitForTimeout(200);

    // Overlay should be hidden, canvas should show content
    await expect(page.locator('#overlay')).toBeHidden();
    await expect(page.locator('#canvas-text')).toBeVisible();
    await expect(page.locator('#canvas-text')).toContainText('Generated content');
  });
});


// =============================================================================
// No unhandled rejections
// =============================================================================

test.describe('error safety', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('no unhandled rejections during normal operation', async ({ page }) => {
    // Send a message and get a response
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'safe-1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'safe-1',
      message: 'All good.',
      auto_canvas: false,
    });
    await page.waitForTimeout(300);

    const log = await getLog(page);
    const rejections = log.filter(e => e.type === 'unhandled_rejection');
    expect(rejections.length).toBe(0);
  });

  test('no unhandled rejections when turn cancelled mid-stream', async ({ page }) => {
    await page.keyboard.type('generate');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'safe-cancel' });
    await page.waitForTimeout(50);
    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'safe-cancel',
      message: 'Starting to...',
      delta: 'Starting to...',
    });
    await page.waitForTimeout(50);
    await injectChatEvent(page, { type: 'turn_cancelled', turn_id: 'safe-cancel' });
    await page.waitForTimeout(500);

    const log = await getLog(page);
    const rejections = log.filter(e => e.type === 'unhandled_rejection');
    expect(rejections.length).toBe(0);
  });
});


// =============================================================================
// Workspace file sidebar
// =============================================================================

test.describe('workspace file sidebar', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
  });

  test('left edge tap opens file sidebar', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await dispatchTouchTap(page, 3, 333);
    await page.waitForTimeout(200);

    const pane = page.locator('#pr-file-pane');
    await expect(pane).toHaveClass(/is-open/);
  });

  test('file list shows harness fixture entries', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await dispatchTouchTap(page, 3, 333);
    await page.waitForTimeout(300);

    const fileList = page.locator('#pr-file-list');
    const text = await fileList.textContent();
    // Harness returns docs/, NOTES.md, README.md
    expect(text).toContain('docs');
    expect(text).toContain('README.md');
  });
});


// =============================================================================
// Status label updates
// =============================================================================

test.describe('status updates', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('status shows ready after turn completion', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'status-1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, {
      type: 'assistant_output',
      role: 'assistant',
      turn_id: 'status-1',
      message: 'Done.',
      auto_canvas: false,
    });
    await page.waitForTimeout(200);

    const statusText = await page.locator('#status-text').textContent();
    expect(statusText).toContain('ready');
  });

  test('status shows stopped after cancellation', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'status-2' });
    await page.waitForTimeout(100);
    await injectChatEvent(page, { type: 'turn_cancelled', turn_id: 'status-2' });
    await page.waitForTimeout(200);

    const statusText = await page.locator('#status-text').textContent();
    expect(statusText).toContain('stopped');
  });
});
