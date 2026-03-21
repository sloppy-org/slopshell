import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  action?: string;
  text?: string;
  url?: string;
  method?: string;
  payload?: Record<string, unknown>;
  [key: string]: unknown;
};

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((payloadData) => {
    const app = (window as any)._taburaApp;
    const activeChatWs = app?.getState?.().chatWs;
    if (activeChatWs && typeof activeChatWs.injectEvent === 'function') {
      activeChatWs.injectEvent(payloadData);
      return;
    }
    const sessions = (window as any).__mockWsSessions || [];
    const candidates = sessions.filter((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    const chatWs = candidates[candidates.length - 1];
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(payloadData);
    }
  }, payload);
}

async function tapElement(page: Page, selector: string) {
  const box = await page.locator(selector).first().boundingBox();
  if (box) {
    const x = Math.round(box.x + box.width / 2);
    const y = Math.round(box.y + box.height / 2);
    try {
      await page.touchscreen.tap(x, y);
      return;
    } catch (_) {
      // Fall back to synthetic touch dispatch below.
    }
  }
  await page.evaluate((sel) => {
    const target = document.querySelector(sel);
    if (!target) return;
    const rect = target.getBoundingClientRect();
    const x = Math.round(rect.left + rect.width / 2);
    const y = Math.round(rect.top + rect.height / 2);
    const touchInit = {
      clientX: x,
      clientY: y,
      pageX: x,
      pageY: y,
      identifier: 0,
      target,
    };
    if (typeof Touch === 'undefined') {
      target.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: x, clientY: y }));
      return;
    }
    const touch = new Touch(touchInit);
    target.dispatchEvent(new TouchEvent('touchstart', { touches: [touch], changedTouches: [touch], bubbles: true }));
    target.dispatchEvent(new TouchEvent('touchend', { touches: [], changedTouches: [touch], bubbles: true, cancelable: true }));
  }, selector);
}

async function setHarnessCancelResponses(page: Page, responses: Array<Record<string, unknown>>) {
  await page.evaluate((entries) => {
    const setter = (window as any).__setCancelResponses;
    if (typeof setter === 'function') setter(entries);
  }, responses);
}

async function setHarnessActivityResponse(page: Page, response: Record<string, unknown>) {
  await page.evaluate((payload) => {
    const setter = (window as any).__setActivityResponse;
    if (typeof setter === 'function') setter(payload);
  }, response);
}

async function setHarnessMessagePostDelay(page: Page, delayMs: number) {
  await page.evaluate((ms) => {
    const setter = (window as any).__setMessagePostDelay;
    if (typeof setter === 'function') setter(ms);
  }, delayMs);
}

async function setHarnessSTTTranscribeResponse(page: Page, payload: Record<string, unknown>, status = 200) {
  await page.evaluate(([body, code]) => {
    const setter = (window as any).__setSTTTranscribeResponse;
    if (typeof setter === 'function') setter(body, code);
  }, [payload, status]);
}

async function waitForApiCancel(page: Page) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'api_fetch' && entry.action === 'cancel');
  }, { timeout: 15_000 }).toBe(true);
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
      event_id: 'art-ctrl-ptt',
      kind: 'text_artifact',
      title: 'test.txt',
      text: content,
    });
  }, text);
  await page.evaluate(() => {
    const ct = document.getElementById('canvas-text');
    if (ct) {
      ct.style.display = '';
      ct.classList.add('is-active');
    }
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  });
}

