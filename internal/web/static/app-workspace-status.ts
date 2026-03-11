import { refs, state } from './app-context.js';

function renderWorkspaceStatus() {
  refs.renderEdgeTopModelButtons?.();
}

function workspaceLabelFromPath(dirPath, fallbackID = 0) {
  const cleanPath = String(dirPath || '').trim().replace(/\/+$/, '');
  if (cleanPath) {
    const parts = cleanPath.split('/').filter(Boolean);
    if (parts.length > 0) {
      return decodeURIComponent(parts[parts.length - 1]);
    }
  }
  return fallbackID > 0 ? `Workspace ${fallbackID}` : 'Workspace';
}

function normalizeWorkspaceRef(workspace) {
  if (!workspace || typeof workspace !== 'object') return null;
  const id = Math.max(0, Number(workspace?.id || 0) || 0);
  const dirPath = String(workspace?.dir_path || '').trim();
  const name = String(workspace?.name || '').trim() || workspaceLabelFromPath(dirPath, id);
  if (!id && !name && !dirPath) return null;
  return {
    id,
    name,
    dir_path: dirPath,
    is_daily: Boolean(workspace?.is_daily),
    sphere: String(workspace?.sphere || '').trim().toLowerCase(),
  };
}

export function workspaceDisplayName(workspace) {
  const normalized = normalizeWorkspaceRef(workspace);
  return normalized?.name || 'Workspace';
}

export function normalizeWorkspaceFocusSnapshot(payload) {
  const source = payload && typeof payload === 'object' ? payload : {};
  const anchor = normalizeWorkspaceRef(source?.anchor);
  const focus = normalizeWorkspaceRef(source?.focus) || anchor;
  return {
    anchor,
    focus,
    explicit: Boolean(source?.explicit) && Boolean(focus),
  };
}

function normalizeWorkspaceBusyState(runState) {
  const source = runState && typeof runState === 'object' ? runState : {};
  const workspaceID = Math.max(0, Number(source?.workspace_id || 0) || 0);
  const dirPath = String(source?.dir_path || '').trim();
  const name = String(source?.workspace_name || '').trim() || workspaceLabelFromPath(dirPath, workspaceID);
  const activeTurns = Math.max(0, Number(source?.active_turns || 0) || 0);
  const queuedTurns = Math.max(0, Number(source?.queued_turns || 0) || 0);
  let status = String(source?.status || '').trim().toLowerCase();
  if (status !== 'running' && status !== 'queued' && status !== 'idle') {
    status = activeTurns > 0 ? 'running' : (queuedTurns > 0 ? 'queued' : 'idle');
  }
  return {
    workspace_id: workspaceID,
    workspace_name: name,
    dir_path: dirPath,
    is_daily: Boolean(source?.is_daily),
    is_anchor: Boolean(source?.is_anchor),
    is_focused: Boolean(source?.is_focused),
    active_turns: activeTurns,
    queued_turns: queuedTurns,
    status,
  };
}

export function normalizeWorkspaceBusyStates(states) {
  const source = Array.isArray(states) ? states : [];
  return source.map((entry) => normalizeWorkspaceBusyState(entry));
}

function workspaceBusySummary(stateEntry) {
  const stateSummary = normalizeWorkspaceBusyState(stateEntry);
  if (stateSummary.status === 'running') {
    const details = [];
    if (stateSummary.active_turns > 0) {
      details.push(`${stateSummary.active_turns} active`);
    }
    if (stateSummary.queued_turns > 0) {
      details.push(`${stateSummary.queued_turns} queued`);
    }
    return details.length > 0 ? `running (${details.join(', ')})` : 'running';
  }
  if (stateSummary.status === 'queued') {
    return stateSummary.queued_turns > 0 ? `queued (${stateSummary.queued_turns} queued)` : 'queued';
  }
  return 'idle';
}

function workspaceBusyLabel(stateEntry) {
  const stateSummary = normalizeWorkspaceBusyState(stateEntry);
  const tags = [];
  if (stateSummary.is_daily) tags.push('daily');
  if (stateSummary.is_anchor) tags.push('anchor');
  if (stateSummary.is_focused && !stateSummary.is_anchor) tags.push('focus');
  if (tags.length === 0) return stateSummary.workspace_name;
  return `${stateSummary.workspace_name} (${tags.join(', ')})`;
}

export function workspaceBusyBadgeText(states) {
  const normalized = normalizeWorkspaceBusyStates(states);
  const active = normalized.filter((entry) => entry.status !== 'idle');
  if (active.length === 0) {
    return 'Busy idle';
  }
  if (active.length === 1) {
    return `Busy ${active[0].workspace_name} ${active[0].status}`;
  }
  return `Busy ${active.length} active`;
}

export function workspaceBusyBadgeTitle(snapshot, states) {
  const focusSnapshot = normalizeWorkspaceFocusSnapshot(snapshot);
  const lines = [];
  if (focusSnapshot.anchor) {
    const anchorPath = String(focusSnapshot.anchor?.dir_path || '').trim();
    lines.push(anchorPath
      ? `Anchor: ${workspaceDisplayName(focusSnapshot.anchor)} (${anchorPath})`
      : `Anchor: ${workspaceDisplayName(focusSnapshot.anchor)}`);
  }
  if (focusSnapshot.focus) {
    const focusLabel = focusSnapshot.explicit
      ? workspaceDisplayName(focusSnapshot.focus)
      : `${workspaceDisplayName(focusSnapshot.focus)} (follows anchor)`;
    const focusPath = String(focusSnapshot.focus?.dir_path || '').trim();
    lines.push(focusPath ? `Focus: ${focusLabel} (${focusPath})` : `Focus: ${focusLabel}`);
  }
  const normalizedStates = normalizeWorkspaceBusyStates(states);
  if (normalizedStates.length === 0) {
    lines.push('Busy: idle');
  } else {
    for (const entry of normalizedStates) {
      lines.push(`${workspaceBusyLabel(entry)}: ${workspaceBusySummary(entry)}`);
    }
  }
  return lines.join('\n');
}

export function applyWorkspaceFocusSnapshot(payload) {
  state.workspaceFocus = normalizeWorkspaceFocusSnapshot(payload);
  renderWorkspaceStatus();
}

export function applyWorkspaceBusyStates(states) {
  state.workspaceBusyStates = normalizeWorkspaceBusyStates(states);
  renderWorkspaceStatus();
}
