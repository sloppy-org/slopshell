import { expect, test, type Page } from '@playwright/test';
import { setLiveMode, stopLiveMode } from './tabura-circle-helpers';

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

async function setDialogueMode(page: Page, enabled: boolean) {
  if (enabled) {
    await setLiveMode(page, 'dialogue');
    return;
  }
  await stopLiveMode(page, 'dialogue');
}

async function setMeetingMode(page: Page) {
  await setLiveMode(page, 'meeting');
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
  await setMeetingMode(page);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);
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

test('hotword runtime uses sloppy model defaults', async ({ page }) => {
  await waitReady(page);

  const config = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/hotword.js');
    return mod.hotwordRuntimeConfigForTest();
  });

  expect(new URL(config.modelFiles.keyword).pathname).toContain('/static/vendor/openwakeword/sloppy.onnx');
  expect(config.defaultThreshold).toBe(0.3);
  expect(config.detectionCooldownMs).toBe(800);
});

test('hotword ring buffer retains four seconds of pre-roll audio', async ({ page }) => {
  await waitReady(page);

  const ringBuffer = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/hotword.js');
    const samples = new Float32Array(70_000);
    for (let i = 0; i < samples.length; i += 1) {
      samples[i] = i;
    }
    mod.setPreRollAudioForTest(samples);
    const preRoll = mod.getPreRollAudio();
    return {
      capacity: mod.hotwordRingBufferCapacitySamplesForTest(),
      length: preRoll.length,
      first: preRoll[0],
      last: preRoll[preRoll.length - 1],
    };
  });

  expect(ringBuffer.capacity).toBe(64_000);
  expect(ringBuffer.length).toBe(64_000);
  expect(ringBuffer.first).toBe(6_000);
  expect(ringBuffer.last).toBe(69_999);
});

test('hotword plus speech starts recording', async ({ page }) => {
  await waitReady(page);
  await setMeetingMode(page);
  await waitForHotwordStart(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 8 }, () => -80),
    ]);
  });
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);
});

test('hotword pre-roll samples are prepended before STT normalization', async ({ page }) => {
  await waitReady(page);

  const combined = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/app-voice.js');
    return Array.from(mod.buildHotwordCaptureSamples(
      new Float32Array([0.3, 0.4]),
      new Float32Array([0.1, 0.2]),
    )).map((value) => Number(value.toFixed(3)));
  });

  expect(combined).toEqual([0.1, 0.2, 0.3, 0.4]);
});

test('hotword capture tolerates initial pause before user speech', async ({ page }) => {
  await waitReady(page);
  await setMeetingMode(page);
  await waitForHotwordStart(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 36 }, () => -80), // ~1.44s initial pause
      ...Array.from({ length: 14 }, () => -12), // user starts speaking
      ...Array.from({ length: 40 }, () => -80),
    ]);
  });
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

test('standalone hotword falls back to pre-roll audio after silence', async ({ page }) => {
  await waitReady(page);
  await setMeetingMode(page);
  await waitForHotwordStart(page);
  await page.evaluate(async () => {
    const mod = await import('/internal/web/static/hotword.js');
    mod.setPreRollAudioForTest(Float32Array.from({ length: 16_000 }, () => 0.2));
    (window as any).__setSTTTranscribeResponse({ text: 'Sloppy' });
    (window as any).__setVadDbFrames(Array.from({ length: 220 }, () => -80));
  });
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    const upload = log.find((entry) => entry.type === 'api_fetch' && entry.action === 'stt_transcribe');
    return Number(upload?.bytes || 0);
  }, { timeout: 10_000 }).toBeGreaterThan(32_000);
});