async function renderPdfArtifactMock(page: Page) {
  await page.evaluate(() => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'art-ctrl-pdf',
      kind: 'pdf_artifact',
      title: 'test.pdf',
      path: '',
    });

    const pane = document.getElementById('canvas-pdf');
    if (!(pane instanceof HTMLElement)) return;
    pane.style.display = '';
    pane.classList.add('is-active');
    pane.innerHTML = '';

    const surface = document.createElement('div');
    surface.className = 'canvas-pdf-surface';
    const pagesHost = document.createElement('div');
    pagesHost.className = 'canvas-pdf-pages';

    const pageNode = document.createElement('section');
    pageNode.className = 'canvas-pdf-page';
    pageNode.dataset.page = '2';

    const pageInner = document.createElement('div');
    pageInner.className = 'canvas-pdf-page-inner';
    pageInner.style.width = '640px';
    pageInner.style.height = '860px';

    const canvas = document.createElement('canvas');
    canvas.className = 'canvas-pdf-canvas';
    canvas.width = 640;
    canvas.height = 860;
    canvas.style.width = '640px';
    canvas.style.height = '860px';
    pageInner.appendChild(canvas);

    const textLayer = document.createElement('div');
    textLayer.className = 'textLayer canvas-pdf-text-layer';
    textLayer.style.setProperty('--scale-factor', '1');

    const addLine = (text: string, topPx: number) => {
      const span = document.createElement('span');
      span.textContent = text;
      span.style.position = 'absolute';
      span.style.left = '56px';
      span.style.top = `${topPx}px`;
      span.style.fontSize = '16px';
      span.style.lineHeight = '1';
      textLayer.appendChild(span);
    };
    addLine('First PDF line', 100);
    addLine('Second PDF line', 136);
    pageInner.appendChild(textLayer);

    pageNode.appendChild(pageInner);
    pagesHost.appendChild(pageNode);
    surface.appendChild(pagesHost);
    pane.appendChild(surface);

    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  });
}

async function waitForLogEntry(page: Page, type: string, action?: string) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(e => e.type === type && (action === undefined || e.action === action));
  }, { timeout: 15_000 }).toBe(true);
}

async function waitForSTTAction(page: Page, action: string) {
  await waitForLogEntry(page, 'stt', action);
}

async function waitForEdgeButtons(page: Page) {
  await expect.poll(async () => page.evaluate(() => {
    const dialogue = document.querySelector('#edge-top-models .edge-live-dialogue-btn');
    const silent = document.querySelector('#edge-top-models .edge-silent-btn');
    return Boolean(dialogue && silent);
  })).toBe(true);
}

async function switchToTestProject(page: Page) {
  await page.evaluate(() => {
    const buttons = Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'));
    const button = buttons.find((node) => node.textContent?.trim().toLowerCase() === 'test');
    if (button instanceof HTMLButtonElement) {
      button.click();
    }
  });
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    const wsOpen = (window as any).WebSocket.OPEN;
    if (String(state?.activeWorkspaceId || '') !== 'test') return '';
    return state?.chatWs?.readyState === wsOpen ? 'ready' : 'waiting';
  })).toBe('ready');
}

async function setDialogueMode(page: Page, enabled: boolean) {
  if (enabled) {
    await switchToTestProject(page);
    await waitForEdgeButtons(page);
    const dialogueButton = page.locator('#edge-top-models .edge-live-dialogue-btn');
    await expect(dialogueButton).toBeEnabled();
    await page.evaluate(() => {
      const button = document.querySelector('#edge-top-models .edge-live-dialogue-btn');
      if (!(button instanceof HTMLButtonElement)) {
        throw new Error('dialogue button not found');
      }
      button.click();
    });
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
    return;
  }
  const stopButton = page.locator('#edge-top-models .edge-live-stop-btn');
  if (await stopButton.count()) {
    await page.evaluate(() => {
      const button = document.querySelector('#edge-top-models .edge-live-stop-btn');
      if (button instanceof HTMLButtonElement) {
        button.click();
      }
    });
  }
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toBeVisible();
}

function countGetUserMediaCalls(log: HarnessLogEntry[]): number {
  return log.filter((entry) => entry.type === 'media' && entry.action === 'get_user_media').length;
}

