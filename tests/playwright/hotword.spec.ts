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

async function waitReady(page: Page, query = '') {
  await page.goto(`/tests/playwright/harness.html${query}`);
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
  await page.evaluate(() => {
    window.localStorage.removeItem('tabura.conversationMode');
  });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
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

async function setConversationListenWindowMs(page: Page, ms: number) {
  await page.evaluate((value) => {
    (window as any).__taburaConversationListenMs = value;
  }, ms);
}

async function waitForEdgeButtons(page: Page) {
  await expect.poll(async () => page.evaluate(() => {
    const conv = document.querySelector('#edge-top-models .edge-conv-btn');
    const silent = document.querySelector('#edge-top-models .edge-silent-btn');
    return Boolean(conv && silent);
  })).toBe(true);
}

async function setConversationMode(page: Page, enabled: boolean) {
  await waitForEdgeButtons(page);
  await page.evaluate((target) => {
    const button = document.querySelector('#edge-top-models .edge-conv-btn');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('conversation button not found');
    }
    const current = button.getAttribute('aria-pressed') === 'true';
    if (current !== target) {
      button.click();
    }
  }, enabled);
  await expect.poll(async () => page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-conv-btn');
    return button instanceof HTMLButtonElement ? button.getAttribute('aria-pressed') : 'false';
  })).toBe(enabled ? 'true' : 'false');
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

test('hotword detection starts recording directly', async ({ page }) => {
  await waitReady(page);
  await setConversationListenWindowMs(page, 1_200);
  await setConversationMode(page, true);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-recording'));
  })).toBe(true);
});

test('hotword plus speech starts recording', async ({ page }) => {
  await waitReady(page);
  await setConversationListenWindowMs(page, 2_500);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 8 }, () => -80),
    ]);
  });
  await setConversationMode(page, true);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);
});

test('hotword is paused during recording', async ({ page }) => {
  await waitReady(page);
  await setConversationListenWindowMs(page, 3_000);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 20 }, () => -12),
    ]);
  });
  await setConversationMode(page, true);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => (window as any).__isHotwordActive())).toBe(false);
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'hotword' && entry.action === 'stop')).toBe(true);
});

test('hotword is paused while TTS playback is active', async ({ page }) => {
  await waitReady(page);
  await setConversationListenWindowMs(page, 2_500);
  await setConversationMode(page, true);
  await waitForHotwordStart(page);
  await page.evaluate(() => {
    (window as any).__setTTSPlaybackDelayMs(500);
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'hotword-tts-1', 'Testing hotword pause during playback.');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'tts');
  }).toBe(true);
  await expect.poll(async () => page.evaluate(() => (window as any).__isHotwordActive())).toBe(false);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'hotword' && entry.action === 'stop')).toBe(true);
});

test('conversation mode off keeps hotword disabled', async ({ page }) => {
  await waitReady(page);
  await setConversationListenWindowMs(page, 1_200);
  await setConversationMode(page, false);
  await clearLog(page);

  await triggerHotword(page);
  await page.waitForTimeout(250);

  const isListening = await page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  });
  expect(isListening).toBe(false);
  const isHotwordActive = await page.evaluate(() => (window as any).__isHotwordActive());
  expect(isHotwordActive).toBe(false);
});

test('hotword init failure degrades gracefully with no crash', async ({ page }) => {
  await waitReady(page, '?hotword=fail');
  await setConversationListenWindowMs(page, 1_200);
  await setConversationMode(page, true);
  await clearLog(page);

  await triggerHotword(page);
  await page.waitForTimeout(250);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'hotword' && entry.action === 'start')).toBe(false);
  expect(log.some((entry) => entry.type === 'unhandled_rejection')).toBe(false);

  await triggerVoiceAssistantTTS(page, 'hotword-degrade-1');
  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);
});

test('conversation mode with hotword active shows pause indicator', async ({ page }) => {
  await waitReady(page);
  await setConversationMode(page, true);
  await waitForHotwordStart(page);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-paused'));
  }), { timeout: 4_000 }).toBe(true);
});

test('follow-up timeout returns to pause indicator', async ({ page }) => {
  await waitReady(page);
  await setConversationListenWindowMs(page, 500);
  await setConversationMode(page, true);
  await waitForHotwordStart(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 120 }, () => -80));
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'pause-return-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-paused'));
  }), { timeout: 4_000 }).toBe(true);
});
