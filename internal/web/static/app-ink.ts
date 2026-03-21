import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromPoint, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const renderEdgeTopModelButtons = (...args) => refs.renderEdgeTopModelButtons(...args);
const renderEdgeTopProjects = (...args) => refs.renderEdgeTopProjects(...args);
const openWorkspaceSidebarFile = (...args) => refs.openWorkspaceSidebarFile(...args);
const activeProject = (...args) => refs.activeProject(...args);
const normalizeProjectRunState = (...args) => refs.normalizeProjectRunState(...args);
const isInkTool = (...args) => refs.isInkTool(...args);
const pdfPageAnchorAtPoint = (...args) => refs.pdfPageAnchorAtPoint(...args);
const persistPdfInkAnnotation = (...args) => refs.persistPdfInkAnnotation(...args);

const INK_STROKE_COLOR = '#111827';
const PDF_INK_STROKE_COLOR = '#0f172a';
const INK_POINT_EPSILON = 0.25;
const INK_TIME_EPSILON_MS = 0.25;
const INK_PREDICTION_STEPS = 2;
const INK_PREDICTION_FRAME_MS = 8;

let delegatedInkPresenter = null;
let delegatedInkPresenterPromise = null;

export function activeArtifactKindForInk() {
  const activePane = document.querySelector('#canvas-viewport .canvas-pane.is-active');
  if (!(activePane instanceof HTMLElement)) return 'text';
  if (activePane.id === 'canvas-pdf') return 'pdf';
  if (activePane.id === 'canvas-image') return 'image';
  return 'text';
}

export function resetInkDraftState() {
  state.inkDraft.activePointerId = null;
  state.inkDraft.activePointerType = '';
  state.inkDraft.activePath = null;
  state.inkDraft.target = '';
  state.inkDraft.page = 0;
  state.inkDraft.pageInner = null;
  state.inkDraft.pageWidth = 0;
  state.inkDraft.pageHeight = 0;
  state.inkDraft.draftLayer = null;
}

export function inkLayerEl() {
  const node = document.getElementById('ink-layer');
  return node instanceof HTMLCanvasElement ? node : null;
}

export function renderInkControls() {
  const controls = document.getElementById('ink-controls');
  if (!(controls instanceof HTMLElement)) return;
  const visible = state.interaction.surface === 'annotate' && isInkTool() && state.inkDraft.dirty && state.inkDraft.target !== 'pdf';
  controls.style.display = visible ? '' : 'none';
  document.body.classList.toggle('ink-controls-visible', visible);
  const submit = document.getElementById('ink-submit');
  const clear = document.getElementById('ink-clear');
  if (submit instanceof HTMLButtonElement) submit.disabled = state.inkSubmitInFlight;
  if (clear instanceof HTMLButtonElement) clear.disabled = state.inkSubmitInFlight;
}

export function syncInteractionBodyState() {
  const tool = String(state.interaction.tool || 'pointer').trim().toLowerCase() || 'pointer';
  document.body.classList.toggle('tool-pointer', tool === 'pointer');
  document.body.classList.toggle('tool-highlight', tool === 'highlight');
  document.body.classList.toggle('tool-ink', tool === 'ink' && state.interaction.surface === 'annotate');
  document.body.classList.toggle('tool-text-note', tool === 'text_note');
  document.body.classList.toggle('tool-prompt', tool === 'prompt');
  document.body.classList.toggle('surface-editor', state.interaction.surface === 'editor');
  document.body.classList.toggle('surface-annotate', state.interaction.surface === 'annotate');
  document.body.classList.toggle('session-dialogue', state.liveSessionActive && state.liveSessionMode === 'dialogue');
  document.body.classList.toggle('session-meeting', state.liveSessionActive && state.liveSessionMode === 'meeting');
  document.body.classList.toggle('session-none', !state.liveSessionActive);
  document.body.classList.toggle('silent-on', Boolean(state.ttsSilent));
  document.body.classList.toggle('silent-off', !state.ttsSilent);
}

export function setPenInkingState(active) {
  document.body.classList.toggle('pen-inking', Boolean(active));
}

export function clearInkDraft() {
  if (state.inkDraft.draftLayer instanceof HTMLElement) {
    state.inkDraft.draftLayer.remove();
  }
  const layer = inkLayerEl();
  clearCanvasLayer(layer);
  state.inkDraft.strokes = [];
  state.inkDraft.dirty = false;
  resetInkDraftState();
  setPenInkingState(false);
  renderInkControls();
}

