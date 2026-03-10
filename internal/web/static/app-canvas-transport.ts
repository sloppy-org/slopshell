import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const clearWelcomeSurface = (...args) => refs.clearWelcomeSurface(...args);
const showWelcomeForActiveProject = (...args) => refs.showWelcomeForActiveProject(...args);
const exitPrReviewMode = (...args) => refs.exitPrReviewMode(...args);
const maybeEnterPrReviewModeFromTextArtifact = (...args) => refs.maybeEnterPrReviewModeFromTextArtifact(...args);
const isLikelyPrReviewArtifact = (...args) => refs.isLikelyPrReviewArtifact(...args);
const paneIdForCanvasKind = (...args) => refs.paneIdForCanvasKind(...args);
const isRealCanvasArtifactEvent = (...args) => refs.isRealCanvasArtifactEvent(...args);
const showCanvasColumn = (...args) => refs.showCanvasColumn(...args);
const hideCanvasColumn = (...args) => refs.hideCanvasColumn(...args);
const isMobileSilent = (...args) => refs.isMobileSilent(...args);
const exitArtifactEditMode = (...args) => refs.exitArtifactEditMode(...args);
const isArtifactEditorActive = (...args) => refs.isArtifactEditorActive(...args);
const resetMailDraftState = (...args) => refs.resetMailDraftState(...args);

export function applyCanvasArtifactEvent(payload) {
  clearWelcomeSurface();
  clearInkDraft();
  if (isArtifactEditorActive()) {
    exitArtifactEditMode({ applyChanges: false });
  }
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind !== 'email_draft') {
    resetMailDraftState();
  }
  if (kind === 'clear_canvas') {
    state.currentCanvasArtifact = {
      kind: '',
      artifactKind: '',
      title: '',
      surfaceDefault: '',
    };
    state.prReviewAwaitingArtifact = false;
    state.workspaceOpenFilePath = '';
    state.workspaceStepInFlight = false;
    exitPrReviewMode();
    renderCanvas(payload);
    hideCanvasColumn();
    void showWelcomeForActiveProject(true);
    return;
  }

  let handledByPrReview = false;
  const textArtifact = kind === 'text_artifact' || kind === 'text';
  if (textArtifact && (state.prReviewAwaitingArtifact || state.prReviewMode || isLikelyPrReviewArtifact(payload))) {
    handledByPrReview = maybeEnterPrReviewModeFromTextArtifact(payload);
  }
  if (state.prReviewAwaitingArtifact) {
    state.prReviewAwaitingArtifact = false;
  }
  if (!handledByPrReview) {
    state.workspaceOpenFilePath = '';
    state.workspaceStepInFlight = false;
    exitPrReviewMode();
  }

  if (!handledByPrReview && state.prReviewMode) {
    exitPrReviewMode();
  }

  if (!handledByPrReview) {
    renderCanvas(payload);
  }

  const meta = payload?.meta && typeof payload.meta === 'object' ? payload.meta : {};
  const hintedSurface = String(
    meta?.surface_default ?? payload?.surface_default ?? '',
  ).trim().toLowerCase();
  const artifactKind = String(meta?.artifact_kind || '').trim().toLowerCase();
  state.currentCanvasArtifact = {
    kind,
    artifactKind,
    title: String(payload?.title || '').trim(),
    surfaceDefault: hintedSurface === 'editor' ? 'editor' : (hintedSurface === 'annotate' ? 'annotate' : ''),
  };

  if (kind) {
    state.indicatorSuppressedByCanvasUpdate = true;
    updateAssistantActivityIndicator();
  }

  const paneId = paneIdForCanvasKind(payload.kind);
  if (!paneId) return;
  const realCanvasArtifact = isRealCanvasArtifactEvent(payload);
  showCanvasColumn(paneId);
  state.canvasActionThisTurn = state.canvasActionThisTurn || realCanvasArtifact;
  if (isMobileSilent() && realCanvasArtifact) {
    const edgeRight = document.getElementById('edge-right');
    if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
  }
}

export function openCanvasWs() {
  const turnToken = state.canvasWsToken + 1;
  state.canvasWsToken = turnToken;
  const targetSessionID = String(state.sessionId || 'local');
  const ws = new WebSocket(wsURL(`canvas/${encodeURIComponent(targetSessionID)}`));
  state.canvasWs = ws;

  ws.onopen = () => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    void loadCanvasSnapshot(targetSessionID);
  };

  ws.onmessage = (event) => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    try {
      const payload = JSON.parse(event.data);
      applyCanvasArtifactEvent(payload);
    } catch (_) {}
  };

  ws.onclose = () => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    state.canvasWs = null;
    window.setTimeout(() => {
      if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
      openCanvasWs();
    }, 1200);
  };
}

export async function loadCanvasSnapshot(sessionID = state.sessionId) {
  try {
    const resp = await fetch(apiURL(`canvas/${encodeURIComponent(sessionID)}/snapshot`));
    if (!resp.ok) {
      if (!state.hasArtifact) {
        exitPrReviewMode();
        clearCanvas();
        await showWelcomeForActiveProject(true);
      }
      return;
    }
    const payload = await resp.json();
    if (payload?.event) {
      applyCanvasArtifactEvent(payload.event);
      return;
    }
    if (!state.hasArtifact) {
      exitPrReviewMode();
      clearCanvas();
      await showWelcomeForActiveProject(true);
    }
  } catch (_) {
    if (!state.hasArtifact) {
      exitPrReviewMode();
      clearCanvas();
      await showWelcomeForActiveProject(true);
    }
  }
}