test.beforeEach(async ({ page }) => {
  page.on('console', (msg) => {
    if (msg.type() === 'error') console.log(`BROWSER [error]: ${msg.text()}`);
  });
  page.on('pageerror', (err) => console.log(`PAGE ERROR: ${err.message}`));
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 15_000 });
  await page.waitForTimeout(200);
  await setHarnessCancelResponses(page, []);
  await setHarnessActivityResponse(page, { active_turns: 0, queued_turns: 0 });
  await setHarnessMessagePostDelay(page, 0);
  await setHarnessSTTTranscribeResponse(page, { text: 'hello world' }, 200);
});

test('click on canvas starts voice recording', async ({ page }) => {
  await clearLog(page);

  // Click on canvas area to start recording (tap = voice)
  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);

  await waitForLogEntry(page, 'recorder', 'start');

  // Recording indicator should be visible
  const indicator = page.locator('#indicator');
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

test('typed idea capture sends the idea prefix unchanged', async ({ page }) => {
  await clearLog(page);

  const input = page.locator('#chat-pane-input');
  await input.fill('idea: better swipe triage');
  await input.press('Enter');

  await waitForLogEntry(page, 'message_sent');

  const log = await getLog(page);
  const sent = log.find((entry) => entry.type === 'message_sent');
  expect(sent?.text).toBe('idea: better swipe triage');
});

test('voice idea capture sends the transcript unchanged', async ({ page }) => {
  await setHarnessSTTTranscribeResponse(page, { text: 'idea: better swipe triage' }, 200);
  await clearLog(page);

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await page.mouse.click(400, 400);

  await waitForLogEntry(page, 'message_sent');

  const log = await getLog(page);
  const sent = log.find((entry) => entry.type === 'message_sent');
  expect(sent?.text).toBe('idea: better swipe triage');
});

test('sequential recordings reuse cached mic stream', async ({ page }) => {
  await clearLog(page);

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');

  await page.mouse.click(420, 360);
  await waitForLogEntry(page, 'recorder', 'start');

  const log = await getLog(page);
  expect(countGetUserMediaCalls(log)).toBe(1);
});

test('unrelated touchend does not suppress first click-to-record', async ({ page }) => {
  await clearLog(page);

  // Simulate a touchend outside workspace tap flow (e.g. UI panel touch).
  await page.evaluate(() => {
    document.dispatchEvent(new Event('touchend', { bubbles: true }));
  });

  // Click fallback must still work.
  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
});

test('missing getUserMedia shows explicit microphone-unavailable notice', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    if (!navigator.mediaDevices) return;
    navigator.mediaDevices.getUserMedia = undefined as unknown as typeof navigator.mediaDevices.getUserMedia;
  });

  await page.mouse.click(400, 400);
  await page.waitForTimeout(120);

  await expect(page.locator('#chat-history')).toContainText('Microphone API unavailable in this browser context');
  await expect(page.locator('#status-text')).toContainText('Microphone API unavailable in this browser context');

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(false);
});

test('touch stop indicator routes through shared cancel endpoint', async ({ page }) => {
  await clearLog(page);

  await injectChatEvent(page, {
    type: 'turn_started',
    turn_id: 'stop-turn-test',
    content: 'background work',
  });
  await expect(page.locator('#chat-history .chat-message.chat-assistant.is-pending')).toHaveCount(1);

  const stopSquare = page.locator('.stop-square');
  await expect(stopSquare).toBeVisible({ timeout: 5_000 });

  await tapElement(page, '.stop-square');
  await waitForApiCancel(page);

  const log = await getLog(page);
  const cancelEntry = log.find((entry) => entry.type === 'api_fetch' && entry.action === 'cancel');
  expect(cancelEntry).toBeTruthy();
  expect(cancelEntry!.method).toBe('POST');
  expect(typeof cancelEntry!.payload?.canceled).toBe('number');
  expect(Number(cancelEntry!.payload?.canceled)).toBeGreaterThan(0);
  expect(Number(cancelEntry!.payload?.canceled)).toBeGreaterThan(0);

  await injectChatEvent(page, { type: 'turn_cancelled', turn_id: 'stop-turn-test' });
  await page.waitForTimeout(100);
  await expect(page.locator('#chat-history .chat-message.chat-assistant.is-pending')).toHaveCount(0);
  await expect(page.locator('#chat-history .chat-message.chat-assistant .chat-bubble').first()).toContainText('Stopped');
});

