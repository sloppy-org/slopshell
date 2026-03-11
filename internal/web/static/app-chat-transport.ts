import * as env from './app-env.js';
import * as context from './app-context.js';
import { openCanonicalActionCommandCenter } from './app-command-center.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, resolveCanvasApprovalRequest, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const renderEdgeTopModelButtons = (...args) => refs.renderEdgeTopModelButtons(...args);
const renderEdgeTopProjects = (...args) => refs.renderEdgeTopProjects(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const stopTTSPlayback = (...args) => refs.stopTTSPlayback(...args);
const clearChatHistory = (...args) => refs.clearChatHistory(...args);
const clearWelcomeSurface = (...args) => refs.clearWelcomeSurface(...args);
const refreshWorkspaceBrowser = (...args) => refs.refreshWorkspaceBrowser(...args);
const showWelcomeForActiveProject = (...args) => refs.showWelcomeForActiveProject(...args);
const loadChatHistory = (...args) => refs.loadChatHistory(...args);
const refreshAssistantActivity = (...args) => refs.refreshAssistantActivity(...args);
const refreshCompanionState = (...args) => refs.refreshCompanionState(...args);
const enqueueTTSAudio = (...args) => refs.enqueueTTSAudio(...args);
const setTTSSpeakLang = (...args) => refs.setTTSSpeakLang(...args);
const getTTSLastSpeakText = (...args) => refs.getTTSLastSpeakText(...args);
const flushTTSChunker = (...args) => refs.flushTTSChunker(...args);
const hasTTSPlayer = (...args) => refs.hasTTSPlayer(...args);
const openCanvasWs = (...args) => refs.openCanvasWs(...args);
const openItemSidebarView = (...args) => refs.openItemSidebarView(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const refreshItemSidebarCounts = (...args) => refs.refreshItemSidebarCounts(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);
const appendRenderedAssistant = (...args) => refs.appendRenderedAssistant(...args);
const ensurePendingForTurn = (...args) => refs.ensurePendingForTurn(...args);
const takePendingRow = (...args) => refs.takePendingRow(...args);
const takeAnyPendingRow = (...args) => refs.takeAnyPendingRow(...args);
const updateAssistantRow = (...args) => refs.updateAssistantRow(...args);
const trackAssistantTurnStarted = (...args) => refs.trackAssistantTurnStarted(...args);
const trackAssistantTurnFinished = (...args) => refs.trackAssistantTurnFinished(...args);
const activeProjectKey = (...args) => refs.activeProjectKey(...args);
const setChatMode = (...args) => refs.setChatMode(...args);
const resetCompanionState = (...args) => refs.resetCompanionState(...args);
const applyCompanionState = (...args) => refs.applyCompanionState(...args);
const setActiveProjectID = (...args) => refs.setActiveProjectID(...args);
const activateProject = (...args) => refs.activateProject(...args);
const showCanvasColumn = (...args) => refs.showCanvasColumn(...args);
const hideCanvasColumn = (...args) => refs.hideCanvasColumn(...args);
const stopVoiceCaptureAndSend = (...args) => refs.stopVoiceCaptureAndSend(...args);
const cancelChatVoiceCapture = (...args) => refs.cancelChatVoiceCapture(...args);
const activateLiveSession = (...args) => refs.activateLiveSession(...args);
const deactivateLiveSession = (...args) => refs.deactivateLiveSession(...args);
const normalizeItemSidebarView = (...args) => refs.normalizeItemSidebarView(...args);
const buildCursorPayload = (...args) => refs.buildCursorPayload(...args);
const toggleTTSSilentMode = (...args) => refs.toggleTTSSilentMode(...args);
const canSpeakTTS = (...args) => refs.canSpeakTTS(...args);
const applyLiveSessionStateSnapshot = (...args) => refs.applyLiveSessionStateSnapshot(...args);
const isDialogueLiveSession = (...args) => refs.isDialogueLiveSession(...args);
const isMeetingLiveSession = (...args) => refs.isMeetingLiveSession(...args);
const isMobileSilent = (...args) => refs.isMobileSilent(...args);
const extractTTSText = (...args) => refs.extractTTSText(...args);
const computeTTSDiff = (...args) => refs.computeTTSDiff(...args);
const queueTTSDiff = (...args) => refs.queueTTSDiff(...args);
const normalizeProjectChatModelReasoningEffort = (...args) => refs.normalizeProjectChatModelReasoningEffort(...args);
const normalizeProjectChatModelAlias = (...args) => refs.normalizeProjectChatModelAlias(...args);
const upsertProject = (...args) => refs.upsertProject(...args);
const defaultItemSidebarCounts = (...args) => refs.defaultItemSidebarCounts(...args);
const setInboxTriggerCount = (...args) => refs.setInboxTriggerCount(...args);
const resetAssistantTurnTracking = (...args) => refs.resetAssistantTurnTracking(...args);
const startVoiceLifecycleOp = (...args) => refs.startVoiceLifecycleOp(...args);
const setVoiceLifecycle = (...args) => refs.setVoiceLifecycle(...args);
const shouldStopInUiClick = (...args) => refs.shouldStopInUiClick(...args);
const handleSTTWSMessage = (...args) => refs.handleSTTWSMessage(...args);
const sttCancel = (...args) => refs.sttCancel(...args);
const switchProjectChatModel = (...args) => refs.switchProjectChatModel(...args);
const parseOptionalBoolean = (...args) => refs.parseOptionalBoolean(...args);
const setSomedayReviewNudgeEnabled = (...args) => refs.setSomedayReviewNudgeEnabled(...args);
const openPrintView = (...args) => refs.openPrintView(...args);
const renderApprovalRequestCard = (...args) => refs.renderApprovalRequestCard(...args);
const resolveApprovalRequestCard = (...args) => refs.resolveApprovalRequestCard(...args);
const cleanForOverlay = (...args) => refs.cleanForOverlay(...args);
const formatItemCompletedLabel = (...args) => refs.formatItemCompletedLabel(...args);
const appendAssistantProgressForTurn = (...args) => refs.appendAssistantProgressForTurn(...args);
const nextLocalMessageId = (...args) => refs.nextLocalMessageId(...args);
const stopChatVoiceMedia = (...args) => refs.stopChatVoiceMedia(...args);
const isAssistantWorking = (...args) => refs.isAssistantWorking(...args);
const isVoiceTranscriptSubmitPending = (...args) => refs.isVoiceTranscriptSubmitPending(...args);
const isTTSSpeaking = (...args) => refs.isTTSSpeaking(...args);
const hasLocalStopCapableWork = (...args) => refs.hasLocalStopCapableWork(...args);
const hasPendingOverlayTurn = (...args) => refs.hasPendingOverlayTurn(...args);
const hasRemoteAssistantWork = (...args) => refs.hasRemoteAssistantWork(...args);
const maybeHandleInlineBugReport = (...args) => refs.maybeHandleInlineBugReport(...args);
const openSidebarItem = (...args) => refs.openSidebarItem(...args);
const loadWorkspaceBrowserPath = (...args) => refs.loadWorkspaceBrowserPath(...args);
const openWorkspaceSidebarFile = (...args) => refs.openWorkspaceSidebarFile(...args);

export function closeChatWs() {
  state.chatWsToken += 1;
  state.chatWsLastMessageAt = 0;
  if (state.chatWs) {
    try { state.chatWs.close(); } catch (_) {}
  }
  state.chatWs = null;
}

export function sendChatWsJSON(payload) {
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) return false;
  ws.send(JSON.stringify(payload));
  return true;
}

export function sendCanvasPositionEvent(anchor, options: Record<string, any> = {}) {
  const cursor = buildCursorPayload(anchor);
  if (!cursor) return false;
  const payload: Record<string, any> = {
    type: 'canvas_position',
    cursor,
    gesture: String(options?.gesture || 'tap').trim().toLowerCase() || 'tap',
    output_mode: state.ttsSilent ? 'silent' : 'voice',
  };
  if (options?.requestResponse) {
    payload.request_response = true;
  }
  return sendChatWsJSON(payload);
}

function approvalRequestCanvasText(payload) {
  const description = String(payload?.description || 'Approval required').trim();
  const action = String(payload?.action || payload?.request_kind || '').trim().replace(/_/g, ' ');
  const reason = String(payload?.reason || '').trim();
  const grantRoot = String(payload?.grant_root || '').trim();
  const lines = ['# Approval required', ''];
  if (description) {
    lines.push(description, '');
  }
  if (action) {
    lines.push(`- Action: ${action}`);
  }
  if (reason) {
    lines.push(`- Reason: ${reason}`);
  }
  if (grantRoot) {
    lines.push(`- Scope: ${grantRoot}`);
  }
  if (lines[lines.length - 1] !== '') {
    lines.push('');
  }
  lines.push('Choose **Approve**, **Reject**, or **Cancel** below to continue.');
  return lines.join('\n');
}

function renderApprovalRequestCanvas(payload) {
  const requestID = String(payload?.request_id || '').trim();
  if (!requestID) return;
  renderCanvas({
    kind: 'text_artifact',
    title: `.tabura/artifacts/tmp/approval-${requestID}.md`,
    text: approvalRequestCanvasText(payload),
    meta: {
      real_artifact: false,
      approval_request: true,
      request_id: requestID,
      action: String(payload?.action || payload?.request_kind || '').trim(),
      reason: String(payload?.reason || '').trim(),
      grant_root: String(payload?.grant_root || '').trim(),
    },
  });
  showCanvasColumn('canvas-text');
}

export function openChatWs() {
  if (!state.chatSessionId) return;
  const turnToken = state.chatWsToken + 1;
  state.chatWsToken = turnToken;
  const targetSessionID = state.chatSessionId;
  const ws = new WebSocket(wsURL(`chat/${encodeURIComponent(targetSessionID)}`));
  ws.binaryType = 'arraybuffer';
  state.chatWs = ws;

  ws.onopen = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    const isReconnect = state.chatWsHasConnected;
    state.chatWsHasConnected = true;
    showStatus('connected');
    if (state.pendingRuntimeReloadStatus) {
      showStatus(state.pendingRuntimeReloadStatus);
      state.pendingRuntimeReloadStatus = '';
    }
    void refreshAssistantActivity();
    if (isReconnect) {
      resetAssistantTurnTracking();
      void loadChatHistory().catch((err) => {
        appendPlainMessage('system', `History sync failed: ${String(err?.message || err)}`);
      });
    }
  };

  ws.onmessage = (event) => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    state.chatWsLastMessageAt = Date.now();
    if (event.data instanceof ArrayBuffer) {
      if (!canSpeakTTS()) return;
      enqueueTTSAudio(event.data);
      return;
    }
    if (event.data instanceof Blob) {
      if (!canSpeakTTS()) return;
      event.data.arrayBuffer()
        .then((audioBuffer) => {
          if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
          if (!canSpeakTTS()) return;
          enqueueTTSAudio(audioBuffer);
        })
        .catch((err) => {
          console.warn('TTS blob decode error:', err);
        });
      return;
    }
    if (typeof event.data !== 'string') return;
    let payload = null;
    try { payload = JSON.parse(event.data); } catch (_) { return; }
    if (handleSTTWSMessage(payload)) return;
    try {
      handleChatEvent(payload);
    } catch (err) {
      console.error('handleChatEvent error:', err);
      const turnID = String(payload?.turn_id || '').trim();
      if (turnID) trackAssistantTurnFinished(turnID);
      state.voiceAwaitingTurn = false;
      appendPlainMessage('system', `Internal error: ${String(err?.message || err)}`);
      showStatus('error');
      updateAssistantActivityIndicator();
    }
  };

  ws.onclose = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    cancelLiveSessionListen();
    if (isMeetingLiveSession()) {
      stopLiveSession();
      applyLiveSessionStateSnapshot();
    }
    if (state.chatVoiceCapture || state.voiceAwaitingTurn) {
      cancelChatVoiceCapture();
      sttCancel();
      state.voiceAwaitingTurn = false;
      updateAssistantActivityIndicator();
    }
    state.chatWs = null;
    showStatus('reconnecting...');
    window.setTimeout(() => {
      if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
      openChatWs();
    }, 1200);
  };
}

