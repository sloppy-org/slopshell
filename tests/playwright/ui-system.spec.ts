import { expect, test, type Page } from '@playwright/test';
import {
  circleSegment,
  clickCircleSegment,
  openCircle,
  setLiveMode,
  switchToWorkspace,
  waitForCircleControls,
} from './tabura-circle-helpers';

type HarnessLogEntry = { type: string; action?: string; text?: string; [key: string]: unknown };

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

async function waitWsReady(page: Page) {
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await waitWsReady(page);
}

async function seedTwoProjects(page: Page) {
  await page.evaluate(() => {
    (window as any).__setProjects([
      {
        id: 'test',
        name: 'Test',
        kind: 'managed',
        sphere: 'private',
        workspace_path: '/tmp/test',
        root_path: '/tmp/test',
        chat_session_id: 'chat-1',
        canvas_session_id: 'local',
        chat_mode: 'chat',
        chat_model: 'spark',
        chat_model_reasoning_effort: 'low',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
      {
        id: 'notes',
        name: 'Notes',
        kind: 'managed',
        sphere: 'private',
        workspace_path: '/tmp/notes',
        root_path: '/tmp/notes',
        chat_session_id: 'chat-2',
        canvas_session_id: 'notes',
        chat_mode: 'chat',
        chat_model: 'spark',
        chat_model_reasoning_effort: 'low',
        unread: false,
        review_pending: false,
        run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
      },
    ], 'test');
  });
}

async function pinEdgeTop(page: Page) {
  await page.evaluate(() => {
    document.getElementById('edge-top')?.classList.add('edge-pinned');
  });
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
    const app = (window as any)._taburaApp;
    const activeChatWs = app?.getState?.().chatWs;
    if (activeChatWs && typeof activeChatWs.injectEvent === 'function') {
      activeChatWs.injectEvent(p);
      return;
    }
    const sessions = (window as any).__mockWsSessions || [];
    const candidates = sessions.filter((ws: any) => ws.url && ws.url.includes('/ws/chat/'));
    const chatWs = candidates[candidates.length - 1];
    if (chatWs && typeof chatWs.injectEvent === 'function') chatWs.injectEvent(p);
  }, payload);
}

async function injectCanvasEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((p) => {
    const sessions = (window as any).__mockWsSessions || [];
    const canvasWs = sessions.find((ws: any) => ws.url && ws.url.includes('/ws/canvas/'));
    if (canvasWs) canvasWs.injectEvent(p);
  }, payload);
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

async function dispatchPrintableKey(page: Page, key: string) {
  await page.evaluate((value) => {
    document.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true }));
    document.dispatchEvent(new KeyboardEvent('keyup', { key: value, bubbles: true }));
  }, key);
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

async function dispatchPenStroke(page: Page, points: Array<{ x: number; y: number; pressure?: number }>) {
  await page.evaluate((rawPoints) => {
    const viewport = document.getElementById('canvas-viewport');
    if (!(viewport instanceof HTMLElement) || !Array.isArray(rawPoints) || rawPoints.length === 0) return;
    const mk = (type: string, point: any) => new PointerEvent(type, {
      bubbles: true,
      cancelable: true,
      pointerId: 41,
      pointerType: 'pen',
      pressure: Number(point.pressure ?? 0.6),
      clientX: Number(point.x),
      clientY: Number(point.y),
    });
    viewport.dispatchEvent(mk('pointerdown', rawPoints[0]));
    for (let i = 1; i < rawPoints.length; i += 1) {
      viewport.dispatchEvent(mk('pointermove', rawPoints[i]));
    }
    viewport.dispatchEvent(mk('pointerup', rawPoints[rawPoints.length - 1]));
  }, points);
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


test.describe('runtime refresh', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('reloads on boot changes and restores the active project context', async ({ page }) => {
    await page.evaluate(() => {
      document.getElementById('edge-right')?.classList.add('edge-pinned');
      const buttons = Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'));
      const target = buttons.find((button) => String(button.textContent || '').trim() === 'Test');
      if (target instanceof HTMLButtonElement) target.click();
    });

    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().activeWorkspaceId || '');
    }, { timeout: 5_000 }).toBe('test');

    await page.evaluate(() => {
      (window as any).__setRuntimeState({ boot_id: 'boot-2' });
    });

    await page.waitForURL(/__tabura_reload=/, { timeout: 5_000 });
    await waitWsReady(page);

    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().activeWorkspaceId || '');
    }, { timeout: 5_000 }).toBe('test');
    await expect(page.locator('#status-label')).toHaveText('Bug fix applied.');

    const edgeRightPinned = await page.evaluate(() => {
      return document.getElementById('edge-right')?.classList.contains('edge-pinned') === true;
    });
    expect(edgeRightPinned).toBe(true);
  });
});


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