test('dialogue turn controller finalizes a buffered fragment on timeout', async ({ page }) => {
  await waitReady(page);
  const events = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/dialogue-turn-policy.js');
    const log: Array<Record<string, unknown>> = [];
    const controller = new mod.DialogueTurnController({
      onContinue(decision: Record<string, unknown>) {
        log.push({ type: 'continue', text: decision.combinedText, reason: decision.reason });
      },
      onFinalize(text: string, decision: Record<string, unknown>) {
        log.push({ type: 'finalize', text, reason: decision.reason });
      },
    });
    controller.consume({ text: 'can you', durationMs: 320, interruptedAssistant: false });
    await new Promise((resolve) => window.setTimeout(resolve, 720));
    controller.flush('continuation_timeout');
    return log;
  });

  expect(events).toEqual([
    { type: 'continue', text: 'can you', reason: 'too_short' },
    { type: 'finalize', text: 'can you', reason: 'continuation_timeout' },
  ]);
});

test('hotword is paused during recording', async ({ page }) => {
  await waitReady(page);
  await setMeetingMode(page);
  await waitForHotwordStart(page);
  await page.evaluate(() => {
    (window as any).__setVadDbFrames([
      ...Array.from({ length: 8 }, () => -80),
      ...Array.from({ length: 10 }, () => -12),
      ...Array.from({ length: 20 }, () => -12),
    ]);
  });
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
  await setDialogueMode(page, true);
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
  expect(log.some((entry) => entry.type === 'hotword' && entry.action === 'start')).toBe(false);
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
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  })).toBe(true);
});

test('Dialogue keeps the live listen indicator armed instead of falling back to pause', async ({ page }) => {
  await waitReady(page);
  await setDialogueListenWindowMs(page, 600);
  await setDialogueMode(page, true);

  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const s = app?.getState?.();
    return Boolean(s?.liveSessionDialogueListenActive) && String(s?.voiceLifecycle || '') === 'listening';
  }), { timeout: 4_000 }).toBe(true);
});

test('meeting mode hotword still starts direct recording', async ({ page }) => {
  await waitReady(page);
  await setMeetingMode(page);
  await waitForHotwordStart(page);
  await clearLog(page);

  await triggerHotword(page);

  await expect.poll(async () => {
    const log = await getLog(page);
    return log.some((entry) => entry.type === 'recorder' && entry.action === 'start');
  }, { timeout: 5_000 }).toBe(true);
});

test('dialogue turn controller buffers incomplete fragments until semantic completion', async ({ page }) => {
  await waitReady(page);
  const events = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/dialogue-turn-policy.js');
    const log: Array<Record<string, unknown>> = [];
    const controller = new mod.DialogueTurnController({
      onContinue(decision: Record<string, unknown>) {
        log.push({ type: 'continue', text: decision.combinedText, reason: decision.reason });
      },
      onFinalize(text: string, decision: Record<string, unknown>) {
        log.push({ type: 'finalize', text, reason: decision.reason });
      },
    });
    controller.consume({ text: 'can you', durationMs: 420, interruptedAssistant: false });
    controller.consume({ text: 'help me write the summary', durationMs: 1100, interruptedAssistant: false });
    return log;
  });

  expect(events).toEqual([
    { type: 'continue', text: 'can you', reason: 'too_short' },
    { type: 'finalize', text: 'can you help me write the summary', reason: 'semantic_completion' },
  ]);
});

test('dialogue turn controller treats short assistant interruptions as backchannels', async ({ page }) => {
  await waitReady(page);
  const events = await page.evaluate(async () => {
    const mod = await import('/internal/web/static/dialogue-turn-policy.js');
    const log: Array<Record<string, unknown>> = [];
    const controller = new mod.DialogueTurnController({
      onBackchannel(decision: Record<string, unknown>) {
        log.push({ type: 'backchannel', text: decision.combinedText, reason: decision.reason });
      },
    });
    controller.consume({ text: 'yeah', durationMs: 260, interruptedAssistant: true });
    return log;
  });

  expect(events).toEqual([
    { type: 'backchannel', text: 'yeah', reason: 'assistant_backchannel' },
  ]);
});
