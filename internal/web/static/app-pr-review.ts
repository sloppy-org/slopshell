import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);
const renderItemSidebarList = (...args) => refs.renderItemSidebarList(...args);
const showCanvasColumn = (...args) => refs.showCanvasColumn(...args);
const closeEdgePanels = (...args) => refs.closeEdgePanels(...args);
const renderSidebarTabs = (...args) => refs.renderSidebarTabs(...args);
const renderSidebarRow = (...args) => refs.renderSidebarRow(...args);
const renderWorkspaceFileList = (...args) => refs.renderWorkspaceFileList(...args);
const clearWelcomeSurface = (...args) => refs.clearWelcomeSurface(...args);
const stepItemSidebarItem = (...args) => refs.stepItemSidebarItem(...args);

export function isMobileViewport() {
  return window.matchMedia('(max-width: 767px)').matches;
}

export function statusBadgeForDiffFile(statusRaw) {
  const normalized = String(statusRaw || '').trim().toLowerCase();
  if (normalized === 'added') return 'A';
  if (normalized === 'deleted') return 'D';
  if (normalized === 'renamed') return 'R';
  return 'M';
}

export function parseUnifiedDiffFiles(diffText) {
  const text = String(diffText || '').replaceAll('\r\n', '\n');
  if (!text.trim()) return [];
  const lines = text.split('\n');
  const files = [];
  let current = null;

  const pushCurrent = () => {
    if (!current) return;
    const diff = current.lines.join('\n').trimEnd();
    if (!diff) return;
    files.push({
      path: String(current.path || '(patch)'),
      status: String(current.status || 'modified'),
      diff,
    });
  };

  const parsePathFromHeader = (line) => {
    const match = /^diff --git a\/(.+?) b\/(.+)$/.exec(line);
    if (!match) return '';
    const right = String(match[2] || '').trim();
    const left = String(match[1] || '').trim();
    if (right && right !== '/dev/null') return right;
    return left;
  };

  const parsePathFromMarker = (line, marker) => {
    if (!line.startsWith(marker)) return '';
    const raw = String(line.slice(marker.length)).trim();
    if (!raw || raw === '/dev/null') return '';
    return raw.startsWith('a/') || raw.startsWith('b/') ? raw.slice(2) : raw;
  };

  for (const line of lines) {
    if (line.startsWith('diff --git ')) {
      pushCurrent();
      current = {
        path: parsePathFromHeader(line) || '(patch)',
        status: 'modified',
        lines: [line],
      };
      continue;
    }

    if (!current) {
      continue;
    }

    current.lines.push(line);
    if (line.startsWith('new file mode ')) {
      current.status = 'added';
      continue;
    }
    if (line.startsWith('deleted file mode ')) {
      current.status = 'deleted';
      continue;
    }
    if (line.startsWith('rename from ')) {
      current.status = 'renamed';
      continue;
    }
    if (line.startsWith('rename to ')) {
      const renamedTo = String(line.slice('rename to '.length)).trim();
      if (renamedTo) current.path = renamedTo;
      current.status = 'renamed';
      continue;
    }
    const plusPath = parsePathFromMarker(line, '+++ ');
    if (plusPath && current.path === '(patch)') {
      current.path = plusPath;
      continue;
    }
    const minusPath = parsePathFromMarker(line, '--- ');
    if (minusPath && current.path === '(patch)') {
      current.path = minusPath;
    }
  }
  pushCurrent();

  if (files.length > 0) return files;
  return [{
    path: '(patch)',
    status: 'modified',
    diff: text.trimEnd(),
  }];
}

export function setPrReviewDrawerOpen(open) {
  const shouldOpen = Boolean(open) && (state.prReviewMode || Boolean(state.activeProjectId));
  state.prReviewDrawerOpen = shouldOpen;
  document.body.classList.toggle('file-sidebar-open', shouldOpen);
  const pane = document.getElementById('pr-file-pane');
  const backdrop = document.getElementById('pr-file-drawer-backdrop');
  if (pane) pane.classList.toggle('is-open', shouldOpen);
  if (backdrop) backdrop.classList.toggle('is-open', shouldOpen);
}

export function setFileSidebarAvailability() {
  const enabled = state.prReviewMode || Boolean(state.activeProjectId);
  document.body.classList.toggle('file-sidebar-enabled', enabled);
  if (!enabled) {
    setPrReviewDrawerOpen(false);
  }
}