export function syncInkLayerSize() {
  const layer = inkLayerEl();
  const viewport = document.getElementById('canvas-viewport');
  if (!(layer instanceof HTMLCanvasElement) || !(viewport instanceof HTMLElement)) return;
  const rect = viewport.getBoundingClientRect();
  const width = Math.max(1, Math.round(rect.width));
  const height = Math.max(1, Math.round(rect.height));
  const changed = syncCanvasSize(layer, width, height);
  if (changed) {
    redrawMainInkLayer();
  }
  void ensureDelegatedInkPresenter(layer);
}

export function pointForViewportEvent(clientX, clientY) {
  const viewport = document.getElementById('canvas-viewport');
  if (!(viewport instanceof HTMLElement)) {
    return { x: clientX, y: clientY };
  }
  const rect = viewport.getBoundingClientRect();
  return {
    x: clientX - rect.left + viewport.scrollLeft,
    y: clientY - rect.top + viewport.scrollTop,
  };
}

function syncCanvasSize(canvas, width, height) {
  if (!(canvas instanceof HTMLCanvasElement)) return false;
  const logicalWidth = Math.max(1, Math.round(width));
  const logicalHeight = Math.max(1, Math.round(height));
  const dpr = Math.max(1, Number(window.devicePixelRatio) || 1);
  const pixelWidth = Math.max(1, Math.round(logicalWidth * dpr));
  const pixelHeight = Math.max(1, Math.round(logicalHeight * dpr));
  const changed = canvas.width !== pixelWidth || canvas.height !== pixelHeight;
  canvas.dataset.logicalWidth = `${logicalWidth}`;
  canvas.dataset.logicalHeight = `${logicalHeight}`;
  canvas.dataset.dpr = `${dpr}`;
  canvas.style.width = `${logicalWidth}px`;
  canvas.style.height = `${logicalHeight}px`;
  if (changed) {
    canvas.width = pixelWidth;
    canvas.height = pixelHeight;
  }
  const ctx = getCanvas2DContext(canvas);
  if (ctx) {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.lineCap = 'round';
    ctx.lineJoin = 'round';
  }
  return changed;
}

function getCanvasLogicalSize(canvas) {
  if (!(canvas instanceof HTMLCanvasElement)) return { width: 1, height: 1 };
  return {
    width: Math.max(1, Number(canvas.dataset.logicalWidth) || canvas.clientWidth || 1),
    height: Math.max(1, Number(canvas.dataset.logicalHeight) || canvas.clientHeight || 1),
  };
}

function getCanvas2DContext(canvas) {
  if (!(canvas instanceof HTMLCanvasElement)) return null;
  let ctx = null;
  try {
    ctx = canvas.getContext('2d', { desynchronized: true });
  } catch (_) {
    ctx = null;
  }
  if (!(ctx instanceof CanvasRenderingContext2D)) {
    ctx = canvas.getContext('2d');
  }
  return ctx instanceof CanvasRenderingContext2D ? ctx : null;
}

function clearCanvasLayer(canvas) {
  if (!(canvas instanceof HTMLCanvasElement)) return;
  const ctx = getCanvas2DContext(canvas);
  if (!ctx) return;
  ctx.save();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.restore();
}

function buildInkPathData(points) {
  if (!Array.isArray(points) || points.length === 0) return '';
  const commands = points.map((point, index) => `${index === 0 ? 'M' : 'L'} ${Number(point?.x || 0).toFixed(2)} ${Number(point?.y || 0).toFixed(2)}`);
  if (points.length === 1) {
    commands.push(`L ${(Number(points[0]?.x || 0) + 0.01).toFixed(2)} ${Number(points[0]?.y || 0).toFixed(2)}`);
  }
  return commands.join(' ');
}

function strokeWidthForPressure(pressure) {
  return Math.max(1.5, Number(pressure) > 0 ? 1.8 + Number(pressure) * 2.8 : 2.4);
}

function normalizePointerTimestamp(pointerEvent) {
  const value = Number(pointerEvent?.timeStamp);
  if (Number.isFinite(value) && value >= 0) return Number(value.toFixed(2));
  return Date.now();
}