test.describe('Tabura Circle', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('renders session and tool controls outside the top panel', async ({ page }) => {
    await expect(page.locator('#tabura-circle-dot')).toBeVisible();
    await waitForCircleControls(page);

    const snapshot = await page.evaluate(() => {
      const circleButtons = Array.from(document.querySelectorAll('#tabura-circle-menu .tabura-circle-segment')).map((button) => ({
        segment: button.getAttribute('data-segment'),
        kind: button.getAttribute('data-kind'),
        text: String(button.textContent || '').trim(),
      }));
      const topButtonTexts = Array.from(document.querySelectorAll('#edge-top-models button')).map((button) => String(button.textContent || '').trim());
      const topModels = document.getElementById('edge-top-models');
      return {
        circleButtons,
        topButtonTexts,
        topButtonCount: document.querySelectorAll('#edge-top-models button').length,
        topOverflows: topModels ? (topModels.scrollWidth > topModels.clientWidth + 1) : null,
      };
    });

    expect(snapshot.circleButtons.map((button) => button.segment)).toEqual([
      'dialogue',
      'meeting',
      'silent',
      'prompt',
      'text_note',
      'pointer',
      'highlight',
      'ink',
    ]);
    expect(snapshot.circleButtons.map((button) => button.kind)).toEqual([
      'session',
      'session',
      'toggle',
      'tool',
      'tool',
      'tool',
      'tool',
      'tool',
    ]);
    expect(snapshot.circleButtons.every((button) => button.text.length > 0)).toBe(true);
    expect(snapshot.topButtonTexts).toHaveLength(0);
    expect(snapshot.topButtonCount).toBe(0);
    expect(snapshot.topOverflows).toBe(false);
  });

  test('circle clicks switch the active interaction mode', async ({ page }) => {
    await clearLog(page);
    await clickCircleSegment(page, 'text_note');
    await waitForLogEntry(page, 'api_fetch', 'runtime_preferences');

    await expect(circleSegment(page, 'text_note')).toHaveAttribute('aria-pressed', 'true');
    await expect(circleSegment(page, 'pointer')).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'text_note');

    const tool = await page.evaluate(() => (window as any)._taburaApp?.getState?.().interaction.tool);
    expect(tool).toBe('text_note');
  });

  test('keyboard shortcuts switch tools without using the top panel', async ({ page }) => {
    await clearLog(page);
    await page.keyboard.press('i');
    await waitForLogEntry(page, 'api_fetch', 'runtime_preferences');
    await expect(circleSegment(page, 'ink')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'ink');
  });

  test('circle collapses again when focus returns to the canvas', async ({ page }) => {
    await openCircle(page);
    await expect(page.locator('#tabura-circle')).toHaveAttribute('data-state', 'expanded');

    await page.mouse.click(420, 320);

    await expect(page.locator('#tabura-circle')).toHaveAttribute('data-state', 'collapsed');
  });

  test('artifact kind picks the default tool for the common case', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-transcript-default',
      title: 'meeting.txt',
      text: 'Transcript line one\nTranscript line two',
      meta: { artifact_kind: 'transcript' },
    });
    await expect(circleSegment(page, 'highlight')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'highlight');

    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-email-default',
      title: 'thread.eml',
      text: 'From: Ada\n\nHello',
      meta: { artifact_kind: 'email_thread' },
    });
    await expect(circleSegment(page, 'text_note')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'text_note');

    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-doc-default',
      title: 'notes.md',
      text: 'Alpha beta gamma',
      meta: { artifact_kind: 'markdown' },
    });
    const interaction = await page.evaluate(() => {
      const state = (window as any)._taburaApp?.getState?.();
      return {
        tool: state?.interaction?.tool,
        surface: state?.interaction?.surface,
      };
    });
    expect(interaction).toEqual({ tool: 'pointer', surface: 'editor' });
  });

  test('pen input switches into ink without a palette click', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-pen-default',
      title: 'transcript.txt',
      text: 'Alpha beta gamma delta',
      meta: { artifact_kind: 'transcript' },
    });
    await clearLog(page);

    const canvasBox = await page.locator('#canvas-text').boundingBox();
    if (!canvasBox) throw new Error('text canvas unavailable for pen test');
    await dispatchPenStroke(page, [
      { x: canvasBox.x + 80, y: canvasBox.y + 90, pressure: 0.7 },
      { x: canvasBox.x + 120, y: canvasBox.y + 130, pressure: 0.7 },
      { x: canvasBox.x + 160, y: canvasBox.y + 170, pressure: 0.7 },
    ]);

    await expect(circleSegment(page, 'ink')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'ink');
    await expect(page.locator('#ink-controls')).toBeVisible();
    const runtimeUpdates = (await getLog(page)).filter((entry) => entry.type === 'api_fetch' && entry.action === 'runtime_preferences');
    expect(runtimeUpdates).toHaveLength(0);
  });

  test('highlight tool marks selected text without entering editor mode', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-highlight-1',
      title: 'notes.md',
      text: 'Alpha beta gamma',
    });
    await expect(page.locator('#canvas-text')).toBeVisible();
    await page.locator('#surface-toggle').click();

    await clearLog(page);
    await page.keyboard.press('h');
    await waitForLogEntry(page, 'api_fetch', 'runtime_preferences');

    await page.evaluate(() => {
      const textNode = document.querySelector('#canvas-text p')?.firstChild;
      if (!(textNode instanceof Text)) {
        throw new Error('text node unavailable for highlight test');
      }
      const range = document.createRange();
      range.setStart(textNode, 0);
      range.setEnd(textNode, 5);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, button: 0 }));
    });

    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(1);
    const interaction = await page.evaluate(() => {
      const state = (window as any)._taburaApp?.getState?.();
      return {
        conversation: state?.interaction?.conversation,
        hasLegacyArtifactEditFlag: Object.prototype.hasOwnProperty.call(state || {}, 'artifactEditMode'),
        artifactEditorActive: document.body.classList.contains('artifact-edit-mode'),
      };
    });
    expect(interaction).toEqual({
      conversation: 'idle',
      hasLegacyArtifactEditFlag: false,
      artifactEditorActive: false,
    });
  });

  test('highlight notes persist across reload for text artifacts', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-highlight-note-1',
      title: 'persist.md',
      text: 'Alpha beta gamma',
    });
    await expect(page.locator('#canvas-text')).toBeVisible();
    await page.locator('#surface-toggle').click();
    await setInteractionTool(page, 'highlight');

    await page.evaluate(() => {
      const textNode = document.querySelector('#canvas-text p')?.firstChild;
      if (!(textNode instanceof Text)) {
        throw new Error('text node unavailable for note persistence test');
      }
      const range = document.createRange();
      range.setStart(textNode, 0);
      range.setEnd(textNode, 5);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, button: 0 }));
    });

    await expect(page.locator('.annotation-bubble')).toBeVisible();
    await page.locator('#annotation-note-input').fill('Needs follow-up');
    await page.locator('#annotation-note-save').click();
    await expect(page.locator('.canvas-annotation-badge')).toHaveText('1');
    await expect(page.locator('.annotation-bubble-note')).toContainText('Needs follow-up');

    await waitReady(page);
    await injectCanvasModuleRef(page);
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-highlight-note-2',
      title: 'persist.md',
      text: 'Alpha beta gamma',
    });
    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(1);
    await expect(page.locator('.canvas-annotation-badge')).toHaveText('1');
    await page.locator('.canvas-annotation-badge').click();
    await expect(page.locator('.annotation-bubble-note')).toContainText('Needs follow-up');
  });

  test('highlight tool persists notes on PDF text selections', async ({ page }) => {
    await injectCanvasModuleRef(page);
    await renderPdfArtifactMock(page);
    await setInteractionTool(page, 'highlight');

    await page.evaluate(() => {
      const textNode = document.querySelector('#canvas-pdf .textLayer span')?.firstChild;
      if (!(textNode instanceof Text)) {
        throw new Error('pdf text node unavailable for highlight test');
      }
      const range = document.createRange();
      range.setStart(textNode, 0);
      range.setEnd(textNode, 9);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, button: 0 }));
    });

    await expect(page.locator('#canvas-pdf .canvas-user-highlight.is-persistent')).toHaveCount(1);
    await page.locator('#annotation-note-input').fill('PDF anchor works');
    await page.locator('#annotation-note-save').click();
    await expect(page.locator('#canvas-pdf .canvas-annotation-badge')).toHaveText('1');
  });

  test('text note tool creates sticky notes on PDF positions', async ({ page }) => {
    await injectCanvasModuleRef(page);
    await renderPdfArtifactMock(page);
    await setInteractionTool(page, 'text_note');

    const pageBox = await page.locator('#canvas-pdf .canvas-pdf-page-inner').boundingBox();
    if (!pageBox) throw new Error('pdf page unavailable for sticky note test');
    await page.mouse.click(pageBox.x + 160, pageBox.y + 220);

    await expect(page.locator('#canvas-pdf .canvas-sticky-note')).toHaveCount(1);
    await expect(page.locator('#annotation-bubble')).toBeVisible();

    await page.locator('#annotation-note-input').fill('Margin note');
    await page.locator('#annotation-note-save').click();
    await expect(page.locator('#canvas-pdf .canvas-annotation-badge')).toHaveText('1');
  });

  test('tablet touch keeps PDF notes local until explicit bundle send', async ({ page }) => {
    await page.setViewportSize({ width: 834, height: 1112 });
    await injectCanvasModuleRef(page);
    await renderPdfArtifactMock(page);
    await setInteractionTool(page, 'text_note');
    await clearLog(page);

    const pageBox = await page.locator('#canvas-pdf .canvas-pdf-page-inner').boundingBox();
    if (!pageBox) throw new Error('pdf page unavailable for tablet touch note test');
    await dispatchTouchTap(page, pageBox.x + 180, pageBox.y + 240);

    await expect(page.locator('#canvas-pdf .canvas-sticky-note')).toHaveCount(1);
    await expect(page.locator('#annotation-bubble')).toBeVisible();

    await page.locator('#annotation-note-input').fill('Follow up on this section');
    await page.locator('#annotation-note-save').click();
    await expect(page.locator('#canvas-pdf .canvas-annotation-badge')).toHaveText('1');

    let sentMessages = (await getLog(page)).filter((entry) => entry.type === 'message_sent');
    expect(sentMessages).toHaveLength(0);

    await page.locator('#annotation-bundle-send').click();
    await waitForLogEntry(page, 'message_sent');

    sentMessages = (await getLog(page)).filter((entry) => entry.type === 'message_sent');
    expect(sentMessages).toHaveLength(1);
    expect(String(sentMessages[0]?.text || '')).toContain('Use these annotations as instructions for the current artifact.');
    expect(String(sentMessages[0]?.text || '')).toContain('PDF sticky note on page 1');
    expect(String(sentMessages[0]?.text || '')).toContain('text: Follow up on this section');
    await expect(page.locator('#canvas-pdf .canvas-annotation-badge')).toHaveCount(0);
  });

  test('ink tool persists page-anchored PDF strokes across rerender', async ({ page }) => {
    await injectCanvasModuleRef(page);
    await renderPdfArtifactMock(page);
    await setInteractionTool(page, 'ink');

    const pageBox = await page.locator('#canvas-pdf .canvas-pdf-page-inner').boundingBox();
    if (!pageBox) throw new Error('pdf page unavailable for ink test');
    await dispatchPenStroke(page, [
      { x: pageBox.x + 110, y: pageBox.y + 180, pressure: 0.7 },
      { x: pageBox.x + 170, y: pageBox.y + 210, pressure: 0.7 },
      { x: pageBox.x + 230, y: pageBox.y + 260, pressure: 0.7 },
    ]);

    await expect(page.locator('#canvas-pdf .canvas-ink-annotation')).toHaveCount(1);
    await expect(page.locator('#ink-controls')).toBeHidden();

    await renderPdfArtifactMock(page);
    await expect(page.locator('#canvas-pdf .canvas-ink-annotation')).toHaveCount(1);
  });

  test('highlight annotations stay local until bundle send', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-highlight-bundle-1',
      title: 'bundle.md',
      text: 'Alpha beta gamma delta',
    });
    await expect(page.locator('#canvas-text')).toBeVisible();
    await page.locator('#surface-toggle').click();
    await setInteractionTool(page, 'highlight');
    await clearLog(page);

    await page.evaluate(() => {
      const textNode = document.querySelector('#canvas-text p')?.firstChild;
      if (!(textNode instanceof Text)) {
        throw new Error('text node unavailable for annotation bundle test');
      }
      const range = document.createRange();
      range.setStart(textNode, 0);
      range.setEnd(textNode, 5);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, button: 0 }));
    });

    await page.locator('#annotation-note-input').fill('Needs follow-up');
    await page.locator('#annotation-note-save').click();
    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(1);

    let sentMessages = (await getLog(page)).filter((entry) => entry.type === 'message_sent');
    expect(sentMessages).toHaveLength(0);

    await page.locator('#annotation-bundle-send').click();
    await waitForLogEntry(page, 'message_sent');

    sentMessages = (await getLog(page)).filter((entry) => entry.type === 'message_sent');
    expect(sentMessages).toHaveLength(1);
    expect(String(sentMessages[0]?.text || '')).toContain('Revise the current artifact using these annotations.');
    expect(String(sentMessages[0]?.text || '')).toContain('Selection: "Alpha"');
    expect(String(sentMessages[0]?.text || '')).toContain('text: Needs follow-up');
    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(0);
  });

  test('double-click on an annotation sends it immediately', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-highlight-bundle-2',
      title: 'immediate.md',
      text: 'Alpha beta gamma delta',
    });
    await expect(page.locator('#canvas-text')).toBeVisible();
    await page.locator('#surface-toggle').click();
    await setInteractionTool(page, 'highlight');

    await page.evaluate(() => {
      const textNode = document.querySelector('#canvas-text p')?.firstChild;
      if (!(textNode instanceof Text)) {
        throw new Error('text node unavailable for immediate annotation test');
      }
      const range = document.createRange();
      range.setStart(textNode, 6);
      range.setEnd(textNode, 10);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, button: 0 }));
    });

    await page.locator('#annotation-note-input').fill('Ship this now');
    await page.locator('#annotation-note-save').click();
    await clearLog(page);

    await page.locator('#canvas-text .canvas-user-highlight.is-persistent').dblclick();
    await waitForLogEntry(page, 'message_sent');

    const sentMessages = (await getLog(page)).filter((entry) => entry.type === 'message_sent');
    expect(sentMessages).toHaveLength(1);
    expect(String(sentMessages[0]?.text || '')).toContain('Handle this annotation immediately instead of waiting for a larger bundle.');
    expect(String(sentMessages[0]?.text || '')).toContain('Selection: "beta"');
    expect(String(sentMessages[0]?.text || '')).toContain('text: Ship this now');
    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(0);
  });

  test('email artifacts opened from the sidebar default to annotate surface', async ({ page }) => {
    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 902,
          title: 'Answer triage email',
          state: 'inbox',
          artifact_id: 502,
          artifact_kind: 'email',
          artifact_title: 'Re: triage follow-up',
          updated_at: '2026-03-08 10:06:00',
        }],
      });
    });

    await page.locator('#edge-left-tap').click();
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await expect(page.locator('#canvas-text')).toContainText('Need a response before tomorrow morning.');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'text_note');
    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().interaction.surface);
    }).toBe('annotate');
  });

  test('scan upload imports annotations, allows correction, and confirms before send', async ({ page }) => {
    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 904,
          title: 'Review annotated printout',
          state: 'inbox',
          artifact_id: 701,
          artifact_kind: 'markdown',
          artifact_title: 'notes.md',
          updated_at: '2026-03-08 10:08:00',
        }],
      });
      (window as any).__setItemSidebarArtifacts({
        701: {
          id: 701,
          kind: 'markdown',
          title: 'notes.md',
          meta_json: JSON.stringify({ text: 'Line one\nLine two\nLine three' }),
        },
      });
      (window as any).__setScanUploadResponse({
        workspace_id: 'test',
        item_id: 904,
        artifact_id: 701,
        scan_artifact: { id: 990, kind: 'image', title: 'Scanned annotations' },
        annotations: [
          { content: 'check null case', anchor_text: 'Line two', line: 2, confidence: 0.91 },
        ],
      });
      (window as any).__setScanConfirmResponse({
        workspace_id: 'test',
        item_id: 904,
        artifact_id: 701,
        scan_artifact_id: 990,
        review_artifact: { id: 991, kind: 'annotation', title: 'Reviewed annotations' },
      });
    });

    await page.locator('#edge-left-tap').click();
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await expect(page.locator('#canvas-text')).toContainText('Line two');
    await expect(page.locator('#scan-upload-trigger')).toBeVisible();

    await page.locator('#scan-upload-trigger').click();
    await page.setInputFiles('#scan-upload-input', {
      name: 'annotated.png',
      mimeType: 'image/png',
      buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3xwAAAAASUVORK5CYII=', 'base64'),
    });
    await waitForLogEntry(page, 'api_fetch', 'scan_upload');

    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(1);
    await expect(page.locator('#annotation-selection-input')).toHaveValue('check null case');

    await page.locator('#annotation-selection-input').fill('check nil case');
    await page.locator('#annotation-selection-save').click();
    await clearLog(page);

    await page.locator('#annotation-bundle-send').click();
    await waitForLogEntry(page, 'api_fetch', 'scan_confirm');
    await waitForLogEntry(page, 'message_sent');

    const sent = (await getLog(page)).find((entry) => entry.type === 'message_sent');
    expect(String(sent?.text || '')).toContain('Selection: "check nil case"');
  });

  test('scan upload preserves paragraph mapping for email artifacts', async ({ page }) => {
    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 905,
          title: 'Review scanned reply notes',
          state: 'inbox',
          artifact_id: 702,
          artifact_kind: 'email',
          artifact_title: 'Re: follow-up',
          updated_at: '2026-03-08 10:09:00',
        }],
      });
      (window as any).__setItemSidebarArtifacts({
        702: {
          id: 702,
          kind: 'email',
          title: 'Re: follow-up',
          meta_json: JSON.stringify({ text: 'First paragraph.\n\nSecond paragraph.' }),
        },
      });
      (window as any).__setScanUploadResponse({
        workspace_id: 'test',
        item_id: 905,
        artifact_id: 702,
        scan_artifact: { id: 992, kind: 'image', title: 'Scanned email annotations' },
        annotations: [
          { content: 'reply here', anchor_text: 'Second paragraph.', paragraph: 2, confidence: 0.87 },
        ],
      });
      (window as any).__setScanConfirmResponse({
        workspace_id: 'test',
        item_id: 905,
        artifact_id: 702,
        scan_artifact_id: 992,
        review_artifact: { id: 993, kind: 'annotation', title: 'Reviewed email annotations' },
      });
    });

    await page.locator('#edge-left-tap').click();
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await page.locator('#pr-file-list .pr-file-item').first().click();
    await expect(page.locator('#canvas-text')).toContainText('Second paragraph.');

    await page.locator('#scan-upload-trigger').click();
    await page.setInputFiles('#scan-upload-input', {
      name: 'annotated-email.png',
      mimeType: 'image/png',
      buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3xwAAAAASUVORK5CYII=', 'base64'),
    });
    await waitForLogEntry(page, 'api_fetch', 'scan_upload');
    await expect(page.locator('#canvas-text .canvas-user-highlight.is-persistent')).toHaveCount(1);

    await clearLog(page);
    await page.locator('#annotation-bundle-send').click();
    await waitForLogEntry(page, 'api_fetch', 'scan_confirm');

    const confirmEntry = (await getLog(page)).find((entry) => entry.type === 'api_fetch' && entry.action === 'scan_confirm');
    const confirmPayload = (confirmEntry?.payload || {}) as any;
    const annotations = Array.isArray(confirmPayload.annotations) ? confirmPayload.annotations : [];
    expect(Number(annotations[0]?.paragraph || 0)).toBe(2);
  });

  test('email thread artifacts opened from the sidebar render thread text on annotate surface', async ({ page }) => {
    await page.evaluate(() => {
      (window as any).__setItemSidebarData({
        inbox: [{
          id: 903,
          title: 'Answer urgent follow-up',
          state: 'inbox',
          artifact_id: 505,
          artifact_kind: 'email_thread',
          artifact_title: 'Urgent follow-up',
          updated_at: '2026-03-08 10:07:00',
        }],
      });
    });

    await page.locator('#edge-left-tap').click();
    await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
    await page.locator('#pr-file-list .pr-file-item').first().click();

    await expect(page.locator('#canvas-text')).toContainText('Need a response before tomorrow morning.');
    await expect(page.locator('#canvas-text')).toContainText('I can confirm the review packet is ready.');
    await expect(page.locator('#canvas-text')).not.toContainText('- Kind:');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'text_note');
    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().interaction.surface);
    }).toBe('annotate');
  });

  test('text artifacts default to editor mode and can switch back to annotate', async ({ page }) => {
    await injectCanvasEvent(page, {
      kind: 'text_artifact',
      event_id: 'art-surface-1',
      title: 'notes.md',
      text: 'Editor first\nThen annotate',
    });
    await expect(page.locator('#canvas-text')).toBeVisible();

    await expect(page.locator('#surface-toggle')).toBeVisible();
    await expect(page.locator('#surface-toggle')).toHaveAttribute('aria-label', 'Switch to annotate');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'pointer');
    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().interaction.surface);
    }).toBe('editor');

    await page.locator('#surface-toggle').click();

    await expect(page.locator('#surface-toggle')).toHaveAttribute('aria-label', 'Switch to editor');
    await expect(page.locator('#tabura-circle-dot')).toHaveAttribute('data-tool', 'pointer');
    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().interaction.surface);
    }).toBe('annotate');
  });

  test('switching tools during live dialogue keeps continuous dialogue active', async ({ page }) => {
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

    await injectChatEvent(page, {
      type: 'system_action',
      action: { type: 'toggle_live_dialogue' },
    });
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');

    await clearLog(page);
    await page.keyboard.press('i');
    await waitForLogEntry(page, 'api_fetch', 'runtime_preferences');

    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
    await expect.poll(async () => page.evaluate(() => {
      const state = (window as any)._taburaApp?.getState?.();
      return {
        tool: state?.interaction?.tool,
        conversation: state?.interaction?.conversation,
        liveSessionActive: state?.liveSessionActive,
        liveSessionMode: state?.liveSessionMode,
      };
    })).toEqual({
      tool: 'ink',
      conversation: 'continuous_dialogue',
      liveSessionActive: true,
      liveSessionMode: 'dialogue',
    });
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

  test('top panel omits legacy execution policy controls', async ({ page }) => {
    await pinEdgeTop(page);
    await expect(page.locator('#edge-top-models .edge-yolo-btn')).toHaveCount(0);
    await expect(page.locator('#edge-top-models .edge-runtime-more-btn')).toHaveCount(0);

    await injectChatEvent(page, { type: 'mode_changed', mode: 'review' });
    await page.waitForTimeout(100);

    await expect(page.locator('#edge-top-models .edge-yolo-btn')).toHaveCount(0);
    await expect(page.locator('#edge-top-models .edge-runtime-more-btn')).toHaveCount(0);
  });
});