test('touch stop retries cancel when first cancel reports zero but work remains', async ({ page }) => {
  await clearLog(page);
  await setHarnessActivityResponse(page, { active_turns: 1, queued_turns: 0 });
  await setHarnessCancelResponses(page, [
    { ok: true, canceled: 0, active_canceled: 0, queued_canceled: 0 },
    { ok: true, canceled: 2, active_canceled: 1, queued_canceled: 1 },
  ]);

  await injectChatEvent(page, { type: 'turn_started', turn_id: 'stop-retry-turn' });
  await page.waitForTimeout(100);
  await tapElement(page, '.stop-square');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.filter((entry) => entry.type === 'api_fetch' && entry.action === 'cancel').length;
  }, { timeout: 15_000 }).toBeGreaterThanOrEqual(2);
});

test('stop indicator auto-hides after stop even when activity poll stays active', async ({ page }) => {
  await clearLog(page);
  // Simulate stale backend activity that can keep stop UI stuck in Safari.
  await setHarnessActivityResponse(page, { active_turns: 1, queued_turns: 0 });
  await setHarnessCancelResponses(page, [
    { ok: true, canceled: 2, active_canceled: 1, queued_canceled: 1 },
  ]);

  await injectChatEvent(page, { type: 'turn_started', turn_id: 'stop-stale-activity-turn' });
  await page.waitForTimeout(120);
  await expect(page.locator('.stop-square')).toBeVisible();

  await tapElement(page, '.stop-square');
  await waitForApiCancel(page);

  // Wait beyond one activity poll cycle to ensure stale active count is observed.
  await page.waitForTimeout(1500);
  await expect(page.locator('#indicator')).toBeHidden();

  // New turn should clear suppression and show indicator again.
  await injectChatEvent(page, { type: 'turn_started', turn_id: 'stop-stale-activity-turn-2' });
  await page.waitForTimeout(120);
  await expect(page.locator('.stop-square')).toBeVisible();
});

test('touch stop while sending transcript aborts pending message submit', async ({ page }) => {
  await clearLog(page);
  await setHarnessMessagePostDelay(page, 2500);

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');
  await expect(page.locator('#status-label')).toContainText('sending');

  await tapElement(page, '.stop-square');
  await waitForApiCancel(page);
  await page.waitForTimeout(2800);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'message_sent')).toBe(false);
  await expect(page.locator('#chat-history .chat-message.chat-assistant.is-pending')).toHaveCount(0);
});

test('duplicate stop action while stopping does not cancel voice transcript send', async ({ page }) => {
  await clearLog(page);
  await setHarnessMessagePostDelay(page, 1500);

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await page.mouse.click(400, 400);
  await page.waitForTimeout(20);
  await page.keyboard.press('Enter');

  await waitForSTTAction(page, 'stop');
  await expect(page.locator('#status-label')).toContainText('sending');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'message_sent');
  }, { timeout: 6_000 }).toBe(true);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'api_fetch' && entry.action === 'cancel')).toBe(false);
  expect(log.some((entry) => entry.type === 'stt' && entry.action === 'cancel')).toBe(false);
});

test('silence auto-stop sends transcript without manual stop click', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 40 }, () => -80),
    ]);
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(250);

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
      ...Array.from({ length: 8 }, () => -41),
      ...Array.from({ length: 10 }, () => -35),
      ...Array.from({ length: 40 }, () => -44),
    ]);
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(250);

  const log = await getLog(page);
  const sent = log.find(e => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');
  expect(log.some(e => e.type === 'stt' && e.action === 'cancel')).toBe(false);
});

