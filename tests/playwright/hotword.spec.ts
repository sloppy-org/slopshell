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
    if (String(state?.activeProjectId || '') !== 'test') return '';
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
  await setDialogueListenWindowMs(page, 1_200);
  await setDialogueMode(page, true);
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

test('hotword runtime pins explicit ONNX wasm asset URLs', async ({ page }) => {
  await waitReady(page);

  const paths = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/hotword.js');
    return mod.resolveOrtWasmPaths();
  });

  expect(new URL(paths.mjs).pathname).toContain('/static/vad/ort-wasm-simd-threaded.mjs');
  expect(new URL(paths.wasm).pathname).toContain('/static/vad/ort-wasm-simd-threaded.wasm');
});

test('hotword plus speech starts recording', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 2_500);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 8 }, () => -80),
    ]);
  });
  await setDialogueMode(page, true);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);
});

test('hotword capture tolerates initial pause before user speech', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 2_500);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 36 }, () => -80), // ~1.44s initial pause
      ...Array.from({ length: 14 }, () => -12), // user starts speaking
      ...Array.from({ length: 40 }, () => -80),
    ]);
  });
  await setDialogueMode(page, true);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await page.waitForTimeout(1_000);
  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'recorder' && entry.action === 'stop')).toBe(false);
});

test('hotword empty transcript re-opens dialogue listen window', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 2_500);
  await setDialogueMode(page, true);
  await waitForHotwordStart(page);
  await page.evaluate(() => {
    (window as any).__setSTTTranscribeResponse({ text: '', reason: 'no_speech_detected' }, 200);
    (window as any).__setVadDbFrames(Array.from({ length: 200 }, () => -80));
  });
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await page.keyboard.press('Enter');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'stop');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive);
  }), { timeout: 4_000 }).toBe(true);
});

test('hotword is paused during recording', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 3_000);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 20 }, () => -12),
    ]);
  });
  await setDialogueMode(page, true);
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
  await setDialogueListenWindowMs(page, 2_500);
  await setDialogueMode(page, true);
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

test('Dialogue off keeps hotword disabled', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 1_200);
  await setDialogueMode(page, false);
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
  await setDialogueListenWindowMs(page, 1_200);
  await setDialogueMode(page, true);
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

test('Dialogue with hotword active shows pause indicator', async ({ page }) => {
  await waitReady(page);
  await setDialogueMode(page, true);
  await waitForHotwordStart(page);

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-paused'));
  }), { timeout: 4_000 }).toBe(true);
});

test('meeting mode hotword still starts direct recording', async ({ page }) => {
  await waitReady(page);
  await switchToTestProject(page);
  await waitForEdgeButtons(page);
  await page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-live-meeting-btn');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('meeting button not found');
    }
    button.click();
  });
  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);
});

test('follow-up timeout returns to pause indicator', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 500);
  await setDialogueMode(page, true);
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

test('hotword re-arms after follow-up timeout and starts a second turn', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 500);
  await setDialogueMode(page, true);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'pause-rearm-1');

  await expect.poll(async () => page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-paused'));
  }), { timeout: 4_000 }).toBe(true);

  await expect.poll(async () => {
    const rearmLog = await getLog(page);
    const sawStop = rearmLog.some((entry) => entry.type === 'hotword' && entry.action === 'stop');
    const sawStart = rearmLog.some((entry) => entry.type === 'hotword' && entry.action === 'start');
    return sawStop && sawStart;
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    const hotwordActive = Boolean((window as any).__isHotwordActive?.());
    if (!s) return false;
    const localActiveTurns = s.assistantActiveTurns?.size || 0;
    return hotwordActive
      && Boolean(s.liveSessionActive)
      && String(s.liveSessionMode || '') === 'dialogue'
      && !Boolean(s.liveSessionDialogueListenActive)
      && !Boolean(s.chatVoiceCapture)
      && !Boolean(s.ttsPlaying)
      && !Boolean(s.voiceAwaitingTurn)
      && Number(s.assistantUnknownTurns || 0) === 0
      && Number(s.assistantRemoteActiveCount || 0) === 0
      && Number(s.assistantRemoteQueuedCount || 0) === 0
      && Number(localActiveTurns) === 0
      && String(s.voiceLifecycle || 'idle') === 'idle';
  }), { timeout: 5_000 }).toBe(true);

  await clearLog(page);
  await expect.poll(async () => {
    await triggerHotword(page);
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'hotword' && entry.action === 'detect');
  }, { timeout: 3_000 }).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 7_000 }).toBe(true);
});