export function normalizeWorkspaceBrowserPath(rawPath) {
  const cleaned = String(rawPath || '').replaceAll('\\', '/').trim();
  if (!cleaned) return '';
  const pieces = cleaned.split('/').filter((piece) => piece && piece !== '.' && piece !== '..');
  return pieces.join('/');
}

export function parentWorkspaceBrowserPath(path) {
  const cleaned = normalizeWorkspaceBrowserPath(path);
  if (!cleaned) return '';
  const pieces = cleaned.split('/');
  pieces.pop();
  return pieces.join('/');
}

export function workspaceNavigableFilePaths() {
  const entries = Array.isArray(state.workspaceBrowserEntries) ? state.workspaceBrowserEntries : [];
  const files = [];
  entries.forEach((entry) => {
    if (Boolean(entry?.is_dir)) return;
    const path = normalizeWorkspaceBrowserPath(entry?.path || '');
    if (!path) return;
    files.push(path);
  });
  return files;
}

export function resolveWorkspaceSteppingCurrentFile() {
  const fromState = normalizeWorkspaceBrowserPath(state.workspaceOpenFilePath);
  if (fromState) return fromState;
  const activeTitle = normalizeWorkspaceBrowserPath(getActiveArtifactTitle());
  if (activeTitle) return activeTitle;
  return '';
}

export function sidebarFileKindForPath(path) {
  const lower = String(path || '').toLowerCase();
  if (lower.endsWith('.pdf')) return 'pdf_artifact';
  for (const ext of SIDEBAR_IMAGE_EXTENSIONS) {
    if (lower.endsWith(ext)) return 'image_artifact';
  }
  return 'text_artifact';
}

export function companionViewKindForPath(path) {
  const normalized = normalizeWorkspaceBrowserPath(path);
  if (normalized === COMPANION_TRANSCRIPT_VIEW_PATH) return 'transcript';
  if (normalized === COMPANION_SUMMARY_VIEW_PATH) return 'summary';
  if (normalized === COMPANION_REFERENCES_VIEW_PATH) return 'references';
  return '';
}

export function workspaceCompanionEntries() {
  return [
    { name: MEETING_TRANSCRIPT_LABEL, path: COMPANION_TRANSCRIPT_VIEW_PATH, is_dir: false },
    { name: MEETING_SUMMARY_LABEL, path: COMPANION_SUMMARY_VIEW_PATH, is_dir: false },
    { name: MEETING_REFERENCES_LABEL, path: COMPANION_REFERENCES_VIEW_PATH, is_dir: false },
  ];
}

export function resetPrReviewUi() {
  document.body.classList.remove('pr-review-mode');
  state.fileSidebarMode = 'items';
  setFileSidebarAvailability();
  renderPrReviewFileList();
}

export function renderPrReviewFileList() {
  const list = document.getElementById('pr-file-list');
  if (!(list instanceof HTMLElement)) return;
  setFileSidebarAvailability();
  if (state.prReviewMode) {
    state.fileSidebarMode = 'pr';
  }
  const mode = state.fileSidebarMode === 'pr' && state.prReviewMode ? 'pr' : state.fileSidebarMode;
  list.innerHTML = '';
  if (mode === 'pr') {
    const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
    files.forEach((file, index) => {
      const statusName = String(file?.status || 'modified').toLowerCase();
      list.appendChild(renderSidebarRow({
        icon: 'file',
        label: String(file?.path || `(file ${index + 1})`),
        active: index === state.prReviewActiveIndex,
        meta: statusBadgeForDiffFile(statusName),
        onClick: () => {
          setPrReviewActiveFile(index);
          if (isMobileViewport()) {
            setPrReviewDrawerOpen(false);
            closeEdgePanels();
          }
        },
      }));
    });
    return;
  }
  renderSidebarTabs(list);
  if (mode === 'workspace') {
    renderWorkspaceFileList(list);
    return;
  }
  renderItemSidebarList(list);
}