test('silence auto-stop works when speech is only slightly above noisy ambient baseline', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    // High ambient noise (~-22 dB), speech only +4 dB above baseline.
    // Regression: this used to miss speech onset and fall into no-speech cancel.
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -22),
      ...Array.from({ length: 10 }, () => -18),
      ...Array.from({ length: 40 }, () => -22),
    ]);
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(250);

  const log = await getLog(page);
  const sent = log.find(e => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');
  expect(log.some(e => e.type === 'stt' && e.action === 'cancel')).toBe(false);
});

test('VAD auto-stop delivers non-empty audio to STT before submitting transcript', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 40 }, () => -80),
    ]);
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(250);

  const log = await getLog(page);
  const sttAppends = log.filter((e: HarnessLogEntry) => e.type === 'stt' && e.action === 'append');
  expect(sttAppends.length).toBeGreaterThan(0);
  const totalBytes = sttAppends.reduce((sum: number, e: HarnessLogEntry) => sum + Number(e.bytes || 0), 0);
  expect(totalBytes).toBeGreaterThan(0);

  const sttStop = log.find((e: HarnessLogEntry) => e.type === 'stt' && e.action === 'stop');
  expect(sttStop).toBeTruthy();

  const sent = log.find((e: HarnessLogEntry) => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');

  const recorderStop = log.find((e: HarnessLogEntry) => e.type === 'recorder' && e.action === 'stop');
  expect(recorderStop).toBeTruthy();

  expect(log.some((e: HarnessLogEntry) => e.type === 'stt' && e.action === 'cancel')).toBe(false);
});

test('no-speech timeout cancels capture in sustained ambient noise', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 220 }, () => -41));
  });

  await page.mouse.click(400, 400);
  await waitForLogEntry(page, 'recorder', 'start');
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(e => e.type === 'stt' && e.action === 'cancel');
  }, { timeout: 8_000 }).toBe(true);

  const log = await getLog(page);
  expect(log.some(e => e.type === 'stt' && e.action === 'cancel')).toBe(true);
  expect(log.some(e => e.type === 'stt' && e.action === 'stop')).toBe(false);
  expect(log.some(e => e.type === 'message_sent')).toBe(false);
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

test('Control long-press starts at mouse location and sends artifact line context', async ({ page }) => {
  await clearLog(page);
  await injectCanvasModuleRef(page);
  await renderTestArtifact(page);

  const canvasText = page.locator('#canvas-text');
  const box = await canvasText.boundingBox();
  if (!box) throw new Error('canvas-text not visible');
  const x = Math.floor(box.x + 50);
  const y = Math.floor(box.y + 50);

  await page.mouse.move(x, y);
  await page.keyboard.down('Control');
  await page.waitForTimeout(300);
  await waitForLogEntry(page, 'recorder', 'start');

  const dotPos = await page.evaluate(() => {
    const dot = document.querySelector('#indicator .record-dot');
    if (!(dot instanceof HTMLElement)) return null;
    return {
      x: Number.parseFloat(dot.style.left || '0'),
      y: Number.parseFloat(dot.style.top || '0'),
    };
  });
  expect(dotPos).toBeTruthy();
  expect(Math.abs(dotPos!.x - x)).toBeLessThanOrEqual(1);
  expect(Math.abs(dotPos!.y - y)).toBeLessThanOrEqual(1);

  await page.keyboard.up('Control');
  await waitForSTTAction(page, 'stop');
  await expect.poll(async () => {
    const log = await getLog(page);
    const sent = log.find((entry) => entry.type === 'message_sent');
    return String(sent?.text || '');
  }).toMatch(/\[Line \d+ of "test\.txt"\] hello world/);
});

