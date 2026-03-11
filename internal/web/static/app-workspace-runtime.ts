import * as env from './app-env.js';
import * as context from './app-context.js';

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
const activeProjectKey = (...args) => refs.activeProjectKey(...args);
const setChatMode = (...args) => refs.setChatMode(...args);
const resetCompanionState = (...args) => refs.resetCompanionState(...args);
const applyCompanionState = (...args) => refs.applyCompanionState(...args);
const chatHistoryEl = (...args) => refs.chatHistoryEl(...args);
const scrollChatToBottom = (...args) => refs.scrollChatToBottom(...args);
const cancelChatVoiceCapture = (...args) => refs.cancelChatVoiceCapture(...args);
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
const readPersistedProjectID = (...args) => refs.readPersistedProjectID(...args);
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
const isTemporaryProjectKind = (...args) => refs.isTemporaryProjectKind(...args);
const shouldRenderAssistantHistoryInChat = (...args) => refs.shouldRenderAssistantHistoryInChat(...args);
const hasLocalAssistantWork = (...args) => refs.hasLocalAssistantWork(...args);

export async function fetchProjects() {
  const resp = await fetch(apiURL('projects'), { cache: 'no-store' });
  if (!resp.ok) throw new Error(`projects list failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const projects = Array.isArray(payload?.projects) ? payload.projects : [];
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
  state.defaultProjectId = String(payload?.default_project_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_project_id || '').trim();
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

function currentExecutionPolicy(project = activeProject()) {
  if (state.yoloMode) return 'autonomous';
  const mode = String(project?.chat_mode || 'chat').trim().toLowerCase();
  if (mode === 'plan' || mode === 'review') return 'reviewed';
  return 'default';
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

export async function refreshCompanionState(projectID = state.activeProjectId) {
  const project = state.projects.find((item) => item.id === String(projectID || '').trim()) || null;
  if (!project) {
    resetCompanionState();
    return null;
  }
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(project.id)}/companion/state`), { cache: 'no-store' });
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
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(project.id)}/companion/config`), {
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
    project_key: activeProjectKey(),
    companion_enabled: payload?.companion_enabled,
    idle_surface: payload?.idle_surface,
    state: state.companionRuntimeState,
    reason: state.companionRuntimeReason,
  });
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  return payload;
}

export async function toggleCompanionIdleSurfacePreference() {
  const nextSurface = state.companionIdleSurface === COMPANION_IDLE_SURFACES.BLACK
    ? COMPANION_IDLE_SURFACES.ROBOT
    : COMPANION_IDLE_SURFACES.BLACK;
  try {
    await updateCompanionConfig({ idle_surface: nextSurface });
    showStatus(nextSurface === COMPANION_IDLE_SURFACES.BLACK ? 'black mode on' : 'black mode off');
  } catch (err) {
    const message = String(err?.message || err || 'idle surface update failed');
    appendPlainMessage('system', `Idle surface update failed: ${message}`);
    showStatus(`idle surface failed: ${message}`);
  }
}

export async function activateLiveSession(mode) {
  const normalized = String(mode || '').trim().toLowerCase();
  if (normalized !== LIVE_SESSION_MODE_DIALOGUE && normalized !== LIVE_SESSION_MODE_MEETING) return false;
  if (!activeProject()) return false;
  const wasMeeting = isMeetingLiveSession();
  if (state.liveSessionActive) {
    stopLiveSession();
    applyLiveSessionStateSnapshot();
  }
  if (wasMeeting && normalized !== LIVE_SESSION_MODE_MEETING) {
    await updateCompanionConfig({ companion_enabled: false }).catch(() => {});
  }
  if (normalized === LIVE_SESSION_MODE_MEETING) {
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
  const started = await startLiveSession(LIVE_SESSION_MODE_DIALOGUE, state.chatWs);
  applyLiveSessionStateSnapshot();
  return started;
}

export async function deactivateLiveSession(options: Record<string, any> = {}) {
  const silent = Boolean(options?.silent);
  const disableMeetingConfig = Boolean(options?.disableMeetingConfig);
  const wasMeeting = isMeetingLiveSession();
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

export function resolveInitialProjectID() {
  const reloadProjectID = String(state.pendingRuntimeReloadContext?.activeProjectId || '').trim();
  if (reloadProjectID && state.projects.some((project) => project.id === reloadProjectID)) {
    return reloadProjectID;
  }
  if (state.serverActiveProjectId && state.projects.some((project) => project.id === state.serverActiveProjectId)) {
    return state.serverActiveProjectId;
  }
  const persisted = readPersistedProjectID();
  if (persisted && state.projects.some((project) => project.id === persisted)) {
    return persisted;
  }
  if (state.defaultProjectId && state.projects.some((project) => project.id === state.defaultProjectId)) {
    return state.defaultProjectId;
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
    if (project.id === state.activeProjectId) {
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
      if (project.id === state.activeProjectId) return;
      void switchProject(project.id);
    });
    host.appendChild(button);
  }
}

export function renderEdgeTopModelButtons() {
  const host = document.getElementById('edge-top-models');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  const sphereWrap = document.createElement('div');
  sphereWrap.className = 'edge-sphere-toggle';
  for (const option of SPHERE_OPTIONS) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-project-btn edge-model-btn edge-sphere-btn';
    button.textContent = option.label;
    button.dataset.sphere = option.id;
    button.setAttribute('aria-pressed', state.activeSphere === option.id ? 'true' : 'false');
    if (state.activeSphere === option.id) {
      button.classList.add('is-active');
    }
    button.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    button.addEventListener('click', () => {
      void setActiveSphere(option.id);
    });
    sphereWrap.appendChild(button);
  }
  host.appendChild(sphereWrap);

  const project = activeProject();
  const selectedAlias = activeProjectChatModelAlias();
  const selectedEffort = activeProjectChatModelReasoningEffort();
  const effortOptions = reasoningEffortOptionsForAlias(selectedAlias);
  for (const alias of PROJECT_CHAT_MODEL_ALIASES) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-project-btn edge-model-btn';
    button.textContent = alias;
    if (alias === selectedAlias) {
      button.classList.add('is-active');
    }
    button.disabled = !project || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    button.addEventListener('click', () => {
      void switchProjectChatModel(alias);
    });
    host.appendChild(button);
  }

  const effortWrap = document.createElement('div');
  effortWrap.className = 'edge-model-effort-wrap';
  const effortSelect = document.createElement('select');
  effortSelect.className = 'edge-model-select edge-reasoning-effort-select';
  effortSelect.setAttribute('aria-label', 'Reasoning effort');
  for (const effort of effortOptions) {
    const option = document.createElement('option');
    option.value = effort;
    option.textContent = effort === 'xhigh' || effort === 'extra_high' ? 'xhigh' : effort.replace(/_/g, ' ');
    effortSelect.appendChild(option);
  }
  effortSelect.value = effortOptions.includes(selectedEffort) ? selectedEffort : (effortOptions[0] || '');
  effortSelect.disabled = !project || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  effortSelect.addEventListener('change', () => {
    const nextEffort = normalizeProjectChatModelReasoningEffort(effortSelect.value, selectedAlias);
    void switchProjectChatModel(selectedAlias, nextEffort);
  });
  effortWrap.appendChild(effortSelect);
  host.appendChild(effortWrap);

  const liveLabel = document.createElement('span');
  liveLabel.className = 'edge-project-btn edge-model-btn edge-live-label';
  liveLabel.textContent = 'Live';
  host.appendChild(liveLabel);

  const liveDisabled = !project || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  if (state.liveSessionActive) {
    const liveStatus = document.createElement('span');
    liveStatus.className = 'edge-project-btn edge-model-btn edge-live-status';
    liveStatus.textContent = liveSessionStatusSummary();
    host.appendChild(liveStatus);

    if (state.hotwordEnabled) {
      const hotwordBadge = document.createElement('span');
      hotwordBadge.className = 'edge-project-btn edge-model-btn edge-live-hotword';
      hotwordBadge.textContent = state.liveSessionHotword || LIVE_SESSION_HOTWORD_DEFAULT;
      host.appendChild(hotwordBadge);
    }

    const stopButton = document.createElement('button');
    stopButton.type = 'button';
    stopButton.className = 'edge-project-btn edge-model-btn edge-live-stop-btn';
    stopButton.textContent = 'Stop';
    stopButton.disabled = liveDisabled;
    stopButton.addEventListener('click', () => {
      void deactivateLiveSession({ disableMeetingConfig: true });
    });
    host.appendChild(stopButton);
  } else {
    const dialogueButton = document.createElement('button');
    dialogueButton.type = 'button';
    dialogueButton.className = 'edge-project-btn edge-model-btn edge-live-dialogue-btn';
    dialogueButton.textContent = 'Dialogue';
    dialogueButton.disabled = liveDisabled || !state.ttsEnabled;
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
    host.appendChild(dialogueButton);

    const meetingButton = document.createElement('button');
    meetingButton.type = 'button';
    meetingButton.className = 'edge-project-btn edge-model-btn edge-live-meeting-btn';
    meetingButton.textContent = 'Meeting';
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
    host.appendChild(meetingButton);
  }

  const yoloButton = document.createElement('button');
  yoloButton.type = 'button';
  yoloButton.className = 'edge-project-btn edge-model-btn edge-yolo-btn';
  yoloButton.textContent = 'Auto';
  yoloButton.title = `Execution policy: ${currentExecutionPolicy(project)}`;
  yoloButton.setAttribute('aria-label', 'Autonomous execution policy');
  yoloButton.setAttribute('aria-pressed', state.yoloMode ? 'true' : 'false');
  if (state.yoloMode) {
    yoloButton.classList.add('is-active');
  }
  yoloButton.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  yoloButton.addEventListener('click', () => {
    toggleYoloMode();
  });
  host.appendChild(yoloButton);

  const silentButton = document.createElement('button');
  silentButton.type = 'button';
  silentButton.className = 'edge-project-btn edge-model-btn edge-silent-btn';
  silentButton.textContent = 'silent';
  silentButton.setAttribute('aria-pressed', state.ttsSilent ? 'true' : 'false');
  if (state.ttsSilent) {
    silentButton.classList.add('is-active');
  }
  silentButton.disabled = !state.ttsEnabled || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  silentButton.addEventListener('click', () => {
    toggleTTSSilentMode();
  });
  host.appendChild(silentButton);

  const blackButton = document.createElement('button');
  blackButton.type = 'button';
  blackButton.className = 'edge-project-btn edge-model-btn edge-companion-surface-btn';
  blackButton.textContent = 'black';
  blackButton.setAttribute('aria-pressed', state.companionIdleSurface === COMPANION_IDLE_SURFACES.BLACK ? 'true' : 'false');
  if (state.companionIdleSurface === COMPANION_IDLE_SURFACES.BLACK) {
    blackButton.classList.add('is-active');
  }
  blackButton.disabled = !project || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  blackButton.addEventListener('click', () => {
    void toggleCompanionIdleSurfacePreference();
  });
  host.appendChild(blackButton);

  const temporarySourceProjectID = project ? String(project.id || '').trim() : '';
  const temporaryButtons = isTemporaryProjectKind(project?.kind)
    ? [
        {
          className: 'edge-temp-persist-btn',
          label: 'keep',
          onClick: () => { void persistTemporaryProject(String(project?.id || '').trim()); },
        },
        {
          className: 'edge-temp-discard-btn',
          label: 'discard',
          onClick: () => { void discardTemporaryProject(String(project?.id || '').trim()); },
        },
      ]
    : [
        {
          className: 'edge-temp-meeting-btn',
          label: 'meeting',
          onClick: () => { void createTemporaryProject('meeting', temporarySourceProjectID); },
        },
        {
          className: 'edge-temp-task-btn',
          label: 'task',
          onClick: () => { void createTemporaryProject('task', temporarySourceProjectID); },
        },
      ];
  for (const action of temporaryButtons) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `edge-project-btn edge-model-btn ${action.className}`;
    button.textContent = action.label;
    button.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    button.addEventListener('click', action.onClick);
    host.appendChild(button);
  }
  renderToolPalette();
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
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(project.id)}/chat-model`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const responsePayload = await resp.json();
    const updatedProject = responsePayload?.project || {};
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