export function closeCanvasWs() {
  state.canvasWsToken += 1;
  if (state.canvasWs) {
    try { state.canvasWs.close(); } catch (_) {}
  }
  state.canvasWs = null;
}

export function assistantMessageUsesCanvasBlocks(text) {
  const lower = String(text || '').toLowerCase();
  return lower.includes(':::file{');
}

export function shouldRenderAssistantHistoryInChat(_renderFormat, markdown, plain) {
  return Boolean(String(markdown || plain || '').trim());
}

export function isVoiceOutputModePayload(payload) {
  return String(payload?.output_mode || '').trim().toLowerCase() === 'voice';
}

function batchItemLabel(item) {
  const titled = String(item?.item_title || '').trim();
  if (titled) return titled;
  const itemID = Number(item?.item_id || 0);
  if (itemID > 0) return `item ${itemID}`;
  return 'item';
}

function batchItemCounts(items) {
  const counts = { total: 0, running: 0, completed: 0, failed: 0 };
  if (!Array.isArray(items)) return counts;
  items.forEach((item) => {
    counts.total += 1;
    const status = String(item?.status || '').trim().toLowerCase();
    if (status === 'completed') counts.completed += 1;
    else if (status === 'failed') counts.failed += 1;
    else if (status === 'running') counts.running += 1;
  });
  return counts;
}

