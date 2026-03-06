import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  action?: string;
  [key: string]: unknown;
};

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function clearLog(page: Page) {
  await page.evaluate(() => { (window as any).__harnessLog.splice(0); });
}

async function waitForLogEntry(page: Page, type: string, action: string) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some(e => e.type === type && e.action === action);
  }, { timeout: 5_000 }).toBe(true);
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
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
});

test('participant WS start sends participant_start and receives participant_started', async ({ page }) => {
  await clearLog(page);

  await page.evaluate(() => {
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    if (chatWs) {
      chatWs.send(JSON.stringify({ type: 'participant_start' }));
    }
  });

  await waitForLogEntry(page, 'participant', 'start');
  const log = await getLog(page);
  expect(log.some(e => e.type === 'participant' && e.action === 'start')).toBe(true);
});

test('participant WS stop sends participant_stop and receives participant_stopped', async ({ page }) => {
  await clearLog(page);

  await page.evaluate(() => {
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    if (chatWs) {
      chatWs.send(JSON.stringify({ type: 'participant_start' }));
    }
  });
  await waitForLogEntry(page, 'participant', 'start');

  await page.evaluate(() => {
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    if (chatWs) {
      chatWs.send(JSON.stringify({ type: 'participant_stop' }));
    }
  });
  await waitForLogEntry(page, 'participant', 'stop');
  const log = await getLog(page);
  expect(log.some(e => e.type === 'participant' && e.action === 'stop')).toBe(true);
});

test('segment injection via __injectParticipantSegment helper works', async ({ page }) => {
  await clearLog(page);

  const received = await page.evaluate(() => {
    return new Promise<boolean>((resolve) => {
      const sessions = (window as any).__mockWsSessions || [];
      const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
      if (!chatWs) { resolve(false); return; }

      const origHandler = chatWs.onmessage;
      let gotSegment = false;
      chatWs.onmessage = (ev: any) => {
        try {
          const msg = JSON.parse(typeof ev.data === 'string' ? ev.data : '{}');
          if (msg.type === 'participant_segment_text') {
            gotSegment = true;
          }
        } catch (_) {}
        if (origHandler) origHandler(ev);
      };

      (window as any).__injectParticipantSegment({ text: 'injected text', segment_id: 42 });

      setTimeout(() => {
        chatWs.onmessage = origHandler;
        resolve(gotSegment);
      }, 50);
    });
  });
  expect(received).toBe(true);
});

test('participant config API returns audio_persistence=none', async ({ page }) => {
  const config = await page.evaluate(async () => {
    const resp = await fetch('/api/participant/config');
    return resp.json();
  });
  expect(config.audio_persistence).toBe('none');
  expect(config.language).toBeTruthy();
});

test('participant config PUT cannot override audio_persistence', async ({ page }) => {
  const config = await page.evaluate(async () => {
    const resp = await fetch('/api/participant/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ language: 'de', audio_persistence: 'disk' }),
    });
    return resp.json();
  });
  expect(config.audio_persistence).toBe('none');
  expect(config.language).toBe('de');
});