function createInkPoint(x, y, pointerEvent) {
  return {
    x: Number(x) || 0,
    y: Number(y) || 0,
    pressure: Number(pointerEvent?.pressure) || 0,
    tilt_x: Number(pointerEvent?.tiltX) || 0,
    tilt_y: Number(pointerEvent?.tiltY) || 0,
    roll: Number(pointerEvent?.twist) || 0,
    timestamp_ms: normalizePointerTimestamp(pointerEvent),
  };
}

function collectPointerSamples(pointerEvent) {
  const samples = [];
  if (pointerEvent && typeof pointerEvent.getCoalescedEvents === 'function') {
    try {
      const coalesced = pointerEvent.getCoalescedEvents();
      if (Array.isArray(coalesced) && coalesced.length > 0) {
        samples.push(...coalesced);
      }
    } catch (_) {}
  }
  samples.push(pointerEvent);
  return samples.filter(Boolean);
}

function pointerEventToInkPoint(pointerEvent) {
  let point = pointForViewportEvent(pointerEvent.clientX, pointerEvent.clientY);
  if (state.inkDraft.target === 'pdf' && state.inkDraft.pageInner instanceof HTMLElement) {
    const bounds = state.inkDraft.pageInner.getBoundingClientRect();
    point = {
      x: clampPoint(pointerEvent.clientX - bounds.left, state.inkDraft.pageWidth),
      y: clampPoint(pointerEvent.clientY - bounds.top, state.inkDraft.pageHeight),
    };
  }
  return createInkPoint(point.x, point.y, pointerEvent);
}

function sameInkPoint(a, b) {
  if (!a || !b) return false;
  return Math.abs((Number(a.x) || 0) - (Number(b.x) || 0)) <= INK_POINT_EPSILON
    && Math.abs((Number(a.y) || 0) - (Number(b.y) || 0)) <= INK_POINT_EPSILON
    && Math.abs((Number(a.pressure) || 0) - (Number(b.pressure) || 0)) <= 0.01
    && Math.abs((Number(a.tilt_x) || 0) - (Number(b.tilt_x) || 0)) <= 0.5
    && Math.abs((Number(a.tilt_y) || 0) - (Number(b.tilt_y) || 0)) <= 0.5
    && Math.abs((Number(a.roll) || 0) - (Number(b.roll) || 0)) <= 0.5
    && Math.abs((Number(a.timestamp_ms) || 0) - (Number(b.timestamp_ms) || 0)) <= INK_TIME_EPSILON_MS;
}

function activeInkStroke() {
  return state.inkDraft.strokes[state.inkDraft.strokes.length - 1] || null;
}

function activeInkBounds() {
  if (state.inkDraft.target === 'pdf') {
    return {
      width: Math.max(1, Number(state.inkDraft.pageWidth) || 1),
      height: Math.max(1, Number(state.inkDraft.pageHeight) || 1),
    };
  }
  return getCanvasLogicalSize(inkLayerEl());
}

function rebuildStrokePrediction(stroke) {
  if (!stroke || !Array.isArray(stroke.points)) return;
  stroke.predicted_points = [];
  stroke.predicted_count = 0;
  if (stroke.points.length < 3) return;
  const [a, b, c] = stroke.points.slice(-3);
  const dt1 = Math.max(1, (Number(b?.timestamp_ms) || 0) - (Number(a?.timestamp_ms) || 0) || INK_PREDICTION_FRAME_MS);
  const dt2 = Math.max(1, (Number(c?.timestamp_ms) || 0) - (Number(b?.timestamp_ms) || 0) || INK_PREDICTION_FRAME_MS);
  const vx1 = ((Number(b?.x) || 0) - (Number(a?.x) || 0)) / dt1;
  const vy1 = ((Number(b?.y) || 0) - (Number(a?.y) || 0)) / dt1;
  const vx2 = ((Number(c?.x) || 0) - (Number(b?.x) || 0)) / dt2;
  const vy2 = ((Number(c?.y) || 0) - (Number(b?.y) || 0)) / dt2;
  const avgDt = Math.max(1, (dt1 + dt2) / 2);
  const ax = (vx2 - vx1) / avgDt;
  const ay = (vy2 - vy1) / avgDt;
  const step = Math.max(INK_PREDICTION_FRAME_MS, Math.min(20, dt2));
  const bounds = activeInkBounds();
  for (let i = 1; i <= INK_PREDICTION_STEPS; i += 1) {
    const dt = step * i;
    stroke.predicted_points.push({
      x: clampPoint((Number(c?.x) || 0) + (vx2 * dt) + (0.5 * ax * dt * dt), bounds.width),
      y: clampPoint((Number(c?.y) || 0) + (vy2 * dt) + (0.5 * ay * dt * dt), bounds.height),
      pressure: Number(c?.pressure) || 0,
      tilt_x: Number(c?.tilt_x) || 0,
      tilt_y: Number(c?.tilt_y) || 0,
      roll: Number(c?.roll) || 0,
      timestamp_ms: Number((Number(c?.timestamp_ms) || 0) + dt),
      predicted: true,
    });
  }
  stroke.predicted_count = stroke.predicted_points.length;
}