function batchSummaryLabel(batch, items, fallbackCount = 0) {
  const batchStatus = String(batch?.status || '').trim().toLowerCase();
  const counts = batchItemCounts(items);
  const total = counts.total || Math.max(0, Number(fallbackCount || 0));
  if (batchStatus === 'running') {
    if (total > 0) return `batch running: ${total} item(s)`;
    return 'batch running';
  }
  if (batchStatus === 'completed') {
    if (counts.total > 0) return `batch completed: ${counts.completed} completed, ${counts.failed} failed`;
    return 'batch completed';
  }
  if (batchStatus === 'failed') {
    if (counts.total > 0) return `batch failed: ${counts.completed} completed, ${counts.failed} failed`;
    return 'batch failed';
  }
  if (total === 0) return 'no matching batch items';
  return 'batch status updated';
}

function batchProgressMessage(payload) {
  const batch = payload?.batch && typeof payload.batch === 'object' ? payload.batch : {};
  const item = payload?.item && typeof payload.item === 'object' ? payload.item : null;
  if (item) {
    const label = batchItemLabel(item);
    const status = String(item?.status || '').trim().toLowerCase() || 'updated';
    if (status === 'completed') {
      return `Batch update: ${label} completed.`;
    }
    if (status === 'failed') {
      const errorMsg = String(item?.error_msg || '').trim();
      if (errorMsg) return `Batch update: ${label} failed: ${errorMsg}.`;
      return `Batch update: ${label} failed.`;
    }
    if (status === 'running') {
      return `Batch update: ${label} started.`;
    }
    return `Batch update: ${label} ${status}.`;
  }
  return `Batch update: ${batchSummaryLabel(batch, payload?.items, payload?.item_count)}.`;
}

