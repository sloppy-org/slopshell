import { expect, type Page } from './live';

export type LiveCircleSegment =
  | 'dialogue'
  | 'meeting'
  | 'silent'
  | 'fast'
  | 'prompt'
  | 'text_note'
  | 'pointer'
  | 'highlight'
  | 'ink';

type PointerMode = 'mouse' | 'touch';

export function circleSegment(page: Page, segment: LiveCircleSegment) {
  return page.locator(`#tabura-circle-menu .tabura-circle-segment[data-segment="${segment}"]`);
}

export async function waitForLiveAppReady(page: Page) {
  await expect(page.locator('#workspace')).toBeVisible();
  await expect(page.locator('#canvas-column')).toBeVisible();
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const state = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return state?.chatWs?.readyState === wsOpen && state?.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 15_000 });
}

export async function openCircle(page: Page, pointer: PointerMode = 'mouse') {
  const root = page.locator('#tabura-circle');
  if ((await root.getAttribute('data-state')) === 'expanded') {
    return;
  }
  await triggerLocator(page, page.locator('#tabura-circle-dot'), pointer);
  await expect(root).toHaveAttribute('data-state', 'expanded');
}

async function triggerLocator(page: Page, locator: ReturnType<Page['locator']>, pointer: PointerMode) {
  if (pointer === 'touch') {
    await locator.tap();
    return;
  }
  await locator.click();
}

export async function clickCircleSegment(page: Page, segment: LiveCircleSegment, pointer: PointerMode = 'mouse') {
  await openCircle(page, pointer);
  const target = circleSegment(page, segment);
  await expect(target).toBeVisible();
  await triggerLocator(page, target, pointer);
}

export async function setCircleToggle(
  page: Page,
  segment: 'silent' | 'fast',
  enabled: boolean,
  pointer: PointerMode = 'mouse',
) {
  const target = circleSegment(page, segment);
  const expected = enabled ? 'true' : 'false';
  if ((await target.getAttribute('aria-pressed')) !== expected) {
    await clickCircleSegment(page, segment, pointer);
  }
  await expect(target).toHaveAttribute('aria-pressed', expected);
}

export async function setLiveSession(
  page: Page,
  mode: 'dialogue' | 'meeting',
  enabled: boolean,
  pointer: PointerMode = 'mouse',
) {
  const target = circleSegment(page, mode);
  if (enabled) {
    if ((await target.getAttribute('aria-pressed')) !== 'true') {
      await clickCircleSegment(page, mode, pointer);
    }
    await expect(target).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText(mode === 'meeting' ? 'Meeting' : 'Dialogue');
    return;
  }
  await page.evaluate(async () => {
    const mod = await import('/internal/web/static/app-workspace-runtime.js');
    await mod.deactivateLiveSession({ disableMeetingConfig: true, silent: true });
  });
  await expect(target).toHaveAttribute('aria-pressed', 'false');
  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Manual');
}

export async function setInteractionTool(
  page: Page,
  tool: 'prompt' | 'text_note' | 'pointer' | 'highlight' | 'ink',
  pointer: PointerMode = 'mouse',
) {
  await clickCircleSegment(page, tool, pointer);
  await expect(circleSegment(page, tool)).toHaveAttribute('aria-pressed', 'true');
}

export async function resetCircleRuntimeState(page: Page, pointer: PointerMode = 'mouse') {
  await openCircle(page, pointer);
  await setLiveSession(page, 'meeting', true, pointer);
  await setCircleToggle(page, 'silent', false, pointer);
  await setCircleToggle(page, 'fast', false, pointer);
  await setInteractionTool(page, 'pointer', pointer);
}