function appendPointerSamplesToStroke(stroke, pointerEvent) {
  if (!stroke || !Array.isArray(stroke.points)) return false;
  let changed = false;
  for (const sample of collectPointerSamples(pointerEvent)) {
    const point = pointerEventToInkPoint(sample);
    const last = stroke.points[stroke.points.length - 1];
    if (last && sameInkPoint(last, point)) continue;
    stroke.points.push(point);
    changed = true;
  }
  rebuildStrokePrediction(stroke);
  return changed;
}

function drawStrokePath(ctx, points, width, color, alpha = 1) {
  if (!(ctx instanceof CanvasRenderingContext2D) || !Array.isArray(points) || points.length === 0) return;
  ctx.save();
  ctx.strokeStyle = color;
  ctx.globalAlpha = alpha;
  ctx.beginPath();
  ctx.lineWidth = Math.max(1.5, Number(width) || 2.4);
  ctx.moveTo(Number(points[0]?.x) || 0, Number(points[0]?.y) || 0);
  for (let i = 1; i < points.length; i += 1) {
    ctx.lineTo(Number(points[i]?.x) || 0, Number(points[i]?.y) || 0);
  }
  if (points.length === 1) {
    ctx.lineTo((Number(points[0]?.x) || 0) + 0.01, Number(points[0]?.y) || 0);
  }
  ctx.stroke();
  ctx.restore();
}

function drawStroke(ctx, stroke, color) {
  if (!(ctx instanceof CanvasRenderingContext2D) || !stroke) return;
  drawStrokePath(ctx, stroke.points, stroke.width, color, 1);
  const predicted = Array.isArray(stroke.predicted_points) ? stroke.predicted_points : [];
  if (predicted.length === 0 || !Array.isArray(stroke.points) || stroke.points.length === 0) return;
  drawStrokePath(ctx, [stroke.points[stroke.points.length - 1], ...predicted], stroke.width, color, 0.35);
}

function redrawMainInkLayer() {
  const layer = inkLayerEl();
  if (!(layer instanceof HTMLCanvasElement)) return;
  const ctx = getCanvas2DContext(layer);
  if (!ctx) return;
  clearCanvasLayer(layer);
  state.inkDraft.strokes.forEach((stroke) => drawStroke(ctx, stroke, INK_STROKE_COLOR));
}

function redrawPdfDraftLayer() {
  const layer = state.inkDraft.draftLayer;
  if (!(layer instanceof HTMLCanvasElement)) return;
  const ctx = getCanvas2DContext(layer);
  if (!ctx) return;
  clearCanvasLayer(layer);
  const stroke = activeInkStroke();
  if (stroke) {
    drawStroke(ctx, stroke, PDF_INK_STROKE_COLOR);
  }
}

function renderActiveInkLayer() {
  if (state.inkDraft.target === 'pdf') {
    redrawPdfDraftLayer();
    return;
  }
  redrawMainInkLayer();
}

function activeMainStrokeWidth() {
  return Math.max(1.5, Number(activeInkStroke()?.width) || 2.4);
}

async function ensureDelegatedInkPresenter(canvas) {
  if (!(canvas instanceof HTMLCanvasElement) || delegatedInkPresenter || delegatedInkPresenterPromise) return;
  const ink = navigator && (navigator as any).ink;
  if (!ink || typeof ink.requestPresenter !== 'function') return;
  delegatedInkPresenterPromise = Promise.resolve()
    .then(() => ink.requestPresenter({ presentationArea: canvas }))
    .then((presenter) => {
      delegatedInkPresenter = presenter || null;
      canvas.dataset.inkPresenter = delegatedInkPresenter ? 'enabled' : 'unavailable';
      return delegatedInkPresenter;
    })
    .catch(() => {
      delegatedInkPresenter = null;
      canvas.dataset.inkPresenter = 'unavailable';
      return null;
    })
    .finally(() => {
      delegatedInkPresenterPromise = null;
    });
  await delegatedInkPresenterPromise;
}

