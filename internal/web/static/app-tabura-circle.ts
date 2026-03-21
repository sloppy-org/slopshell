import { appURL } from './paths.js';
import { refs, state } from './app-context.js';

const TOOL_GLYPHS: Record<string, string> = {
  pointer: '↗',
  highlight: '▰',
  ink: '✒',
  text_note: '▣',
  prompt: '●',
};

let circleBound = false;
let circleExpanded = false;
let longPressTimer: number | null = null;
let longPressTriggered = false;

const selectInteractionTool = (...args) => refs.selectInteractionTool(...args);
const activateLiveSession = (...args) => refs.activateLiveSession(...args);
const deactivateLiveSession = (...args) => refs.deactivateLiveSession(...args);
const toggleTTSSilentMode = (...args) => refs.toggleTTSSilentMode(...args);
const handleStopAction = (...args) => refs.handleStopAction(...args);
const showStatus = (...args) => refs.showStatus(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);

function circleRoot() {
  const node = document.getElementById('tabura-circle');
  return node instanceof HTMLElement ? node : null;
}

function circleDot() {
  const node = document.getElementById('tabura-circle-dot');
  return node instanceof HTMLButtonElement ? node : null;
}

function circleMenu() {
  const node = document.getElementById('tabura-circle-menu');
  return node instanceof HTMLElement ? node : null;
}

function currentSession() {
  if (!state.liveSessionActive) return 'none';
  const mode = String(state.liveSessionMode || '').trim().toLowerCase();
  return mode === 'meeting' ? 'meeting' : 'dialogue';
}

function clearLongPressTimer() {
  if (longPressTimer !== null) {
    window.clearTimeout(longPressTimer);
    longPressTimer = null;
  }
}

function setCircleExpanded(next: boolean) {
  circleExpanded = Boolean(next);
  document.body.classList.toggle('tabura-circle-expanded', circleExpanded);
  renderTaburaCircle();
}

function toggleCircleExpanded() {
  setCircleExpanded(!circleExpanded);
}

export function collapseTaburaCircle() {
  if (!circleExpanded) return;
  setCircleExpanded(false);
}

function handleOutsideCircleClick(event: MouseEvent) {
  if (!circleExpanded) return;
  const root = circleRoot();
  const target = event.target;
  if (!(root instanceof HTMLElement) || !(target instanceof Node)) return;
  if (root.contains(target)) return;
  collapseTaburaCircle();
}

async function selectCircleTool(tool: string) {
  try {
    await selectInteractionTool(tool);
    showStatus(`${String(tool || '').replace('_', ' ')} tool on`);
  } catch (err) {
    showStatus(`tool switch failed: ${String(err?.message || err || 'unknown error')}`);
  }
}

async function selectCircleSession(session: string) {
  const next = String(session || '').trim().toLowerCase();
  if (next !== 'dialogue' && next !== 'meeting') return;
  if (!state.activeWorkspaceId) return;
  try {
    if (state.liveSessionActive && currentSession() === next) {
      await deactivateLiveSession({ disableMeetingConfig: true });
      return;
    }
    const started = await activateLiveSession(next);
    if (started) {
      showStatus(next === 'meeting' ? 'live meeting on' : 'live dialogue on');
    }
  } catch (err) {
    const message = String(err?.message || err || `live ${next} failed`);
    if (next === 'meeting' && typeof appendPlainMessage === 'function') {
      appendPlainMessage('system', `Live meeting failed: ${message}`);
    }
    showStatus(`live ${next} failed: ${message}`);
  }
}

function onSegmentClick(event: Event) {
  const target = event.target;
  if (!(target instanceof HTMLElement)) return;
  const segment = target.closest('.tabura-circle-segment');
  if (!(segment instanceof HTMLButtonElement)) return;
  const kind = String(segment.dataset.kind || '').trim().toLowerCase();
  const name = String(segment.dataset.segment || '').trim().toLowerCase();
  if (kind === 'tool') {
    void selectCircleTool(name);
    return;
  }
  if (kind === 'session') {
    void selectCircleSession(name);
    return;
  }
  if (kind === 'toggle') {
    toggleTTSSilentMode();
  }
}