export async function assertCircleNoOverlap(page: Page, tolerancePx = 1) {
  await openCircle(page);
  const metrics = await page.evaluate((tolerance) => {
    const ids = ['dialogue', 'meeting', 'silent', 'fast', 'prompt', 'text_note', 'pointer', 'highlight', 'ink'];
    const rects = ids.map((id) => {
      const node = document.querySelector(`#tabura-circle-menu .tabura-circle-segment[data-segment="${id}"]`);
      if (!(node instanceof HTMLElement)) {
        throw new Error(`missing circle segment: ${id}`);
      }
      const rect = node.getBoundingClientRect();
      return {
        id,
        left: rect.left,
        right: rect.right,
        top: rect.top,
        bottom: rect.bottom,
      };
    });
    const overlapPairs: Array<{ a: string; b: string; overlapX: number; overlapY: number }> = [];
    for (let i = 0; i < rects.length; i += 1) {
      for (let j = i + 1; j < rects.length; j += 1) {
        const a = rects[i]!;
        const b = rects[j]!;
        const overlapX = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        const overlapY = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (overlapX > tolerance && overlapY > tolerance) {
          overlapPairs.push({ a: a.id, b: b.id, overlapX, overlapY });
        }
      }
    }
    return { rects, overlapPairs };
  }, tolerancePx);
  expect(metrics.overlapPairs, JSON.stringify(metrics, null, 2)).toEqual([]);
}

export async function submitPrompt(page: Page, text: string) {
  const input = page.locator('#chat-pane-input');
  await expect(input).toBeVisible();
  await input.fill(text);
  await input.press('Enter');
}

export async function assistantReplyCount(page: Page) {
  return page.locator('#chat-history .chat-message.chat-assistant:not(.is-pending)').count();
}

export async function waitForAssistantReply(
  page: Page,
  previousCount: number,
  needle = '',
  timeoutMs = 90_000,
) {
  const expectedNeedle = needle.trim().toLowerCase();
  const assistantRows = page.locator('#chat-history .chat-message.chat-assistant:not(.is-pending)');
  await expect.poll(async () => {
    const errorText = await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      return String(app?.getState?.().assistantLastError || '').trim();
    });
    if (errorText) return `error:${errorText}`;
    const count = await assistantRows.count();
    if (count <= previousCount) return 'waiting';
    if (!expectedNeedle) return 'ready';
    const text = String(await assistantRows.last().textContent() || '').trim().toLowerCase();
    return text.includes(expectedNeedle) ? 'ready' : `text:${text}`;
  }, {
    timeout: timeoutMs,
    intervals: [500, 1_000, 2_000, 4_000],
  }).toBe('ready');
  return String(await assistantRows.last().textContent() || '').trim();
}

