import { expect, test, type Page } from '@playwright/test';

async function waitWsReady(page: Page) {
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    return s.chatWs && s.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
}

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await waitWsReady(page);
}

async function openTopPanel(page: Page) {
  await page.locator('#edge-top-tap').click();
  await expect(page.locator('#bug-report-button')).toBeVisible();
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

test.describe('bug report flow', () => {
  test('top panel bug action captures a bundle with notes and annotations', async ({ page }) => {
    await waitReady(page);

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await expect(page.locator('#bug-report-sheet')).toBeVisible();

    await page.locator('#bug-report-note').fill('The edge indicator froze after the second tap.');

    const canvas = page.locator('#bug-report-ink');
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    if (!box) return;
    await page.mouse.move(box.x + 30, box.y + 30);
    await page.mouse.down();
    await page.mouse.move(box.x + 120, box.y + 90);
    await page.mouse.up();

    await page.locator('#bug-report-save').click();

    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__bugReportRequests.length);
    }).toBe(1);

    const request = await page.evaluate(() => (window as any).__bugReportRequests[0]);
    expect(request.trigger).toBe('button');
    expect(request.note).toContain('indicator froze');
    expect(String(request.screenshot_data_url || '')).toContain('data:image/png;base64,');
    expect(String(request.annotated_data_url || '')).toContain('data:image/png;base64,');
    expect(Array.isArray(request.recent_events)).toBe(true);
    expect(request.recent_events.length).toBeGreaterThan(0);
    expect(Array.isArray(request.browser_logs)).toBe(true);
    expect(String(request.device?.ua || '')).not.toBe('');
    expect(String(request.device?.platform || '')).not.toBe('');
    expect(String(request.device?.screen || '')).toMatch(/^\d+x\d+$/);
    expect(String(request.device?.timezone || '')).not.toBe('');
    expect(Number.isFinite(Number(request.device?.hardware_concurrency))).toBe(true);
    expect(Number.isFinite(Number(request.device?.max_touch_points))).toBe(true);
    await expect(page.locator('#bug-report-sheet')).toBeHidden();
    await expect(page.locator('#canvas-text')).toContainText('Bug report filed');
    await expect(page.locator('#canvas-text')).toContainText('#77');
  });

  test('pen interaction reports pen mode instead of the active tool id', async ({ page }) => {
    await waitReady(page);

    await dispatchPenStroke(page, [
      { x: 90, y: 90, pressure: 0.6 },
      { x: 150, y: 140, pressure: 0.7 },
    ]);

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await page.locator('#bug-report-note').fill('Pen-origin repro.');
    await page.locator('#bug-report-save').click();

    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__bugReportRequests.length);
    }).toBe(1);

    const request = await page.evaluate(() => (window as any).__bugReportRequests[0]);
    expect(request.active_mode).toBe('pen');
    expect(request.canvas_state?.last_input_origin).toBe('pen');
  });

  test('filed bug reports appear in inbox immediately', async ({ page }) => {
    await waitReady(page);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-list')).toContainText('Review parser cleanup');

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await page.locator('#bug-report-note').fill('Harness inbox refresh');
    await page.locator('#bug-report-save').click();

    await expect(page.locator('#pr-file-list')).toContainText('Bug report: Harness repro');
    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '3');
  });

  test('non-actionable bug reports are saved locally without a GitHub issue', async ({ page }) => {
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__taburaBugReportTestEnv = {
        issueMode: 'local',
      };
    });

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await page.locator('#bug-report-save').click();

    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__bugReportRequests.length);
    }).toBe(1);

    await expect(page.locator('#canvas-text')).toContainText('Bug report saved locally');
    await expect(page.locator('#canvas-text')).toContainText('GitHub auto-filing: auto-filing skipped');
    await expect(page.locator('#canvas-text')).not.toContainText('Issue: [#77]');
  });

  test('closed bug report disappears from the open inbox after github ingest refresh', async ({ page }) => {
    await waitReady(page);

    await page.locator('#edge-left-tap').click();
    await expect(page.locator('#pr-file-list')).toContainText('Review parser cleanup');

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await page.locator('#bug-report-note').fill('Harness github close refresh');
    await page.locator('#bug-report-save').click();

    await expect(page.locator('#pr-file-list')).toContainText('Bug report: Harness repro');
    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '3');

    await page.evaluate(() => {
      const data = (window as any).__itemSidebarData || {};
      const inbox = Array.isArray(data.inbox)
        ? data.inbox.filter((entry: any) => String(entry?.title || '') !== 'Bug report: Harness repro')
        : [];
      const done = Array.isArray(data.done) ? data.done.slice() : [];
      done.unshift({
        id: 103,
        title: 'Bug report: Harness repro',
        state: 'done',
        sphere: 'private',
        artifact_id: 0,
        source: 'github',
        source_ref: 'krystophny/tabura#77',
        artifact_title: 'Bug report: Harness repro',
        artifact_kind: 'github_issue',
        actor_name: '',
        created_at: '2026-03-08 15:04:05',
        updated_at: '2026-03-11 09:00:00',
      });
      (window as any).__itemSidebarData = {
        ...data,
        inbox,
        done,
      };

      const sessions = (window as any).__mockWsSessions || [];
      const chatWs = sessions.find((entry: any) => String(entry?.url || '').includes('/chat/'));
      if (chatWs && typeof chatWs.injectEvent === 'function') {
        chatWs.injectEvent({ type: 'items_ingested', count: 1, source: 'github' });
      }
    });

    await expect(page.locator('#pr-file-list')).not.toContainText('Bug report: Harness repro');
    await expect(page.locator('#edge-left-tap')).toHaveAttribute('data-inbox-count', '2');
  });

  test('keyboard shortcut opens the bug report sheet', async ({ page }) => {
    await waitReady(page);

    await page.keyboard.down('Control');
    await page.keyboard.down('Alt');
    await page.keyboard.press('b');
    await page.keyboard.up('Alt');
    await page.keyboard.up('Control');

    await expect(page.locator('#bug-report-sheet')).toBeVisible();
    await page.locator('#bug-report-cancel').click();
    await expect(page.locator('#bug-report-sheet')).toBeHidden();
  });

  test('voice trigger phrase opens bug capture instead of sending chat', async ({ page }) => {
    await waitReady(page);

    await page.evaluate(async () => {
      const mod = await import('../../internal/web/static/app-chat-transport.js');
      await mod.submitMessage('report bug', { kind: 'voice_transcript' });
    });

    await expect(page.locator('#bug-report-sheet')).toBeVisible();
    const sentMessages = await page.evaluate(() => {
      return ((window as any).__harnessLog || []).filter((entry: any) => entry?.type === 'message_sent');
    });
    expect(sentMessages).toHaveLength(0);
  });

  test('two-finger hold opens the bug report sheet', async ({ page }) => {
    await waitReady(page);

    await page.evaluate(async () => {
      if (typeof Touch === 'undefined') return;
      const target = document.body;
      const first = new Touch({ identifier: 1, target, clientX: 40, clientY: 40, pageX: 40, pageY: 40 });
      const second = new Touch({ identifier: 2, target, clientX: 90, clientY: 40, pageX: 90, pageY: 40 });
      target.dispatchEvent(new TouchEvent('touchstart', {
        touches: [first, second],
        changedTouches: [first, second],
        bubbles: true,
      }));
      await new Promise((resolve) => setTimeout(resolve, 760));
      target.dispatchEvent(new TouchEvent('touchend', {
        touches: [],
        changedTouches: [first, second],
        bubbles: true,
      }));
    });

    await expect(page.locator('#bug-report-sheet')).toBeVisible();
  });

  test('firefox-bug-report uses the browser-safe preview on Firefox', async ({ page, browserName }) => {
    test.skip(browserName !== 'firefox', 'Firefox-only regression coverage');
    await waitReady(page);

    await page.evaluate(() => {
      (window as any).__taburaBugReportTestEnv = {};
    });

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await expect(page.locator('#bug-report-sheet')).toBeVisible();
    await expect(page.locator('.bug-report-sheet__preview')).toHaveAttribute('data-capture-mode', 'fallback-firefox');

    const preview = await page.evaluate(async () => {
      const img = document.getElementById('bug-report-preview') as HTMLImageElement | null;
      if (!img) return null;
      if (!img.complete) await img.decode();
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth || 1;
      canvas.height = img.naturalHeight || 1;
      const ctx = canvas.getContext('2d');
      if (!ctx) return null;
      ctx.drawImage(img, 0, 0);
      const pixel = Array.from(ctx.getImageData(20, 20, 1, 1).data);
      return {
        src: img.src,
        mode: img.dataset.captureMode || '',
        pixel,
      };
    });

    expect(preview).not.toBeNull();
    expect(preview?.mode).toBe('fallback-firefox');
    expect(String(preview?.src || '')).toContain('data:image/png;base64,');
    expect(preview?.pixel?.[0]).not.toBe(255);
    expect(preview?.pixel?.[1]).not.toBe(255);
    expect(preview?.pixel?.[2]).not.toBe(255);
  });

  test('meeting bug report includes participant diagnostics', async ({ page }) => {
    await waitReady(page);

    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      const state = app?.getState?.();
      if (state) {
        state.livePolicy = 'meeting';
        state.liveSessionMode = 'meeting';
        state.companionRuntimeState = 'listening';
        state.companionRuntimeReason = 'participant_started';
        state.turnPolicyProfile = 'balanced';
      }
      (window as any).__participantStatus = {
        ok: true,
        active_sessions: 1,
        active_session_id: 'psess-harness-001',
        decision_summary: {
          pickup: 'Not picked up because the latest request did not address Tabura and did not match the tracked speaker.',
          overlap: 'Wrong-speaker overlap is suppressed while another speaker owns the pending turn.',
        },
        directed_speech_gate: {
          decision: 'target_speaker_follow_up',
          reason: 'target_speaker_follow_up',
          speaker: 'Alice',
          target_speaker: 'Alice',
          speaker_matched: true,
        },
        interaction_policy: {
          decision: 'interrupt',
          reason: 'target_speaker_overlap',
          speaker: 'Alice',
          target_speaker: 'Alice',
          pending_speaker: 'Alice',
        },
        replay_eval: {
          corpus_version: 'meeting-v1',
          profile: 'balanced',
          metrics: {
            false_barge_ins: 0,
            missed_speaker_starts: 0,
            overlap_yields: 2,
          },
        },
      };
    });

    await openTopPanel(page);
    await page.locator('#bug-report-button').click();
    await page.locator('#bug-report-note').fill('Meeting overlap reproduced.');
    await page.locator('#bug-report-save').click();

    await expect.poll(async () => {
      return page.evaluate(() => (window as any).__bugReportRequests.length);
    }).toBe(1);

    const request = await page.evaluate(() => (window as any).__bugReportRequests[0]);
    expect(request.meeting_diagnostics?.live_policy).toBe('meeting');
    expect(request.meeting_diagnostics?.participant_status?.directed_speech_gate?.decision).toBe('target_speaker_follow_up');
    expect(request.meeting_diagnostics?.participant_status?.interaction_policy?.reason).toBe('target_speaker_overlap');
    expect(request.meeting_diagnostics?.participant_status?.decision_summary?.overlap).toContain('Wrong-speaker overlap');
    expect(request.meeting_diagnostics?.participant_status?.replay_eval?.corpus_version).toBe('meeting-v1');
    expect(request.meeting_diagnostics?.participant_status?.replay_eval?.metrics?.overlap_yields).toBe(2);
  });
});