export async function loadWorkspaceBrowserPath(path = '') {
  const projectID = String(state.activeProjectId || '').trim();
  if (!projectID) {
    state.workspaceBrowserPath = '';
    state.workspaceBrowserEntries = [];
    state.workspaceBrowserLoading = false;
    state.workspaceBrowserError = '';
    state.workspaceBrowserActivePath = '';
    state.workspaceBrowserActiveIsDir = false;
    renderPrReviewFileList();
    return false;
  }
  const requestedPath = normalizeWorkspaceBrowserPath(path);
  state.workspaceBrowserLoading = true;
  state.workspaceBrowserError = '';
  renderPrReviewFileList();
  try {
    const resp = await fetch(apiURL(`workspaces/active/files?path=${encodeURIComponent(requestedPath)}`), { cache: 'no-store' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail || 'file list unavailable');
    }
    const payload = await resp.json();
    if (projectID !== String(state.activeProjectId || '')) return false;
    state.workspaceBrowserPath = normalizeWorkspaceBrowserPath(payload?.path || requestedPath);
    state.workspaceBrowserActivePath = state.workspaceBrowserPath;
    state.workspaceBrowserActiveIsDir = Boolean(state.workspaceBrowserPath);
    const entriesRaw = Array.isArray(payload?.entries) ? payload.entries : [];
    state.workspaceBrowserEntries = entriesRaw.map((entry) => ({
      name: String(entry?.name || ''),
      path: normalizeWorkspaceBrowserPath(entry?.path || ''),
      is_dir: Boolean(entry?.is_dir),
    }));
    state.workspaceBrowserLoading = false;
    state.workspaceBrowserError = '';
    renderPrReviewFileList();
    return true;
  } catch (err) {
    if (projectID !== String(state.activeProjectId || '')) return false;
    state.workspaceBrowserLoading = false;
    state.workspaceBrowserError = String(err?.message || err || 'file list unavailable');
    state.workspaceBrowserEntries = [];
    state.workspaceBrowserActivePath = '';
    state.workspaceBrowserActiveIsDir = false;
    renderPrReviewFileList();
    return false;
  }
}

