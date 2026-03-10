import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return s.chatWs?.readyState === wsOpen && s.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 8_000 });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    const activeChatWs = app?.getState?.().chatWs;
    if (activeChatWs && typeof activeChatWs.injectEvent === 'function') {
      activeChatWs.injectEvent(eventPayload);
      return;
    }
    const sessions = (window as any).__mockWsSessions || [];
    const candidates = sessions.filter((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    const chatWs = candidates[candidates.length - 1];
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
}

async function setHarnessMeetingState(
  page: Page,
  {
    enabled = true,
    idleSurface = 'robot',
    runtimeState = 'idle',
    reason = '',
  }: {
    enabled?: boolean;
    idleSurface?: 'robot' | 'black';
    runtimeState?: 'idle' | 'listening' | 'thinking' | 'talking' | 'error';
    reason?: string;
  } = {},
) {
  await page.evaluate(async (nextState) => {
    const app = (window as any)._taburaApp;
    const appState = app?.getState?.();
    if (appState) {
      appState.projects = [
        {
          id: 'test',
          name: 'Test',
          kind: 'managed',
          project_key: '/tmp/test',
          root_path: '/tmp',
          chat_session_id: 'chat-1',
          canvas_session_id: 'local',
          chat_mode: 'chat',
          chat_model: 'spark',
          chat_model_reasoning_effort: 'low',
          run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
        },
        {
          id: 'hub',
          name: 'Hub',
          kind: 'hub',
          project_key: '__hub__',
          root_path: '/tmp/hub',
          chat_session_id: 'chat-hub',
          canvas_session_id: 'local',
          chat_mode: 'chat',
          chat_model: 'spark',
          chat_model_reasoning_effort: 'low',
          run_state: { active_turns: 0, queued_turns: 0, is_working: false, status: 'idle' },
        },
      ];
      appState.activeProjectId = 'test';
      appState.hasArtifact = false;
      appState.companionEnabled = Boolean(nextState.enabled);
      appState.companionIdleSurface = String(nextState.idleSurface || 'robot');
      appState.companionRuntimeState = String(nextState.runtimeState || 'idle');
      appState.companionRuntimeReason = String(nextState.reason || nextState.runtimeState || 'idle');
    }
    document.querySelectorAll('#canvas-viewport .canvas-pane').forEach((node) => {
      if (!(node instanceof HTMLElement)) return;
      node.style.display = 'none';
      node.classList.remove('is-active');
    });
    (window as any).__participantConfig = {
      ...(window as any).__participantConfig,
      companion_enabled: Boolean(nextState.enabled),
      idle_surface: String(nextState.idleSurface || 'robot'),
    };
    (window as any).__companionRuntimeState = {
      state: String(nextState.runtimeState || 'idle'),
      reason: String(nextState.reason || nextState.runtimeState || 'idle'),
      project_key: '/tmp/test',
      updated_at: Math.floor(Date.now() / 1000),
    };
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string' && ws.url.includes('/ws/chat/'));
    if (chatWs?.injectEvent) {
      chatWs.injectEvent({
        type: 'companion_state',
        project_key: '/tmp/test',
        state: String(nextState.runtimeState || 'idle'),
        reason: String(nextState.reason || nextState.runtimeState || 'idle'),
        companion_enabled: Boolean(nextState.enabled),
        idle_surface: String(nextState.idleSurface || 'robot'),
      });
    }
    if (typeof app?.syncCompanionIdleSurface === 'function') {
      app.syncCompanionIdleSurface();
    }
  }, { enabled, idleSurface, runtimeState, reason });
}

async function waitForMeetingSurface(page: Page, state: string, surface: string) {
  await expect.poll(async () => page.evaluate(() => {
    const node = document.getElementById('companion-idle-surface');
    if (!(node instanceof HTMLElement)) return null;
    return {
      display: window.getComputedStyle(node).display,
      state: node.dataset.state || '',
      surface: node.dataset.surface || '',
    };
  })).toEqual({
    display: 'block',
    state,
    surface,
  });
}

async function switchToProject(page: Page, projectID: string) {
  await page.evaluate((targetProjectID) => {
    const buttons = Array.from(document.querySelectorAll('#edge-top-projects .edge-project-btn'));
    const button = buttons.find((node) => node.textContent?.trim().toLowerCase() === targetProjectID);
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error(`project button not found: ${targetProjectID}`);
    }
    button.click();
  }, projectID);
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    const state = app?.getState?.();
    if (!state) return null;
    return {
      activeProjectId: state.activeProjectId || '',
      projectSwitchInFlight: Boolean(state.projectSwitchInFlight),
    };
  })).toEqual({
    activeProjectId: projectID,
    projectSwitchInFlight: false,
  });
}

