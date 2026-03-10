import * as env from './app-env.js';
import * as context from './app-context.js';

const { apiURL, setRecording, clearLineHighlight, getInputAnchor, setInputAnchor, buildContextPrefix, updateOverlay, hideOverlay, cancelLiveSessionListen, isLiveSessionListenActive } = env;
const { refs, state, isVoiceTurn, VOICE_LIFECYCLE } = context;

const showStatus = (...args) => refs.showStatus(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const stopTTSPlayback = (...args) => refs.stopTTSPlayback(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);
const takePendingRow = (...args) => refs.takePendingRow(...args);
const updateAssistantRow = (...args) => refs.updateAssistantRow(...args);
const trackAssistantTurnFinished = (...args) => refs.trackAssistantTurnFinished(...args);
const appendRenderedAssistant = (...args) => refs.appendRenderedAssistant(...args);
const refreshAssistantActivity = (...args) => refs.refreshAssistantActivity(...args);
const stopVoiceCaptureAndSend = (...args) => refs.stopVoiceCaptureAndSend(...args);
const deactivateLiveSession = (...args) => refs.deactivateLiveSession(...args);
const isMobileSilent = (...args) => refs.isMobileSilent(...args);
const nextLocalMessageId = (...args) => refs.nextLocalMessageId(...args);
const stopChatVoiceMedia = (...args) => refs.stopChatVoiceMedia(...args);
const isAssistantWorking = (...args) => refs.isAssistantWorking(...args);
const isVoiceTranscriptSubmitPending = (...args) => refs.isVoiceTranscriptSubmitPending(...args);
const isTTSSpeaking = (...args) => refs.isTTSSpeaking(...args);
const hasLocalStopCapableWork = (...args) => refs.hasLocalStopCapableWork(...args);
const hasPendingOverlayTurn = (...args) => refs.hasPendingOverlayTurn(...args);
const hasRemoteAssistantWork = (...args) => refs.hasRemoteAssistantWork(...args);
const startVoiceLifecycleOp = (...args) => refs.startVoiceLifecycleOp(...args);
const setVoiceLifecycle = (...args) => refs.setVoiceLifecycle(...args);
const shouldStopInUiClick = (...args) => refs.shouldStopInUiClick(...args);
const sttCancel = (...args) => refs.sttCancel(...args);
const maybeHandleInlineBugReport = (...args) => refs.maybeHandleInlineBugReport(...args);
const maybeHandleDictationCommand = (...args) => refs.maybeHandleDictationCommand(...args);

const STOP_REQUEST_TIMEOUT_MS = 3500;
const VOICE_TRANSCRIPT_SUBMIT_GUARD_MS = 220;

function buildCursorPayload(anchor) {
  if (!anchor || typeof anchor !== 'object') return null;
  const payload = {
    view: String(anchor.view || '').trim(),
    element: String(anchor.element || '').trim(),
    title: String(anchor.title || '').trim(),
    page: Number.parseInt(String(anchor.page || ''), 10) || 0,
    line: Number.parseInt(String(anchor.line || ''), 10) || 0,
    relative_x: Number(anchor.relativeX),
    relative_y: Number(anchor.relativeY),
    selected_text: String(anchor.selectedText || '').trim(),
    surrounding_text: String(anchor.surroundingText || '').trim(),
    item_id: Number.parseInt(String(anchor.itemID || anchor.item_id || ''), 10) || 0,
    item_title: String(anchor.itemTitle || anchor.item_title || '').trim(),
    item_state: String(anchor.itemState || anchor.item_state || '').trim(),
    workspace_id: Number.parseInt(String(anchor.workspaceID || anchor.workspace_id || ''), 10) || 0,
    workspace_name: String(anchor.workspaceName || anchor.workspace_name || '').trim(),
    path: String(anchor.path || '').trim(),
    is_dir: anchor.isDir === true || anchor.is_dir === true,
  };
  if (!Number.isFinite(payload.relative_x)) delete payload.relative_x;
  if (!Number.isFinite(payload.relative_y)) delete payload.relative_y;
  if (!payload.view) delete payload.view;
  if (!payload.element) delete payload.element;
  if (!payload.title) delete payload.title;
  if (!payload.page) delete payload.page;
  if (!payload.line) delete payload.line;
  if (!payload.selected_text) delete payload.selected_text;
  if (!payload.surrounding_text) delete payload.surrounding_text;
  if (!payload.item_id) delete payload.item_id;
  if (!payload.item_title) delete payload.item_title;
  if (!payload.item_state) delete payload.item_state;
  if (!payload.workspace_id) delete payload.workspace_id;
  if (!payload.workspace_name) delete payload.workspace_name;
  if (!payload.path) delete payload.path;
  if (!payload.is_dir) delete payload.is_dir;
  if (Object.keys(payload).length === 0) return null;
  return payload;
}

function mergeCursorPayload(primary, fallback) {
  const base = primary && typeof primary === 'object' ? { ...primary } : {};
  const extra = fallback && typeof fallback === 'object' ? fallback : null;
  if (!extra) {
    return Object.keys(base).length > 0 ? base : null;
  }
  Object.entries(extra).forEach(([key, value]) => {
    if (base[key] !== undefined && base[key] !== null && base[key] !== '' && base[key] !== 0 && base[key] !== false) {
      return;
    }
    if (value === undefined || value === null || value === '' || value === 0 || value === false) {
      return;
    }
    base[key] = value;
  });
  return Object.keys(base).length > 0 ? base : null;
}

function buildSidebarSelectionCursorPayload() {
  const activeProject = Array.isArray(state.projects)
    ? state.projects.find((project) => String(project?.id || '') === String(state.activeProjectId || ''))
    : null;
  if (!state.prReviewDrawerOpen) return null;
  if (state.fileSidebarMode === 'items') {
    const activeID = Number(state.itemSidebarActiveItemID || 0);
    if (activeID <= 0) return null;
    const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
    const item = items.find((entry) => Number(entry?.id || 0) === activeID);
    if (!item) return null;
    return buildCursorPayload({
      view: String(state.itemSidebarView || 'items').trim().toLowerCase(),
      element: 'item_row',
      itemID: activeID,
      itemTitle: String(item?.title || '').trim(),
      itemState: String(item?.state || state.itemSidebarView || '').trim().toLowerCase(),
      workspaceID: Number(item?.workspace_id || 0),
      workspaceName: String(item?.workspace_name || '').trim(),
      title: String(item?.title || '').trim(),
    });
  }
  if (state.fileSidebarMode === 'workspace') {
    const path = String(state.workspaceBrowserActivePath || '').trim();
    if (!path) return null;
    return buildCursorPayload({
      view: 'workspace_browser',
      element: state.workspaceBrowserActiveIsDir ? 'workspace_folder' : 'workspace_file',
      workspaceID: String(activeProject?.id || '').trim(),
      workspaceName: String(activeProject?.name || '').trim(),
      path,
      title: path,
      isDir: Boolean(state.workspaceBrowserActiveIsDir),
    });
  }
  return null;
}

export function setPendingSubmit(controller, kind = '') {
  state.pendingSubmitController = controller || null;
  state.pendingSubmitKind = String(kind || '').trim();
}

export function clearPendingSubmit(controller = null) {
  if (controller && state.pendingSubmitController !== controller) return;
  state.pendingSubmitController = null;
  state.pendingSubmitKind = '';
}

export function abortPendingSubmit(kind = '') {
  const controller = state.pendingSubmitController;
  if (!controller) return false;
  const requiredKind = String(kind || '').trim();
  if (requiredKind && state.pendingSubmitKind !== requiredKind) return false;
  clearPendingSubmit(controller);
  try { controller.abort(); } catch (_) {}
  return true;
}

export function abortError() {
  try {
    return new DOMException('aborted', 'AbortError');
  } catch (_) {
    const err = new Error('aborted');
    err.name = 'AbortError';
    return err;
  }
}

export function waitWithAbort(delayMs, signal) {
  const ms = Number(delayMs);
  if (!Number.isFinite(ms) || ms <= 0) return Promise.resolve();
  if (!signal) {
    return new Promise((resolve) => window.setTimeout(resolve, ms));
  }
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer);
      signal.removeEventListener('abort', onAbort);
      reject(abortError());
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

export async function submitMessage(text, options = {}) {
  const trimmed = String(text || '').trim();
  const submitKind = String(options?.kind || '').trim();
  if (!trimmed || !state.chatSessionId) {
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = false;
    }
    return false;
  }
  cancelLiveSessionListen();
  startVoiceLifecycleOp('submit-message');
  if (await maybeHandleDictationCommand(trimmed)) {
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = false;
      state.voiceAwaitingTurn = false;
    }
    return true;
  }
  let submitController = null;
  if (submitKind) {
    submitController = new AbortController();
    setPendingSubmit(submitController, submitKind);
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = true;
    }
  }
  state.indicatorSuppressedByCanvasUpdate = false;
  stopTTSPlayback();
  if (await maybeHandleInlineBugReport(trimmed, {
    trigger: submitKind === 'voice_transcript' ? 'voice' : 'chat',
  })) {
    clearPendingSubmit(submitController);
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = false;
    }
    return true;
  }
  let finalText = trimmed;
  const anchor = getInputAnchor();
  if (anchor) {
    const prefix = buildContextPrefix(anchor);
    if (prefix) finalText = `${prefix} ${finalText}`;
    setInputAnchor(null);
    clearLineHighlight();
  }
  state.assistantLastError = '';
  updateAssistantActivityIndicator();
  appendPlainMessage('user', finalText);

  if (!finalText.startsWith('/') && (isVoiceTurn() || isMobileSilent())) {
    const pending = appendRenderedAssistant('_Thinking..._', { pending: true, localId: nextLocalMessageId() });
    state.pendingQueue.push(pending);
    updateAssistantActivityIndicator();
  }

  const body = {
    text: finalText,
    output_mode: state.ttsSilent ? 'silent' : 'voice',
    capture_mode: submitKind === 'voice_transcript' ? 'voice' : 'text',
  };
  const cursorPayload = mergeCursorPayload(buildCursorPayload(anchor), buildSidebarSelectionCursorPayload());
  if (cursorPayload) {
    body.cursor = cursorPayload;
  }
  try {
    if (submitKind === 'voice_transcript' && submitController) {
      await waitWithAbort(VOICE_TRANSCRIPT_SUBMIT_GUARD_MS, submitController.signal);
      if (submitController.signal.aborted) {
        throw abortError();
      }
    }
    const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(state.chatSessionId)}/messages`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal: submitController ? submitController.signal : undefined,
    });
    if (!resp.ok) {
      state.voiceAwaitingTurn = false;
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      appendPlainMessage('system', `Send failed: ${detail}`);
      updateOverlay(`**Send failed:** ${detail}`);
      updateAssistantActivityIndicator();
      return false;
    }
    const payload = await resp.json();
    if (payload?.kind === 'command') {
      const commandName = String(payload?.result?.name || '').trim().toLowerCase();
      if (commandName === 'pr') {
        state.prReviewAwaitingArtifact = true;
      }
      if (payload?.result?.message) {
        appendPlainMessage('system', String(payload.result.message));
      }
    }
    return true;
  } catch (err) {
    if (err && (err.name === 'AbortError' || String(err?.message || '').toLowerCase().includes('aborted'))) {
      state.voiceAwaitingTurn = false;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      showStatus('stopped');
      updateAssistantActivityIndicator();
      return false;
    }
    state.voiceAwaitingTurn = false;
    const pending = takePendingRow('');
    pending?.remove();
    trackAssistantTurnFinished('');
    appendPlainMessage('system', `Send failed: ${String(err?.message || err)}`);
    updateOverlay(`**Send failed:** ${String(err?.message || err)}`);
    updateAssistantActivityIndicator();
    return false;
  } finally {
    clearPendingSubmit(submitController);
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = false;
    }
  }
}

export function forceVoiceLifecycleIdle(statusText = 'stopped') {
  cancelLiveSessionListen();
  state.voiceTranscriptSubmitInFlight = false;
  abortPendingSubmit('voice_transcript');
  sttCancel();
  stopTTSPlayback();
  if (state.chatVoiceCapture) {
    stopChatVoiceMedia(state.chatVoiceCapture);
    state.chatVoiceCapture = null;
  }
  setRecording(false);
  state.voiceAwaitingTurn = false;
  state.indicatorSuppressedByCanvasUpdate = false;
  state.assistantCancelInFlight = false;
  state.assistantActiveTurns.clear();
  state.assistantUnknownTurns = 0;
  state.voiceTurns.clear();
  for (const row of state.pendingByTurn.values()) {
    if (row instanceof HTMLElement) updateAssistantRow(row, '_Stopped._', false);
  }
  for (const row of state.pendingQueue) {
    if (row instanceof HTMLElement) updateAssistantRow(row, '_Stopped._', false);
  }
  state.pendingByTurn.clear();
  state.pendingQueue = [];
  hideOverlay();
  showStatus(statusText);
  setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'force-idle');
  updateAssistantActivityIndicator();
}

export async function cancelActiveAssistantTurn(options = null) {
  const force = Boolean(options && options.force);
  const silent = Boolean(options && options.silent);
  if (!state.chatSessionId || state.assistantCancelInFlight || (silent && state.assistantSilentCancelInFlight)) return false;
  if (!force) {
    await refreshAssistantActivity();
    if (!isAssistantWorking()) {
      if (!silent) {
        showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
        updateAssistantActivityIndicator();
      }
      return false;
    }
  }
  if (!silent) {
    state.assistantCancelInFlight = true;
    updateAssistantActivityIndicator();
    showStatus('stopping...');
  } else {
    state.assistantSilentCancelInFlight = true;
  }
  let canceled = 0;
  let timeoutId = null;
  try {
    const controller = new AbortController();
    timeoutId = window.setTimeout(() => {
      controller.abort();
    }, STOP_REQUEST_TIMEOUT_MS);
    const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(state.chatSessionId)}/cancel`), {
      method: 'POST',
      signal: controller.signal,
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      if (!silent) showStatus(`stop failed: ${detail}`);
      return false;
    }
    const payload = await resp.json();
    canceled = Number(payload?.canceled || 0);
    if (canceled <= 0) {
      await refreshAssistantActivity();
      if (!silent && !isAssistantWorking()) {
        showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
      }
    }
  } catch (err) {
    if (!silent) {
      if (String(err?.name || '') === 'AbortError') {
        showStatus('stop request timed out');
      } else {
        showStatus(`stop failed: ${String(err?.message || err)}`);
      }
    }
    return false;
  } finally {
    if (timeoutId !== null) {
      window.clearTimeout(timeoutId);
      timeoutId = null;
    }
    if (!silent) {
      state.assistantCancelInFlight = false;
      updateAssistantActivityIndicator();
    } else {
      state.assistantSilentCancelInFlight = false;
    }
    window.setTimeout(() => { void refreshAssistantActivity(); }, 120);
  }
  return canceled > 0;
}