function updateDelegatedInkPresenter(pointerEvent) {
  if (!delegatedInkPresenter || typeof delegatedInkPresenter.updateInkTrailStartPoint !== 'function') return;
  if (state.inkDraft.target === 'pdf') return;
  try {
    delegatedInkPresenter.updateInkTrailStartPoint(pointerEvent, {
      color: INK_STROKE_COLOR,
      diameter: activeMainStrokeWidth(),
    });
  } catch (_) {}
}

function clampPoint(value, max) {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(Number(max) || 0, value));
}

function isDialogueInkStreamingActive() {
  return Boolean(state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_DIALOGUE);
}

function sendInkChatEvent(payload) {
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) return false;
  ws.send(JSON.stringify(payload));
  return true;
}

function strokeBounds(strokes) {
  const points = Array.isArray(strokes)
    ? strokes.flatMap((stroke) => (Array.isArray(stroke?.points) ? stroke.points : []))
    : [];
  if (points.length === 0) return null;
  let minX = Number(points[0]?.x) || 0;
  let minY = Number(points[0]?.y) || 0;
  let maxX = minX;
  let maxY = minY;
  points.forEach((point) => {
    const x = Number(point?.x) || 0;
    const y = Number(point?.y) || 0;
    minX = Math.min(minX, x);
    minY = Math.min(minY, y);
    maxX = Math.max(maxX, x);
    maxY = Math.max(maxY, y);
  });
  return {
    minX,
    minY,
    maxX,
    maxY,
    width: Math.max(0, maxX - minX),
    height: Math.max(0, maxY - minY),
  };
}

function mapInkPointToClient(point) {
  if (!point) return null;
  if (state.inkDraft.target === 'pdf' && state.inkDraft.pageInner instanceof HTMLElement) {
    const rect = state.inkDraft.pageInner.getBoundingClientRect();
    return {
      x: rect.left + (Number(point.x) || 0),
      y: rect.top + (Number(point.y) || 0),
    };
  }
  const viewport = document.getElementById('canvas-viewport');
  if (!(viewport instanceof HTMLElement)) return null;
  const rect = viewport.getBoundingClientRect();
  return {
    x: rect.left + (Number(point.x) || 0) - viewport.scrollLeft,
    y: rect.top + (Number(point.y) || 0) - viewport.scrollTop,
  };
}

function uniqueTextSamples(values) {
  const seen = new Set();
  const out = [];
  values.forEach((value) => {
    const clean = String(value || '').trim();
    if (!clean || seen.has(clean)) return;
    seen.add(clean);
    out.push(clean);
  });
  return out;
}