export async function openWorkspaceSidebarFile(path) {
  const filePath = normalizeWorkspaceBrowserPath(path);
  if (!filePath) return false;
  state.fileSidebarMode = 'workspace';
  state.workspaceBrowserActivePath = filePath;
  state.workspaceBrowserActiveIsDir = false;
  clearWelcomeSurface();
  const companionViewKind = companionViewKindForPath(filePath);
  if (companionViewKind) {
    return openCompanionWorkspaceView(companionViewKind, filePath);
  }
  const kind = sidebarFileKindForPath(filePath);
  if (kind === 'image_artifact') {
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'image_artifact',
      event_id: `workspace-file-${Date.now()}`,
      title: filePath,
      path: filePath,
    });
    showCanvasColumn('canvas-image');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  }
  if (kind === 'pdf_artifact') {
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'pdf_artifact',
      event_id: `workspace-file-${Date.now()}`,
      title: filePath,
      path: filePath,
    });
    showCanvasColumn('canvas-pdf');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  }

  const sid = String(state.sessionId || 'local');
  try {
    const resp = await fetch(apiURL(`files/${encodeURIComponent(sid)}/${encodeURIComponent(filePath)}`), { cache: 'no-store' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const contentType = String(resp.headers.get('content-type') || '').toLowerCase();
    if (contentType.startsWith('image/')) {
      state.workspaceOpenFilePath = filePath;
      renderPrReviewFileList();
      renderCanvas({
        kind: 'image_artifact',
        event_id: `workspace-file-${Date.now()}`,
        title: filePath,
        path: filePath,
      });
      showCanvasColumn('canvas-image');
      if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
      return true;
    }
    if (contentType.includes('application/pdf')) {
      state.workspaceOpenFilePath = filePath;
      renderPrReviewFileList();
      renderCanvas({
        kind: 'pdf_artifact',
        event_id: `workspace-file-${Date.now()}`,
        title: filePath,
        path: filePath,
      });
      showCanvasColumn('canvas-pdf');
      if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
      return true;
    }
    const text = await resp.text();
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'text_artifact',
      event_id: `workspace-file-${Date.now()}`,
      title: filePath,
      text,
    });
    showCanvasColumn('canvas-text');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  } catch (err) {
    showStatus(`open failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function openCompanionWorkspaceView(viewKind, filePath) {
  const projectID = String(state.activeProjectId || '').trim();
  if (!projectID) return false;
  const titles = {
    transcript: MEETING_TRANSCRIPT_LABEL,
    summary: MEETING_SUMMARY_LABEL,
    references: MEETING_REFERENCES_LABEL,
  };
  const endpoint = viewKind === 'transcript' || viewKind === 'summary' || viewKind === 'references'
    ? viewKind
    : '';
  if (!endpoint) return false;
  try {
    const resp = await fetch(apiURL(`workspaces/active/${endpoint}?format=md`), { cache: 'no-store' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const text = await resp.text();
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'text_artifact',
      event_id: `workspace-companion-${viewKind}-${Date.now()}`,
      title: titles[viewKind] || filePath,
      text,
    });
    if (viewKind === 'summary') {
      void renderMeetingSummaryItems(filePath);
    }
    showCanvasColumn('canvas-text');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  } catch (err) {
    appendPlainMessage('system', `${titles[viewKind] || 'Meeting view'} failed: ${String(err?.message || err)}`);
    return false;
  }
}

export function clearMeetingSummaryItemsPanel() {
  const existing = document.getElementById(MEETING_SUMMARY_ITEMS_PANEL_ID);
  if (existing instanceof HTMLElement) {
    existing.remove();
  }
}

export function isCurrentMeetingSummaryView(filePath) {
  return Boolean(String(state.activeProjectId || '').trim())
    && normalizeWorkspaceBrowserPath(state.workspaceOpenFilePath) === normalizeWorkspaceBrowserPath(filePath);
}

export function meetingSummaryItemsButtonLabel(count) {
  return count === 1 ? 'Create 1 inbox item' : `Create ${count} inbox items`;
}

export function setMeetingSummaryItemsBusy(panel, busy, label = '') {
  if (!(panel instanceof HTMLElement)) return;
  panel.dataset.busy = busy ? 'true' : 'false';
  panel.querySelectorAll('input,button').forEach((node) => {
    if (node instanceof HTMLInputElement || node instanceof HTMLButtonElement) {
      node.disabled = busy;
    }
  });
  const submit = panel.querySelector('.meeting-summary-items-submit');
  if (submit instanceof HTMLButtonElement && label) {
    submit.textContent = label;
  }
}

export function countSelectedMeetingSummaryItems(panel) {
  if (!(panel instanceof HTMLElement)) return 0;
  return panel.querySelectorAll('.meeting-summary-items-list input[type="checkbox"]:checked').length;
}

export function updateMeetingSummaryItemsSelection(panel) {
  if (!(panel instanceof HTMLElement)) return;
  const count = countSelectedMeetingSummaryItems(panel);
  const submit = panel.querySelector('.meeting-summary-items-submit');
  if (submit instanceof HTMLButtonElement) {
    submit.disabled = count === 0 || panel.dataset.busy === 'true';
    submit.textContent = meetingSummaryItemsButtonLabel(count);
  }
}

export async function submitMeetingSummaryItems(panel) {
  if (!(panel instanceof HTMLElement)) return false;
  const selected = Array.from(panel.querySelectorAll('.meeting-summary-items-list input[type="checkbox"]:checked'))
    .map((node) => Number((node as HTMLInputElement).value || '-1'))
    .filter((value) => Number.isInteger(value) && value >= 0);
  if (selected.length === 0) {
    showStatus('select at least one action item');
    return false;
  }
  setMeetingSummaryItemsBusy(panel, true, 'Creating inbox items...');
  try {
    const resp = await fetch(apiURL('workspaces/active/meeting-items'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ selected }),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    const createdItems = Array.isArray(payload?.created_items) ? payload.created_items : [];
    const createdCount = createdItems.length;
    const createdSet = new Set(selected);
    panel.querySelectorAll('.meeting-summary-items-list input[type="checkbox"]').forEach((node) => {
      if (!(node instanceof HTMLInputElement)) return;
      if (!createdSet.has(Number(node.value || '-1'))) return;
      node.checked = false;
      node.disabled = true;
      const row = node.closest('.meeting-summary-items-row');
      if (row instanceof HTMLElement) {
        row.classList.add('is-created');
      }
    });
    const status = panel.querySelector('.meeting-summary-items-status');
    if (status instanceof HTMLElement) {
      status.textContent = createdCount === 1
        ? '1 inbox item created from this summary.'
        : `${createdCount} inbox items created from this summary.`;
    }
    await loadItemSidebarView('inbox');
    setMeetingSummaryItemsBusy(panel, false);
    panel.querySelectorAll('.meeting-summary-items-list input[type="checkbox"]').forEach((node) => {
      if (!(node instanceof HTMLInputElement)) return;
      if (createdSet.has(Number(node.value || '-1'))) {
        node.disabled = true;
      }
    });
    updateMeetingSummaryItemsSelection(panel);
    showStatus(createdCount === 1 ? 'meeting item added to inbox' : 'meeting items added to inbox');
    return true;
  } catch (err) {
    setMeetingSummaryItemsBusy(panel, false);
    updateMeetingSummaryItemsSelection(panel);
    showStatus(`meeting item create failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function renderMeetingSummaryItems(filePath) {
  clearMeetingSummaryItemsPanel();
  const pane = document.getElementById('canvas-text');
  if (!(pane instanceof HTMLElement) || !isCurrentMeetingSummaryView(filePath)) return false;

  const panel = document.createElement('section');
  panel.id = MEETING_SUMMARY_ITEMS_PANEL_ID;
  panel.className = 'meeting-summary-items';

  const heading = document.createElement('h2');
  heading.textContent = 'Proposed Inbox Items';
  panel.appendChild(heading);

  const intro = document.createElement('p');
  intro.className = 'meeting-summary-items-copy';
  intro.textContent = 'Select the action items you want to turn into inbox items.';
  panel.appendChild(intro);

  const status = document.createElement('p');
  status.className = 'meeting-summary-items-status';
  status.textContent = 'Loading action items...';
  panel.appendChild(status);

  pane.appendChild(panel);

  try {
    const resp = await fetch(apiURL('workspaces/active/meeting-items'), { cache: 'no-store' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    if (!panel.isConnected || !isCurrentMeetingSummaryView(filePath)) return false;
    const proposedItems = Array.isArray(payload?.proposed_items) ? payload.proposed_items : [];
    if (proposedItems.length === 0) {
      status.textContent = 'No action items detected in this summary yet.';
      return true;
    }
    status.textContent = 'Choose the actions you want to keep.';

    const list = document.createElement('div');
    list.className = 'meeting-summary-items-list';
    proposedItems.forEach((proposal) => {
      const index = Number(proposal?.index || 0);
      const title = String(proposal?.title || '').trim();
      if (!title) return;
      const row = document.createElement('label');
      row.className = 'meeting-summary-items-row';

      const checkbox = document.createElement('input');
      checkbox.type = 'checkbox';
      checkbox.value = String(index);
      checkbox.checked = true;
      checkbox.addEventListener('change', () => {
        updateMeetingSummaryItemsSelection(panel);
      });

      const body = document.createElement('span');
      body.className = 'meeting-summary-items-body';

      const titleNode = document.createElement('span');
      titleNode.className = 'meeting-summary-items-title';
      titleNode.textContent = title;
      body.appendChild(titleNode);

      const metaParts = [];
      const actorName = String(proposal?.actor_name || '').trim();
      const evidence = String(proposal?.evidence || '').trim();
      if (actorName) metaParts.push(actorName);
      if (evidence) metaParts.push(evidence);
      if (metaParts.length > 0) {
        const meta = document.createElement('span');
        meta.className = 'meeting-summary-items-meta';
        meta.textContent = metaParts.join(' | ');
        body.appendChild(meta);
      }

      row.appendChild(checkbox);
      row.appendChild(body);
      list.appendChild(row);
    });
    panel.appendChild(list);

    const submit = document.createElement('button');
    submit.type = 'button';
    submit.className = 'meeting-summary-items-submit';
    submit.addEventListener('click', () => {
      void submitMeetingSummaryItems(panel);
    });
    panel.appendChild(submit);
    updateMeetingSummaryItemsSelection(panel);
    return true;
  } catch (err) {
    if (!panel.isConnected || !isCurrentMeetingSummaryView(filePath)) return false;
    status.textContent = `Action items unavailable: ${String(err?.message || err || 'unknown error')}`;
    return false;
  }
}

export async function refreshWorkspaceBrowser(resetPath = false) {
  const nextPath = resetPath ? '' : state.workspaceBrowserPath;
  return loadWorkspaceBrowserPath(nextPath);
}

export function stepWorkspaceFile(delta) {
  if (state.prReviewMode) return false;
  if (state.workspaceStepInFlight) return false;
  const shift = Number(delta);
  if (!Number.isFinite(shift) || shift === 0) return false;
  const files = workspaceNavigableFilePaths();
  if (files.length <= 1) return false;
  const currentFile = resolveWorkspaceSteppingCurrentFile();
  if (!currentFile) return false;
  const currentIndex = files.indexOf(currentFile);
  if (currentIndex < 0) return false;
  const nextIndex = ((currentIndex + Math.trunc(shift)) % files.length + files.length) % files.length;
  if (nextIndex === currentIndex) return false;
  const nextFile = files[nextIndex];
  if (!nextFile) return false;
  state.workspaceStepInFlight = true;
  void openWorkspaceSidebarFile(nextFile).finally(() => {
    state.workspaceStepInFlight = false;
  });
  return true;
}

export function renderActivePrReviewFile() {
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (!state.prReviewMode || files.length === 0) return false;
  if (state.prReviewActiveIndex < 0 || state.prReviewActiveIndex >= files.length) {
    state.prReviewActiveIndex = 0;
  }
  const file = files[state.prReviewActiveIndex];
  if (!file) return false;
  clearWelcomeSurface();
  renderCanvas({
    kind: 'text_artifact',
    event_id: `pr-review-${Date.now()}-${state.prReviewActiveIndex}`,
    title: String(file.path || ''),
    text: String(file.diff || ''),
  });
  showCanvasColumn('canvas-text');
  renderPrReviewFileList();
  return true;
}

export function setPrReviewActiveFile(index) {
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (!state.prReviewMode || files.length === 0) return false;
  const total = files.length;
  let next = Number(index);
  if (!Number.isFinite(next)) return false;
  next = ((Math.trunc(next) % total) + total) % total;
  if (next === state.prReviewActiveIndex) {
    renderPrReviewFileList();
    return false;
  }
  state.prReviewActiveIndex = next;
  return renderActivePrReviewFile();
}

export function stepPrReviewFile(delta) {
  if (!state.prReviewMode) return false;
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (files.length <= 1) return false;
  const shift = Number(delta);
  if (!Number.isFinite(shift) || shift === 0) return false;
  return setPrReviewActiveFile(state.prReviewActiveIndex + shift);
}

export function stepCanvasFile(delta) {
  if (state.prReviewMode) {
    return stepPrReviewFile(delta);
  }
  if (stepItemSidebarItem(delta)) {
    return true;
  }
  return stepWorkspaceFile(delta);
}

export function exitPrReviewMode() {
  if (!state.prReviewMode && (!state.prReviewFiles || state.prReviewFiles.length === 0)) {
    return;
  }
  state.prReviewMode = false;
  state.prReviewFiles = [];
  state.prReviewActiveIndex = 0;
  state.prReviewTitle = '';
  state.prReviewPRNumber = '';
  resetPrReviewUi();
}

export function maybeEnterPrReviewModeFromTextArtifact(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind !== 'text_artifact' && kind !== 'text') return false;
  const title = String(payload?.title || '').trim();
  const text = String(payload?.text || '');
  if (!text.trim()) return false;
  const titleHint = /\.diff$|\.patch$/i.test(title);
  const hasDiffHeader = text.includes('\ndiff --git ') || text.startsWith('diff --git ');
  if (!titleHint && !hasDiffHeader) return false;
  const files = parseUnifiedDiffFiles(text);
  if (files.length === 0) return false;
  if (!titleHint && files.length < 2) return false;

  state.prReviewMode = true;
  state.prReviewFiles = files;
  state.prReviewActiveIndex = 0;
  state.prReviewTitle = title;
  const numberMatch = /(?:^|[^0-9])pr[-_]?(\d+)(?:[^0-9]|$)/i.exec(title);
  state.prReviewPRNumber = numberMatch ? String(numberMatch[1]) : '';
  document.body.classList.add('pr-review-mode');
  setPrReviewDrawerOpen(false);
  renderPrReviewFileList();
  return renderActivePrReviewFile();
}

export function isLikelyPrReviewArtifact(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind !== 'text_artifact' && kind !== 'text') return false;
  const title = String(payload?.title || '').trim().toLowerCase();
  if (!title) return false;
  return /(?:^|\/)\.tabura\/artifacts\/pr\/pr-\d+\.(?:diff|patch)$/.test(title)
    || /(?:^|\/)artifacts\/pr\/pr-\d+\.(?:diff|patch)$/.test(title);
}
