import * as context from './app-context.js';
import { artifactKindPreferredTool } from './artifact-taxonomy.js';

const {
  refs,
  state,
  TOOL_PALETTE_MODES,
  TOOL_PALETTE_POSITION_STORAGE_KEY,
} = context;

const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const createSelectionAnnotation = (...args) => refs.createSelectionAnnotation(...args);
const isArtifactEditorActive = (...args) => refs.isArtifactEditorActive(...args);
const renderInkControls = (...args) => refs.renderInkControls(...args);
const updateRuntimePreferences = (...args) => refs.updateRuntimePreferences(...args);
const syncInteractionBodyState = (...args) => refs.syncInteractionBodyState(...args);

let toolPaletteBound = false;
let toolPaletteDrag = null;
let toolPaletteResizeBound = false;

export function normalizeInteractionTool(modeRaw) {
  const mode = String(modeRaw || '').trim().toLowerCase();
  if (mode === 'highlight' || mode === 'select') return 'highlight';
  if (mode === 'ink' || mode === 'draw' || mode === 'pen') return 'ink';
  if (mode === 'text_note' || mode === 'text-note' || mode === 'text' || mode === 'note' || mode === 'keyboard' || mode === 'typing') return 'text_note';
  if (mode === 'prompt' || mode === 'voice' || mode === 'talk' || mode === 'mic') return 'prompt';
  return 'pointer';
}

export function normalizeInteractionSurface(modeRaw) {
  return String(modeRaw || '').trim().toLowerCase() === 'editor' ? 'editor' : 'annotate';
}

function customToolPalettePosition(raw) {
  if (!raw || typeof raw !== 'object') return null;
  const x = Number(raw.x);
  const y = Number(raw.y);
  if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
  return { x: Math.round(x), y: Math.round(y) };
}

function readToolPalettePosition() {
  try {
    return customToolPalettePosition(JSON.parse(window.localStorage.getItem(TOOL_PALETTE_POSITION_STORAGE_KEY) || 'null'));
  } catch (_) {
    return null;
  }
}

function persistToolPalettePosition(position) {
  try {
    if (!position) {
      window.localStorage.removeItem(TOOL_PALETTE_POSITION_STORAGE_KEY);
      return;
    }
    window.localStorage.setItem(TOOL_PALETTE_POSITION_STORAGE_KEY, JSON.stringify(position));
  } catch (_) {}
}

function clampToolPalettePosition(host, position) {
  const next = customToolPalettePosition(position);
  if (!(host instanceof HTMLElement) || !next) return null;
  const rect = host.getBoundingClientRect();
  const margin = 12;
  const width = Math.max(1, Math.round(rect.width || host.offsetWidth || 0));
  const height = Math.max(1, Math.round(rect.height || host.offsetHeight || 0));
  return {
    x: Math.min(Math.max(margin, next.x), Math.max(margin, window.innerWidth - width - margin)),
    y: Math.min(Math.max(margin, next.y), Math.max(margin, window.innerHeight - height - margin)),
  };
}

function resetToolPalettePosition(host) {
  if (!(host instanceof HTMLElement)) return;
  host.style.removeProperty('left');
  host.style.removeProperty('top');
  host.style.removeProperty('right');
  host.style.removeProperty('bottom');
  host.style.removeProperty('transform');
}

function applyToolPalettePosition(host, position) {
  if (!(host instanceof HTMLElement)) return null;
  const clamped = clampToolPalettePosition(host, position);
  if (!clamped) {
    state.toolPalettePosition = null;
    resetToolPalettePosition(host);
    return null;
  }
  host.style.left = `${clamped.x}px`;
  host.style.top = `${clamped.y}px`;
  host.style.right = 'auto';
  host.style.bottom = 'auto';
  host.style.transform = 'none';
  state.toolPalettePosition = clamped;
  return clamped;
}

function ensureToolPalettePosition(host) {
  if (!(host instanceof HTMLElement)) return;
  if (!state.toolPalettePosition) {
    state.toolPalettePosition = readToolPalettePosition();
  }
  if (state.toolPalettePosition) {
    const clamped = applyToolPalettePosition(host, state.toolPalettePosition);
    if (clamped) {
      persistToolPalettePosition(clamped);
      return;
    }
  }
  resetToolPalettePosition(host);
}