export async function browserWsTtsToStt(page: Page, text: string, mode: 'http' | 'ws', lang = 'en') {
  return page.evaluate(async ({ prompt, routeMode, ttsLang }) => {
    const runtimeResp = await fetch('/api/runtime/workspaces');
    if (!runtimeResp.ok) {
      throw new Error(`/api/runtime/workspaces failed: HTTP ${runtimeResp.status}`);
    }
    const runtimeBody = await runtimeResp.json();
    const sessionID = String(runtimeBody?.workspaces?.[0]?.chat_session_id || '').trim();
    if (!sessionID) {
      throw new Error('chat_session_id missing from runtime workspaces');
    }
    const wsURL = new URL(`/ws/chat/${encodeURIComponent(sessionID)}`, window.location.href);
    wsURL.protocol = wsURL.protocol === 'https:' ? 'wss:' : 'ws:';

    function openSocket() {
      return new Promise<WebSocket>((resolve, reject) => {
        const ws = new WebSocket(wsURL.toString());
        ws.binaryType = 'arraybuffer';
        ws.addEventListener('open', () => resolve(ws), { once: true });
        ws.addEventListener('error', () => reject(new Error(`websocket connect failed: ${wsURL}`)), { once: true });
      });
    }

    function waitForJSON(ws: WebSocket, predicate: (msg: any) => boolean, timeoutMs: number) {
      return new Promise<any>((resolve, reject) => {
        const timer = window.setTimeout(() => {
          cleanup();
          reject(new Error(`JSON websocket wait timed out after ${timeoutMs}ms`));
        }, timeoutMs);
        const onMessage = (event: MessageEvent) => {
          if (typeof event.data !== 'string') return;
          let parsed: any;
          try {
            parsed = JSON.parse(event.data);
          } catch {
            return;
          }
          if (!predicate(parsed)) return;
          cleanup();
          resolve(parsed);
        };
        const cleanup = () => {
          window.clearTimeout(timer);
          ws.removeEventListener('message', onMessage);
        };
        ws.addEventListener('message', onMessage);
      });
    }

    function waitForBinary(ws: WebSocket, timeoutMs: number) {
      return new Promise<Uint8Array>((resolve, reject) => {
        const timer = window.setTimeout(() => {
          cleanup();
          reject(new Error(`binary websocket wait timed out after ${timeoutMs}ms`));
        }, timeoutMs);
        const onMessage = async (event: MessageEvent) => {
          if (typeof event.data === 'string') return;
          let bytes: Uint8Array;
          if (event.data instanceof ArrayBuffer) {
            bytes = new Uint8Array(event.data);
          } else if (ArrayBuffer.isView(event.data)) {
            bytes = new Uint8Array(event.data.buffer);
          } else if (event.data instanceof Blob) {
            bytes = new Uint8Array(await event.data.arrayBuffer());
          } else {
            return;
          }
          cleanup();
          resolve(bytes);
        };
        const cleanup = () => {
          window.clearTimeout(timer);
          ws.removeEventListener('message', onMessage);
        };
        ws.addEventListener('message', onMessage);
      });
    }

    const ttsWS = await openSocket();
    ttsWS.send(JSON.stringify({ type: 'tts_speak', text: prompt, lang: ttsLang }));
    const wav = await waitForBinary(ttsWS, 30_000);
    ttsWS.close();

    if (routeMode === 'http') {
      const form = new FormData();
      form.append('mime_type', 'audio/wav');
      form.append('file', new Blob([wav], { type: 'audio/wav' }), 'tts-roundtrip.wav');
      const sttResp = await fetch('/api/stt/transcribe', {
        method: 'POST',
        body: form,
      });
      const sttBody = await sttResp.json();
      return {
        mode: routeMode,
        wavBytes: wav.byteLength,
        status: sttResp.status,
        transcript: String(sttBody?.text || '').trim(),
      };
    }

    const sttWS = await openSocket();
    sttWS.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
    await waitForJSON(sttWS, (message) => message?.type === 'stt_started', 10_000);
    sttWS.send(wav);
    sttWS.send(JSON.stringify({ type: 'stt_stop' }));
    const result = await waitForJSON(
      sttWS,
      (message) => ['stt_result', 'stt_empty', 'stt_error'].includes(String(message?.type || '')),
      60_000,
    );
    sttWS.close();
    return {
      mode: routeMode,
      wavBytes: wav.byteLength,
      status: result?.type,
      transcript: String(result?.text || '').trim(),
    };
  }, { prompt: text, routeMode: mode, ttsLang: lang });
}

export async function submitVoiceTranscript(
  page: Page,
  text: string,
  options: { silent?: boolean; fast?: boolean } = {},
) {
  return page.evaluate(async ({ prompt, silentMode, fastMode }) => {
    const runtimeResp = await fetch('/api/runtime/workspaces');
    if (!runtimeResp.ok) {
      throw new Error(`/api/runtime/workspaces failed: HTTP ${runtimeResp.status}`);
    }
    const runtimeBody = await runtimeResp.json();
    const sessionID = String(runtimeBody?.workspaces?.[0]?.chat_session_id || '').trim();
    if (!sessionID) {
      throw new Error('chat_session_id missing from runtime workspaces');
    }
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(sessionID)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        text: prompt,
        output_mode: silentMode ? 'silent' : 'voice',
        capture_mode: 'voice',
        fast_mode: fastMode,
      }),
    });
    return {
      status: resp.status,
      body: await resp.text(),
    };
  }, {
    prompt: text,
    silentMode: options.silent !== false,
    fastMode: Boolean(options.fast),
  });
}