test.describe('approval_request event', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await page.evaluate(() => {
      document.getElementById('edge-right-tap')?.click();
    });
    await page.waitForTimeout(200);
  });

  test('renders approval card and sends approval response', async ({ page }) => {
    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      const ws = app?.getState?.().chatWs;
      (window as any).__approvalMessages = [];
      if (!ws) return;
      const originalSend = ws.send.bind(ws);
      ws.send = (data: string) => {
        try {
          (window as any).__approvalMessages.push(JSON.parse(String(data)));
        } catch (_) {}
        originalSend(data);
      };
    });

    await injectChatEvent(page, {
      type: 'approval_request',
      request_id: 'approval-1',
      action: 'command_execution',
      description: 'Allow command execution: run git status',
      reason: 'run git status',
    });

    const card = page.locator('.chat-approval-request').last();
    await expect(card.locator('.chat-approval-title')).toHaveText('Allow command execution: run git status');
    await expect(card.locator('.chat-approval-detail')).toHaveText('run git status');
    await page.locator('.chat-approval-request').getByRole('button', { name: 'Approve' }).click();

    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__approvalMessages || []);
    }).toEqual([
      {
        type: 'approval_response',
        request_id: 'approval-1',
        decision: 'accept',
      },
    ]);

    await injectChatEvent(page, { type: 'approval_resolved', request_id: 'approval-1', decision: 'accept' });
    await expect(card.locator('.chat-approval-status')).toHaveText('Approved');
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
    await expect(page.locator('#overlay')).toBeHidden();

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

  test('error event updates chat and status while keeping the overlay hidden', async ({ page }) => {
    await page.keyboard.type('test');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    await injectChatEvent(page, { type: 'turn_started', turn_id: 'err-1' });
    await page.waitForTimeout(100);

    await injectChatEvent(page, { type: 'error', turn_id: 'err-1', error: 'backend failed' });
    await page.waitForTimeout(100);

    const overlay = page.locator('#overlay');
    await expect(overlay).toBeHidden();
    await expect(page.locator('#chat-history')).toContainText('backend failed');
    await expect(page.locator('#status-text')).toContainText('backend failed');
  });
});


