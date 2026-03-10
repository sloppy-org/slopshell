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

async function renderPdfArtifactMock(page: Page) {
  await page.evaluate(() => {
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
    pageNode.dataset.page = '1';

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

    const line = document.createElement('span');
    line.textContent = 'Persistent PDF note';
    line.style.position = 'absolute';
    line.style.left = '72px';
    line.style.top = '132px';
    line.style.fontSize = '18px';
    line.style.lineHeight = '1';
    textLayer.appendChild(line);
    pageInner.appendChild(textLayer);

    pageNode.appendChild(pageInner);
    pagesHost.appendChild(pageNode);
    surface.appendChild(pagesHost);
    pane.appendChild(surface);

    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    if (state) {
      state.currentCanvasArtifact = {
        kind: 'pdf_artifact',
        title: 'test.pdf',
        path: 'docs/test.pdf',
        event_id: 'art-pdf-1',
      };
      state.hasArtifact = true;
    }
    document.dispatchEvent(new CustomEvent('tabura:canvas-rendered', {
      detail: {
        kind: 'pdf_artifact',
        title: 'test.pdf',
        path: 'docs/test.pdf',
        event_id: 'art-pdf-1',
      },
    }));
  });
}

async function setInteractionTool(page: Page, tool: 'pointer' | 'highlight' | 'ink' | 'text_note' | 'prompt') {
  await page.evaluate((mode) => {
    (window as any).__setRuntimeState?.({ tool: mode });
    const app = (window as any)._taburaApp;
    if (app?.getState) {
      const interaction = app.getState().interaction;
      interaction.tool = mode;
      interaction.toolPinned = true;
    }
  }, tool);
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

test('dialogue tap starts local capture with the tapped cursor context', async ({ page }) => {
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
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }).toBe(true);

  let log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);

  await page.mouse.click(x, y);
  await expect.poll(async () => {
    const nextLog = await getLog(page);
    return nextLog.find((entry) => entry.type === 'message_sent') || null;
  }).not.toBeNull();

  log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  const sentEntry = log.find((entry) => entry.type === 'message_sent');
  expect(String(sentEntry?.text || '')).toContain('hello world');
  expect(sentEntry?.cursor).toMatchObject({
    title: 'test.txt',
  });
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
  expect(log.filter((entry) => entry.type === 'canvas_position')).toHaveLength(2);
  expect(firstDot?.indicatorClass).toContain('is-cursor');
  expect(secondDot?.indicatorClass).toContain('is-cursor');
  expect(firstDot?.left).toBe(`${firstX}px`);
  expect(firstDot?.top).toBe(`${firstY}px`);
  expect(secondDot?.left).toBe(`${secondX}px`);
  expect(secondDot?.top).toBe(`${secondY}px`);
});

test('request_position stays local in annotation tools instead of dispatching a reply', async ({ page }) => {
  await renderPdfArtifactMock(page);
  await setInteractionTool(page, 'text_note');
  await clearLog(page);
  await injectChatEvent(page, {
    type: 'request_position',
    prompt: 'Tap where the comment should go.',
  });

  await page.mouse.click(420, 360);
  await page.waitForTimeout(150);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'start')).toBe(false);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  await expect(page.locator('#annotation-bubble')).toBeVisible();
  await expect(page.locator('#canvas-pdf .canvas-sticky-note')).toHaveCount(1);
  expect(await page.evaluate(() => (window as any)._taburaApp.getState().requestedPositionPrompt)).toBe('Tap where the comment should go.');
});

test('request_position in prompt tool starts a local capture instead of streaming a reply', async ({ page }) => {
  await renderTestArtifact(page);
  await setInteractionTool(page, 'prompt');
  await clearLog(page);
  await injectChatEvent(page, {
    type: 'request_position',
    prompt: 'Tap where the comment should go.',
  });

  await page.mouse.click(420, 360);
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }).toBe(true);

  let log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  expect(await page.evaluate(() => (window as any)._taburaApp.getState().requestedPositionPrompt)).toBe('');

  await page.mouse.click(420, 360);
  await expect.poll(async () => {
    const nextLog = await getLog(page);
    return nextLog.find((entry) => entry.type === 'message_sent') || null;
  }).not.toBeNull();

  log = await getLog(page);
  expect(log.some((entry) => entry.type === 'canvas_position')).toBe(false);
  const sentEntry = log.find((entry) => entry.type === 'message_sent');
  expect(String(sentEntry?.text || '')).toContain('hello world');
  expect(sentEntry?.cursor).toMatchObject({
    title: 'test.txt',
  });
});
