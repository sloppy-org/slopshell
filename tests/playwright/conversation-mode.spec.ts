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
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
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

async function setSilentMode(page: Page, enabled: boolean) {
  await waitForEdgeButtons(page);
  await page.evaluate((target) => {
    const button = document.querySelector('#edge-top-models .edge-silent-btn');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('silent button not found');
    }
    const current = button.getAttribute('aria-pressed') === 'true';
    if (current !== target) {
      button.click();
    }
  }, enabled);
  await expect.poll(async () => page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-silent-btn');
    return button instanceof HTMLButtonElement ? button.getAttribute('aria-pressed') : 'false';
  })).toBe(enabled ? 'true' : 'false');
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
  await page.evaluate(() => {
    window.localStorage.removeItem('tabura.conversationMode');
  });
});

test('conversation mode toggle persists in localStorage', async ({ page }) => {
  await setConversationMode(page, true);

  const persisted = await page.evaluate(() => window.localStorage.getItem('tabura.conversationMode'));
  expect(persisted).toBe('true');

  await page.reload();
  await page.waitForTimeout(200);
  await waitForEdgeButtons(page);
  await expect.poll(async () => page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-conv-btn');
    return button instanceof HTMLButtonElement ? button.getAttribute('aria-pressed') : 'false';
  })).toBe('true');
});

test('conversation mode shows listening indicator after TTS playback completes', async ({ page }) => {
  await setConversationListenWindowMs(page, 1_200);
  await setConversationMode(page, true);
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-listening-1');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'tts');
  }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);
});

test('conversation mode off does not open listening indicator after TTS', async ({ page }) => {
  await setConversationListenWindowMs(page, 1_200);
  await setConversationMode(page, false);
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-disabled-1');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'tts');
  }).toBe(true);

  await page.waitForTimeout(300);
  const isListening = await page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  });
  expect(isListening).toBe(false);
});

test('speech onset during conversation listen starts recording', async ({ page }) => {
  await setConversationListenWindowMs(page, 3_000);
  await setConversationMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 12 }, () => -80),
    ]);
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-speech-1');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-recording'));
  })).toBe(true);
});

test('conversation listen timeout hides listening indicator', async ({ page }) => {
  await setConversationListenWindowMs(page, 500);
  await setConversationMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 120 }, () => -80));
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-timeout-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  }), { timeout: 4_000 }).toBe(false);
});

test('tap during conversation listen cancels listen and starts recording', async ({ page }) => {
  await setConversationListenWindowMs(page, 3_000);
  await setConversationMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 200 }, () => -80));
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-tap-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  await page.mouse.click(420, 360);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 3_000 }).toBe(true);

  const stillListening = await page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  });
  expect(stillListening).toBe(false);
});

test('PTT during conversation listen cancels listen and starts push-to-talk', async ({ page }) => {
  await setConversationListenWindowMs(page, 3_000);
  await setConversationMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 200 }, () => -80));
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-ptt-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  await page.keyboard.down('Control');
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 3_000 }).toBe(true);
  await page.keyboard.up('Control');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'stt' && entry.action === 'stop');
  }, { timeout: 3_000 }).toBe(true);
});

test('silent mode with conversation enabled does not open conversation listen', async ({ page }) => {
  await setConversationListenWindowMs(page, 1_200);
  await setConversationMode(page, true);
  await setSilentMode(page, true);
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-silent-1');
  await page.waitForTimeout(500);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'tts')).toBe(false);

  const isListening = await page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  });
  expect(isListening).toBe(false);
});

test('conversation listen timeout returns to pause indicator when hotword is active', async ({ page }) => {
  await setConversationListenWindowMs(page, 500);
  await setConversationMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 120 }, () => -80));
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-pause-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  })).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-paused'));
  }), { timeout: 4_000 }).toBe(true);
});
