import * as env from './app-env.js';
import * as context from './app-context.js';

const {
  clearLineHighlight,
  getUiState,
  setUiMode,
  showIndicatorMode,
  hideIndicator,
  hideOverlay,
  isRecording,
  getLastInputPosition,
  isLiveSessionListenActive,
  isHotwordActive,
} = env;

const { refs, state, isVoiceTurn, VOICE_LIFECYCLE } = context;

const exitArtifactEditMode = (...args) => refs.exitArtifactEditMode(...args);
const exitPrReviewMode = (...args) => refs.exitPrReviewMode(...args);
const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const persistLastView = (...args) => refs.persistLastView(...args);
const shouldShowCompanionIdleSurface = (...args) => refs.shouldShowCompanionIdleSurface(...args);
const updateCompanionIdleSurface = (...args) => refs.updateCompanionIdleSurface(...args);
const requestHotwordSync = (...args) => refs.requestHotwordSync(...args);
const canSpeakTTS = (...args) => refs.canSpeakTTS(...args);
const isDialogueLiveSession = (...args) => refs.isDialogueLiveSession(...args);
const beginVoiceCapture = (...args) => refs.beginVoiceCapture(...args);

export function showCanvasColumn(paneId) {
  const col = document.getElementById('canvas-column');
  if (!col) return;
  if (paneId !== 'canvas-text' && state.artifactEditMode) {
    exitArtifactEditMode({ applyChanges: true });
  }
  if (paneId !== 'canvas-text') {
    exitPrReviewMode();
  }
  const viewport = col.querySelector('#canvas-viewport');
  if (viewport) {
    viewport.querySelectorAll('.canvas-pane').forEach((p) => {
      p.style.display = 'none';
      p.classList.remove('is-active');
    });
    const target = document.getElementById(paneId);
    if (target) {
      target.style.display = '';
      target.classList.add('is-active');
    }
  }
  state.hasArtifact = true;
  setUiMode('artifact');
  persistLastView({ mode: 'artifact' });
  if (!isVoiceTurn() && isDirectAssistantWorking()) {
    hideOverlay();
  }
  updateAssistantActivityIndicator();
}

export function hideCanvasColumn() {
  if (state.artifactEditMode) {
    exitArtifactEditMode({ applyChanges: true });
  }
  exitPrReviewMode();
  clearInkDraft();
  state.hasArtifact = false;
  state.workspaceOpenFilePath = '';
  state.workspaceStepInFlight = false;
  setUiMode('rasa');
  clearLineHighlight();
  persistLastView({ mode: 'rasa' });
  const viewport = document.getElementById('canvas-viewport');
  if (viewport) {
    viewport.querySelectorAll('.canvas-pane').forEach((p) => {
      p.style.display = 'none';
      p.classList.remove('is-active');
    });
  }
  updateAssistantActivityIndicator();
}

export function chatHistoryEl() {
  return document.getElementById('chat-history');
}

export function scrollChatToBottom(host) {
  if (!(host instanceof HTMLElement)) return;
  host.scrollTop = host.scrollHeight;
}

export function syncChatScroll(host) {
  if (!(host instanceof HTMLElement)) return;
  scrollChatToBottom(host);
  window.requestAnimationFrame(() => scrollChatToBottom(host));
}

export function setChatMode(mode) {
  const normalized = String(mode || 'chat').toLowerCase();
  state.chatMode = normalized === 'plan' || normalized === 'review' ? normalized : 'chat';
  const pill = document.getElementById('chat-mode-pill');
  if (pill) {
    pill.textContent = state.chatMode;
    pill.className = `badge ${state.chatMode === 'plan' || state.chatMode === 'review' ? 'review' : ''}`;
  }
}

export function hasLocalAssistantWork() {
  return state.pendingQueue.length > 0
    || state.pendingByTurn.size > 0
    || state.assistantActiveTurns.size > 0
    || state.assistantUnknownTurns > 0;
}

export function hasRemoteAssistantWork() {
  return state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0;
}

export function hasLocalStopCapableWork() {
  return state.assistantActiveTurns.size > 0
    || state.assistantUnknownTurns > 0
    || state.assistantCancelInFlight;
}

export function isVoiceTranscriptSubmitPending() {
  return Boolean(state.pendingSubmitController) && state.pendingSubmitKind === 'voice_transcript';
}

export function hasPendingOverlayTurn() {
  const ui = getUiState();
  if (!ui || !ui.overlayVisible) return false;
  return Boolean(String(ui.overlayTurnId || '').trim());
}

export function isDirectAssistantWorking() {
  return hasLocalStopCapableWork()
    || state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0;
}

export function isAssistantWorking() {
  return isDirectAssistantWorking();
}

export function isTTSSpeaking() {
  return state.ttsPlaying;
}

export function startVoiceLifecycleOp(reason = '') {
  state.voiceLifecycleSeq += 1;
  state.voiceLifecycleReason = String(reason || '');
  return state.voiceLifecycleSeq;
}

export function setVoiceLifecycle(next, reason = '') {
  const normalized = Object.values(VOICE_LIFECYCLE).includes(next)
    ? next
    : VOICE_LIFECYCLE.IDLE;
  state.voiceLifecycle = normalized;
  if (reason) {
    state.voiceLifecycleReason = String(reason);
  }
  return state.voiceLifecycle;
}