test('Control long-press on PDF sends page context from cursor position', async ({ page }) => {
  await clearLog(page);
  await injectCanvasModuleRef(page);
  await renderPdfArtifactMock(page);

  const pdfLine = page.locator('#canvas-pdf .textLayer span').first();
  const box = await pdfLine.boundingBox();
  if (!box) throw new Error('mock PDF text line not visible');
  const x = Math.floor(box.x + 8);
  const y = Math.floor(box.y + 8);

  const anchor = await page.evaluate(async (point) => {
    const ui = await import('../../internal/web/static/ui.js');
    return ui.getAnchorFromPoint(point.x, point.y);
  }, { x, y });
  expect(anchor).toBeTruthy();
  expect(anchor.page).toBe(2);
  expect(anchor.title).toBe('test.pdf');
  await expect.poll(async () => page.evaluate(() => (window as any)._taburaApp?.getState?.().hasArtifact)).toBe(true);

  await page.mouse.move(x, y);
  await page.keyboard.down('Control');
  await page.waitForTimeout(300);
  await waitForLogEntry(page, 'recorder', 'start');
  await expect.poll(async () => page.evaluate(() => (window as any)._taburaApp?.getState?.().hasArtifact)).toBe(true);
  const captureAnchor = await page.evaluate(async () => {
    const ui = await import('../../internal/web/static/ui.js');
    return ui.getInputAnchor();
  });
  expect(captureAnchor).toBeTruthy();
  expect(captureAnchor.page).toBe(2);

  const dotPos = await page.evaluate(() => {
    const dot = document.querySelector('#indicator .record-dot');
    if (!(dot instanceof HTMLElement)) return null;
    return {
      x: Number.parseFloat(dot.style.left || '0'),
      y: Number.parseFloat(dot.style.top || '0'),
    };
  });
  expect(dotPos).toBeTruthy();
  expect(Math.abs(dotPos!.x - x)).toBeLessThanOrEqual(1);
  expect(Math.abs(dotPos!.y - y)).toBeLessThanOrEqual(1);

  await page.keyboard.up('Control');
  await waitForSTTAction(page, 'stop');
  await expect.poll(async () => {
    const log = await getLog(page);
    const sent = log.find((entry) => entry.type === 'message_sent');
    return String(sent?.text || '');
  }).toMatch(/\[Page 2(?:, line \d+)? of "test\.pdf"\] hello world/);
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

  // Stop recording (will auto-send via voice capture)
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');

  // Poll for message_sent — STT transcription + message post is async
  await waitForLogEntry(page, 'message_sent');

  const log = await getLog(page);
  const sent = log.find(e => e.type === 'message_sent');
  expect(sent).toBeTruthy();
  expect(sent!.text).toBe('hello world');
});

test('ios-style mp4 recorder payload keeps mp4 mime for STT upload', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    const setMime = (window as any).__setMediaRecorderMimeType;
    if (typeof setMime === 'function') setMime('audio/mp4');
  });

  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'start');
  await waitForSTTAction(page, 'stop');

  const log = await getLog(page);
  const sttStart = log.find((entry) => entry.type === 'stt' && entry.action === 'start');
  expect(sttStart).toBeTruthy();
  expect(sttStart?.mime_type).toBe('audio/mp4');
});

test('empty recorder mime with mp4 chunks resolves mime to mp4 for STT upload', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    const setRecorderMime = (window as any).__setMediaRecorderMimeType;
    const setChunkMime = (window as any).__setMediaRecorderChunkMimeType;
    if (typeof setRecorderMime === 'function') setRecorderMime('');
    if (typeof setChunkMime === 'function') setChunkMime('audio/mp4');
  });

  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'start');
  await waitForSTTAction(page, 'stop');

  const log = await getLog(page);
  const sttStart = log.find((entry) => entry.type === 'stt' && entry.action === 'start');
  expect(sttStart).toBeTruthy();
  expect(sttStart?.mime_type).toBe('audio/mp4');
});

test('stt empty reason is surfaced as visible voice capture error', async ({ page }) => {
  await clearLog(page);
  await setHarnessSTTTranscribeResponse(page, { text: '', reason: 'recording_too_short' }, 200);

  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');

  await expect(page.locator('#chat-history .chat-message.chat-system .chat-bubble').last())
    .toContainText('recording too short');
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'message_sent')).toBe(false);
});