function buildInkEventPayload() {
  if (!isDialogueInkStreamingActive()) return null;
  const strokes = Array.isArray(state.inkDraft.strokes) ? state.inkDraft.strokes : [];
  if (strokes.length === 0) return null;
  const bounds = strokeBounds(strokes);
  if (!bounds) return null;

  const sampledPoints = strokes.flatMap((stroke) => {
    const points = Array.isArray(stroke?.points) ? stroke.points : [];
    if (points.length === 0) return [];
    const middle = points[Math.floor(points.length / 2)];
    return [points[0], middle, points[points.length - 1]];
  });
  sampledPoints.push({
    x: bounds.minX + (bounds.width / 2),
    y: bounds.minY + (bounds.height / 2),
  });

  const rawLocations = sampledPoints
    .map((point) => mapInkPointToClient(point))
    .filter(Boolean)
    .map((clientPoint) => getLocationFromPoint(clientPoint.x, clientPoint.y))
    .filter((location) => location && typeof location === 'object');

  const locations: any[] = rawLocations;
  const lineNumbers = locations
    .map((location) => Number(location?.line || 0))
    .filter((line) => Number.isFinite(line) && line > 0);
  const surroundingTexts = uniqueTextSamples(locations.map((location) => location?.surroundingText));
  const cursor = locations.find((location) => location && (location.line || location.page || Number.isFinite(location.relativeX) || location.title)) || null;

  const width = state.inkDraft.target === 'pdf'
    ? Math.max(1, Number(state.inkDraft.pageWidth) || 1)
    : getCanvasLogicalSize(inkLayerEl()).width;
  const height = state.inkDraft.target === 'pdf'
    ? Math.max(1, Number(state.inkDraft.pageHeight) || 1)
    : getCanvasLogicalSize(inkLayerEl()).height;

  const snapshotDataURL = state.inkDraft.target === 'pdf'
    ? buildPdfInkSnapshotDataURL()
    : `data:image/png;base64,${buildInkPNGBase64()}`;
  const overlappingLines = lineNumbers.length > 0
    ? { start: Math.min(...lineNumbers), end: Math.max(...lineNumbers) }
    : null;

  return {
    type: 'canvas_ink',
    cursor: cursor ? {
      line: Number(cursor.line || 0) || undefined,
      page: Number(cursor.page || 0) || undefined,
      title: String(cursor.title || ''),
      surrounding_text: String(cursor.surroundingText || ''),
      relative_x: Number.isFinite(cursor.relativeX) ? cursor.relativeX : undefined,
      relative_y: Number.isFinite(cursor.relativeY) ? cursor.relativeY : undefined,
    } : null,
    artifact_kind: activeArtifactKindForInk(),
    output_mode: state.ttsSilent ? 'silent' : 'voice',
    request_response: true,
    total_strokes: strokes.length,
    bounding_box: {
      relative_x: bounds.minX / width,
      relative_y: bounds.minY / height,
      relative_width: bounds.width / width,
      relative_height: bounds.height / height,
    },
    overlapping_lines: overlappingLines,
    overlapping_text: surroundingTexts.join('\n'),
    snapshot_data_url: snapshotDataURL,
    strokes: strokes.map((stroke) => ({
      pointer_type: stroke.pointer_type,
      width: stroke.width,
      predicted_count: Number(stroke?.predicted_count) || 0,
      points: (Array.isArray(stroke?.points) ? stroke.points : []).map((point) => ({
        x: point.x,
        y: point.y,
        pressure: point.pressure,
        tilt_x: point.tilt_x,
        tilt_y: point.tilt_y,
        roll: point.roll,
        timestamp_ms: point.timestamp_ms,
      })),
    })),
  };
}

function buildPdfInkSnapshotDataURL() {
  if (state.inkDraft.target !== 'pdf' || state.inkDraft.strokes.length === 0) return '';
  const width = Math.max(1, Number(state.inkDraft.pageWidth) || 1);
  const height = Math.max(1, Number(state.inkDraft.pageHeight) || 1);
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.strokeStyle = INK_STROKE_COLOR;
  state.inkDraft.strokes.forEach((stroke) => {
    const points = Array.isArray(stroke?.points) ? stroke.points : [];
    if (points.length === 0) return;
    ctx.beginPath();
    ctx.lineWidth = Math.max(1.5, Number(stroke?.width) || 2.4);
    ctx.moveTo(Number(points[0]?.x) || 0, Number(points[0]?.y) || 0);
    for (let i = 1; i < points.length; i += 1) {
      ctx.lineTo(Number(points[i]?.x) || 0, Number(points[i]?.y) || 0);
    }
    if (points.length === 1) {
      ctx.lineTo((Number(points[0]?.x) || 0) + 0.01, Number(points[0]?.y) || 0);
    }
    ctx.stroke();
  });
  return canvas.toDataURL('image/png');
}

function ensurePdfInkDraftLayer(pageInner, width, height) {
  if (!(pageInner instanceof HTMLElement)) return null;
  let layer = pageInner.querySelector('.canvas-ink-draft-layer');
  if (!(layer instanceof HTMLCanvasElement)) {
    layer = document.createElement('canvas');
    layer.classList.add('canvas-ink-draft-layer');
    layer.setAttribute('aria-hidden', 'true');
    pageInner.appendChild(layer);
  }
  syncCanvasSize(layer, width, height);
  return layer;
}