export function deriveVoiceLifecycle() {
  if (isRecording()) return VOICE_LIFECYCLE.RECORDING;
  if (state.chatVoiceCapture?.stopping) return VOICE_LIFECYCLE.STOPPING_RECORDING;
  if (state.voiceAwaitingTurn) return VOICE_LIFECYCLE.AWAITING_TURN;
  if (isLiveSessionListenActive()) return VOICE_LIFECYCLE.LISTENING;
  if (hasLocalStopCapableWork()) return VOICE_LIFECYCLE.ASSISTANT_WORKING;
  if (isTTSSpeaking()) return VOICE_LIFECYCLE.TTS_PLAYING;
  return VOICE_LIFECYCLE.IDLE;
}

export function syncVoiceLifecycle(reason = '') {
  return setVoiceLifecycle(deriveVoiceLifecycle(), reason);
}

export function isStopCapableLifecycle(mode = state.voiceLifecycle) {
  return mode === VOICE_LIFECYCLE.LISTENING
    || mode === VOICE_LIFECYCLE.STOPPING_RECORDING
    || mode === VOICE_LIFECYCLE.AWAITING_TURN
    || mode === VOICE_LIFECYCLE.ASSISTANT_WORKING;
}

export function isUiReadyForStatus() {
  const mode = syncVoiceLifecycle('ready-check');
  return mode === VOICE_LIFECYCLE.IDLE;
}

export function canStartLiveDialogueListen() {
  if (!canSpeakTTS()) return false;
  if (!isDialogueLiveSession()) return false;
  const mode = syncVoiceLifecycle('can-start-live-dialogue');
  if (mode === VOICE_LIFECYCLE.RECORDING || mode === VOICE_LIFECYCLE.STOPPING_RECORDING) return false;
  if (mode === VOICE_LIFECYCLE.TTS_PLAYING) return false;
  if (state.chatVoiceCapture) return false;
  if (mode !== VOICE_LIFECYCLE.LISTENING && isStopCapableLifecycle(mode)) return false;
  return true;
}

export function beginConversationVoiceCapture() {
  const x = Math.floor(window.innerWidth / 2);
  const y = Math.floor(window.innerHeight / 2);
  void beginVoiceCapture(x, y, null, { hotwordTriggered: true });
}

export function currentIndicatorMode() {
  if (shouldShowCompanionIdleSurface()) return '';
  const uiState = getUiState();
  const mode = state.voiceLifecycle;
  if (mode === VOICE_LIFECYCLE.RECORDING) return 'recording';
  if (state.liveSessionActive && uiState.cursorPinned) return 'cursor';
  if (mode === VOICE_LIFECYCLE.LISTENING) return 'listening';
  if (isStopCapableLifecycle(mode)) return 'play';
  if (state.liveSessionActive && state.hotwordActive) return 'paused';
  if (state.indicatorSuppressedByCanvasUpdate) return '';
  return '';
}

export function shouldStopInUiClick() {
  return isStopCapableLifecycle(syncVoiceLifecycle('ui-stop-check'));
}

export function isUiStopGestureActive() {
  return shouldStopInUiClick()
    || isVoiceTranscriptSubmitPending()
    || state.voiceTranscriptSubmitInFlight
    || hasPendingOverlayTurn();
}

export function updateAssistantActivityIndicator() {
  if (!hasLocalAssistantWork() && state.assistantRemoteActiveCount <= 0 && state.assistantRemoteQueuedCount <= 0) {
    state.assistantUnknownTurns = 0;
    state.assistantActiveTurns.clear();
  }
  syncVoiceLifecycle('indicator-update');
  state.hotwordActive = isHotwordActive();
  updateCompanionIdleSurface();
  const pos = getLastInputPosition();
  const px = Number.isFinite(pos?.x) && pos.x > 0 ? pos.x : Math.floor(window.innerWidth / 2);
  const py = Number.isFinite(pos?.y) && pos.y > 0 ? pos.y : Math.floor(window.innerHeight / 2);
  const mode = currentIndicatorMode();
  if (mode) {
    showIndicatorMode(mode, px, py);
  } else {
    hideIndicator();
  }
  requestHotwordSync();
}

export function paneIdForCanvasKind(kind) {
  const normalized = String(kind || '').trim().toLowerCase();
  if (normalized === 'image_artifact' || normalized === 'image') return 'canvas-image';
  if (normalized === 'pdf_artifact' || normalized === 'pdf') return 'canvas-pdf';
  if (normalized === 'text_artifact' || normalized === 'text') return 'canvas-text';
  return '';
}

export function isTemporaryCanvasArtifactTitle(title) {
  const normalized = String(title || '')
    .trim()
    .replaceAll('\\', '/')
    .replace(/^\.\//, '')
    .toLowerCase();
  return normalized.startsWith('.tabura/artifacts/tmp/')
    || normalized.startsWith('tabura/artifacts/tmp/');
}

export function isRealCanvasArtifactEvent(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (!kind || kind === 'clear_canvas') return false;
  if (kind === 'image_artifact' || kind === 'image' || kind === 'pdf_artifact' || kind === 'pdf') {
    return true;
  }
  if (kind !== 'text_artifact' && kind !== 'text') return false;

  const meta = payload?.meta;
  if (meta && typeof meta === 'object' && typeof meta.real_artifact === 'boolean') {
    return meta.real_artifact;
  }

  const title = String(payload?.title || '').trim();
  if (!title) return false;
  return !isTemporaryCanvasArtifactTitle(title);
}