function scheduleLongPressManage() {
  clearLongPressTimer();
  longPressTriggered = false;
  longPressTimer = window.setTimeout(() => {
    longPressTimer = null;
    longPressTriggered = true;
    openManagementPage();
  }, 460);
}

function cancelLongPressManage() {
  clearLongPressTimer();
}

export function initTaburaCircle() {
  const root = circleRoot();
  const dot = circleDot();
  const menu = circleMenu();
  if (!(root instanceof HTMLElement) || !(dot instanceof HTMLButtonElement) || !(menu instanceof HTMLElement)) return;
  if (circleBound) return;
  circleBound = true;

  dot.addEventListener('click', (event) => {
    if (longPressTriggered) {
      longPressTriggered = false;
      event.preventDefault();
      return;
    }
    event.preventDefault();
    toggleCircleExpanded();
  });
  dot.addEventListener('contextmenu', (event) => {
    event.preventDefault();
    openManagementPage();
  });
  dot.addEventListener('pointerdown', () => {
    scheduleLongPressManage();
  });
  dot.addEventListener('pointerup', cancelLongPressManage);
  dot.addEventListener('pointercancel', cancelLongPressManage);
  dot.addEventListener('pointerleave', cancelLongPressManage);

  menu.addEventListener('click', onSegmentClick);
  document.addEventListener('click', handleOutsideCircleClick, true);
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if (circleExpanded) {
      event.preventDefault();
      collapseTaburaCircle();
      return;
    }
    if (state.liveSessionActive) {
      event.preventDefault();
      void handleStopAction();
    }
  });
}

export function renderTaburaCircle() {
  const root = circleRoot();
  const dot = circleDot();
  const menu = circleMenu();
  if (!(root instanceof HTMLElement) || !(dot instanceof HTMLButtonElement) || !(menu instanceof HTMLElement)) return;
  initTaburaCircle();

  root.classList.toggle('is-expanded', circleExpanded);
  root.classList.toggle('is-collapsed', !circleExpanded);
  root.dataset.state = circleExpanded ? 'expanded' : 'collapsed';

  const tool = String(state.interaction.tool || 'pointer').trim().toLowerCase() || 'pointer';
  const session = currentSession();
  dot.dataset.tool = tool;
  dot.dataset.session = session;
  dot.dataset.silent = String(Boolean(state.ttsSilent));
  dot.textContent = TOOL_GLYPHS[tool] || TOOL_GLYPHS.pointer;
  dot.title = circleExpanded ? 'Close Tabura Circle' : 'Open Tabura Circle';
  dot.setAttribute('aria-expanded', circleExpanded ? 'true' : 'false');

  const segments = menu.querySelectorAll('.tabura-circle-segment');
  const disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  segments.forEach((node) => {
    if (!(node instanceof HTMLButtonElement)) return;
    const kind = String(node.dataset.kind || '').trim().toLowerCase();
    const name = String(node.dataset.segment || '').trim().toLowerCase();
    if (kind === 'tool') {
      node.setAttribute('aria-pressed', String(name === tool));
      node.disabled = disabled;
      return;
    }
    if (kind === 'session') {
      node.setAttribute('aria-pressed', String(name === session));
      node.disabled = disabled || !state.activeWorkspaceId;
      return;
    }
    node.setAttribute('aria-pressed', String(Boolean(state.ttsSilent)));
    node.disabled = !state.ttsEnabled || disabled;
  });
}

export function openManagementPage(path = 'manage') {
  const targetURL = appURL(String(path || 'manage'));
  if (state.liveSessionActive) {
    window.open(targetURL, '_blank', 'noopener');
    return;
  }
  window.location.assign(targetURL);
}
