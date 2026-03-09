import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  action?: string;
  text?: string;
  [key: string]: unknown;
};

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => {
    (window as any).__harnessLog.splice(0);
  });
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
  });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    const sessionId = String(app?.getState?.().chatSessionId || '');
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string'
      && ws.url.includes('/ws/chat/')
      && (!sessionId || ws.url.includes(`/ws/chat/${sessionId}`)));
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
}

async function renderTestArtifact(page: Page) {
  await page.evaluate(() => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'cursor-artifact',
      kind: 'text_artifact',
      title: 'test.txt',
      text: 'Line one\nLine two\nLine three\nLine four\nLine five',
    });
    const pane = document.getElementById('canvas-text');
    if (pane) {
      pane.style.display = '';
      pane.classList.add('is-active');
    }
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().hasArtifact = true;
  });
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
    if (String(state?.activeProjectId || '') !== 'test') return '';
    return state?.chatWs?.readyState === wsOpen ? 'ready' : 'waiting';
  })).toBe('ready');
}

async function setLiveMode(page: Page, mode: 'dialogue' | 'meeting') {
  await switchToTestProject(page);
  await waitForEdgeButtons(page);
  const buttonSelector = mode === 'dialogue'
    ? '#edge-top-models .edge-live-dialogue-btn'
    : '#edge-top-models .edge-live-meeting-btn';
  await page.evaluate((selector) => {
    const button = document.querySelector(selector);
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error(`live mode button not found: ${selector}`);
    }
    button.click();
  }, buttonSelector);
  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText(
    mode === 'dialogue' ? 'Dialogue' : 'Meeting',
  );
}

async function currentDotPosition(page: Page) {
  return page.evaluate(() => {
    const dot = document.querySelector('#indicator .record-dot');
    if (!(dot instanceof HTMLElement)) return null;
    return {
      left: dot.style.left,
      top: dot.style.top,
      indicatorClass: document.getElementById('indicator')?.className || '',
    };
  });
}

async function submitVoiceStyleMessage(page: Page, text: string) {
  await page.evaluate(async (messageText) => {
    const app = (window as any)._taburaApp;
    if (app?.getState) {
      app.getState().lastInputOrigin = 'voice';
    }
    const mod = await import('../../internal/web/static/app-chat-submit.js');
    await mod.submitMessage(messageText, { kind: 'voice_transcript' });
  }, text);
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
});

test('dialogue tap pins a cursor dot and scopes the next voice message without starting capture', async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__taburaConversationListenMs = 1_200;
  });
  await setLiveMode(page, 'dialogue');
  await renderTestArtifact(page);
  await clearLog(page);
  await triggerVoiceAssistantTTS(page, 'cursor-dialogue-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  const x = 420;
  const y = 360;
  await page.mouse.click(x, y);
  await page.waitForTimeout(200);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(false);

  await page.evaluate(async ({ x: anchorX, y: anchorY }) => {
    const ui = await import('../../internal/web/static/ui.js');
    ui.pinCursorAnchor(anchorX, anchorY, { line: 3, title: 'test.txt' });
  }, { x, y });

  await submitVoiceStyleMessage(page, 'fix this');
  await expect.poll(async () => {
    const nextLog = await getLog(page);
    return nextLog.find((entry) => entry.type === 'message_sent')?.text || '';
  }).toBe('[Line 3 of "test.txt"] fix this');
});

test('meeting taps move the pinned cursor without starting a new recording', async ({ page }) => {
  await setLiveMode(page, 'meeting');
  await renderTestArtifact(page);
  await clearLog(page);

  const firstX = 420;
  const firstY = 360;
  const secondX = 520;
  const secondY = 430;

  await page.mouse.click(firstX, firstY);
  await page.waitForTimeout(120);
  const firstDot = await currentDotPosition(page);

  await page.mouse.click(secondX, secondY);
  await page.waitForTimeout(120);
  const secondDot = await currentDotPosition(page);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(false);
  expect(firstDot?.indicatorClass).toContain('is-cursor');
  expect(secondDot?.indicatorClass).toContain('is-cursor');
  expect(firstDot?.left).toBe(`${firstX}px`);
  expect(firstDot?.top).toBe(`${firstY}px`);
  expect(secondDot?.left).toBe(`${secondX}px`);
  expect(secondDot?.top).toBe(`${secondY}px`);
});
