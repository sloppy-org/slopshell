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