export async function cancelActiveAssistantTurnWithRetry(maxAttempts = 3, options = null) {
  const silent = Boolean(options && options.silent);
  const attempts = Number.isFinite(maxAttempts) ? Math.max(1, Math.floor(maxAttempts)) : 1;
  for (let i = 0; i < attempts; i += 1) {
    const canceled = await cancelActiveAssistantTurn({ force: true, silent });
    if (canceled) return true;
    await refreshAssistantActivity();
    if (!isAssistantWorking()) return false;
    if (i + 1 < attempts) {
      await new Promise((resolve) => window.setTimeout(resolve, 140));
    }
  }
  return false;
}

export async function handleStopAction() {
  startVoiceLifecycleOp('stop-action');
  if (isLiveSessionListenActive()) {
    cancelLiveSessionListen();
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'stop-listening');
    showStatus('ready');
    updateAssistantActivityIndicator();
    return;
  }
  if (state.liveSessionActive && !state.chatVoiceCapture && !isAssistantWorking()) {
    await deactivateLiveSession({ disableMeetingConfig: true });
    return;
  }

  const capture = state.chatVoiceCapture;
  if (capture && capture.stopping) {
    if (!isVoiceTranscriptSubmitPending() && !state.voiceTranscriptSubmitInFlight) {
      return;
    }
  }
  const isCaptureActive = Boolean(capture && !capture.stopping);
  if (isCaptureActive) {
    await stopVoiceCaptureAndSend();
    return;
  }

  if (isTTSSpeaking()) {
    stopTTSPlayback();
  }

  const localStopCapable = shouldStopInUiClick()
    || hasLocalStopCapableWork()
    || state.voiceAwaitingTurn
    || state.voiceTranscriptSubmitInFlight
    || isVoiceTranscriptSubmitPending()
    || hasPendingOverlayTurn();
  if (!localStopCapable && !hasRemoteAssistantWork()) return;
  forceVoiceLifecycleIdle('stopped');
  void cancelActiveAssistantTurnWithRetry(3, { silent: true }).finally(() => {
    void refreshAssistantActivity();
  });
}