test('participant capture sends 16k wav segments and clears rolling buffer after ship', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const waitFor = async (check: () => boolean) => {
      const deadline = Date.now() + 1_000;
      while (Date.now() < deadline) {
        if (check()) return;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error('timed out waiting for participant segment flush');
    };
    const vadMock = (window as any).__taburaVadMock;
    const originalCreate = vadMock.create;
    let callbacks: any = null;
    vadMock.create = (next: any) => {
      callbacks = next;
      return {
        start() {},
        pause() {},
        destroy() {},
      };
    };

    try {
      const { ParticipantCapture } = await import('/internal/web/static/participant-capture.js');
      const ws = {
        readyState: (window as any).WebSocket.OPEN,
        sent: [] as any[],
        send(data: any) {
          this.sent.push(data);
        },
      };
      const capture = new ParticipantCapture({ sessionRamCapMB: 0.02, maxSegmentDurationMS: 8_000 });
      await capture.start(ws as any);
      callbacks.onSpeechEnd(new Float32Array(1_600).fill(0.25));
      await waitFor(() => ws.sent.some((entry: any) => entry instanceof Blob) && capture.sessionBufferedChunks === 1);

      const blob = ws.sent.find((entry: any) => entry instanceof Blob);
      const bytes = blob ? new Uint8Array(await blob.arrayBuffer()) : new Uint8Array();
      return {
        blobType: blob?.type || '',
        sentCount: ws.sent.length,
        pendingSegmentSamples: capture.pendingSegmentSamples,
        sessionBufferedChunks: capture.sessionBufferedChunks,
        sessionBufferedBytes: capture.sessionBufferedBytes,
        riff: String.fromCharCode(...bytes.slice(0, 4)),
        wave: String.fromCharCode(...bytes.slice(8, 12)),
      };
    } finally {
      vadMock.create = originalCreate;
    }
  });

  expect(result.sentCount).toBe(2);
  expect(result.blobType).toBe('audio/wav');
  expect(result.riff).toBe('RIFF');
  expect(result.wave).toBe('WAVE');
  expect(result.pendingSegmentSamples).toBe(0);
  expect(result.sessionBufferedChunks).toBe(1);
  expect(result.sessionBufferedBytes).toBeGreaterThan(44);
});

test('participant capture zeroizes RAM buffers on stop', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const waitFor = async (check: () => boolean) => {
      const deadline = Date.now() + 1_000;
      while (Date.now() < deadline) {
        if (check()) return;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error('timed out waiting for participant buffer fill');
    };
    const vadMock = (window as any).__taburaVadMock;
    const originalCreate = vadMock.create;
    let callbacks: any = null;
    vadMock.create = (next: any) => {
      callbacks = next;
      return {
        start() {},
        pause() {},
        destroy() {},
      };
    };

    try {
      const { ParticipantCapture } = await import('/internal/web/static/participant-capture.js');
      const ws = {
        readyState: (window as any).WebSocket.OPEN,
        sent: [] as any[],
        send(data: any) {
          this.sent.push(data);
        },
      };
      const capture = new ParticipantCapture({ sessionRamCapMB: 0.02, maxSegmentDurationMS: 8_000 });
      await capture.start(ws as any);
      callbacks.onSpeechEnd(new Float32Array(1_600).fill(0.25));
      await waitFor(() => capture.sessionBufferedChunks === 1 && capture.sessionBufferedBytes > 44);

      const beforeStop = {
        pendingSegmentSamples: capture.pendingSegmentSamples,
        sessionBufferedChunks: capture.sessionBufferedChunks,
        sessionBufferedBytes: capture.sessionBufferedBytes,
      };
      capture.stop();
      const controlMessages = ws.sent
        .filter((entry: any) => typeof entry === 'string')
        .map((entry: string) => JSON.parse(entry).type);
      return {
        beforeStop,
        afterStop: {
          pendingSegmentSamples: capture.pendingSegmentSamples,
          sessionBufferedChunks: capture.sessionBufferedChunks,
          sessionBufferedBytes: capture.sessionBufferedBytes,
        },
        controlMessages,
      };
    } finally {
      vadMock.create = originalCreate;
    }
  });

  expect(result.beforeStop.sessionBufferedChunks).toBe(1);
  expect(result.beforeStop.sessionBufferedBytes).toBeGreaterThan(44);
  expect(result.afterStop.pendingSegmentSamples).toBe(0);
  expect(result.afterStop.sessionBufferedChunks).toBe(0);
  expect(result.afterStop.sessionBufferedBytes).toBe(0);
  expect(result.controlMessages).toEqual(['participant_start', 'participant_stop']);
});

