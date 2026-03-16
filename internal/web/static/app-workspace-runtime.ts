import * as env from './app-env.js';
import * as context from './app-context.js';
import {
  applyWorkspaceBusyStates,
  applyWorkspaceFocusSnapshot,
  normalizeWorkspaceBusyStates,
  normalizeWorkspaceFocusSnapshot,
  workspaceBusyBadgeText,
  workspaceBusyBadgeTitle,
  workspaceDisplayName,
} from './app-workspace-status.js';
const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES, SPHERE_OPTIONS } = context;
const showStatus = (...args) => refs.showStatus(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const switchProject = (...args) => refs.switchProject(...args);
const openChatWs = (...args) => refs.openChatWs(...args);
const closeChatWs = (...args) => refs.closeChatWs(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);
const appendRenderedAssistant = (...args) => refs.appendRenderedAssistant(...args);
const activeProject = (...args) => refs.activeProject(...args);
const activeWorkspacePath = (...args) => refs.activeWorkspacePath(...args);
const setChatMode = (...args) => refs.setChatMode(...args);
const resetCompanionState = (...args) => refs.resetCompanionState(...args);
const applyCompanionState = (...args) => refs.applyCompanionState(...args);
const chatHistoryEl = (...args) => refs.chatHistoryEl(...args);
const scrollChatToBottom = (...args) => refs.scrollChatToBottom(...args);
const cancelChatVoiceCapture = (...args) => refs.cancelChatVoiceCapture(...args);
const resetDialogueTurnController = (...args) => refs.resetDialogueTurnController(...args);
const toggleTTSSilentMode = (...args) => refs.toggleTTSSilentMode(...args);
const requestHotwordSync = (...args) => refs.requestHotwordSync(...args);
const clearWelcomeSurface = (...args) => refs.clearWelcomeSurface(...args);
const requestMicRefresh = (...args) => refs.requestMicRefresh(...args);
const releaseMicStream = (...args) => refs.releaseMicStream(...args);
const sttCancel = (...args) => refs.sttCancel(...args);
const hasLocalStopCapableWork = (...args) => refs.hasLocalStopCapableWork(...args);
const isVoiceTranscriptSubmitPending = (...args) => refs.isVoiceTranscriptSubmitPending(...args);
const applyLiveSessionStateSnapshot = (...args) => refs.applyLiveSessionStateSnapshot(...args);
const isMeetingLiveSession = (...args) => refs.isMeetingLiveSession(...args);
const liveSessionStatusSummary = (...args) => refs.liveSessionStatusSummary(...args);
const readPersistedWorkspaceID = (...args) => refs.readPersistedWorkspaceID(...args);
const toggleYoloMode = (...args) => refs.toggleYoloMode(...args);
const updateRuntimePreferences = (...args) => refs.updateRuntimePreferences(...args);
const normalizeActiveSphere = (...args) => refs.normalizeActiveSphere(...args);
const persistActiveSpherePreference = (...args) => refs.persistActiveSpherePreference(...args);
const activeProjectChatModelAlias = (...args) => refs.activeProjectChatModelAlias(...args);
const activeProjectChatModelReasoningEffort = (...args) => refs.activeProjectChatModelReasoningEffort(...args);
const normalizeProjectChatModelReasoningEffort = (...args) => refs.normalizeProjectChatModelReasoningEffort(...args);
const reasoningEffortOptionsForAlias = (...args) => refs.reasoningEffortOptionsForAlias(...args);
const normalizeProjectChatModelAlias = (...args) => refs.normalizeProjectChatModelAlias(...args);
const renderToolPalette = (...args) => refs.renderToolPalette(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const refreshItemSidebarCounts = (...args) => refs.refreshItemSidebarCounts(...args);
const openInboxMailTriage = (...args) => refs.openInboxMailTriage(...args);
const openJunkMailTriage = (...args) => refs.openJunkMailTriage(...args);
const isTemporaryProjectKind = (...args) => refs.isTemporaryProjectKind(...args);
const shouldRenderAssistantHistoryInChat = (...args) => refs.shouldRenderAssistantHistoryInChat(...args);
const hasLocalAssistantWork = (...args) => refs.hasLocalAssistantWork(...args);
export { applyWorkspaceBusyStates, applyWorkspaceFocusSnapshot } from './app-workspace-status.js';
export async function fetchProjects() {
  const resp = await fetch(apiURL('runtime/workspaces'), { cache: 'no-store' });
  if (!resp.ok) throw new Error(`workspaces list failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const projects = Array.isArray(payload?.workspaces) ? payload.workspaces : [];
  state.projects = projects.map((project) => ({
    ...project,
    id: String(project?.id || ''),
    sphere: String(project?.sphere || '').trim().toLowerCase(),
    chat_mode: String(project?.chat_mode || 'chat'),
    chat_model_reasoning_effort: String(project?.chat_model_reasoning_effort || '').trim().toLowerCase(),
    run_state: normalizeProjectRunState(project?.run_state),
    unread: Boolean(project?.unread),
    review_pending: Boolean(project?.review_pending),
  })).filter((project) => project.id);
  state.defaultWorkspaceId = String(payload?.default_workspace_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_workspace_id || '').trim();
  await refreshWorkspaceRuntimeState().catch(() => {});
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
}
export function projectMatchesSphere(project, sphere = state.activeSphere) {
  if (!project) return false;
  const activeSphere = normalizeActiveSphere(sphere);
  const projectSphere = String(project?.sphere || '').trim().toLowerCase();
  return !projectSphere || projectSphere === activeSphere;
}
function visibleProjectsForSphere(sphere = state.activeSphere) {
  return state.projects.filter((project) => projectMatchesSphere(project, sphere));
}

export async function refreshWorkspaceRuntimeState() {
  const [focusResp, busyResp] = await Promise.all([
    fetch(apiURL('workspace/focus'), { cache: 'no-store' }),
    fetch(apiURL('workspaces/busy'), { cache: 'no-store' }),
  ]);
  if (!focusResp.ok) {
    throw new Error(`workspace focus failed: HTTP ${focusResp.status}`);
  }
  if (!busyResp.ok) {
    throw new Error(`workspace busy failed: HTTP ${busyResp.status}`);
  }
  const focusPayload = await focusResp.json();
  const busyPayload = await busyResp.json();
  applyWorkspaceFocusSnapshot(focusPayload);
  applyWorkspaceBusyStates(busyPayload?.states || []);
  return {
    focus: state.workspaceFocus,
    states: state.workspaceBusyStates,
  };
}
async function ensureVisibleActiveProject() {
  const current = activeProject();
  if (!current || projectMatchesSphere(current, state.activeSphere)) {
    return;
  }
  const fallback = visibleProjectsForSphere().find((project) => project.id !== current.id)
    || null;
  if (!fallback || fallback.id === current.id) {
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    return;
  }
  await switchProject(fallback.id);
}
export async function setActiveSphere(nextSphere) {
  const sphere = normalizeActiveSphere(nextSphere);
  if (sphere === state.activeSphere && String(state.activeSphere || '').trim()) {
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    return true;
  }
  const previousSphere = state.activeSphere;
  state.activeSphere = sphere;
  persistActiveSpherePreference(sphere);
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
  try {
    await updateRuntimePreferences({ active_sphere: sphere });
    await ensureVisibleActiveProject();
    if (state.prReviewDrawerOpen && state.fileSidebarMode === 'items') {
      await loadItemSidebarView(state.itemSidebarView);
    } else {
      await refreshItemSidebarCounts().catch(() => false);
    }
    showStatus(`${sphere} sphere on`);
    return true;
  } catch (err) {
    state.activeSphere = previousSphere || 'private';
    persistActiveSpherePreference(state.activeSphere);
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus(`sphere switch failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}
export function normalizeProjectRunState(runState) {
  const activeTurns = Math.max(0, Number(runState?.active_turns || 0) || 0);
  const queuedTurns = Math.max(0, Number(runState?.queued_turns || 0) || 0);
  let status = String(runState?.status || '').trim().toLowerCase();
  if (status !== 'running' && status !== 'queued' && status !== 'idle') {
    status = activeTurns > 0 ? 'running' : (queuedTurns > 0 ? 'queued' : 'idle');
  }
  return {
    active_turns: activeTurns,
    queued_turns: queuedTurns,
    is_working: Boolean(runState?.is_working) || activeTurns > 0 || queuedTurns > 0,
    status,
    active_turn_id: String(runState?.active_turn_id || '').trim(),
  };
}
export function projectRunStateSummary(project) {
  const runState = normalizeProjectRunState(project?.run_state);
  if (runState.status === 'running') {
    return `${runState.active_turns} active, ${runState.queued_turns} queued`;
  }
  if (runState.status === 'queued') {
    return `${runState.queued_turns} queued`;
  }
  return 'idle';
}
export function upsertProject(project) {
  if (!project || !project.id) return;
  project.chat_mode = String(project.chat_mode || 'chat');
  if (project.chat_model_reasoning_effort !== undefined) {
    project.chat_model_reasoning_effort = String(project.chat_model_reasoning_effort || '').trim().toLowerCase();
  }
  project.run_state = normalizeProjectRunState(project.run_state);
  project.unread = Boolean(project.unread);
  project.review_pending = Boolean(project.review_pending);
  const index = state.projects.findIndex((item) => item.id === project.id);
  if (index >= 0) {
    state.projects[index] = project;
  } else {
    state.projects.push(project);
  }
  renderEdgeTopModelButtons();
}
export async function refreshCompanionState(workspaceID = state.activeWorkspaceId) {
  const project = state.projects.find((item) => item.id === String(workspaceID || '').trim()) || null;
  if (!project) {
    resetCompanionState();
    return null;
  }
  const resp = await fetch(apiURL('workspaces/active/companion/state'), { cache: 'no-store' });
  if (!resp.ok) {
    resetCompanionState();
    throw new Error(`meeting state failed: HTTP ${resp.status}`);
  }
  const payload = await resp.json();
  applyCompanionState(payload);
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  return payload;
}
export async function updateCompanionConfig(patch) {
  const project = activeProject();
  if (!project || !project.id) return null;
  const resp = await fetch(apiURL('workspaces/active/companion/config'), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch || {}),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  applyCompanionState({
    workspace_path: activeWorkspacePath(),
    companion_enabled: payload?.companion_enabled,
    idle_surface: payload?.idle_surface,
    state: state.companionRuntimeState,
    reason: state.companionRuntimeReason,
  });
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  return payload;
}
function normalizeLivePolicy(policy) {
  return String(policy || '').trim().toLowerCase() === LIVE_SESSION_MODE_MEETING
    ? LIVE_SESSION_MODE_MEETING
    : LIVE_SESSION_MODE_DIALOGUE;
}
export async function updateLivePolicy(policy) {
  const nextPolicy = normalizeLivePolicy(policy);
  const resp = await fetch(apiURL('live-policy'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ policy: nextPolicy }),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  state.livePolicy = normalizeLivePolicy(payload?.policy || nextPolicy);
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  return payload;
}
export async function activateLiveSession(mode) {
  const normalized = String(mode || '').trim().toLowerCase();
  if (normalized !== LIVE_SESSION_MODE_DIALOGUE && normalized !== LIVE_SESSION_MODE_MEETING) return false;
  if (!activeProject()) return false;
  const wasMeeting = isMeetingLiveSession();
  if (normalizeLivePolicy(state.livePolicy) !== normalized) {
    await updateLivePolicy(normalized);
  }
  if (state.liveSessionActive) {
    stopLiveSession();
    applyLiveSessionStateSnapshot();
  }
  if (normalized === LIVE_SESSION_MODE_MEETING) {
    applyCompanionState({
      workspace_path: activeWorkspacePath(),
      companion_enabled: true,
      idle_surface: state.companionIdleSurface,
      state: state.companionRuntimeState,
      reason: state.companionRuntimeReason,
    });
    await updateCompanionConfig({ companion_enabled: true });
    try {
      const started = await startLiveSession(LIVE_SESSION_MODE_MEETING, state.chatWs);
      applyLiveSessionStateSnapshot();
      return started;
    } catch (err) {
      await updateCompanionConfig({ companion_enabled: false }).catch(() => {});
      applyLiveSessionStateSnapshot();
      throw err;
    }
  }
  applyCompanionState({
    workspace_path: activeWorkspacePath(),
    companion_enabled: true,
    idle_surface: state.companionIdleSurface,
    state: state.companionRuntimeState,
    reason: state.companionRuntimeReason,
  });
  await updateCompanionConfig({ companion_enabled: true }).catch(() => {});
  const started = await startLiveSession(LIVE_SESSION_MODE_DIALOGUE, state.chatWs);
  applyLiveSessionStateSnapshot();
  return started;
}
export async function deactivateLiveSession(options: Record<string, any> = {}) {
  const silent = Boolean(options?.silent);
  const disableMeetingConfig = Boolean(options?.disableMeetingConfig);
  const wasMeeting = isMeetingLiveSession();
  resetDialogueTurnController();
  stopLiveSession();
  applyLiveSessionStateSnapshot();
  if (disableMeetingConfig && wasMeeting) {
    await updateCompanionConfig({ companion_enabled: false }).catch(() => {});
  }
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  if (!silent) {
    showStatus('live off');
  }
}

export function resolveInitialWorkspaceID() {
  const reloadWorkspaceID = String(state.pendingRuntimeReloadContext?.activeWorkspaceId || '').trim();
  if (reloadWorkspaceID && state.projects.some((project) => project.id === reloadWorkspaceID)) {
    return reloadWorkspaceID;
  }
  if (state.serverActiveProjectId && state.projects.some((project) => project.id === state.serverActiveProjectId)) {
    return state.serverActiveProjectId;
  }
  const persisted = readPersistedWorkspaceID();
  if (persisted && state.projects.some((project) => project.id === persisted)) {
    return persisted;
  }
  if (state.defaultWorkspaceId && state.projects.some((project) => project.id === state.defaultWorkspaceId)) {
    return state.defaultWorkspaceId;
  }
  return state.projects[0]?.id || '';
}

export function renderEdgeTopProjects() {
  const host = document.getElementById('edge-top-projects');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  for (const project of visibleProjectsForSphere()) {
    const runState = normalizeProjectRunState(project.run_state);
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-project-btn';
    if (project.id === state.activeWorkspaceId) {
      button.classList.add('is-active');
    }
    if (runState.is_working) {
      button.classList.add('is-working');
    }
    if (project.unread) {
      button.classList.add('is-unread');
    }
    if (runState.status === 'running') {
      button.classList.add('is-running');
    }
    if (runState.status === 'queued') {
      button.classList.add('is-queued');
    }
    button.dataset.runState = runState.status;
    button.textContent = String(project.name || project.id || 'Workspace');
    const summary = projectRunStateSummary(project);
    const rootPath = String(project.root_path || '').trim();
    button.title = rootPath ? `${summary} | ${rootPath}` : summary;
    button.setAttribute('aria-label', `${String(project.name || project.id || 'Workspace')}: ${summary}`);
    button.addEventListener('click', () => {
      if (project.id === state.activeWorkspaceId) return;
      void switchProject(project.id);
    });
    host.appendChild(button);
  }
}

export function renderEdgeTopModelButtons() {
  const host = document.getElementById('edge-top-models');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  const project = activeProject();
  const focusSnapshot = normalizeWorkspaceFocusSnapshot(state.workspaceFocus);
  const hasBusyWork = normalizeWorkspaceBusyStates(state.workspaceBusyStates).some((entry) => entry.status !== 'idle');
  const shell = document.createElement('div');
  shell.className = 'edge-runtime-shell';

  const summary = document.createElement('div');
  summary.className = 'edge-runtime-summary';

  const kicker = document.createElement('div');
  kicker.className = 'edge-runtime-kicker';
  kicker.textContent = 'Today';
  summary.appendChild(kicker);

  const title = document.createElement('div');
  title.className = 'edge-runtime-title';
  title.textContent = String(project?.name || project?.id || 'No workspace selected').trim() || 'No workspace selected';
  summary.appendChild(title);

  const detail = document.createElement('div');
  detail.className = 'edge-runtime-detail';
  const detailParts = [];
  if (focusSnapshot.focus) {
    detailParts.push(focusSnapshot.explicit
      ? `Focus ${workspaceDisplayName(focusSnapshot.focus)}`
      : `Anchor ${workspaceDisplayName(focusSnapshot.focus)}`);
  } else if (focusSnapshot.anchor) {
    detailParts.push(`Anchor ${workspaceDisplayName(focusSnapshot.anchor)}`);
  }
  detailParts.push(workspaceBusyBadgeText(state.workspaceBusyStates));
  const detailPath = String(
    focusSnapshot.focus?.dir_path
      || focusSnapshot.anchor?.dir_path
      || project?.root_path
      || '',
  ).trim();
  if (detailPath) {
    detailParts.push(detailPath);
  }
  detail.textContent = detailParts.join(' • ');
  detail.title = workspaceBusyBadgeTitle(focusSnapshot, state.workspaceBusyStates);
  summary.appendChild(detail);

  const busy = document.createElement('span');
  busy.className = `edge-runtime-busy${hasBusyWork ? ' is-busy' : ''}`;
  busy.textContent = workspaceBusyBadgeText(state.workspaceBusyStates);
  busy.title = workspaceBusyBadgeTitle(focusSnapshot, state.workspaceBusyStates);
  summary.appendChild(busy);

  const actions = document.createElement('div');
  actions.className = 'edge-runtime-actions';

  const liveDisabled = !project || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  if (state.liveSessionActive) {
    const liveStatus = document.createElement('span');
    liveStatus.className = 'edge-live-status';
    liveStatus.textContent = liveSessionStatusSummary();
    if (state.hotwordEnabled) {
      liveStatus.title = `Live session hotword: ${state.liveSessionHotword || LIVE_SESSION_HOTWORD_DEFAULT}`;
    }
    actions.appendChild(liveStatus);

    const stopButton = document.createElement('button');
    stopButton.type = 'button';
    stopButton.className = 'edge-project-btn edge-live-stop-btn';
    stopButton.textContent = 'Stop';
    stopButton.disabled = liveDisabled;
    stopButton.addEventListener('click', () => {
      void deactivateLiveSession({ disableMeetingConfig: true });
    });
    actions.appendChild(stopButton);
  } else {
    const dialogueButton = document.createElement('button');
    dialogueButton.type = 'button';
    dialogueButton.className = 'edge-project-btn edge-live-dialogue-btn';
    dialogueButton.textContent = 'Dialogue';
    dialogueButton.setAttribute('aria-pressed', state.livePolicy === LIVE_SESSION_MODE_DIALOGUE ? 'true' : 'false');
    if (state.livePolicy === LIVE_SESSION_MODE_DIALOGUE) {
      dialogueButton.classList.add('is-active');
    }
    dialogueButton.disabled = liveDisabled;
    dialogueButton.addEventListener('click', () => {
      void activateLiveSession(LIVE_SESSION_MODE_DIALOGUE)
        .then((started) => {
          renderEdgeTopModelButtons();
          updateAssistantActivityIndicator();
          if (started) {
            showStatus('live dialogue on');
          }
        })
        .catch((err) => {
          const message = String(err?.message || err || 'live dialogue failed');
          showStatus(`live dialogue failed: ${message}`);
        });
    });
    actions.appendChild(dialogueButton);

    const meetingButton = document.createElement('button');
    meetingButton.type = 'button';
    meetingButton.className = 'edge-project-btn edge-live-meeting-btn';
    meetingButton.textContent = 'Meeting';
    meetingButton.setAttribute('aria-pressed', state.livePolicy === LIVE_SESSION_MODE_MEETING ? 'true' : 'false');
    if (state.livePolicy === LIVE_SESSION_MODE_MEETING) {
      meetingButton.classList.add('is-active');
    }
    meetingButton.disabled = liveDisabled;
    meetingButton.addEventListener('click', () => {
      void activateLiveSession(LIVE_SESSION_MODE_MEETING)
        .then((started) => {
          renderEdgeTopModelButtons();
          updateAssistantActivityIndicator();
          if (started) {
            showStatus('live meeting on');
          }
        })
        .catch((err) => {
          const message = String(err?.message || err || 'live meeting failed');
          appendPlainMessage('system', `Live meeting failed: ${message}`);
          showStatus(`live meeting failed: ${message}`);
        });
    });
    actions.appendChild(meetingButton);
  }

  if (state.ttsEnabled) {
    const silentButton = document.createElement('button');
    silentButton.type = 'button';
    silentButton.className = 'edge-project-btn edge-silent-btn';
    silentButton.textContent = 'Silent';
    silentButton.setAttribute('aria-pressed', state.ttsSilent ? 'true' : 'false');
    if (state.ttsSilent) {
      silentButton.classList.add('is-active');
    }
    silentButton.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    silentButton.addEventListener('click', () => {
      toggleTTSSilentMode();
    });
    actions.appendChild(silentButton);
  }

  const triageBusy = state.mailTriage?.loading || state.mailTriage?.submitting;
  const inboxTriageButton = document.createElement('button');
  inboxTriageButton.type = 'button';
  inboxTriageButton.className = 'edge-project-btn edge-mail-triage-btn';
  inboxTriageButton.textContent = 'Inbox Triage';
  inboxTriageButton.disabled = triageBusy;
  inboxTriageButton.addEventListener('click', () => {
    void openInboxMailTriage();
  });
  actions.appendChild(inboxTriageButton);

  const junkTriageButton = document.createElement('button');
  junkTriageButton.type = 'button';
  junkTriageButton.className = 'edge-project-btn edge-mail-triage-btn';
  junkTriageButton.textContent = 'Junk Audit';
  junkTriageButton.disabled = triageBusy;
  junkTriageButton.addEventListener('click', () => {
    void openJunkMailTriage();
  });
  actions.appendChild(junkTriageButton);

  shell.appendChild(summary);
  shell.appendChild(actions);
  host.appendChild(shell);
}

export async function switchProjectChatModel(modelAlias, reasoningEffort = '') {
  const project = activeProject();
  if (!project || !project.id) return;
  const nextAlias = normalizeProjectChatModelAlias(modelAlias);
  if (!nextAlias) return;
  const currentAlias = activeProjectChatModelAlias();
  const rawEffort = String(reasoningEffort || '').trim().toLowerCase();
  const includeEffort = rawEffort !== '';
  const nextEffort = includeEffort ? normalizeProjectChatModelReasoningEffort(rawEffort, nextAlias) : '';
  const currentEffort = activeProjectChatModelReasoningEffort();
  if (nextAlias === currentAlias && (!includeEffort || nextEffort === currentEffort)) return;
  if (state.projectModelSwitchInFlight || state.projectSwitchInFlight) return;

  state.projectModelSwitchInFlight = true;
  renderEdgeTopModelButtons();
  showStatus(`switching model to ${nextAlias}...`);
  try {
    const payload: Record<string, any> = { model: nextAlias };
    if (includeEffort) {
      payload.reasoning_effort = nextEffort;
    }
    const resp = await fetch(apiURL(`runtime/workspaces/${encodeURIComponent(project.id)}/chat-model`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const responsePayload = await resp.json();
    const updatedProject = responsePayload?.workspace || {};
    upsertProject(updatedProject);
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus('ready');
  } catch (err) {
    const message = String(err?.message || err || 'model switch failed');
    appendPlainMessage('system', `Model switch failed: ${message}`);
    showStatus(`model switch failed: ${message}`);
  } finally {
    state.projectModelSwitchInFlight = false;
    renderEdgeTopModelButtons();
  }
}

export async function createTemporaryProject(kind, sourceWorkspaceID = '') {
  const projectKind = String(kind || '').trim().toLowerCase();
  if (!isTemporaryProjectKind(projectKind)) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus(`starting ${projectKind}...`);
  const payload: Record<string, any> = {
    kind: projectKind,
    activate: true,
  };
  const sourceID = String(sourceWorkspaceID || '').trim();
  if (sourceID) {
    payload.source_workspace_id = sourceID;
  }
  try {
    const resp = await fetch(apiURL('runtime/workspaces'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const responsePayload = await resp.json();
    const project = responsePayload?.workspace || {};
    const workspaceID = String(project?.id || '').trim();
    await fetchProjects();
    if (workspaceID) {
      await switchProject(workspaceID);
      return;
    }
    showStatus(`${projectKind} ready`);
  } catch (err) {
    const message = String(err?.message || err || `${projectKind} start failed`);
    appendPlainMessage('system', `${projectKind} start failed: ${message}`);
    showStatus(`${projectKind} start failed: ${message}`);
  }
}

export async function persistTemporaryProject(workspaceID) {
  const id = String(workspaceID || '').trim();
  if (!id) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus('saving session...');
  try {
    const resp = await fetch(apiURL(`runtime/workspaces/${encodeURIComponent(id)}/persist`), { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    if (payload?.workspace) {
      upsertProject(payload.workspace);
    }
    await fetchProjects();
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus('session saved');
  } catch (err) {
    const message = String(err?.message || err || 'session save failed');
    appendPlainMessage('system', `Session save failed: ${message}`);
    showStatus(`session save failed: ${message}`);
  }
}

export async function discardTemporaryProject(workspaceID) {
  const id = String(workspaceID || '').trim();
  if (!id) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus('discarding session...');
  try {
    const resp = await fetch(apiURL(`runtime/workspaces/${encodeURIComponent(id)}/discard`), { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    const nextWorkspaceID = String(payload?.active_workspace_id || '').trim() || state.defaultWorkspaceId || state.projects[0]?.id || '';
    await fetchProjects();
    if (nextWorkspaceID) {
      await switchProject(nextWorkspaceID);
      return;
    }
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus('session discarded');
  } catch (err) {
    const message = String(err?.message || err || 'session discard failed');
    appendPlainMessage('system', `Session discard failed: ${message}`);
    showStatus(`session discard failed: ${message}`);
  }
}

export async function activateProject(workspaceID) {
  const resp = await fetch(apiURL(`runtime/workspaces/${encodeURIComponent(workspaceID)}/activate`), { method: 'POST' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const project = payload?.workspace || {};
  const activeSphere = normalizeActiveSphere(payload?.active_sphere || state.activeSphere);
  if (activeSphere) {
    state.activeSphere = activeSphere;
    persistActiveSpherePreference(activeSphere);
  }
  state.chatSessionId = String(project.chat_session_id || '');
  state.sessionId = String(project.canvas_session_id || 'local');
  setChatMode(project.chat_mode || 'chat');
  if (!state.chatSessionId) throw new Error('chat session ID missing');
  upsertProject(project);
  await refreshWorkspaceRuntimeState().catch(() => {});
  clearWelcomeSurface();
  return project;
}

export async function loadChatHistory() {
  if (!state.chatSessionId) return;
  const host = chatHistoryEl();
  if (!host) return;
  host.innerHTML = '';
  const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(state.chatSessionId)}/history`));
  if (!resp.ok) throw new Error(`chat history failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const session = payload?.session || {};
  setChatMode(session.mode || state.chatMode);
  const messages = Array.isArray(payload?.messages) ? payload.messages : [];
  for (const msg of messages) {
    const role = String(msg.role || 'assistant').toLowerCase();
    const renderFormat = String(msg.render_format || '').toLowerCase();
    const markdown = String(msg.content_markdown || '');
    const plain = String(msg.content_plain || markdown);
    if (role === 'assistant') {
      if (!shouldRenderAssistantHistoryInChat(renderFormat, markdown, plain)) continue;
      appendRenderedAssistant(markdown || plain, {
        provider: msg.provider,
        providerModel: msg.provider_model,
      });
    } else {
      appendPlainMessage(role, plain);
    }
  }
  scrollChatToBottom(host);
  updateAssistantActivityIndicator();
}

export async function refreshAssistantActivity() {
  if (!state.chatSessionId || state.assistantActivityInFlight) return;
  const targetSessionID = state.chatSessionId;
  state.assistantActivityInFlight = true;
  try {
    const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(targetSessionID)}/activity`), { cache: 'no-store' });
    if (!resp.ok) {
      if (!hasLocalAssistantWork() && !state.assistantCancelInFlight) {
        state.assistantRemoteActiveCount = 0;
        state.assistantRemoteQueuedCount = 0;
        updateAssistantActivityIndicator();
      }
      return;
    }
    if (targetSessionID !== state.chatSessionId) return;
    const payload = await resp.json();
    const activeTurns = Number(payload?.active_turns || 0);
    const queuedTurns = Number(payload?.queued_turns || 0);
    if (!Number.isFinite(activeTurns) || activeTurns < 0) return;
    if (!Number.isFinite(queuedTurns) || queuedTurns < 0) return;
    state.assistantRemoteActiveCount = activeTurns;
    state.assistantRemoteQueuedCount = queuedTurns;
    const project = activeProject();
    if (project?.id) {
      upsertProject({
        ...project,
        run_state: payload,
      });
      renderEdgeTopProjects();
    }
    const recentlyStarted = (Date.now() - state.assistantLastStartedAt) < ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS;
    if (activeTurns <= 0
      && queuedTurns <= 0
      && !state.assistantCancelInFlight
      && !state.voiceAwaitingTurn
      && !state.voiceTranscriptSubmitInFlight
      && !isVoiceTranscriptSubmitPending()
      && !recentlyStarted) {
      state.assistantActiveTurns.clear();
      state.assistantUnknownTurns = 0;
    }
    updateAssistantActivityIndicator();
  } catch (_) {
    if (!hasLocalAssistantWork() && !state.assistantCancelInFlight) {
      state.assistantRemoteActiveCount = 0;
      state.assistantRemoteQueuedCount = 0;
      updateAssistantActivityIndicator();
    }
  } finally {
    state.assistantActivityInFlight = false;
  }
}

export async function refreshProjectRunStates() {
  if (state.projectRunStatesInFlight) return;
  state.projectRunStatesInFlight = true;
  try {
    const resp = await fetch(apiURL('runtime/workspaces/activity'), { cache: 'no-store' });
    if (!resp.ok) return;
    const payload = await resp.json();
    const items = Array.isArray(payload?.workspaces) ? payload.workspaces : [];
    for (const item of items) {
      const workspaceID = String(item?.workspace_id || '').trim();
      if (!workspaceID) continue;
      const existing = state.projects.find((project) => project.id === workspaceID);
      if (!existing) continue;
      upsertProject({
        ...existing,
        chat_mode: item?.chat_mode || existing.chat_mode,
        run_state: item?.run_state,
        unread: item?.unread,
        review_pending: item?.review_pending,
      });
    }
    renderEdgeTopProjects();
  } finally {
    state.projectRunStatesInFlight = false;
  }
}

export function startAssistantActivityWatcher() {
  if (state.assistantActivityTimer !== null) return;
  const clearAssistantActivityTimer = () => {
    if (state.assistantActivityTimer !== null) {
      window.clearTimeout(state.assistantActivityTimer);
      state.assistantActivityTimer = null;
    }
  };
  const scheduleAssistantActivityTick = (delayMs = ASSISTANT_ACTIVITY_POLL_MS) => {
    clearAssistantActivityTimer();
    if (document.hidden) return;
    const delay = Number.isFinite(delayMs) ? Math.max(0, Math.floor(delayMs)) : ASSISTANT_ACTIVITY_POLL_MS;
    state.assistantActivityTimer = window.setTimeout(() => {
      state.assistantActivityTimer = null;
      void tick();
    }, delay);
  };
  const tick = async () => {
    if (document.hidden) return;
    if (hasLocalStopCapableWork() && state.chatWs && state.chatWsLastMessageAt > 0) {
      const elapsed = Date.now() - state.chatWsLastMessageAt;
      if (elapsed > CHAT_WS_STALE_THRESHOLD_MS) {
        state.chatWsLastMessageAt = 0;
        closeChatWs();
        openChatWs();
        scheduleAssistantActivityTick(ASSISTANT_ACTIVITY_POLL_MS);
        return;
      }
    }
    try {
      await refreshAssistantActivity();
      await refreshProjectRunStates();
    } finally {
      scheduleAssistantActivityTick(ASSISTANT_ACTIVITY_POLL_MS);
    }
  };
  scheduleAssistantActivityTick(0);
  window.addEventListener('focus', () => {
    scheduleAssistantActivityTick(0);
    if (document.hidden || state.chatVoiceCapture) return;
    requestMicRefresh();
    releaseMicStream({ force: true });
    requestHotwordSync();
  });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      clearAssistantActivityTimer();
      requestMicRefresh();
      stopHotwordMonitor();
      state.hotwordActive = false;
      cancelLiveSessionListen();
      releaseMicStream({ force: true });
      if (state.chatVoiceCapture) {
        cancelChatVoiceCapture();
      }
      if (state.voiceAwaitingTurn) {
        sttCancel();
        state.voiceAwaitingTurn = false;
        updateAssistantActivityIndicator();
      }
      return;
    }
    scheduleAssistantActivityTick(0);
    requestHotwordSync();
  });
  window.addEventListener('pageshow', () => {
    scheduleAssistantActivityTick(0);
    if (state.chatVoiceCapture) return;
    requestMicRefresh();
    releaseMicStream({ force: true });
    requestHotwordSync();
  });
  window.addEventListener('pagehide', () => {
    clearAssistantActivityTimer();
    requestMicRefresh();
    stopHotwordMonitor();
    state.hotwordActive = false;
    releaseMicStream({ force: true });
  });
  const mediaDevices = navigator.mediaDevices;
  if (mediaDevices && typeof mediaDevices.addEventListener === 'function') {
    mediaDevices.addEventListener('devicechange', () => {
      requestMicRefresh();
      releaseMicStream({ force: true });
      requestHotwordSync();
    });
  }
}