// =============================================================================
// Live Dialogue: multi-turn loop
// =============================================================================

test.describe('Live Dialogue multi-turn', () => {
  async function switchToTestProject(page: Page) {
    await switchToWorkspace(page, 'test');
  }

  async function waitForEdgeButtons(page: Page) {
    await waitForCircleControls(page);
  }

  async function enableConversationMode(page: Page) {
    await switchToTestProject(page);
    await waitForEdgeButtons(page);
    await setLiveMode(page, 'dialogue');
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
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
      (window as any).__taburaConversationListenMs = 1200;
    });
  });

  test('TTS playback completion triggers listen window in Dialogue', async ({ page }) => {
    await enableConversationMode(page);
    await clearLog(page);

    await triggerVoiceAssistantTTS(page, 'conv-1');

    // The dialogue loop now surfaces listen readiness through lifecycle state
    // while the companion view can suppress the floating indicator.
    await expect.poll(async () => page.evaluate(() => {
      const app = (window as any)._taburaApp;
      const state = app?.getState?.();
      return Boolean(state?.liveSessionDialogueListenActive)
        && String(state?.voiceLifecycle || '') === 'listening';
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

    // Even with empty message, Dialogue should not stall
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
    await setInteractionTool(page, 'text_note');

    await dispatchPrintableKey(page, 'a');
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

  test('silent mode persists in runtime preferences', async ({ page }) => {
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

    const stored = await page.evaluate(() => {
      return (window as any).__getRuntimeState?.().silent_mode ?? null;
    });
    expect(stored).toBe(true);
  });

  test('system toggle_live_dialogue enters live dialogue', async ({ page }) => {
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

    await injectChatEvent(page, {
      type: 'system_action',
      action: { type: 'toggle_live_dialogue' },
    });
    await page.waitForTimeout(200);

    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
  });

  test('system_action_suppressed shows duplicate command status', async ({ page }) => {
    await injectChatEvent(page, {
      type: 'system_action_suppressed',
      action: {
        type: 'system_action_suppressed',
        action_type: 'shell',
        status: 'cooldown_suppressed',
        cooldown_ms: 1500,
      },
    });

    await expect(page.locator('#status-text')).toContainText('duplicate shell suppressed during cooldown');
  });
});


test.describe('system_action model and project switching', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
  });

  test('switch_workspace triggers project activate API', async ({ page }) => {
    await seedTwoProjects(page);
    await clearLog(page);

    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'switch_workspace',
        workspace_id: 'notes',
      },
    });

    await expect.poll(async () => {
      const log = await getLog(page);
      return log.some(
        (entry) => entry.type === 'api_fetch'
          && entry.action === 'project_activate'
          && String(entry.payload?.workspace_id || '') === 'notes',
      );
    }, { timeout: 5_000 }).toBe(true);

    await expect.poll(async () => {
      return page.evaluate(() => (window as any)._taburaApp?.getState?.().activeWorkspaceId || '');
    }, { timeout: 5_000 }).toBe('notes');
  });
});

