import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);
const applyCanvasArtifactEvent = (...args) => refs.applyCanvasArtifactEvent(...args);
const normalizeDisplayText = (...args) => refs.normalizeDisplayText(...args);
const normalizeActiveSphere = (...args) => refs.normalizeActiveSphere(...args);
const readSomedayReviewNudgeLastShownAt = (...args) => refs.readSomedayReviewNudgeLastShownAt(...args);
const persistSomedayReviewNudgeLastShownAt = (...args) => refs.persistSomedayReviewNudgeLastShownAt(...args);

function appendSphereQuery(path, sphere = state.activeSphere, allSpheres = false) {
  if (allSpheres) {
    return String(path || '');
  }
  const cleanSphere = normalizeActiveSphere(sphere);
  const separator = String(path || '').includes('?') ? '&' : '?';
  return `${path}${separator}sphere=${encodeURIComponent(cleanSphere)}`;
}

export function defaultItemSidebarCounts() {
  return {
    inbox: 0,
    waiting: 0,
    someday: 0,
    done: 0,
  };
}

export function normalizeItemSidebarView(rawView) {
  const value = String(rawView || '').trim().toLowerCase();
  if (ITEM_SIDEBAR_VIEWS.includes(value)) return value;
  return 'inbox';
}

export function normalizeItemSidebarFilters(rawFilters = null) {
  const filters = rawFilters && typeof rawFilters === 'object' ? rawFilters : {};
  const source = String(filters.source || '').trim().toLowerCase();
  const projectID = String(filters.project_id || '').trim();
  const allSpheres = filters.all_spheres === true;
  const workspaceRaw = filters.workspace_id;
  const workspaceUnassigned = String(workspaceRaw || '').trim().toLowerCase() === 'null'
    || filters.workspace_unassigned === true;
  let workspaceID = null;
  if (!workspaceUnassigned && Number.isFinite(Number(workspaceRaw)) && Number(workspaceRaw) > 0) {
    workspaceID = Math.trunc(Number(workspaceRaw));
  }
  return {
    all_spheres: allSpheres,
    source,
    workspace_id: workspaceID,
    project_id: projectID,
    workspace_unassigned: workspaceUnassigned,
  };
}

function appendItemSidebarFilterQuery(path, filters = state.itemSidebarFilters) {
  const normalized = normalizeItemSidebarFilters(filters);
  let nextPath = String(path || '');
  if (normalized.source) {
    nextPath = `${nextPath}${nextPath.includes('?') ? '&' : '?'}source=${encodeURIComponent(normalized.source)}`;
  }
  if (normalized.workspace_unassigned) {
    nextPath = `${nextPath}${nextPath.includes('?') ? '&' : '?'}workspace_id=null`;
  } else if (Number.isFinite(normalized.workspace_id) && normalized.workspace_id > 0) {
    nextPath = `${nextPath}${nextPath.includes('?') ? '&' : '?'}workspace_id=${encodeURIComponent(String(normalized.workspace_id))}`;
  }
  if (normalized.project_id) {
    nextPath = `${nextPath}${nextPath.includes('?') ? '&' : '?'}project_id=${encodeURIComponent(normalized.project_id)}`;
  }
  return nextPath;
}

export function itemSidebarEndpoint(view, filters = state.itemSidebarFilters) {
  const normalized = normalizeItemSidebarView(view);
  const normalizedFilters = normalizeItemSidebarFilters(filters);
  if (normalized === 'done') return appendItemSidebarFilterQuery(appendSphereQuery(`items/${normalized}?limit=50`, state.activeSphere, normalizedFilters.all_spheres), normalizedFilters);
  return appendItemSidebarFilterQuery(appendSphereQuery(`items/${normalized}`, state.activeSphere, normalizedFilters.all_spheres), normalizedFilters);
}

export function itemSidebarCountsEndpoint(filters = state.itemSidebarFilters) {
  const normalizedFilters = normalizeItemSidebarFilters(filters);
  return appendItemSidebarFilterQuery(appendSphereQuery('items/counts', state.activeSphere, normalizedFilters.all_spheres), normalizedFilters);
}