function batchStatusIsActive(payload) {
  const batchStatus = String(payload?.batch?.status || '').trim().toLowerCase();
  if (batchStatus === 'running') return true;
  return payload?.status?.active === true;
}

function applyBatchStatus(payload) {
  const label = batchSummaryLabel(payload?.batch, payload?.items, payload?.item_count);
  state.batchStatusLabel = label;
  state.batchStatusActive = batchStatusIsActive(payload);
  showStatus(label);
  refreshBatchItemSidebar();
}

function refreshBatchItemSidebar() {
  if (state.fileSidebarMode === 'items' && state.prReviewDrawerOpen) {
    void loadItemSidebarView(state.itemSidebarView);
    return;
  }
  void refreshItemSidebarCounts().catch(() => {});
}

export function handleChatEvent(payload) {
  const type = String(payload?.type || '').trim();
  if (!type) return;

  if (handleLiveSessionMessage(payload)) {
    applyLiveSessionStateSnapshot();
    renderEdgeTopModelButtons();
    updateAssistantActivityIndicator();
    return;
  }

  if (type === 'companion_state') {
    const projectKey = String(payload?.project_key || '').trim();
    const currentProjectKey = activeProjectKey();
    if (!projectKey || !currentProjectKey || projectKey === currentProjectKey) {
      applyCompanionState(payload);
      updateAssistantActivityIndicator();
    }
    return;
  }

  if (type === 'live_policy_changed') {
    state.livePolicy = String(payload?.policy || state.livePolicy || LIVE_SESSION_MODE_DIALOGUE).trim().toLowerCase() === LIVE_SESSION_MODE_MEETING
      ? LIVE_SESSION_MODE_MEETING
      : LIVE_SESSION_MODE_DIALOGUE;
    renderEdgeTopModelButtons();
    updateAssistantActivityIndicator();
    return;
  }

  if (type === 'mode_changed') {
    const nextMode = String(payload.mode || 'chat').trim().toLowerCase();
    setChatMode(nextMode);
    const activeProjectID = String(state.activeProjectId || '').trim();
    if (activeProjectID) {
      const existing = state.projects.find((item) => item.id === activeProjectID);
      if (existing) {
        upsertProject({
          ...existing,
          chat_mode: nextMode,
        });
      }
    }
    renderEdgeTopModelButtons();
    const message = String(payload.message || '').trim();
    if (message) appendPlainMessage('system', message);
    return;
  }

  if (type === 'action') {
    const action = String(payload.action || '').trim();
    if (action === 'open_canvas') {
      showCanvasColumn('canvas-text');
      state.canvasActionThisTurn = true;
    } else if (action === 'open_chat') {
      // No more canvas - stay on rasa
    }
    return;
  }

  if (type === 'system_action') {
    const action = payload && typeof payload.action === 'object' ? payload.action : {};
    const actionType = String(action?.type || '').trim();
    if (actionType === 'switch_project') {
      const projectID = String(action?.project_id || '').trim();
      if (projectID) {
        void switchProject(projectID);
      }
    } else if (actionType === 'switch_model') {
      const projectID = String(action?.project_id || '').trim();
      const alias = normalizeProjectChatModelAlias(action?.alias);
      const effortRaw = String(action?.effort || '').trim().toLowerCase();
      if (projectID && alias) {
        const existing = state.projects.find((item) => item.id === projectID);
        if (existing) {
          const nextEffort = normalizeProjectChatModelReasoningEffort(
            effortRaw || existing.chat_model_reasoning_effort || '',
            alias,
          );
          upsertProject({
            ...existing,
            chat_model: alias,
            chat_model_reasoning_effort: nextEffort,
          });
          renderEdgeTopProjects();
          renderEdgeTopModelButtons();
          showStatus(`model set to ${alias}`);
          return;
        }
      }
      if (alias) {
        const effort = effortRaw ? normalizeProjectChatModelReasoningEffort(effortRaw, alias) : '';
        void switchProjectChatModel(alias, effort);
      }
    } else if (actionType === 'toggle_silent') {
      toggleTTSSilentMode();
    } else if (actionType === 'show_item_sidebar_view') {
      const view = normalizeItemSidebarView(action?.view || 'inbox');
      const actionFilters = action?.filters && typeof action.filters === 'object' ? action.filters : {};
      const filters = action?.clear_filters
        ? (actionFilters.all_spheres === true ? { all_spheres: true } : {})
        : actionFilters;
      void openItemSidebarView(view, filters);
    } else if (actionType === 'set_someday_review_nudge') {
      const enabled = parseOptionalBoolean(action?.enabled);
      if (enabled !== null) {
        setSomedayReviewNudgeEnabled(enabled);
        showStatus(enabled ? 'someday reminders on' : 'someday reminders off');
      }
    } else if (actionType === 'item_state_changed') {
      const nextView = String(action?.view || '').trim();
      if (nextView) {
        void openItemSidebarView(nextView);
      } else if (state.fileSidebarMode === 'items' && state.prReviewDrawerOpen) {
        void loadItemSidebarView(state.itemSidebarView);
      } else {
        void refreshItemSidebarCounts().catch(() => {});
      }
    } else if (actionType === 'open_item_sidebar_item') {
      const itemID = Number(action?.item_id || 0);
      if (itemID > 0) {
        const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
        const item = items.find((entry) => Number(entry?.id || 0) === itemID);
        if (item) {
          void openSidebarItem(item);
        }
      }
    } else if (actionType === 'suggest_canonical_actions') {
      const actions = Array.isArray(action?.actions)
        ? action.actions.map((value) => String(value || '').trim()).filter(Boolean)
        : [];
      const itemID = Number(action?.item_id || 0);
      const itemState = normalizeItemSidebarView(action?.item_state || '');
      const message = String(action?.message || '').trim();
      const present = () => {
        if (itemID > 0) {
          state.itemSidebarActiveItemID = itemID;
          state.currentCanvasArtifact.itemID = itemID;
        }
        if (message) {
          showStatus(message);
        }
        openCanonicalActionCommandCenter(actions, {
          hint: message || 'Choose a current-artifact action.',
        });
      };
      if (itemID > 0 && itemState) {
        void openItemSidebarView(itemState).finally(present);
      } else {
        present();
      }
    } else if (actionType === 'open_workspace_path') {
      const path = String(action?.path || '').trim();
      if (path) {
        if (action?.is_dir === true) {
          void loadWorkspaceBrowserPath(path);
        } else {
          void openWorkspaceSidebarFile(path);
        }
      }
    } else if (actionType === 'print_item') {
      openPrintView(String(action?.url || '').trim());
    } else if (actionType === 'batch_status') {
      applyBatchStatus(action);
    } else if (actionType === 'toggle_live_dialogue') {
      const next = state.liveSessionActive ? '' : LIVE_SESSION_MODE_DIALOGUE;
      const action = next
        ? activateLiveSession(next)
        : deactivateLiveSession({ disableMeetingConfig: true });
      Promise.resolve(action)
        .then(() => {
          renderEdgeTopModelButtons();
          updateAssistantActivityIndicator();
          showStatus(next ? 'live dialogue on' : 'live off');
        })
        .catch((err) => {
          const message = String(err?.message || err || 'live toggle failed');
          showStatus(`live toggle failed: ${message}`);
        });
    }
    return;
  }

  if (type === 'batch_progress') {
    const message = batchProgressMessage(payload);
    if (message) appendPlainMessage('system', message);
    applyBatchStatus(payload);
    return;
  }

  if (type === 'system_action_confirmation_required') {
    const action = payload && typeof payload.action === 'object' ? payload.action : {};
    const summary = String(action?.summary || '').trim();
    if (summary) {
      showStatus('confirmation required');
      appendPlainMessage('system', `Confirmation required: ${summary}`);
    }
    return;
  }

  if (type === 'approval_request') {
    renderApprovalRequestCard(payload);
    renderApprovalRequestCanvas(payload);
    showStatus('approval required');
    return;
  }

  if (type === 'approval_resolved') {
    resolveApprovalRequestCard(payload?.request_id, payload?.decision);
    resolveCanvasApprovalRequest(payload?.request_id, payload?.decision);
    return;
  }

  if (type === 'approval_error') {
    const message = String(payload?.error || 'approval failed').trim();
    if (message) {
      showStatus(message);
    }
    return;
  }

  if (type === 'turn_started') {
    const turnID = String(payload.turn_id || '').trim();
    const turnIsVoice = isVoiceOutputModePayload(payload) || state.voiceAwaitingTurn || isVoiceTurn();
    if (turnID) {
      if (turnIsVoice) state.voiceTurns.add(turnID);
      else state.voiceTurns.delete(turnID);
    }
    trackAssistantTurnStarted(turnID);
    state.voiceAwaitingTurn = false;
    state.indicatorSuppressedByCanvasUpdate = false;
    ensurePendingForTurn(turnID);
    // A previous canvas update can suppress indicator rendering. Re-sync after
    // clearing suppression so stop control is available immediately on turn start.
    updateAssistantActivityIndicator();
    if (isMobileSilent()) {
      const edgeRight = document.getElementById('edge-right');
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    }
    state.canvasActionThisTurn = false;
    state.turnFirstResponseShown = false;
    // Reset TTS state for new turn
    stopTTSPlayback();
    const pos = getLastInputPosition();
    if (isVoiceTurn() || state.hasArtifact) {
      hideOverlay();
    } else if (isMobileSilent()) {
      hideOverlay();
    } else {
      showOverlay(pos.x, pos.y + 24);
      updateOverlay('_Thinking..._');
      getUiState().overlayTurnId = payload.turn_id || null;
    }
    return;
  }

  if (type === 'request_position') {
    const prompt = String(payload?.prompt || '').trim();
    state.requestedPositionPrompt = prompt || 'Tap where you want it.';
    showStatus(state.requestedPositionPrompt);
    updateAssistantActivityIndicator();
    return;
  }

  if (type === 'assistant_message') {
    const turnID = String(payload.turn_id || '').trim();
    trackAssistantTurnStarted(turnID);
    const md = String(payload.message || '');
    const autoCanvas = Boolean(payload.auto_canvas);
    const renderOnCanvas = Boolean(payload.render_on_canvas) || autoCanvas || assistantMessageUsesCanvasBlocks(md);
    const row = ensurePendingForTurn(turnID);
    if (String(md || '').trim()) {
      updateAssistantRow(row, md, true);
    } else if (!renderOnCanvas) {
      updateAssistantRow(row, '_Thinking..._', true);
    }

    if (autoCanvas) {
      state.indicatorSuppressedByCanvasUpdate = true;
      updateAssistantActivityIndicator();
      if (!isVoiceTurn()) {
        hideOverlay();
      }
    }

    // First non-empty response: show on canvas (silent) / speak (voice)
    const trimmedMd = String(md || '').trim();
    const shouldSpeakStreaming = isVoiceOutputModePayload(payload) || (turnID ? state.voiceTurns.has(turnID) : false) || isVoiceTurn();
    if (trimmedMd && !state.turnFirstResponseShown) {
      state.turnFirstResponseShown = true;
      if (isMobileSilent()) {
        renderCanvas({ kind: 'text_artifact', title: '', text: md });
      }
      if (shouldSpeakStreaming && canSpeakTTS()) {
        const { ttsText, ttsLang } = extractTTSText(md);
        if (ttsLang) setTTSSpeakLang(ttsLang);
        const diff = computeTTSDiff(ttsText);
        queueTTSDiff(diff);
      }
    }

    if (!isVoiceTurn() && !isMobileSilent() && !state.hasArtifact) {
      const cleaned = cleanForOverlay(md);
      if (cleaned) updateOverlay(cleaned);
    } else if (!isVoiceTurn()) {
      hideOverlay();
    }
    return;
  }

  if (type === 'assistant_output' || type === 'message_persisted') {
    if (String(payload.role || '') !== 'assistant') return;
    const turnID = String(payload.turn_id || '').trim();
    const md = String(payload.message || '');
    const providerOptions = {
      provider: payload.provider,
      providerLabel: payload.provider_label,
      providerModel: payload.provider_model,
    };
    const autoCanvas = Boolean(payload.auto_canvas);
    const lastTTSText = getTTSLastSpeakText();
    const inferredText = md || lastTTSText;
    const renderOnCanvas = Boolean(payload.render_on_canvas) || autoCanvas || assistantMessageUsesCanvasBlocks(inferredText);
    // Persisted text may be empty for voice-only responses; fall back to TTS text.
    const displayMd = md || (lastTTSText ? `_${lastTTSText}_` : '');
    const hasDisplayMd = Boolean(String(displayMd || '').trim());
    const mobileSilent = isMobileSilent();
    const row = takePendingRow(turnID);
    if (row && hasDisplayMd) {
      updateAssistantRow(row, displayMd, false, providerOptions);
    } else if (row) {
      row.classList.remove('is-pending');
      updateAssistantRow(row, '', false, providerOptions);
    } else if (hasDisplayMd) {
      appendRenderedAssistant(displayMd, providerOptions);
    }
    const shouldSpeakTurn = isVoiceOutputModePayload(payload) || (turnID ? state.voiceTurns.has(turnID) : false) || isVoiceTurn();
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus(state.batchStatusActive && state.batchStatusLabel ? state.batchStatusLabel : 'ready');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();

    if (shouldSpeakTurn && canSpeakTTS() && md.trim()) {
      const { ttsText, ttsLang } = extractTTSText(md);
      if (ttsLang) setTTSSpeakLang(ttsLang);
      const diff = computeTTSDiff(ttsText);
      queueTTSDiff(diff);
    } else if (autoCanvas) {
      state.indicatorSuppressedByCanvasUpdate = true;
      updateAssistantActivityIndicator();
    }

    flushTTSChunker();
    if (mobileSilent) {
      if (state.canvasActionThisTurn) {
        // LLM touched the canvas this turn — keep showing the document.
        const edgeRight = document.getElementById('edge-right');
        if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
      } else if (hasDisplayMd) {
        // Mirror final answer on canvas while keeping chat in focus.
        renderCanvas({
          kind: 'text_artifact',
          title: '',
          text: displayMd,
        });
      }
      hideOverlay();
      state.canvasActionThisTurn = false;
      return;
    }
    if (!isVoiceTurn()) {
      if (autoCanvas || state.hasArtifact) {
        hideOverlay();
        state.canvasActionThisTurn = false;
        return;
      }
      const cleaned = cleanForOverlay(md);
      if (state.canvasActionThisTurn && !cleaned) {
        hideOverlay();
      } else if (cleaned) {
        updateOverlay(cleaned);
      } else {
        hideOverlay();
      }
    }
    state.canvasActionThisTurn = false;
    // If live dialogue is active but no TTS was queued (e.g. TTS error,
    // empty md, or all text already spoken during streaming), kick the listen
    // cycle so the hands-free loop does not stall.
    if (isDialogueLiveSession() && !hasTTSPlayer() && shouldSpeakTurn && canSpeakTTS()) {
      onLiveSessionTTSPlaybackComplete();
    }
    return;
  }

  if (type === 'item_completed') {
    const turnID = String(payload.turn_id || '').trim();
    const line = formatItemCompletedLabel(payload);
    appendAssistantProgressForTurn(turnID, line);
    return;
  }

  if (type === 'turn_completed') {
    void refreshAssistantActivity();
    return;
  }

  if (type === 'turn_cancelled') {
    state.voiceAwaitingTurn = false;
    const turnID = String(payload.turn_id || '').trim();
    let row = takePendingRow(turnID);
    if (!row && !turnID) {
      row = takeAnyPendingRow();
    }
    if (row) updateAssistantRow(row, '_Stopped._', false);
    trackAssistantTurnFinished(turnID);
    state.indicatorSuppressedByCanvasUpdate = false;
    state.assistantLastError = '';
    showStatus('stopped');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    hideOverlay();
    window.setTimeout(() => {
      hideOverlay();
      void refreshAssistantActivity();
    }, 180);
    return;
  }

  if (type === 'turn_queue_cleared') {
    state.voiceAwaitingTurn = false;
    const count = Number(payload?.count || 0);
    const limit = Number.isFinite(count) && count > 0 ? Math.floor(count) : state.pendingQueue.length;
    for (let i = 0; i < limit; i += 1) {
      const row = takePendingRow('');
      if (!row) break;
      updateAssistantRow(row, '_Stopped._', false);
      trackAssistantTurnFinished('');
    }
    showStatus('queue cleared');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    return;
  }

  if (type === 'context_usage') {
    state.contextUsed = Number(payload.context_used) || 0;
    state.contextMax = Number(payload.context_max) || 0;
    return;
  }

  if (type === 'context_compact') {
    appendPlainMessage('system', 'Context auto-compacted to free space.');
    state.contextUsed = 0;
    state.contextMax = 0;
    return;
  }

  if (type === 'chat_cleared') {
    stopTTSPlayback();
    clearChatHistory();
    resetAssistantTurnTracking({ clearError: true });
    appendPlainMessage('system', 'Chat cleared.');
    state.contextUsed = 0;
    state.contextMax = 0;
    return;
  }

  if (type === 'chat_compacted') {
    void loadChatHistory().catch(() => {});
    const message = String(payload.message || 'Chat compacted.').trim();
    appendPlainMessage('system', message);
    return;
  }

  if (type === 'error') {
    state.voiceAwaitingTurn = false;
    const turnID = String(payload.turn_id || '').trim();
    const row = takePendingRow(turnID);
    if (row) row.classList.remove('is-pending');
    trackAssistantTurnFinished(turnID);
    const errText = String(payload.error || 'assistant request failed');
    state.assistantLastError = errText;
    appendPlainMessage('system', errText);
    showStatus(errText);
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    updateOverlay(`**Error:** ${errText}`);
    window.setTimeout(() => hideOverlay(), 2000);
    if (isDialogueLiveSession() && canSpeakTTS()) {
      onLiveSessionTTSPlaybackComplete();
    }
  }
}

