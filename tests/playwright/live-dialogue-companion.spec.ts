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

async function setDialogueListenWindowMs(page: Page, ms: number) {
  await page.evaluate((value) => {
    (window as any).__taburaConversationListenMs = value;
  }, ms);
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

async function enableCompanion(page: Page) {
  await page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    if (!s) throw new Error('app state unavailable');
    s.companionEnabled = true;
    s.companionIdleSurface = 'robot';
  });
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

async function triggerHotword(page: Page) {
  await page.evaluate(() => {
    (window as any).__triggerHotwordDetection();
  });
}

async function waitForHotwordStart(page: Page) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'hotword' && entry.action === 'start');
  }, { timeout: 4_000 }).toBe(true);
}

test.beforeEach(async ({ page }) => {
  await waitReady(page);
});

test('companion face stays idle until dialogue speech is detected', async ({ page }) => {
  await setDialogueListenWindowMs(page, 3_000);
  await setDialogueMode(page, true);
  await enableCompanion(page);
  await page.evaluate(() => {
    (window as any)._taburaApp?.syncCompanionIdleSurface?.();
  });
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 12 }, () => -80),
    ]);
  });
  await clearLog(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const surface = document.getElementById('companion-idle-surface');
    return surface?.dataset.state || '';
  })).toBe('idle');
});

test('"recording..." status text suppressed when companion visible', async ({ page }) => {
  await setDialogueListenWindowMs(page, 3_000);
  await setDialogueMode(page, true);
  await enableCompanion(page);
  await page.evaluate(() => {
    (window as any)._taburaApp?.syncCompanionIdleSurface?.();
  });
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 12 }, () => -80),
    ]);
  });
  await clearLog(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  const statusText = await page.evaluate(() => {
    const el = document.getElementById('status-text');
    return el?.textContent || '';
  });
  expect(statusText).not.toBe('recording...');
});

test('companion reaches thinking state during assistant turn', async ({ page }) => {
  await setDialogueListenWindowMs(page, 3_000);
  await setDialogueMode(page, true);
  await enableCompanion(page);
  await page.evaluate(() => {
    (window as any)._taburaApp?.syncCompanionIdleSurface?.();
  });
  await clearLog(page);

  // Set up voice-awaiting-turn state to enter thinking
  await page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app.getState();
    s.lastInputOrigin = 'voice';
    s.voiceAwaitingTurn = true;
    app?.syncCompanionIdleSurface?.();
  });
  await injectChatEvent(page, { type: 'turn_started', turn_id: 'companion-trans-1' });

  // Should reach thinking state (awaiting turn / assistant working)
  await expect.poll(async () => page.evaluate(() => {
    const surface = document.getElementById('companion-idle-surface');
    return surface?.dataset.state || '';
  }), { timeout: 5_000 }).toBe('thinking');

  // Complete the turn so TTS fires, then returns to idle/listening
  await injectChatEvent(page, { type: 'assistant_message', turn_id: 'companion-trans-1', message: 'Done.' });
  await injectChatEvent(page, { type: 'assistant_output', role: 'assistant', turn_id: 'companion-trans-1', message: 'Done.' });

  await expect.poll(async () => page.evaluate(() => {
    const surface = document.getElementById('companion-idle-surface');
    const st = surface?.dataset.state || '';
    return st === 'idle' || st === 'listening';
  }), { timeout: 5_000 }).toBe(true);
});