export function normalizeItemSidebarCounts(rawCounts) {
  const counts = defaultItemSidebarCounts();
  if (!rawCounts || typeof rawCounts !== 'object') return counts;
  ITEM_SIDEBAR_VIEWS.forEach((view) => {
    const value = Number(rawCounts[view] ?? 0);
    counts[view] = Number.isFinite(value) && value > 0 ? Math.trunc(value) : 0;
  });
  return counts;
}

export function setInboxTriggerCount(count) {
  const edgeLeftTap = document.getElementById('edge-left-tap');
  if (!(edgeLeftTap instanceof HTMLElement)) return;
  const value = Math.max(0, Number(count) || 0);
  edgeLeftTap.dataset.inboxCount = value > 0 ? String(value) : '';
  edgeLeftTap.classList.toggle('has-inbox-count', value > 0);
}

export function applyItemSidebarCounts(rawCounts) {
  state.itemSidebarCounts = normalizeItemSidebarCounts(rawCounts);
  setInboxTriggerCount(state.itemSidebarCounts.inbox);
  maybeShowSomedayReviewNudge();
}

export function maybeShowSomedayReviewNudge() {
  if (!state.somedayReviewNudgeEnabled) return false;
  const somedayCount = Number(state.itemSidebarCounts?.someday || 0);
  if (somedayCount <= 0) return false;
  if (state.fileSidebarMode === 'items' && state.itemSidebarView === 'someday' && state.prReviewDrawerOpen) {
    persistSomedayReviewNudgeLastShownAt();
    return false;
  }
  const lastShownAt = readSomedayReviewNudgeLastShownAt();
  if (lastShownAt > 0 && (Date.now() - lastShownAt) < SOMEDAY_REVIEW_NUDGE_INTERVAL_MS) {
    return false;
  }
  const suffix = somedayCount === 1 ? '' : 's';
  appendPlainMessage('system', `You have ${somedayCount} item${suffix} in someday. Say "review my someday list" to open them.`);
  showStatus('review someday list');
  persistSomedayReviewNudgeLastShownAt();
  return true;
}