export async function switchProject(projectID) {
  const nextProjectID = String(projectID || '').trim();
  if (!nextProjectID) return;
  if (state.projectSwitchInFlight) return;
  if (nextProjectID === state.activeProjectId && state.chatSessionId) return;

  state.projectSwitchInFlight = true;
  showStatus('switching workspace...');
  await deactivateLiveSession({ silent: true, disableMeetingConfig: true });
  cancelChatVoiceCapture();
  closeChatWs();
  closeCanvasWs();
  clearChatHistory();
  clearCanvas();
  clearWelcomeSurface();
  resetCompanionState();
  state.fileSidebarMode = 'items';
  state.workspaceBrowserPath = '';
  state.workspaceBrowserEntries = [];
  state.workspaceBrowserLoading = false;
  state.workspaceBrowserError = '';
  state.workspaceBrowserActivePath = '';
  state.workspaceBrowserActiveIsDir = false;
  state.workspaceOpenFilePath = '';
  state.workspaceStepInFlight = false;
  state.itemSidebarItems = [];
  state.itemSidebarCounts = defaultItemSidebarCounts();
  state.itemSidebarLoading = false;
  state.itemSidebarError = '';
  state.itemSidebarActiveItemID = 0;
  setInboxTriggerCount(0);
  hideCanvasColumn();
  hideOverlay();
  hideTextInput();
  resetAssistantTurnTracking({ clearError: true });
  setActiveProjectID(nextProjectID);
  try {
    const project = await activateProject(nextProjectID);
    state.chatWsHasConnected = false;
    upsertProject(project);
    renderEdgeTopProjects();
    await refreshWorkspaceBrowser(true);
    await loadItemSidebarView(state.itemSidebarView).catch(() => {});
    openCanvasWs();
    await showWelcomeForActiveProject(true);
    await loadChatHistory();
    await refreshAssistantActivity();
    await refreshCompanionState(project.id).catch(() => {});
    openChatWs();
    showStatus(`ready`);
  } catch (err) {
    const message = String(err?.message || err || 'workspace switch failed');
    appendPlainMessage('system', `Workspace switch failed: ${message}`);
    showStatus(`workspace switch failed: ${message}`);
  } finally {
    state.projectSwitchInFlight = false;
    renderEdgeTopModelButtons();
  }
}

export {
  abortError,
  abortPendingSubmit,
  cancelActiveAssistantTurn,
  cancelActiveAssistantTurnWithRetry,
  clearPendingSubmit,
  forceVoiceLifecycleIdle,
  handleStopAction,
  setPendingSubmit,
  submitMessage,
  waitWithAbort,
} from './app-chat-submit.js';
