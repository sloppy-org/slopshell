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

async function injectTurnEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    const sessionId = String(app?.getState?.().chatSessionId || '');
    const sessions = (window as any).__mockWsSessions || [];
    const turnWs = sessions.find((ws: any) => typeof ws.url === 'string'
      && ws.url.includes('/ws/turn/')
      && (!sessionId || ws.url.includes(`/ws/turn/${sessionId}`)));
    if (turnWs?.injectEvent) {
      turnWs.injectEvent(eventPayload);
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

async function setSilentMode(page: Page, enabled: boolean) {
  await expect.poll(async () => page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-silent-btn');
    return button instanceof HTMLButtonElement;
  })).toBe(true);
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
});

test('Live panel swaps Dialogue/Meeting choices for active status and Stop', async ({ page }) => {
  await waitForEdgeButtons(page);
  await expect(page.locator('#edge-top .edge-panel-title')).toHaveText('Runtime');
  await expect(page.locator('#edge-top-models')).toHaveAttribute('aria-label', 'Workspace runtime controls');
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toBeVisible();
  await expect(page.locator('#edge-top-models .edge-live-meeting-btn')).toBeVisible();

  await setDialogueMode(page, true);

  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
  await expect(page.locator('#edge-top-models .edge-live-stop-btn')).toBeVisible();
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toHaveCount(0);
  await expect(page.locator('#edge-top-models')).not.toContainText('Companion');
  await expect(page.locator('#edge-top-models')).not.toContainText('Conversation');

  await setDialogueMode(page, false);
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toBeVisible();
  await expect(page.locator('#edge-top-models .edge-live-meeting-btn')).toBeVisible();
});

test('Meeting entry shows active status and returns to choices on Stop', async ({ page }) => {
  await switchToTestProject(page);
  await waitForEdgeButtons(page);
  const meetingButton = page.locator('#edge-top-models .edge-live-meeting-btn');
  await expect(meetingButton).toBeEnabled();
  await page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-live-meeting-btn');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('meeting button not found');
    }
    button.click();
  });

  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');

  await page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-live-stop-btn');
    if (button instanceof HTMLButtonElement) {
      button.click();
    }
  });
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toBeVisible();
  await expect(page.locator('#edge-top-models .edge-live-meeting-btn')).toBeVisible();
});

test('Live policy persists from runtime and reacts to websocket policy changes', async ({ page }) => {
  await page.goto('/tests/playwright/harness.html?live_policy=meeting');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
  await page.waitForTimeout(200);
  await waitForEdgeButtons(page);

  const initialSessionState = await page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    return {
      activeWorkspaceId: String(state?.activeWorkspaceId || ''),
      chatSessionId: String(state?.chatSessionId || ''),
      chatMode: String(state?.chatMode || ''),
    };
  });

  await expect(page.locator('#edge-top-models .edge-live-meeting-btn')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toHaveAttribute('aria-pressed', 'false');

  await clearLog(page);
  await page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-live-dialogue-btn');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('dialogue button not found');
    }
    button.click();
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.find((entry) => entry.action === 'live_policy')?.payload || null;
  }).toEqual({ policy: 'dialogue' });
  await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    return {
      activeWorkspaceId: String(state?.activeWorkspaceId || ''),
      chatSessionId: String(state?.chatSessionId || ''),
      chatMode: String(state?.chatMode || ''),
    };
  })).toEqual(initialSessionState);

  await page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-live-stop-btn');
    if (button instanceof HTMLButtonElement) {
      button.click();
    }
  });
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toBeVisible();

  await injectChatEvent(page, { type: 'live_policy_changed', policy: 'meeting' });
  await expect(page.locator('#edge-top-models .edge-live-meeting-btn')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#edge-top-models .edge-live-dialogue-btn')).toHaveAttribute('aria-pressed', 'false');
});

test('Dialogue shows listening indicator immediately and after TTS playback', async ({ page }) => {
  await setDialogueMode(page, true);
  await clearLog(page);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  })).toBe(true);

  await triggerVoiceAssistantTTS(page, 'conv-listening-1');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'tts');
  }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  })).toBe(true);
});

test('Dialogue mode follows server turn actions for continue and finalize', async ({ page }) => {
  await setDialogueMode(page, true);
  await clearLog(page);

  await injectTurnEvent(page, {
    type: 'turn_action',
    action: 'continue_listening',
    reason: 'fragment',
  });

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return {
      listenActive: Boolean(s?.liveSessionDialogueListenActive),
      lifecycle: String(s?.voiceLifecycle || ''),
    };
  })).toEqual({
    listenActive: true,
    lifecycle: 'listening',
  });

  await injectTurnEvent(page, {
    type: 'turn_action',
    action: 'finalize_user_turn',
    text: 'Server-owned transcript.',
    reason: 'semantic_completion',
  });

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.find((entry) => entry.type === 'message_sent')?.text || '';
  }).toBe('Server-owned transcript.');
});

test('Dialogue off does not open listening indicator after TTS', async ({ page }) => {
  await setDialogueListenWindowMs(page, 1_200);
  await setDialogueMode(page, false);
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

test('speech onset during dialogue listen starts recording', async ({ page }) => {
  await setDialogueMode(page, true);
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
    const app = (window as any)._taburaApp;
    return Boolean(app?.getState?.().chatVoiceCapture);
  })).toBe(true);
});