test.describe('system_action print item', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await clearLog(page);
  });

  test('loads the print view into the hidden print iframe', async ({ page }) => {
    await injectChatEvent(page, {
      type: 'system_action',
      action: {
        type: 'print_item',
        item_id: 42,
        url: '/api/items/42/print',
      },
    });

    await expect.poll(async () => {
      return page.evaluate(() => {
        const frame = document.getElementById('print-frame');
        if (!(frame instanceof HTMLIFrameElement)) return '';
        return String(frame.getAttribute('src') || '');
      });
    }, { timeout: 5_000 }).toContain('/api/items/42/print');

    const log = await getLog(page);
    const printEntry = log.find((entry) => entry.type === 'print' && entry.action === 'open');
    expect(String(printEntry?.url || '')).toContain('/api/items/42/print');
    await expect(page.locator('#status-label')).toHaveText('print view opened');
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
    await setInteractionTool(page, 'prompt');
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

  test('right-click opens bottom composer on mobile', async ({ page }) => {
    // Simulate contextmenu via evaluate since mobile doesn't have right-click
    await page.evaluate(() => {
      const ev = new MouseEvent('contextmenu', { clientX: 187, clientY: 333, bubbles: true });
      document.getElementById('canvas-viewport')?.dispatchEvent(ev);
    });
    await page.waitForTimeout(100);
    await expect(page.locator('#edge-right')).toHaveClass(/edge-pinned/);
    await expect(page.locator('#chat-pane-input')).toBeFocused();
  });
});