export function beginInkStroke(pointerEvent) {
  const pdfAnchor = activeArtifactKindForInk() === 'pdf'
    ? pdfPageAnchorAtPoint(pointerEvent.clientX, pointerEvent.clientY)
    : null;
  if (pdfAnchor) {
    const draftLayer = ensurePdfInkDraftLayer(pdfAnchor.pageInner, pdfAnchor.width, pdfAnchor.height);
    if (!(draftLayer instanceof HTMLCanvasElement)) return false;
    const stroke = {
      pointer_type: String(pointerEvent.pointerType || 'pen').trim().toLowerCase() || 'pen',
      width: strokeWidthForPressure(pointerEvent.pressure),
      points: [createInkPoint(pdfAnchor.xPx, pdfAnchor.yPx, pointerEvent)],
      predicted_points: [],
      predicted_count: 0,
    };
    state.inkDraft.strokes = [stroke];
    state.inkDraft.activePointerId = pointerEvent.pointerId;
    state.inkDraft.activePointerType = stroke.pointer_type;
    state.inkDraft.activePath = stroke;
    state.inkDraft.target = 'pdf';
    state.inkDraft.page = pdfAnchor.pageNumber;
    state.inkDraft.pageInner = pdfAnchor.pageInner;
    state.inkDraft.pageWidth = pdfAnchor.width;
    state.inkDraft.pageHeight = pdfAnchor.height;
    state.inkDraft.draftLayer = draftLayer;
    state.inkDraft.dirty = false;
    redrawPdfDraftLayer();
    renderInkControls();
    return true;
  }
  const layer = inkLayerEl();
  if (!(layer instanceof HTMLCanvasElement)) return false;
  syncInkLayerSize();
  const point = pointForViewportEvent(pointerEvent.clientX, pointerEvent.clientY);
  const stroke = {
    pointer_type: String(pointerEvent.pointerType || 'pen').trim().toLowerCase() || 'pen',
    width: strokeWidthForPressure(pointerEvent.pressure),
    points: [createInkPoint(point.x, point.y, pointerEvent)],
    predicted_points: [],
    predicted_count: 0,
  };
  state.inkDraft.strokes.push(stroke);
  state.inkDraft.activePointerId = pointerEvent.pointerId;
  state.inkDraft.activePointerType = stroke.pointer_type;
  state.inkDraft.activePath = stroke;
  state.inkDraft.dirty = true;
  redrawMainInkLayer();
  updateDelegatedInkPresenter(pointerEvent);
  renderInkControls();
  return true;
}

export function extendInkStroke(pointerEvent) {
  if (state.inkDraft.activePointerId !== pointerEvent.pointerId) return false;
  const stroke = activeInkStroke();
  if (!stroke) return false;
  const changed = appendPointerSamplesToStroke(stroke, pointerEvent);
  renderActiveInkLayer();
  updateDelegatedInkPresenter(pointerEvent);
  return changed || (Number(stroke.predicted_count) || 0) > 0;
}

export function finalizeInkStroke(pointerEvent) {
  if (state.inkDraft.activePointerId !== pointerEvent.pointerId) return false;
  extendInkStroke(pointerEvent);
  const stroke = activeInkStroke();
  if (stroke) {
    stroke.predicted_points = [];
    stroke.predicted_count = 0;
  }
  renderActiveInkLayer();
  const livePayload = buildInkEventPayload();
  if (livePayload) {
    sendInkChatEvent(livePayload);
  }
  if (state.inkDraft.target === 'pdf') {
    persistPdfInkAnnotation(state.inkDraft.page, state.inkDraft.pageWidth, state.inkDraft.pageHeight, stroke);
    if (state.inkDraft.draftLayer instanceof HTMLElement) {
      state.inkDraft.draftLayer.remove();
    }
    state.inkDraft.strokes = [];
    state.inkDraft.dirty = false;
    resetInkDraftState();
    renderInkControls();
    return true;
  }
  resetInkDraftState();
  renderInkControls();
  return true;
}

export function buildInkSVGMarkup() {
  const layer = inkLayerEl();
  if (!(layer instanceof HTMLCanvasElement)) return '';
  syncInkLayerSize();
  const { width, height } = getCanvasLogicalSize(layer);
  const paths = state.inkDraft.strokes.map((stroke) => {
    const d = buildInkPathData(stroke?.points);
    if (!d) return '';
    return `<path fill="none" stroke="${INK_STROKE_COLOR}" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke" stroke-width="${Math.max(1.5, Number(stroke?.width) || 2.4).toFixed(2)}" d="${d}" />`;
  }).join('');
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}">${paths}</svg>`;
}