async function clearCanvas(page: Page) {
  await page.evaluate(() => {
    const button = document.getElementById('btn-edge-rasa');
    if (button instanceof HTMLButtonElement) {
      button.click();
    }
  });
  await expect.poll(async () => page.evaluate(() => {
    const app = (window as any)._taburaApp;
    return Boolean(app?.getState?.().hasArtifact);
  })).toBe(false);
}

async function switchSidebarToFiles(page: Page) {
  await page.getByRole('button', { name: 'Files' }).click();
  await expect(page.locator('.sidebar-tab.is-active')).toContainText('Files');
}

test('workspace sidebar exposes meeting transcript, summary, and references viewer entries', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);

  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
  await switchSidebarToFiles(page);
  await expect(page.locator('#pr-file-list')).toContainText('Meeting Transcript');
  await expect(page.locator('#pr-file-list')).toContainText('Meeting Summary');
  await expect(page.locator('#pr-file-list')).toContainText('Meeting References');

  await page.getByRole('button', { name: 'Meeting Transcript' }).click();
  await expect(page.locator('#canvas-text')).toContainText('Harness meeting transcript');

  await page.getByRole('button', { name: 'Meeting Summary' }).click();
  await expect(page.locator('#canvas-text')).toContainText('Harness meeting summary');

  await page.getByRole('button', { name: 'Meeting References' }).click();
  await expect(page.locator('#canvas-text')).toContainText('Acme');
  await expect(page.locator('#canvas-text')).toContainText('Budget');
});

test('meeting summary proposes selectable inbox items and creates the chosen ones', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);

  await page.evaluate(() => {
    (window as any).__setItemSidebarData({
      inbox: [],
      waiting: [],
      someday: [],
      done: [],
    });
    (window as any).__setMeetingSummaryProposals([
      {
        title: 'Draft the revised agenda',
        actor_name: 'Alice',
        evidence: 'ACTION: Alice will draft the revised agenda.',
      },
      {
        title: 'Review the budget appendix',
        actor_name: '',
        evidence: 'TODO: review the budget appendix.',
      },
    ]);
  });

  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
  await switchSidebarToFiles(page);
  await page.getByRole('button', { name: 'Meeting Summary' }).click();
  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);

  await expect(page.locator('#meeting-summary-items')).toContainText('Draft the revised agenda');
  await expect(page.locator('#meeting-summary-items')).toContainText('Review the budget appendix');

  await page.getByLabel(/Review the budget appendix/).uncheck();
  await page.getByRole('button', { name: 'Create 1 inbox item' }).click();

  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
  await expect(page.locator('#pr-file-list')).toContainText('Draft the revised agenda');
  await expect(page.locator('#pr-file-list')).toContainText('Alice');
  await expect(page.locator('#pr-file-list')).not.toContainText('Review the budget appendix');

  const log = await page.evaluate(() => (window as any).__harnessLog);
  expect(log.some((entry: any) => entry?.action === 'meeting_items_create'
    && Array.isArray(entry?.payload?.selected)
    && entry.payload.selected.length === 1
    && Number(entry.payload.selected[0]) === 0)).toBe(true);
});

test('meeting idle surface tracks runtime state and hides behind open artifacts', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);
  await switchToProject(page, 'test');
  await setHarnessMeetingState(page, { enabled: true, idleSurface: 'robot', runtimeState: 'idle' });
  await clearCanvas(page);
  await page.evaluate(() => {
    (window as any)._taburaApp?.syncCompanionIdleSurface?.();
  });

  await waitForMeetingSurface(page, 'idle', 'robot');

  for (const nextState of ['listening', 'thinking', 'talking', 'error'] as const) {
    await setHarnessMeetingState(page, {
      enabled: true,
      idleSurface: 'robot',
      runtimeState: nextState,
      reason: nextState,
    });
    await waitForMeetingSurface(page, nextState, 'robot');
  }

  await page.locator('#edge-left-tap').click();
  await switchSidebarToFiles(page);
  await page.getByRole('button', { name: 'Meeting Transcript' }).click();
  await expect(page.locator('#canvas-text')).toContainText('Harness meeting transcript');
  await expect(page.locator('#companion-idle-surface')).toBeHidden();
});

test('black mode toggle updates the meeting idle surface preference', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);
  await switchToProject(page, 'test');
  await setHarnessMeetingState(page, { enabled: true, idleSurface: 'robot', runtimeState: 'idle' });
  await clearCanvas(page);
  await page.evaluate(() => {
    (window as any)._taburaApp?.syncCompanionIdleSurface?.();
  });

  const blackButton = page.locator('#edge-top-models .edge-companion-surface-btn');
  await expect(blackButton).toHaveAttribute('aria-pressed', 'false');
  await page.evaluate(() => {
    const button = document.querySelector('#edge-top-models .edge-companion-surface-btn');
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error('black button missing');
    }
    button.click();
  });

  await waitForMeetingSurface(page, 'idle', 'black');
  await expect(blackButton).toHaveAttribute('aria-pressed', 'true');
});