test('dialogue PTT empty transcript surfaces error instead of silent drop', async ({ page }) => {
  await clearLog(page);
  await setDialogueMode(page, true);
  await setHarnessSTTTranscribeResponse(page, { text: '', reason: 'no_speech_detected' }, 200);

  await page.keyboard.down('Control');
  await page.waitForTimeout(300);
  await waitForLogEntry(page, 'recorder', 'start');

  await page.keyboard.up('Control');
  await waitForSTTAction(page, 'stop');

  await expect(page.locator('#chat-history .chat-message.chat-system .chat-bubble').last())
    .toContainText('no clear speech detected');
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'message_sent')).toBe(false);
});

test('stop does not call MediaRecorder.requestData before recorder.stop', async ({ page }) => {
  await clearLog(page);
  await page.evaluate(() => {
    const setRequestData = (window as any).__setMediaRecorderRequestDataEnabled;
    if (typeof setRequestData === 'function') setRequestData(true);
  });

  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');

  const log = await getLog(page);
  const requestDataCalls = log.filter((entry) => entry.type === 'recorder' && entry.action === 'requestData');
  expect(requestDataCalls).toHaveLength(0);
});

test('recording indicator shows symbol', async ({ page }) => {
  await clearLog(page);

  await page.mouse.click(400, 400);
  await page.waitForTimeout(500);
  await waitForLogEntry(page, 'recorder', 'start');

  const indicator = page.locator('#indicator');
  await expect(indicator).toBeVisible();
  await expect(page.locator('.record-dot')).toBeVisible();
  await expect(page.locator('.stop-square')).toBeHidden();

  // Stop recording and transition to working/play indicator
  await page.mouse.click(400, 400);
  await waitForSTTAction(page, 'stop');
  await page.waitForTimeout(200);
  await expect(indicator).toBeVisible();
  await expect(page.locator('.stop-square')).toBeVisible();
  await expect(page.locator('.record-dot')).toBeHidden();
});

test('focus refreshes cached mic stream before next recording', async ({ page }) => {
  await clearLog(page);

  await page.evaluate(async () => {
    await (window as any)._taburaApp.acquireMicStream();
  });

  await clearLog(page);
  await page.evaluate(() => {
    window.dispatchEvent(new Event('focus'));
  });

  await page.evaluate(async () => {
    await (window as any)._taburaApp.acquireMicStream();
  });

  const log = await getLog(page);
  expect(countGetUserMediaCalls(log)).toBe(1);
});

test('pageshow refreshes cached mic stream before next recording', async ({ page }) => {
  await clearLog(page);

  await page.evaluate(async () => {
    await (window as any)._taburaApp.acquireMicStream();
  });

  await clearLog(page);
  await page.evaluate(() => {
    window.dispatchEvent(new Event('pageshow'));
  });

  await page.evaluate(async () => {
    await (window as any)._taburaApp.acquireMicStream();
  });

  const log = await getLog(page);
  expect(countGetUserMediaCalls(log)).toBe(1);
});

test('ended mic track invalidates cached stream before next recording', async ({ page }) => {
  await clearLog(page);

  await page.evaluate(async () => {
    await (window as any)._taburaApp.acquireMicStream();
  });

  await clearLog(page);
  await page.evaluate(() => {
    const trigger = (window as any).__triggerMicTrackEnded;
    if (typeof trigger === 'function') trigger();
  });

  await page.evaluate(async () => {
    await (window as any)._taburaApp.acquireMicStream();
  });

  const log = await getLog(page);
  expect(countGetUserMediaCalls(log)).toBe(1);
});

