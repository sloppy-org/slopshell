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

const STOP_REQUEST_TIMEOUT_MS = 3500;
const VOICE_TRANSCRIPT_SUBMIT_GUARD_MS = 220;

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
    return;
  }
  cancelLiveSessionListen();
  startVoiceLifecycleOp('submit-message');
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
    return;
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
    input_mode: submitKind === 'voice_transcript' ? 'voice' : 'text',
  };
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
      return;
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
  } catch (err) {
    if (err && (err.name === 'AbortError' || String(err?.message || '').toLowerCase().includes('aborted'))) {
      state.voiceAwaitingTurn = false;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      showStatus('stopped');
      updateAssistantActivityIndicator();
      return;
    }
    state.voiceAwaitingTurn = false;
    const pending = takePendingRow('');
    pending?.remove();
    trackAssistantTurnFinished('');
    appendPlainMessage('system', `Send failed: ${String(err?.message || err)}`);
    updateOverlay(`**Send failed:** ${String(err?.message || err)}`);
    updateAssistantActivityIndicator();
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