export function initToolPalette() {
  const host = document.getElementById('tool-palette');
  if (!(host instanceof HTMLElement)) return;
  ensureToolPalettePosition(host);
  if (toolPaletteBound) return;
  toolPaletteBound = true;

  host.addEventListener('pointerdown', (ev) => {
    if (!(ev.target instanceof Element) || ev.target.closest('.tool-palette-btn')) return;
    if (ev.button !== 0) return;
    const rect = host.getBoundingClientRect();
    toolPaletteDrag = {
      pointerId: ev.pointerId,
      offsetX: ev.clientX - rect.left,
      offsetY: ev.clientY - rect.top,
    };
    host.classList.add('is-dragging');
    try { host.setPointerCapture(ev.pointerId); } catch (_) {}
    ev.preventDefault();
  });

  const finishDrag = (ev) => {
    if (!toolPaletteDrag || toolPaletteDrag.pointerId !== ev.pointerId) return;
    const clamped = applyToolPalettePosition(host, {
      x: ev.clientX - toolPaletteDrag.offsetX,
      y: ev.clientY - toolPaletteDrag.offsetY,
    });
    persistToolPalettePosition(clamped);
    toolPaletteDrag = null;
    host.classList.remove('is-dragging');
    try { host.releasePointerCapture(ev.pointerId); } catch (_) {}
  };

  host.addEventListener('pointermove', (ev) => {
    if (!toolPaletteDrag || toolPaletteDrag.pointerId !== ev.pointerId) return;
    applyToolPalettePosition(host, {
      x: ev.clientX - toolPaletteDrag.offsetX,
      y: ev.clientY - toolPaletteDrag.offsetY,
    });
    ev.preventDefault();
  });
  host.addEventListener('pointerup', finishDrag);
  host.addEventListener('pointercancel', finishDrag);

  if (toolPaletteResizeBound) return;
  toolPaletteResizeBound = true;
  window.addEventListener('resize', () => {
    const palette = document.getElementById('tool-palette');
    if (!(palette instanceof HTMLElement) || !state.toolPalettePosition) return;
    const clamped = applyToolPalettePosition(palette, state.toolPalettePosition);
    persistToolPalettePosition(clamped);
  });
}

export function interactionConversationMode({
  surface = state.interaction.surface,
  tool = state.interaction.tool,
} = {}) {
  if (state.liveSessionActive
    && (state.liveSessionMode === 'dialogue' || state.liveSessionMode === 'meeting')) {
    return 'continuous_dialogue';
  }
  if (surface === 'annotate' && (tool === 'pointer' || tool === 'prompt')) {
    return 'push_to_talk';
  }
  return 'idle';
}

export function isInkTool() {
  return state.interaction.tool === 'ink';
}

export function prefersTextComposer() {
  return state.interaction.surface === 'editor' || state.interaction.tool === 'text_note';
}

export function currentCanvasPaneId() {
  const pane = document.querySelector('#canvas-viewport .canvas-pane.is-active');
  return pane instanceof HTMLElement ? pane.id : '';
}

export function canToggleInteractionSurface() {
  return state.hasArtifact && currentCanvasPaneId() === 'canvas-text' && !state.prReviewMode;
}

function interactionToolDefaultForCurrentArtifact(paneId) {
  const descriptor: Record<string, any> = state.currentCanvasArtifact || {};
  const artifactKind = String(descriptor.artifactKind || '').trim().toLowerCase();
  if (artifactKind) return artifactKindPreferredTool(artifactKind);
  if (paneId === 'canvas-pdf' || paneId === 'canvas-image') return 'highlight';
  return 'pointer';
}

function shouldAnnotateTextArtifactByDefault() {
  if (state.prReviewMode) return true;
  const descriptor: Record<string, any> = state.currentCanvasArtifact || {};
  if (descriptor.surfaceDefault === 'annotate') return true;
  if (interactionToolDefaultForCurrentArtifact('canvas-text') !== 'pointer') return true;
  const title = String(descriptor.title || '').trim().toLowerCase();
  return title.startsWith('.tabura/artifacts/pr/') && title.endsWith('.diff');
}

export function interactionSurfaceDefaultForPane(paneId) {
  if (paneId !== 'canvas-text') return 'annotate';
  if (shouldAnnotateTextArtifactByDefault()) return 'annotate';
  return 'editor';
}