export function buildInkPNGBase64() {
  syncInkLayerSize();
  const layer = inkLayerEl();
  if (!(layer instanceof HTMLCanvasElement)) return '';
  const { width, height } = getCanvasLogicalSize(layer);
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.strokeStyle = INK_STROKE_COLOR;
  for (const stroke of state.inkDraft.strokes) {
    const points = Array.isArray(stroke?.points) ? stroke.points : [];
    if (points.length === 0) continue;
    ctx.beginPath();
    ctx.lineWidth = Math.max(1.5, Number(stroke?.width) || 2.4);
    ctx.moveTo(Number(points[0]?.x) || 0, Number(points[0]?.y) || 0);
    for (let i = 1; i < points.length; i += 1) {
      ctx.lineTo(Number(points[i]?.x) || 0, Number(points[i]?.y) || 0);
    }
    if (points.length === 1) {
      ctx.lineTo((Number(points[0]?.x) || 0) + 0.01, Number(points[0]?.y) || 0);
    }
    ctx.stroke();
  }
  return canvas.toDataURL('image/png').replace(/^data:image\/png;base64,/, '');
}

export async function submitInkDraft() {
  if (state.inkSubmitInFlight || state.inkDraft.strokes.length === 0) return false;
  const project = activeProject();
  if (!project?.id) return false;
  const wasBlankCanvas = !state.hasArtifact;
  state.inkSubmitInFlight = true;
  renderInkControls();
  try {
    const payload = {
      workspace_id: project.id,
      artifact_kind: activeArtifactKindForInk(),
      artifact_title: String(getActiveArtifactTitle() || ''),
      artifact_path: String(state.workspaceOpenFilePath || ''),
      strokes: state.inkDraft.strokes.map((stroke) => ({
        pointer_type: stroke.pointer_type,
        width: stroke.width,
        predicted_count: Number(stroke?.predicted_count) || 0,
        points: stroke.points.map((point) => ({
          x: point.x,
          y: point.y,
          pressure: point.pressure,
          tilt_x: point.tilt_x,
          tilt_y: point.tilt_y,
          roll: point.roll,
          timestamp_ms: point.timestamp_ms,
        })),
      })),
      svg: buildInkSVGMarkup(),
      png_base64: buildInkPNGBase64(),
    };
    const resp = await fetch(apiURL('ink/submit'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const result = await resp.json();
    const pngPath = String(result?.ink_png_path || '').trim();
    const summaryPath = String(result?.summary_path || '').trim();
    const inkPath = String(result?.ink_svg_path || '').trim();
    const revisionHistoryPath = String(result?.revision_history_path || '').trim();
    clearInkDraft();
    if (revisionHistoryPath) {
      showStatus(`ink saved: ${revisionHistoryPath}`);
    } else if (summaryPath) {
      showStatus(`ink saved: ${summaryPath}`);
    } else if (inkPath) {
      showStatus(`ink saved: ${inkPath}`);
    } else {
      showStatus('ink saved');
    }
    if (pngPath) {
      await openWorkspaceSidebarFile(pngPath);
    } else if (summaryPath) {
      await openWorkspaceSidebarFile(summaryPath);
    } else if (inkPath) {
      await openWorkspaceSidebarFile(inkPath);
    }
    if (wasBlankCanvas && pngPath) {
      showStatus(`ink saved as image: ${pngPath}`);
    }
    return true;
  } catch (err) {
    showStatus(`ink submit failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  } finally {
    state.inkSubmitInFlight = false;
    renderInkControls();
  }
}

export async function fetchProjects() {
  const resp = await fetch(apiURL('runtime/workspaces'), { cache: 'no-store' });
  if (!resp.ok) throw new Error(`workspaces list failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const projects = Array.isArray(payload?.workspaces) ? payload.workspaces : [];
  state.projects = projects.map((project) => ({
    ...project,
    id: String(project?.id || ''),
    chat_mode: String(project?.chat_mode || 'chat'),
    chat_model_reasoning_effort: String(project?.chat_model_reasoning_effort || '').trim().toLowerCase(),
    run_state: normalizeProjectRunState(project?.run_state),
    unread: Boolean(project?.unread),
    review_pending: Boolean(project?.review_pending),
  })).filter((project) => project.id);
  state.defaultWorkspaceId = String(payload?.default_workspace_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_workspace_id || '').trim();
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
}