test('dialogue listen stays armed when only frame probabilities arrive without speech-start callbacks', async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__taburaVadMock = {
      init() { return true; },
      create(callbacks) {
        let running = false;
        let timer = null;
        return {
          start() {
            if (running) return;
            running = true;
            timer = window.setInterval(() => {
              callbacks?.onFrameProcessed?.({ isSpeech: 0.9 });
            }, 40);
          },
          pause() {
            running = false;
            if (timer !== null) {
              window.clearInterval(timer);
              timer = null;
            }
          },
          destroy() {
            running = false;
            if (timer !== null) {
              window.clearInterval(timer);
              timer = null;
            }
          },
        };
      },
    };
  });

  await setDialogueMode(page, true);
  await clearLog(page);

  await page.waitForTimeout(1_000);

  const recorderStarted = await page.evaluate(() => {
    const log = (window as any).__harnessLog;
    return Array.isArray(log) && log.some((entry: any) => entry?.type === 'recorder' && entry?.action === 'start');
  });
  expect(recorderStarted).toBe(false);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  })).toBe(true);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 2_000 }).toBe(false);
});

test('dialogue listen stays armed without a fixed timeout', async ({ page }) => {
  await setDialogueMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 120 }, () => -80));
  });
  await clearLog(page);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  })).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  }), { timeout: 4_000 }).toBe(true);
});

test('tap during dialogue listen cancels listen and starts recording', async ({ page }) => {
  await setDialogueMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 200 }, () => -80));
  });
  await clearLog(page);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  })).toBe(true);

  await page.mouse.click(420, 360);
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }).toBe(true);

  const captureActive = await page.evaluate(() => {
    const app = (window as any)._taburaApp;
    return Boolean(app?.getState?.().chatVoiceCapture);
  });
  expect(captureActive).toBe(true);
});

test('PTT during dialogue listen cancels listen and starts push-to-talk', async ({ page }) => {
  await setDialogueMode(page, true);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames(Array.from({ length: 200 }, () => -80));
  });
  await clearLog(page);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
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

test('silent mode keeps dialogue listening active without speaking', async ({ page }) => {
  await setDialogueListenWindowMs(page, 1_200);
  await setSilentMode(page, true);
  await setDialogueMode(page, true);
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  }), { timeout: 4_000 }).toBe(true);
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-silent-1');
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  }), { timeout: 4_000 }).toBe(true);

  const log = await getLog(page);
  expect(log.some((entry) => entry.type === 'tts')).toBe(false);
});

test('dialogue shows the companion robot by default', async ({ page }) => {
  await setDialogueMode(page, true);

  await expect.poll(async () => page.evaluate(() => {
    const surface = document.getElementById('companion-idle-surface');
    if (!(surface instanceof HTMLElement)) return null;
    return {
      hidden: surface.getAttribute('aria-hidden'),
      display: surface.style.display || window.getComputedStyle(surface).display,
      surface: surface.dataset.surface || '',
    };
  })).toEqual({
    hidden: 'false',
    display: 'block',
    surface: 'robot',
  });
});

test('dialogue barge-in interrupts TTS and starts recording with connected turn intelligence', async ({ page }) => {
  await setDialogueMode(page, true);
  await injectTurnEvent(page, {
    type: 'turn_ready',
    session_id: 'test-session',
    profile: 'balanced',
    eval_logging_enabled: true,
  });
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    return Boolean(app?.getState?.().dialogueDiagnostics?.connected);
  })).toBe(true);
  await page.evaluate(() => {
    (window as any).__setTTSPlaybackDelayMs(750);
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 12 }, () => -12),
      ...Array.from({ length: 12 }, () => -80),
    ]);
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-barge-1', 'Please interrupt me if you need to.');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    return Boolean(app?.getState?.().chatVoiceCapture);
  })).toBe(true);
});

test('dialogue barge-in interrupts TTS and starts recording without turn intelligence', async ({ page }) => {
  await setDialogueMode(page, true);
  await page.evaluate(() => {
    (window as any).__setTTSPlaybackDelayMs(750);
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 12 }, () => -12),
      ...Array.from({ length: 12 }, () => -80),
    ]);
  });
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'conv-barge-local-1', 'Please interrupt me if you need to.');

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    return {
      recording: Boolean(state?.chatVoiceCapture),
      speaking: Boolean(state?.ttsPlaying),
    };
  })).toEqual({
    recording: true,
    speaking: false,
  });
});

test('dialogue listen shows hard error when VAD is unavailable', async ({ page }) => {
  // Remove the VAD mock so initVAD returns null (real bundle does not exist)
  await page.evaluate(() => {
    (window as any).__taburaVadMock = null;
  });
  await setDialogueListenWindowMs(page, 10_000);
  await setDialogueMode(page, true);
  await clearLog(page);

  await triggerVoiceAssistantTTS(page, 'vad-error-1');

  // The error must surface as a visible status message, not silently fall back.
  await expect.poll(async () => page.evaluate(() => {
    const el = document.getElementById('status-text');
    return el?.textContent?.includes('speech detection unavailable') ?? false;
  }), { timeout: 5_000 }).toBe(true);

  // The error must also appear as a system chat message.
  await expect(page.locator('.chat-system').first()).toContainText('speech detection unavailable');

  // The listening indicator must not stay stuck.
  const isListening = await page.evaluate(() => {
    const indicator = document.getElementById('indicator');
    return Boolean(indicator?.classList.contains('is-listening'));
  });
  expect(isListening).toBe(false);
});
