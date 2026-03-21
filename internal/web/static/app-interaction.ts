import * as context from './app-context.js';
import { artifactKindPreferredTool } from './artifact-taxonomy.js';

const { refs, state } = context;

const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const createSelectionAnnotation = (...args) => refs.createSelectionAnnotation(...args);
const isArtifactEditorActive = (...args) => refs.isArtifactEditorActive(...args);
const renderInkControls = (...args) => refs.renderInkControls(...args);
const updateRuntimePreferences = (...args) => refs.updateRuntimePreferences(...args);
const syncInteractionBodyState = (...args) => refs.syncInteractionBodyState(...args);
const renderTaburaCircle = (...args) => refs.renderTaburaCircle(...args);

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

export function initToolPalette() {
  renderTaburaCircle();
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
  const defaultTool = interactionToolDefaultForCurrentArtifact(paneId);
  state.interaction.surface = nextSurface;
  if (!state.interaction.toolPinned) {
    state.interaction.tool = defaultTool;
  }
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
    state.interaction.toolPinned = true;
    return;
  }
  state.interaction.tool = nextTool;
  state.interaction.toolPinned = true;
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
  initToolPalette();
  renderTaburaCircle();
}

export function maybeApplySelectionHighlight() {
  if (isArtifactEditorActive()) return false;
  if (state.interaction.surface !== 'annotate' || state.interaction.tool !== 'highlight') return false;
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return false;
  return createSelectionAnnotation();
}