test('participant capture drops oldest session chunks when RAM cap is reached', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const waitFor = async (check: () => boolean) => {
      const deadline = Date.now() + 1_000;
      while (Date.now() < deadline) {
        if (check()) return;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error('timed out waiting for participant segment flush');
    };
    const vadMock = (window as any).__taburaVadMock;
    const originalCreate = vadMock.create;
    let callbacks: any = null;
    vadMock.create = (next: any) => {
      callbacks = next;
      return {
        start() {},
        pause() {},
        destroy() {},
      };
    };

    try {
      const { ParticipantCapture } = await import('/internal/web/static/participant-capture.js');
      const ws = {
        readyState: (window as any).WebSocket.OPEN,
        sent: [] as any[],
        send(data: any) {
          this.sent.push(data);
        },
      };
      const capMB = 0.005;
      const capture = new ParticipantCapture({ sessionRamCapMB: capMB, maxSegmentDurationMS: 8_000 });
      await capture.start(ws as any);
      const segmentCount = () => ws.sent.filter((entry: any) => entry instanceof Blob).length;
      callbacks.onSpeechEnd(new Float32Array(1_600).fill(0.25));
      await waitFor(() => segmentCount() === 1 && capture.sessionBufferedChunks === 1);
      callbacks.onSpeechEnd(new Float32Array(1_600).fill(0.25));
      await waitFor(() => segmentCount() === 2 && capture.sessionBufferedChunks === 1);
      callbacks.onSpeechEnd(new Float32Array(1_600).fill(0.25));
      await waitFor(() => segmentCount() === 3 && capture.sessionBufferedChunks === 1);
      return {
        sentCount: ws.sent.length,
        sessionBufferedChunks: capture.sessionBufferedChunks,
        sessionBufferedBytes: capture.sessionBufferedBytes,
        capBytes: Math.floor(capMB * 1024 * 1024),
      };
    } finally {
      vadMock.create = originalCreate;
    }
  });

  expect(result.sentCount).toBe(4);
  expect(result.sessionBufferedChunks).toBe(1);
  expect(result.sessionBufferedBytes).toBeGreaterThan(44);
  expect(result.sessionBufferedBytes).toBeLessThanOrEqual(result.capBytes);
});

test('participant capture clears RAM buffers on error without touching web storage', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const waitFor = async (check: () => boolean) => {
      const deadline = Date.now() + 1_000;
      while (Date.now() < deadline) {
        if (check()) return;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error('timed out waiting for participant buffer fill');
    };
    const beforeStorageKeys = Object.keys(window.localStorage).sort();
    const beforeDBs = typeof indexedDB.databases === 'function'
      ? (await indexedDB.databases()).map((db) => db.name || '').sort()
      : [];
    const vadMock = (window as any).__taburaVadMock;
    const originalCreate = vadMock.create;
    let callbacks: any = null;
    vadMock.create = (next: any) => {
      callbacks = next;
      return {
        start() {},
        pause() {},
        destroy() {},
      };
    };

    try {
      const { ParticipantCapture } = await import('/internal/web/static/participant-capture.js');
      const ws = {
        readyState: (window as any).WebSocket.OPEN,
        sent: [] as any[],
        send(data: any) {
          this.sent.push(data);
        },
      };
      const capture = new ParticipantCapture({ sessionRamCapMB: 0.02, maxSegmentDurationMS: 8_000 });
      await capture.start(ws as any);
      callbacks.onSpeechEnd(new Float32Array(1_600).fill(0.25));
      await waitFor(() => capture.sessionBufferedChunks === 1);
      capture.handleMessage({ type: 'participant_error', error: 'forced test error' });
      const afterStorageKeys = Object.keys(window.localStorage).sort();
      const afterDBs = typeof indexedDB.databases === 'function'
        ? (await indexedDB.databases()).map((db) => db.name || '').sort()
        : [];
      return {
        pendingSegmentSamples: capture.pendingSegmentSamples,
        sessionBufferedChunks: capture.sessionBufferedChunks,
        sessionBufferedBytes: capture.sessionBufferedBytes,
        beforeStorageKeys,
        afterStorageKeys,
        beforeDBs,
        afterDBs,
      };
    } finally {
      vadMock.create = originalCreate;
    }
  });

  expect(result.pendingSegmentSamples).toBe(0);
  expect(result.sessionBufferedChunks).toBe(0);
  expect(result.sessionBufferedBytes).toBe(0);
  expect(result.afterStorageKeys).toEqual(result.beforeStorageKeys);
  expect(result.afterDBs).toEqual(result.beforeDBs);
});