// =============================================================================
// Voice lifecycle: STT result -> message submission
// =============================================================================

test.describe('voice-to-message flow', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
    await setInteractionTool(page, 'prompt');
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
    await setInteractionTool(page, 'prompt');
  });

  test('text input -> turn started -> streaming -> final output -> dismiss', async ({ page }) => {
    await setInteractionTool(page, 'text_note');

    // Submit message
    await dispatchPrintableKey(page, 'e');
    await page.keyboard.type('xplain this');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(200);

    // Turn started keeps the overlay hidden
    await injectChatEvent(page, { type: 'turn_started', turn_id: 'full-1' });
    await page.waitForTimeout(100);
    await expect(page.locator('#overlay')).toBeHidden();

    // Streaming response updates the pending assistant row
    await injectChatEvent(page, {
      type: 'assistant_message',
      turn_id: 'full-1',
      message: 'Here is the explanation.',
      delta: 'Here is the explanation.',
    });
    await page.waitForTimeout(100);
    await expect(page.locator('#chat-history .chat-message.chat-assistant').first()).toContainText('explanation');

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
    await expect(page.locator('#overlay')).toBeHidden();

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

    await page.getByRole('button', { name: 'Files' }).click();
    await expect(page.locator('.sidebar-tab.is-active')).toContainText('Files');
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