export async function createTemporaryProject(kind, sourceProjectID = '') {
  const projectKind = String(kind || '').trim().toLowerCase();
  if (!isTemporaryProjectKind(projectKind)) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus(`starting ${projectKind}...`);
  const payload: Record<string, any> = {
    kind: projectKind,
    activate: true,
  };
  const sourceID = String(sourceProjectID || '').trim();
  if (sourceID) {
    payload.source_project_id = sourceID;
  }
  try {
    const resp = await fetch(apiURL('projects'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const responsePayload = await resp.json();
    const project = responsePayload?.project || {};
    const projectID = String(project?.id || '').trim();
    await fetchProjects();
    if (projectID) {
      await switchProject(projectID);
      return;
    }
    showStatus(`${projectKind} ready`);
  } catch (err) {
    const message = String(err?.message || err || `${projectKind} start failed`);
    appendPlainMessage('system', `${projectKind} start failed: ${message}`);
    showStatus(`${projectKind} start failed: ${message}`);
  }
}

export async function persistTemporaryProject(projectID) {
  const id = String(projectID || '').trim();
  if (!id) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus('saving session...');
  try {
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(id)}/persist`), { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    if (payload?.project) {
      upsertProject(payload.project);
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

export async function discardTemporaryProject(projectID) {
  const id = String(projectID || '').trim();
  if (!id) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus('discarding session...');
  try {
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(id)}/discard`), { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    const nextProjectID = String(payload?.active_project_id || '').trim() || state.defaultProjectId || state.projects[0]?.id || '';
    await fetchProjects();
    if (nextProjectID) {
      await switchProject(nextProjectID);
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

export async function activateProject(projectID) {
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(projectID)}/activate`), { method: 'POST' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const project = payload?.project || {};
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
      appendRenderedAssistant(markdown || plain);
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
    const resp = await fetch(apiURL('projects/activity'), { cache: 'no-store' });
    if (!resp.ok) return;
    const payload = await resp.json();
    const items = Array.isArray(payload?.projects) ? payload.projects : [];
    for (const item of items) {
      const projectID = String(item?.project_id || '').trim();
      if (!projectID) continue;
      const existing = state.projects.find((project) => project.id === projectID);
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