export async function refreshItemSidebarCounts() {
  const projectID = String(state.activeProjectId || '').trim();
  if (!projectID) {
    applyItemSidebarCounts(defaultItemSidebarCounts());
    return false;
  }
  const resp = await fetch(apiURL(appendSphereQuery('items/counts')), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  if (projectID !== String(state.activeProjectId || '').trim()) return false;
  applyItemSidebarCounts(payload?.counts);
  return true;
}

export function isEmailSidebarItem(item) {
  return String(item?.artifact_kind || '').trim().toLowerCase() === 'email';
}

export function itemSidebarActionLabel(action, item = null) {
  const normalized = String(action || '').trim().toLowerCase();
  if (normalized === 'done') {
    return isEmailSidebarItem(item) ? 'Archive' : 'Done';
  }
  if (normalized === 'inbox') return 'Back to Inbox';
  if (normalized === 'delete') return 'Delete';
  if (normalized === 'delegate') return 'Delegate';
  if (normalized === 'later') return 'Later';
  if (normalized === 'someday') return 'Someday';
  return '';
}

export function itemSidebarStatusText(action, item = null, actorName = '') {
  const label = itemSidebarActionLabel(action, item).toLowerCase();
  if (String(action || '').trim().toLowerCase() === 'delegate' && String(actorName || '').trim()) {
    return `delegated to ${String(actorName || '').trim()}`;
  }
  if (!label) return 'updated';
  if (label === 'back to inbox') return 'returned to inbox';
  if (label === 'later') return 'moved to later';
  if (label === 'someday') return 'moved to someday';
  return `${label}d`;
}

export function defaultItemSidebarLaterVisibleAfter(now = new Date()) {
  const base = new Date(now);
  base.setUTCDate(base.getUTCDate() + 1);
  base.setUTCHours(ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, 0, 0, 0);
  return base.toISOString();
}

export function itemSidebarGestureAction(dx) {
  const offset = Number(dx) || 0;
  if (offset >= ITEM_SIDEBAR_GESTURE_LONG_PX) {
    return { action: 'delete', label: 'Delete' };
  }
  if (offset >= ITEM_SIDEBAR_GESTURE_COMMIT_PX) {
    return { action: 'done', label: 'Done' };
  }
  if (offset <= -ITEM_SIDEBAR_GESTURE_LONG_PX) {
    return { action: 'later', label: 'Later' };
  }
  if (offset <= -ITEM_SIDEBAR_GESTURE_COMMIT_PX) {
    return { action: 'delegate', label: 'Delegate' };
  }
  return null;
}

export function itemSidebarMenuEl() {
  let menu = document.getElementById(ITEM_SIDEBAR_MENU_ID);
  if (menu instanceof HTMLElement) return menu;
  menu = document.createElement('div');
  menu.id = ITEM_SIDEBAR_MENU_ID;
  menu.className = 'item-sidebar-menu';
  menu.setAttribute('role', 'menu');
  menu.setAttribute('aria-hidden', 'true');
  document.body.appendChild(menu);
  return menu;
}

export function hideItemSidebarMenu() {
  const menu = document.getElementById(ITEM_SIDEBAR_MENU_ID);
  if (!(menu instanceof HTMLElement)) return;
  menu.innerHTML = '';
  menu.classList.remove('is-open');
  menu.setAttribute('aria-hidden', 'true');
  state.itemSidebarMenuOpen = false;
}

export function positionItemSidebarMenu(menu, x, y) {
  if (!(menu instanceof HTMLElement)) return;
  menu.style.left = '0px';
  menu.style.top = '0px';
  menu.style.maxHeight = `${Math.max(160, window.innerHeight - 24)}px`;
  menu.classList.add('is-open');
  menu.setAttribute('aria-hidden', 'false');
  const rect = menu.getBoundingClientRect();
  const maxLeft = Math.max(12, window.innerWidth - rect.width - 12);
  const maxTop = Math.max(12, window.innerHeight - rect.height - 12);
  const left = Math.min(Math.max(12, Number(x) || 12), maxLeft);
  const top = Math.min(Math.max(12, Number(y) || 12), maxTop);
  menu.style.left = `${left}px`;
  menu.style.top = `${top}px`;
  state.itemSidebarMenuOpen = true;
}

export function showItemSidebarMenu(entries, x, y) {
  const items = Array.isArray(entries) ? entries.filter((entry) => entry && entry.label) : [];
  if (items.length === 0) {
    hideItemSidebarMenu();
    return;
  }
  const menu = itemSidebarMenuEl();
  menu.innerHTML = '';
  items.forEach((entry) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'item-sidebar-menu-item';
    if (entry.action) {
      button.dataset.action = String(entry.action);
    }
    button.textContent = String(entry.label || '');
    button.addEventListener('click', (event) => {
      event.preventDefault();
      const handler = typeof entry.onClick === 'function' ? entry.onClick : null;
      hideItemSidebarMenu();
      if (handler) {
        void Promise.resolve(handler());
      }
    });
    menu.appendChild(button);
  });
  positionItemSidebarMenu(menu, x, y);
}