// Safari broken MediaRecorder: stop fires before dataavailable, blob is empty.
// These tests verify the _vadAudioBlob codepath handles this correctly.
test.describe('safari-recorder=broken', () => {
  test.beforeEach(async ({ page }) => {
    page.on('console', (msg) => {
      if (msg.type() === 'error') console.log(`BROWSER [error]: ${msg.text()}`);
    });
    page.on('pageerror', (err) => console.log(`PAGE ERROR: ${err.message}`));
    // Inject getUserMedia mock before any page script runs.  The harness
    // inline script normally overrides this, but on some WebKit CI
    // environments the harness-level override silently fails.  The init
    // script guarantees a working mock is always in place.
    await page.addInitScript(() => {
      const makeMockStream = () => {
        const track: any = new EventTarget();
        track.kind = 'audio'; track.enabled = true; track.muted = false;
        track.readyState = 'live';
        track.stop = () => { track.readyState = 'ended'; track.dispatchEvent(new Event('ended')); };
        const stream: any = new EventTarget();
        stream.active = true;
        stream.getTracks = () => [track];
        stream.getAudioTracks = () => [track];
        stream.clone = () => makeMockStream();
        return stream;
      };
      const mock = async () => makeMockStream();
      // Will be overridden by the harness script if it runs successfully.
      // Acts as a safety net for environments where the harness override fails.
      Object.defineProperty(Navigator.prototype, 'mediaDevices', {
        get() {
          const md: any = new EventTarget();
          md.getUserMedia = mock;
          md.addEventListener = EventTarget.prototype.addEventListener;
          md.removeEventListener = EventTarget.prototype.removeEventListener;
          md.dispatchEvent = EventTarget.prototype.dispatchEvent;
          // Cache so the same object is returned on every access.
          Object.defineProperty(this, 'mediaDevices', { value: md, writable: true, configurable: true });
          return md;
        },
        configurable: true,
      });
    });
    await page.goto('/tests/playwright/harness.html?safari-recorder=broken');
    await page.waitForFunction(() => {
      const app = (window as any)._taburaApp;
      if (typeof app?.getState !== 'function') return false;
      const s = app.getState();
      return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
    }, null, { timeout: 15_000 });
    await page.waitForTimeout(200);
    await setHarnessCancelResponses(page, []);
    await setHarnessActivityResponse(page, { active_turns: 0, queued_turns: 0 });
    await setHarnessMessagePostDelay(page, 0);
    await setHarnessSTTTranscribeResponse(page, { text: 'hello world' }, 200);
  });

  test('VAD auto-stop sends transcript via _vadAudioBlob despite broken recorder', async ({ page }) => {
    await clearLog(page);
    await page.evaluate(() => {
      (window as any).__setVadDbFrames([
        ...Array.from({ length: 8 }, () => -80),
        ...Array.from({ length: 10 }, () => -12),
        ...Array.from({ length: 40 }, () => -80),
      ]);
    });

    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'recorder', 'start');
    await waitForSTTAction(page, 'stop');
    await page.waitForTimeout(250);

    const log = await getLog(page);
    const sttStart = log.find((e: HarnessLogEntry) => e.type === 'stt' && e.action === 'start');
    expect(sttStart).toBeTruthy();
    expect(sttStart!.mime_type).toBe('audio/wav');

    const sttAppends = log.filter((e: HarnessLogEntry) => e.type === 'stt' && e.action === 'append');
    expect(sttAppends.length).toBeGreaterThan(0);
    const totalBytes = sttAppends.reduce((sum: number, e: HarnessLogEntry) => sum + Number(e.bytes || 0), 0);
    expect(totalBytes).toBeGreaterThan(0);

    const sent = log.find((e: HarnessLogEntry) => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('hello world');
  });

  test('manual tap-stop still works with broken recorder via accumulated chunks', async ({ page }) => {
    await clearLog(page);

    await page.mouse.click(400, 400);
    await waitForLogEntry(page, 'recorder', 'start');

    // Recorder chunk interval is 250ms; wait long enough for several chunks.
    await page.waitForTimeout(1000);

    await page.mouse.click(400, 400);
    await waitForSTTAction(page, 'stop');

    await expect.poll(async () => {
      const log = await getLog(page);
      return log.some((e: HarnessLogEntry) => e.type === 'message_sent');
    }, { timeout: 15_000 }).toBe(true);

    const log = await getLog(page);
    const sent = log.find((e: HarnessLogEntry) => e.type === 'message_sent');
    expect(sent).toBeTruthy();
    expect(sent!.text).toBe('hello world');
  });
});