export function applyInteractionDefaultsForPane(paneId) {
  const nextSurface = interactionSurfaceDefaultForPane(paneId);
  state.interaction.surface = nextSurface;
  state.interaction.tool = interactionToolDefaultForCurrentArtifact(paneId);
  state.interaction.conversation = interactionConversationMode({
    surface: nextSurface,
    tool: state.interaction.tool,
  });
  if (nextSurface !== 'annotate') {
    clearInkDraft();
  }
  syncInteractionBodyState();
  renderInkControls();
  renderInteractionSurfaceToggle();
  renderToolPalette();
}

export function setInteractionSurface(surface) {
  const nextSurface = normalizeInteractionSurface(surface);
  if (state.interaction.surface === nextSurface) return;
  state.interaction.surface = nextSurface;
  state.interaction.conversation = interactionConversationMode({
    surface: nextSurface,
    tool: state.interaction.tool,
  });
  if (nextSurface !== 'annotate') {
    clearInkDraft();
  }
  syncInteractionBodyState();
  renderInkControls();
  renderInteractionSurfaceToggle();
  renderToolPalette();
}

export function renderInteractionSurfaceToggle() {
  const host = document.getElementById('surface-toggle');
  if (!(host instanceof HTMLButtonElement)) return;
  if (!canToggleInteractionSurface()) {
    host.style.display = 'none';
    return;
  }
  host.style.display = '';
  const nextSurface = state.interaction.surface === 'editor' ? 'annotate' : 'editor';
  host.dataset.surface = state.interaction.surface;
  host.textContent = nextSurface === 'annotate' ? 'Annotate' : 'Editor';
  host.setAttribute('aria-label', `Switch to ${nextSurface}`);
  host.setAttribute('title', `Switch to ${nextSurface}`);
  host.setAttribute('aria-pressed', state.interaction.surface === 'annotate' ? 'true' : 'false');
}

export async function selectInteractionTool(tool) {
  const nextTool = normalizeInteractionTool(tool);
  if (state.interaction.surface !== 'annotate') {
    state.interaction.surface = 'annotate';
  }
  await updateRuntimePreferences({ tool: nextTool });
}

export function setInteractionToolLocal(tool) {
  const nextTool = normalizeInteractionTool(tool);
  if (state.interaction.tool === nextTool && state.interaction.conversation === interactionConversationMode({
    surface: state.interaction.surface,
    tool: nextTool,
  })) {
    return;
  }
  state.interaction.tool = nextTool;
  state.interaction.conversation = interactionConversationMode({
    surface: state.interaction.surface,
    tool: nextTool,
  });
  if (nextTool !== 'ink') {
    clearInkDraft();
  }
  syncInteractionBodyState();
  renderInkControls();
  renderToolPalette();
}

export function renderToolPalette() {
  const host = document.getElementById('tool-palette');
  if (!(host instanceof HTMLElement)) return;
  initToolPalette();
  if (state.interaction.surface !== 'annotate') {
    host.replaceChildren();
    host.style.display = 'none';
    return;
  }
  ensureToolPalettePosition(host);
  host.style.display = '';
  host.replaceChildren();
  const disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  for (const mode of TOOL_PALETTE_MODES) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'tool-palette-btn';
    button.dataset.mode = mode.id;
    button.setAttribute('aria-label', mode.label);
    button.setAttribute('title', mode.label);
    button.setAttribute('aria-pressed', state.interaction.tool === mode.id ? 'true' : 'false');
    if (state.interaction.tool === mode.id) {
      button.classList.add('is-active');
    }
    button.disabled = disabled;
    button.innerHTML = mode.icon;
    button.addEventListener('click', () => {
      updateRuntimePreferences({ tool: mode.id })
        .then(() => {
          if (mode.id !== 'ink') {
            clearInkDraft();
          }
          renderInkControls();
          refs.showStatus(`${mode.id.replace('_', ' ')} tool on`);
        })
        .catch((err) => {
          refs.showStatus(`tool switch failed: ${String(err?.message || err || 'unknown error')}`);
        });
    });
    host.appendChild(button);
  }
}

export function maybeApplySelectionHighlight() {
  if (isArtifactEditorActive()) return false;
  if (state.interaction.surface !== 'annotate' || state.interaction.tool !== 'highlight') return false;
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return false;
  return createSelectionAnnotation();
}