export async function fetchItemSidebarActors() {
  const resp = await fetch(apiURL('actors'), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const actors = Array.isArray(payload?.actors) ? payload.actors : [];
  return actors
    .map((actor) => ({
      id: Number(actor?.id || 0),
      name: String(actor?.name || '').trim(),
    }))
    .filter((actor) => actor.id > 0 && actor.name);
}

export async function fetchItemSidebarWorkspaces() {
  const resp = await fetch(apiURL(appendSphereQuery('workspaces')), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const workspaces = Array.isArray(payload?.workspaces) ? payload.workspaces : [];
  return workspaces
    .map((workspace) => ({
      id: Number(workspace?.id || 0),
      name: String(workspace?.name || '').trim(),
    }))
    .filter((workspace) => workspace.id > 0 && workspace.name);
}

export async function fetchItemSidebarProjects() {
  const resp = await fetch(apiURL('projects'), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const projects = Array.isArray(payload?.projects) ? payload.projects : [];
  return projects
    .map((project) => ({
      id: String(project?.id || '').trim(),
      name: String(project?.name || '').trim(),
      sphere: String(project?.sphere || '').trim().toLowerCase(),
    }))
    .filter((project) => project.id
      && project.name
      && project.id !== 'hub'
      && (!project.sphere || project.sphere === normalizeActiveSphere(state.activeSphere)));
}

export async function performItemSidebarSphereUpdate(item, nextSphere) {
  const itemID = Number(item?.id || 0);
  const sphere = normalizeActiveSphere(nextSphere);
  if (itemID <= 0 || !sphere) return false;
  try {
    const resp = await fetch(apiURL(`items/${encodeURIComponent(String(itemID))}`), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sphere }),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    state.itemSidebarActiveItemID = itemID;
    await loadItemSidebarView(state.itemSidebarView);
    showStatus(`moved to ${sphere}`);
    return true;
  } catch (err) {
    showStatus(`sphere move failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function performItemSidebarWorkspaceUpdate(item, workspaceID = null, workspaceName = '') {
  const itemID = Number(item?.id || 0);
  if (itemID <= 0) return false;
  const body = { workspace_id: workspaceID };
  try {
    const resp = await fetch(apiURL(`items/${encodeURIComponent(String(itemID))}/workspace`), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    state.itemSidebarActiveItemID = itemID;
    await loadItemSidebarView(state.itemSidebarView);
    const warning = String(payload?.warning || '').trim();
    const label = workspaceID ? `workspace set to ${String(workspaceName || '').trim() || 'selected workspace'}` : 'workspace cleared';
    showStatus(warning ? `${label}. ${warning}` : label);
    return true;
  } catch (err) {
    showStatus(`workspace picker failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function performItemSidebarProjectUpdate(item, projectID = '', projectName = '') {
  const itemID = Number(item?.id || 0);
  if (itemID <= 0) return false;
  const body = { project_id: projectID || null };
  try {
    const resp = await fetch(apiURL(`items/${encodeURIComponent(String(itemID))}/project`), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    state.itemSidebarActiveItemID = itemID;
    await loadItemSidebarView(state.itemSidebarView);
    showStatus(projectID ? `project set to ${String(projectName || '').trim() || 'selected project'}` : 'project cleared');
    return true;
  } catch (err) {
    showStatus(`project picker failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function performItemSidebarTriage(item, action, options = {}) {
  const itemID = Number(item?.id || 0);
  if (itemID <= 0) return false;
  const normalizedAction = String(action || '').trim().toLowerCase();
  if (!normalizedAction) return false;
  const body = { action: normalizedAction };
  let actorName = '';
  if (normalizedAction === 'later') {
    body.visible_after = defaultItemSidebarLaterVisibleAfter(options.now || new Date());
  } else if (normalizedAction === 'delegate') {
    const actorID = Number(options.actorID || 0);
    if (actorID <= 0) return false;
    body.actor_id = actorID;
    actorName = String(options.actorName || '').trim();
  }
  try {
    const resp = await fetch(apiURL(`items/${encodeURIComponent(String(itemID))}/triage`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    if (normalizedAction === 'delete') {
      if (state.itemSidebarActiveItemID === itemID) {
        state.itemSidebarActiveItemID = 0;
      }
    } else {
      state.itemSidebarActiveItemID = itemID;
    }
    await loadItemSidebarView(state.itemSidebarView);
    showStatus(itemSidebarStatusText(normalizedAction, item, actorName));
    return true;
  } catch (err) {
    showStatus(`item action failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function performItemSidebarStateUpdate(item, nextState) {
  const itemID = Number(item?.id || 0);
  const normalizedState = normalizeItemSidebarView(nextState);
  if (itemID <= 0 || !normalizedState) return false;
  try {
    const resp = await fetch(apiURL(`items/${encodeURIComponent(String(itemID))}/state`), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: normalizedState }),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    state.itemSidebarActiveItemID = itemID;
    await loadItemSidebarView(state.itemSidebarView);
    showStatus(itemSidebarStatusText(normalizedState, item));
    return true;
  } catch (err) {
    showStatus(`item update failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function showItemSidebarDelegateMenu(item, x, y) {
  try {
    const actors = await fetchItemSidebarActors();
    if (actors.length === 0) {
      showStatus('no actors available');
      return false;
    }
    showItemSidebarMenu(
      actors.map((actor) => ({
        label: actor.name,
        action: 'delegate',
        onClick: () => performItemSidebarTriage(item, 'delegate', {
          actorID: actor.id,
          actorName: actor.name,
        }),
      })),
      x,
      y,
    );
    return true;
  } catch (err) {
    showStatus(`delegate picker failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function showItemSidebarWorkspaceMenu(item, x, y) {
  try {
    const workspaces = await fetchItemSidebarWorkspaces();
    if (workspaces.length === 0) {
      showStatus('no workspaces available');
      return false;
    }
    const currentWorkspaceID = Number(item?.workspace_id || 0);
    const entries = [];
    if (currentWorkspaceID > 0) {
      entries.push({
        label: 'Clear workspace',
        action: 'clear_workspace',
        onClick: () => performItemSidebarWorkspaceUpdate(item, null, ''),
      });
    }
    workspaces.forEach((workspace) => {
      entries.push({
        label: workspace.id === currentWorkspaceID ? `${workspace.name} (current)` : workspace.name,
        action: 'reassign_workspace',
        onClick: () => performItemSidebarWorkspaceUpdate(item, workspace.id, workspace.name),
      });
    });
    showItemSidebarMenu(entries, x, y);
    return true;
  } catch (err) {
    showStatus(`workspace picker failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function showItemSidebarProjectMenu(item, x, y) {
  try {
    const projects = await fetchItemSidebarProjects();
    if (projects.length === 0) {
      showStatus('no projects available');
      return false;
    }
    const currentProjectID = String(item?.project_id || '').trim();
    const entries = [];
    if (currentProjectID) {
      entries.push({
        label: 'Clear project',
        action: 'clear_project',
        onClick: () => performItemSidebarProjectUpdate(item, '', ''),
      });
    }
    projects.forEach((project) => {
      entries.push({
        label: project.id === currentProjectID ? `${project.name} (current)` : project.name,
        action: 'reassign_project',
        onClick: () => performItemSidebarProjectUpdate(item, project.id, project.name),
      });
    });
    showItemSidebarMenu(entries, x, y);
    return true;
  } catch (err) {
    showStatus(`project picker failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export function showItemSidebarActionMenu(item, x, y) {
  const view = normalizeItemSidebarView(state.itemSidebarView);
  const nextSphere = normalizeActiveSphere(item?.sphere) === 'work' ? 'private' : 'work';
  const sphereEntry = Number(item?.workspace_id || 0) > 0
    ? []
    : [{
        label: nextSphere === 'work' ? 'Move to Work' : 'Move to Private',
        action: 'move_sphere',
        onClick: () => performItemSidebarSphereUpdate(item, nextSphere),
      }];
  const entries = view === 'someday'
    ? [
      {
        label: itemSidebarActionLabel('inbox', item),
        action: 'inbox',
        onClick: () => performItemSidebarStateUpdate(item, 'inbox'),
      },
      {
        label: itemSidebarActionLabel('done', item),
        action: 'done',
        onClick: () => performItemSidebarTriage(item, 'done'),
      },
      {
        label: 'Workspace...',
        action: 'workspace',
        onClick: () => showItemSidebarWorkspaceMenu(item, x, y),
      },
      {
        label: 'Project...',
        action: 'project',
        onClick: () => showItemSidebarProjectMenu(item, x, y),
      },
      ...sphereEntry,
      {
        label: itemSidebarActionLabel('delete', item),
        action: 'delete',
        onClick: () => performItemSidebarTriage(item, 'delete'),
      },
    ]
    : [
      {
        label: itemSidebarActionLabel('done', item),
        action: 'done',
        onClick: () => performItemSidebarTriage(item, 'done'),
      },
      {
        label: 'Workspace...',
        action: 'workspace',
        onClick: () => showItemSidebarWorkspaceMenu(item, x, y),
      },
      {
        label: 'Project...',
        action: 'project',
        onClick: () => showItemSidebarProjectMenu(item, x, y),
      },
      ...sphereEntry,
      {
        label: itemSidebarActionLabel('later', item),
        action: 'later',
        onClick: () => performItemSidebarTriage(item, 'later'),
      },
      {
        label: itemSidebarActionLabel('delegate', item),
        action: 'delegate',
        onClick: () => showItemSidebarDelegateMenu(item, x, y),
      },
      {
        label: itemSidebarActionLabel('someday', item),
        action: 'someday',
        onClick: () => performItemSidebarTriage(item, 'someday'),
      },
      {
        label: itemSidebarActionLabel('delete', item),
        action: 'delete',
        onClick: () => performItemSidebarTriage(item, 'delete'),
      },
    ];
  showItemSidebarMenu(entries, x, y);
}

export function parseSidebarArtifactMeta(raw) {
  const text = String(raw || '').trim();
  if (!text) return {};
  try {
    const parsed = JSON.parse(text);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch (_) {
    return {};
  }
}

export function ideaRefinementHeading(entry) {
  const explicit = String(entry?.heading || '').trim();
  if (explicit) return explicit;
  const kind = String(entry?.kind || '').trim().toLowerCase();
  if (kind === 'expand') return 'Expansion';
  if (kind === 'pros_cons') return 'Pros and Cons';
  if (kind === 'alternatives') return 'Alternatives';
  if (kind === 'implementation') return 'Implementation Outline';
  return 'Idea Notes';
}

export function appendIdeaPromotionPreview(detail, preview) {
  const target = String(preview?.target || '').trim().toLowerCase();
  if (!target) return;
  detail.push('', '## Promotion Review', '');
  if (target === 'task') {
    detail.push('- Pending: task draft');
    detail.push('- Confirm with: `create this idea task`');
  } else if (target === 'items') {
    detail.push('- Pending: item proposals');
    detail.push('- Confirm with: `create these idea items` or `create selected idea items 1,2`');
  } else if (target === 'github') {
    detail.push('- Pending: GitHub issue draft');
    detail.push('- Confirm with: `create this idea GitHub issue`');
  }
  detail.push('- Optional: add `and mark this idea done` or `and keep this idea`');
  if (target === 'github') {
    const title = String(preview?.issue?.title || '').trim();
    const body = String(preview?.issue?.body || '').trim();
    if (title) {
      detail.push('', `### ${title}`);
    }
    if (body) {
      detail.push('', body);
    }
    return;
  }
  const candidates = Array.isArray(preview?.candidates) ? preview.candidates : [];
  candidates.forEach((entry, offset) => {
    const title = String(entry?.title || '').trim();
    if (!title) return;
    const index = Number(entry?.index || offset + 1) || (offset + 1);
    detail.push('', `### ${index}. ${title}`);
    const body = String(entry?.details || '').trim();
    if (body) {
      detail.push('', body);
    }
  });
}

export function appendIdeaPromotions(detail, promotions) {
  const records = Array.isArray(promotions) ? promotions : [];
  if (records.length === 0) return;
  detail.push('', '## Promotions');
  records.forEach((entry) => {
    const target = String(entry?.target || '').trim().toLowerCase();
    if (!target) return;
    let label = target;
    if (target === 'github') label = 'GitHub issue';
    let line = `- ${label}`;
    const count = Number(entry?.count || 0);
    if (count > 0) line += ` x${count}`;
    const createdAt = String(entry?.created_at || '').trim();
    if (createdAt) line += ` on ${createdAt}`;
    const refs = Array.isArray(entry?.refs)
      ? entry.refs.map((ref) => String(ref || '').trim()).filter(Boolean)
      : [];
    if (refs.length > 0) line += ` [${refs.join(', ')}]`;
    detail.push(line);
  });
}

export function buildIdeaNoteMarkdown(title, artifactMeta) {
  const noteTitle = String(artifactMeta?.title || title || 'Idea').trim() || 'Idea';
  const notes = Array.isArray(artifactMeta?.notes)
    ? artifactMeta.notes.map((entry) => String(entry || '').trim()).filter(Boolean)
    : [];
  const transcript = String(artifactMeta?.transcript || '').trim();
  if (notes.length === 0 && transcript) {
    notes.push(transcript);
  }
  const detail = [
    `# ${noteTitle}`,
    '',
    '## Notes',
  ];
  if (notes.length > 0) {
    notes.forEach((note) => {
      detail.push(`- ${note}`);
    });
  } else {
    detail.push('- No notes yet.');
  }
  detail.push('', '## Context');
  const captureMode = String(artifactMeta?.capture_mode || '').trim();
  if (captureMode) detail.push(`- Captured: ${captureMode}`);
  const workspace = String(artifactMeta?.workspace || '').trim();
  if (workspace) detail.push(`- Workspace: ${workspace}`);
  const capturedAt = String(artifactMeta?.captured_at || '').trim();
  if (capturedAt) detail.push(`- Date: ${capturedAt}`);
  if (detail[detail.length - 1] === '## Context') {
    detail.push('- Date: unavailable');
  }
  const refinements = Array.isArray(artifactMeta?.refinements) ? artifactMeta.refinements : [];
  refinements.forEach((entry) => {
    const body = String(entry?.body || '').trim();
    if (!body) return;
    detail.push('', `## ${ideaRefinementHeading(entry)}`, '', body);
  });
  appendIdeaPromotionPreview(detail, artifactMeta?.promotion_preview);
  appendIdeaPromotions(detail, artifactMeta?.promotions);
  return detail.join('\n');
}

export function buildSidebarItemFallbackText(item, artifact = null) {
  const artifactMeta = parseSidebarArtifactMeta(artifact?.meta_json || '');
  const title = String(artifact?.title || item?.artifact_title || item?.title || 'Item').trim() || 'Item';
  const artifactKind = String(artifact?.kind || item?.artifact_kind || '').trim().toLowerCase();
  if (artifactKind === 'idea_note') {
    return buildIdeaNoteMarkdown(title, artifactMeta);
  }
  const detail = [
    `# ${title}`,
    '',
    `- Item: ${String(item?.title || title).trim() || title}`,
    `- Kind: ${normalizeDisplayText(artifact?.kind || item?.artifact_kind || 'note') || 'note'}`,
  ];
  const sourceRef = String(item?.source_ref || '').trim();
  if (sourceRef) detail.push(`- Source: ${sourceRef}`);
  const refURL = String(artifact?.ref_url || '').trim();
  if (refURL) detail.push(`- Link: ${refURL}`);
  const body = String(
    artifactMeta.transcript
    || artifactMeta.text
    || artifactMeta.body
    || artifactMeta.summary
    || artifactMeta.content
    || '',
  ).trim();
  if (body) {
    detail.push('', '## Details', '', body);
  }
  return detail.join('\n');
}

export async function openSidebarArtifactItem(item) {
  const artifactID = Number(item?.artifact_id || 0);
  if (artifactID <= 0) {
    applyCanvasArtifactEvent({
      kind: 'text_artifact',
      event_id: `sidebar-item-${Number(item?.id || 0)}-${Date.now()}`,
      title: String(item?.title || 'Item'),
      text: buildSidebarItemFallbackText(item),
    });
    return true;
  }
  const resp = await fetch(apiURL(`artifacts/${encodeURIComponent(String(artifactID))}`), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const artifact = payload?.artifact || {};
  const refPath = String(artifact?.ref_path || '').trim();
  const artifactKind = String(artifact?.kind || item?.artifact_kind || '').trim().toLowerCase();
  if (refPath && !refPath.startsWith('/')) {
    if (artifactKind === 'pdf' || artifactKind === 'pdf_artifact' || refPath.toLowerCase().endsWith('.pdf')) {
      applyCanvasArtifactEvent({
        kind: 'pdf_artifact',
        event_id: `sidebar-item-${artifactID}-${Date.now()}`,
        title: String(artifact?.title || item?.artifact_title || item?.title || refPath),
        path: refPath,
      });
      return true;
    }
    if (SIDEBAR_IMAGE_EXTENSIONS.has(`.${String(refPath.split('.').pop() || '').toLowerCase()}`)) {
      applyCanvasArtifactEvent({
        kind: 'image_artifact',
        event_id: `sidebar-item-${artifactID}-${Date.now()}`,
        title: String(artifact?.title || item?.artifact_title || item?.title || refPath),
        path: refPath,
      });
      return true;
    }
  }
  applyCanvasArtifactEvent({
    kind: 'text_artifact',
    event_id: `sidebar-item-${artifactID}-${Date.now()}`,
    title: String(artifact?.title || item?.artifact_title || item?.title || 'Item'),
    text: buildSidebarItemFallbackText(item, artifact),
  });
  return true;
}
