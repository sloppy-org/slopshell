import { apiURL, getActiveArtifactTitle, getActiveTextEventId, getUiState, isOverlayVisible, isTextInputVisible } from './app-env.js';
import { refs, state } from './app-context.js';
import { showCanvasColumn } from './app-canvas-ui.js';
import { renderCanvas } from './canvas.js';
import { TABURA_CIRCLE_BUG_ICON } from './tabura-circle-contract.js';

const showStatus = (...args) => refs.showStatus(...args);
const fetchRuntimeMeta = (...args) => refs.fetchRuntimeMeta(...args);
const acquireMicStream = (...args) => refs.acquireMicStream(...args);
const newMediaRecorder = (...args) => refs.newMediaRecorder(...args);
const sttStart = (...args) => refs.sttStart(...args);
const sttSendBlob = (...args) => refs.sttSendBlob(...args);
const sttStop = (...args) => refs.sttStop(...args);
const sttCancel = (...args) => refs.sttCancel(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const refreshItemSidebarCounts = (...args) => refs.refreshItemSidebarCounts(...args);

const BUG_REPORT_SHORTCUT_KEY = 'b';
const BUG_REPORT_TOUCH_HOLD_MS = 700;
const BUG_REPORT_EVENT_LIMIT = 24;
const BUG_REPORT_LOG_LIMIT = 40;
const BUG_REPORT_STROKE_COLOR = '#d92d20';
const BUG_REPORT_STROKE_WIDTH = 4;
const BUG_REPORT_CAPTURE_TIMEOUT_MS = 1500;

const recentEvents = [];
const browserLogs = [];
let bugReportUiReady = false;
let browserLogCaptureReady = false;
let interactionCaptureReady = false;
let pendingReport = null;
let activeStroke = null;
let noteRecording = null;
let twoFingerHold = null;

function pushBounded(list, value, limit) {
  if (!Array.isArray(list)) return;
  list.push(value);
  while (list.length > limit) {
    list.shift();
  }
}

function formatNow() {
  return new Date().toISOString();
}

function safeText(value) {
  return String(value == null ? '' : value).replace(/\s+/g, ' ').trim();
}

function normalizeBugReportInputMode(value) {
  const clean = safeText(value).toLowerCase();
  switch (clean) {
    case 'pen':
    case 'voice':
    case 'keyboard':
      return clean;
    case 'text':
      return 'keyboard';
    default:
      return '';
  }
}

function hasKeyboardBugReportEvidence(events) {
  if (!Array.isArray(events)) return false;
  return events.some((entry) => /^key\b/i.test(safeText(entry).replace(/^\S+\s+/, '')));
}

function resolveBugReportInputMode(trigger, events) {
  const origin = normalizeBugReportInputMode(state.lastInputOrigin);
  if (origin === 'voice' || origin === 'pen') return origin;
  if (origin === 'keyboard' && hasKeyboardBugReportEvidence(events)) return 'keyboard';
  const cleanTrigger = safeText(trigger).toLowerCase();
  if (cleanTrigger === 'voice') return 'voice';
  if (cleanTrigger === 'shortcut') return 'keyboard';
  return '';
}

function bugReportTestEnv() {
  return window.__taburaBugReportTestEnv || {};
}

function stringifyConsoleArg(value) {
  if (typeof value === 'string') return value;
  if (value instanceof Error) return value.stack || value.message || String(value);
  try {
    return JSON.stringify(value);
  } catch (_) {
    return String(value);
  }
}

function bugReportIssueCanvasMarkdown(payload) {
  const issueNumber = Number(payload?.issue_number || 0);
  const issueURL = safeText(payload?.issue_url);
  const issueError = safeText(payload?.issue_error);
  const bundlePath = safeText(payload?.bundle_path);
  const filed = issueNumber > 0 || Boolean(issueURL);
  const lines = [filed ? '# Bug report filed' : '# Bug report saved locally', ''];
  if (issueNumber > 0 && issueURL) {
    lines.push(`- Issue: [#${issueNumber}](${issueURL})`);
  } else if (issueURL) {
    lines.push(`- Issue: ${issueURL}`);
  }
  if (issueError) {
    lines.push(filed ? `- Issue filing error: ${issueError}` : `- GitHub auto-filing: ${issueError}`);
  }
  if (bundlePath) {
    lines.push(`- Bundle: \`${bundlePath}\``);
  }
  return lines.join('\n');
}

function recordRecentEvent(label) {
  const clean = safeText(label);
  if (!clean) return;
  pushBounded(recentEvents, `${formatNow()} ${clean}`, BUG_REPORT_EVENT_LIMIT);
}

function recordBrowserLog(level, args) {
  const message = args.map((value) => stringifyConsoleArg(value)).join(' ').trim();
  if (!message) return;
  pushBounded(browserLogs, `${formatNow()} ${level}: ${message}`, BUG_REPORT_LOG_LIMIT);
}

function installBrowserLogCapture() {
  if (browserLogCaptureReady) return;
  browserLogCaptureReady = true;
  ['log', 'warn', 'error'].forEach((level) => {
    const original = console[level];
    if (typeof original !== 'function') return;
    console[level] = (...args) => {
      recordBrowserLog(level, args);
      original.apply(console, args);
    };
  });
  window.addEventListener('error', (event) => {
    recordBrowserLog('error', [event?.message || event?.error || 'window error']);
  });
  window.addEventListener('unhandledrejection', (event) => {
    recordBrowserLog('error', [event?.reason || 'unhandled rejection']);
  });
}

function installInteractionCapture() {
  if (interactionCaptureReady) return;
  interactionCaptureReady = true;
  document.addEventListener('pointerdown', (event) => {
    if (event.target instanceof Element && event.target.closest('#bug-report-sheet')) return;
    recordRecentEvent(`pointer ${event.pointerType || 'mouse'} at (${Math.round(event.clientX)},${Math.round(event.clientY)})`);
  }, true);
  document.addEventListener('keydown', (event) => {
    if (event.target instanceof Element && event.target.closest('#bug-report-sheet')) return;
    const mods = [
      event.ctrlKey ? 'Ctrl' : '',
      event.altKey ? 'Alt' : '',
      event.metaKey ? 'Meta' : '',
      event.shiftKey ? 'Shift' : '',
    ].filter(Boolean).join('+');
    const key = safeText(event.key) || 'unknown';
    recordRecentEvent(`key ${mods ? `${mods}+` : ''}${key}`);
  }, true);
}

function bugReportNodes() {
  return {
    button: document.getElementById('bug-report-button'),
    sheet: document.getElementById('bug-report-sheet'),
    previewFrame: document.querySelector('#bug-report-sheet .bug-report-sheet__preview'),
    preview: document.getElementById('bug-report-preview'),
    ink: document.getElementById('bug-report-ink'),
    note: document.getElementById('bug-report-note'),
    record: document.getElementById('bug-report-record'),
    save: document.getElementById('bug-report-save'),
    cancel: document.getElementById('bug-report-cancel'),
    clear: document.getElementById('bug-report-clear'),
  };
}

function ensureBugReportUi() {
  let button = document.getElementById('bug-report-button');
  if (!(button instanceof HTMLButtonElement)) {
    const edgeTopActions = document.getElementById('edge-top-actions');
    const nextButton = document.createElement('button');
    nextButton.id = 'bug-report-button';
    nextButton.type = 'button';
    nextButton.className = 'edge-btn edge-icon-btn';
    nextButton.setAttribute('aria-label', 'Report bug');
    nextButton.title = 'Report bug';
    if (edgeTopActions instanceof HTMLElement) {
      edgeTopActions.insertBefore(nextButton, edgeTopActions.firstChild);
    } else {
      document.body.appendChild(nextButton);
    }
    button = nextButton;
  }
  if (button instanceof HTMLButtonElement) {
    button.innerHTML = `<span class="tabura-circle-icon" aria-hidden="true">${TABURA_CIRCLE_BUG_ICON}</span>`;
  }

  if (document.getElementById('bug-report-sheet')) return;

  const sheet = document.createElement('section');
  sheet.id = 'bug-report-sheet';
  sheet.className = 'bug-report-sheet';
  sheet.hidden = true;
  sheet.innerHTML = `
    <div class="bug-report-sheet__backdrop"></div>
    <div class="bug-report-sheet__panel" role="dialog" aria-modal="true" aria-labelledby="bug-report-title">
      <div class="bug-report-sheet__header">
        <div>
          <h2 id="bug-report-title">Bug Report</h2>
          <p>Captured instantly. Add ink, a note, or a short voice memo before saving.</p>
        </div>
        <div class="bug-report-sheet__actions">
          <button id="bug-report-clear" type="button" class="edge-btn">Clear Ink</button>
          <button id="bug-report-record" type="button" class="edge-btn">Voice Note</button>
          <button id="bug-report-cancel" type="button" class="edge-btn">Cancel</button>
        </div>
      </div>
      <div class="bug-report-sheet__preview">
        <img id="bug-report-preview" alt="Bug report screenshot">
        <canvas id="bug-report-ink"></canvas>
      </div>
      <label class="bug-report-sheet__label" for="bug-report-note">Note</label>
      <textarea id="bug-report-note" rows="4" placeholder="What broke? What did you expect?"></textarea>
      <div class="bug-report-sheet__footer">
        <button id="bug-report-save" type="button">Save Bug Bundle</button>
      </div>
    </div>
  `;
  document.body.appendChild(sheet);
}

function openBugReportSheet() {
  const { sheet } = bugReportNodes();
  if (!(sheet instanceof HTMLElement)) return;
  sheet.hidden = false;
  document.body.classList.add('bug-report-open');
}

function closeBugReportSheet() {
  const { sheet, note } = bugReportNodes();
  if (sheet instanceof HTMLElement) {
    sheet.hidden = true;
  }
  if (note instanceof HTMLTextAreaElement) {
    note.value = '';
  }
  document.body.classList.remove('bug-report-open');
  pendingReport = null;
  activeStroke = null;
  clearBugReportInk();
  void stopBugReportVoiceNote(true);
}

function syncBugReportCanvasSize() {
  const { previewFrame, preview, ink } = bugReportNodes();
  if (!(preview instanceof HTMLImageElement) || !(ink instanceof HTMLCanvasElement)) return;
  const target = previewFrame instanceof HTMLElement ? previewFrame : preview;
  const rect = target.getBoundingClientRect();
  const width = Math.max(1, Math.round(rect.width));
  const height = Math.max(1, Math.round(rect.height));
  if (width === 0 || height === 0) return;
  ink.width = width;
  ink.height = height;
  ink.style.width = `${width}px`;
  ink.style.height = `${height}px`;
  redrawBugReportInk();
}

function drawStroke(ctx, stroke) {
  if (!ctx || !stroke || !Array.isArray(stroke.points) || stroke.points.length === 0) return;
  ctx.lineJoin = 'round';
  ctx.lineCap = 'round';
  ctx.strokeStyle = BUG_REPORT_STROKE_COLOR;
  ctx.lineWidth = BUG_REPORT_STROKE_WIDTH;
  ctx.beginPath();
  stroke.points.forEach((point, index) => {
    if (index === 0) {
      ctx.moveTo(point.x, point.y);
      return;
    }
    ctx.lineTo(point.x, point.y);
  });
  ctx.stroke();
}

function redrawBugReportInk() {
  const { ink } = bugReportNodes();
  if (!(ink instanceof HTMLCanvasElement)) return;
  const ctx = ink.getContext('2d');
  if (!ctx) return;
  ctx.clearRect(0, 0, ink.width, ink.height);
  const strokes = Array.isArray(pendingReport?.strokes) ? pendingReport.strokes : [];
  strokes.forEach((stroke) => drawStroke(ctx, stroke));
  if (activeStroke) {
    drawStroke(ctx, activeStroke);
  }
}

function clearBugReportInk() {
  if (pendingReport) {
    pendingReport.strokes = [];
  }
  activeStroke = null;
  redrawBugReportInk();
}

function buildCanvasState(inputMode = '') {
  const uiState = getUiState();
  return {
    has_artifact: Boolean(state.hasArtifact),
    artifact_title: safeText(getActiveArtifactTitle()),
    artifact_event_id: safeText(getActiveTextEventId()),
    chat_mode: safeText(state.chatMode),
    active_sphere: safeText(state.activeSphere),
    interaction_conversation: safeText(state.interaction?.conversation),
    interaction_surface: safeText(state.interaction?.surface),
    interaction_tool: safeText(state.interaction?.tool),
    overlay_visible: Boolean(isOverlayVisible()),
    text_input_visible: Boolean(isTextInputVisible()),
    pr_review_mode: Boolean(state.prReviewMode),
    active_workspace_id: safeText(state.activeWorkspaceId),
    item_sidebar_view: safeText(state.itemSidebarView),
    workspace_browser_path: safeText(state.workspaceBrowserPath),
    last_input_origin: normalizeBugReportInputMode(inputMode),
    last_input_x: Number.isFinite(uiState?.lastInputX) ? Math.round(uiState.lastInputX) : null,
    last_input_y: Number.isFinite(uiState?.lastInputY) ? Math.round(uiState.lastInputY) : null,
  };
}

function cleanStringList(values) {
  if (!Array.isArray(values)) return [];
  return values
    .map((value) => safeText(value))
    .filter(Boolean);
}

function normalizeBrandList(brands) {
  if (!Array.isArray(brands)) return [];
  return brands
    .map((entry) => {
      const brand = safeText(entry?.brand);
      const version = safeText(entry?.version);
      if (!brand && !version) return null;
      return { brand, version };
    })
    .filter(Boolean);
}

async function readUserAgentData(): Promise<Record<string, any>> {
  const userAgentData = (navigator as any).userAgentData;
  if (!userAgentData || typeof userAgentData !== 'object') {
    return {};
  }
  const device: Record<string, any> = {
    mobile: Boolean(userAgentData.mobile),
    brands: normalizeBrandList(userAgentData.brands),
    platform: safeText(userAgentData.platform),
  };
  if (typeof userAgentData.getHighEntropyValues === 'function') {
    try {
      const detail = await userAgentData.getHighEntropyValues([
        'architecture',
        'fullVersionList',
        'model',
        'platformVersion',
        'uaFullVersion',
      ]);
      device.architecture = safeText(detail?.architecture);
      device.full_version_list = normalizeBrandList(detail?.fullVersionList);
      device.model = safeText(detail?.model);
      device.os_version = safeText(detail?.platformVersion);
      device.browser_version = safeText(detail?.uaFullVersion);
    } catch (_) {}
  }
  return device;
}

async function buildDeviceState() {
  const uaData = await readUserAgentData();
  const timeZone = safeText(Intl.DateTimeFormat().resolvedOptions?.().timeZone);
  return {
    ua: safeText(navigator.userAgent),
    platform: safeText(uaData.platform || navigator.platform),
    os_version: safeText(uaData.os_version),
    architecture: safeText(uaData.architecture),
    model: safeText(uaData.model),
    mobile: Boolean(uaData.mobile),
    brands: normalizeBrandList(uaData.brands),
    full_version_list: normalizeBrandList(uaData.full_version_list),
    browser_version: safeText(uaData.browser_version),
    vendor: safeText(navigator.vendor),
    language: safeText(navigator.language),
    languages: cleanStringList(navigator.languages),
    viewport: `${window.innerWidth}x${window.innerHeight}`,
    screen: `${window.screen?.width || 0}x${window.screen?.height || 0}`,
    color_depth: Number(window.screen?.colorDepth || 0),
    pixel_ratio: window.devicePixelRatio || 1,
    timezone: timeZone,
    hardware_concurrency: Number(navigator.hardwareConcurrency || 0),
    device_memory_gb: Number((navigator as any).deviceMemory || 0),
    max_touch_points: Number(navigator.maxTouchPoints || 0),
  };
}

function collectStyleText() {
  let css = '';
  for (const sheet of Array.from(document.styleSheets || [])) {
    try {
      const rules = Array.from(sheet.cssRules || []);
      css += rules.map((rule) => rule.cssText).join('\n');
    } catch (_) {}
  }
  return css;
}

function syncCloneInputs(sourceRoot, cloneRoot) {
  const sourceInputs = sourceRoot.querySelectorAll('input, textarea, select');
  const cloneInputs = cloneRoot.querySelectorAll('input, textarea, select');
  sourceInputs.forEach((source, index) => {
    const target = cloneInputs[index];
    if (!target) return;
    if (source instanceof HTMLTextAreaElement && target instanceof HTMLTextAreaElement) {
      target.textContent = source.value;
      target.value = source.value;
      return;
    }
    if (source instanceof HTMLInputElement && target instanceof HTMLInputElement) {
      target.setAttribute('value', source.value);
      target.value = source.value;
      if (source.checked) {
        target.setAttribute('checked', 'checked');
      } else {
        target.removeAttribute('checked');
      }
      return;
    }
    if (source instanceof HTMLSelectElement && target instanceof HTMLSelectElement) {
      target.value = source.value;
      Array.from(target.options).forEach((option) => {
        option.selected = option.value === source.value;
      });
    }
  });
}

function replaceCloneCanvases(sourceRoot, cloneRoot) {
  const sourceCanvases = sourceRoot.querySelectorAll('canvas');
  const cloneCanvases = cloneRoot.querySelectorAll('canvas');
  sourceCanvases.forEach((source, index) => {
    const target = cloneCanvases[index];
    if (!(source instanceof HTMLCanvasElement) || !(target instanceof HTMLCanvasElement)) return;
    const img = document.createElement('img');
    try {
      img.src = source.toDataURL('image/png');
    } catch (_) {
      return;
    }
    img.width = source.width;
    img.height = source.height;
    img.style.cssText = target.style.cssText;
    img.className = target.className;
    target.replaceWith(img);
  });
}

function removeCloneNoise(cloneRoot) {
  cloneRoot.querySelectorAll('#bug-report-button, #bug-report-sheet').forEach((node) => node.remove());
}

async function loadImageFromURL(url) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error('image load failed'));
    img.src = url;
  });
}

function fallbackScreenshotReasonLabel(reason) {
  if (reason === 'firefox') return 'Live screenshot unavailable in Firefox';
  if (reason === 'loading') return 'Preparing bug report preview';
  if (reason === 'timeout') return 'Live screenshot timed out';
  if (reason === 'render-error') return 'Live screenshot failed';
  return 'Using browser-safe preview';
}

function currentBugReportUserAgent() {
  const testEnv = bugReportTestEnv();
  const forced = safeText(testEnv.userAgent || testEnv.forceUserAgent);
  return forced || safeText(navigator.userAgent);
}

function shouldPreferBrowserSafeBugReportPreview() {
  const testEnv = bugReportTestEnv();
  if (testEnv.forceLiveCapture === true) return false;
  if (testEnv.forceFallbackCapture === true) return true;
  return /(firefox|fxios)/i.test(currentBugReportUserAgent());
}

function setBugReportPreviewMode(mode) {
  const { previewFrame, preview } = bugReportNodes();
  if (previewFrame instanceof HTMLElement) {
    previewFrame.dataset.captureMode = mode;
  }
  if (preview instanceof HTMLImageElement) {
    preview.dataset.captureMode = mode;
  }
}

function applyBugReportPreview(dataURL, mode) {
  const { preview } = bugReportNodes();
  setBugReportPreviewMode(mode);
  if (preview instanceof HTMLImageElement) {
    preview.src = dataURL;
  }
}

function buildFallbackScreenshotDataURL(reason = '') {
  const canvas = document.createElement('canvas');
  canvas.width = 1280;
  canvas.height = 720;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  const gradient = ctx.createLinearGradient(0, 0, canvas.width, canvas.height);
  gradient.addColorStop(0, '#ede1cc');
  gradient.addColorStop(1, '#d8ccb5');
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = '#17120f';
  ctx.fillRect(36, 36, canvas.width - 72, canvas.height - 72);
  ctx.fillStyle = '#f7f0e2';
  ctx.fillRect(52, 52, canvas.width - 104, canvas.height - 104);
  ctx.fillStyle = '#8a3b12';
  ctx.fillRect(52, 52, canvas.width - 104, 18);
  ctx.fillStyle = '#17120f';
  ctx.font = 'bold 34px monospace';
  ctx.fillText('Tabura Bug Report Preview', 84, 134);
  ctx.font = '24px monospace';
  ctx.fillText(fallbackScreenshotReasonLabel(reason), 84, 186);
  ctx.font = '20px monospace';
  [
    `time: ${formatNow()}`,
    `artifact: ${safeText(getActiveArtifactTitle()) || 'none'}`,
    `tool: ${safeText(state.interaction?.tool) || 'unknown'}`,
    `project: ${safeText(state.activeWorkspaceId) || 'none'}`,
    `browser: ${currentBugReportUserAgent() || 'unknown'}`,
  ].forEach((line, index) => {
    ctx.fillText(line, 84, 248 + (index * 36));
  });
  return canvas.toDataURL('image/png');
}

async function captureViewportScreenshotFromClone() {
  const testEnv = bugReportTestEnv();
  if (safeText(testEnv.screenshotDataURL)) {
    return { dataURL: safeText(testEnv.screenshotDataURL), mode: 'test' };
  }
  const root = document.getElementById('view-main') || document.body;
  const rect = root.getBoundingClientRect();
  const width = Math.max(1, Math.round(rect.width || window.innerWidth || 1));
  const height = Math.max(1, Math.round(rect.height || window.innerHeight || 1));
  const clone = root.cloneNode(true);
  if (!(clone instanceof HTMLElement)) {
    throw new Error('clone failed');
  }
  syncCloneInputs(root, clone);
  replaceCloneCanvases(root, clone);
  removeCloneNoise(clone);
  const styles = collectStyleText();
  const svg = `
    <svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">
      <foreignObject width="100%" height="100%">
        <div xmlns="http://www.w3.org/1999/xhtml">
          <style>${styles}</style>
          ${clone.outerHTML}
        </div>
      </foreignObject>
    </svg>
  `;
  try {
    const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    try {
      const img = await loadImageFromURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');
      if (!ctx) throw new Error('canvas unavailable');
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, width, height);
      ctx.drawImage(img as CanvasImageSource, 0, 0);
      return { dataURL: canvas.toDataURL('image/png'), mode: 'live' };
    } finally {
      URL.revokeObjectURL(url);
    }
  } catch (err) {
    throw err;
  }
}

async function captureViewportScreenshot() {
  const testEnv = bugReportTestEnv();
  if (safeText(testEnv.screenshotDataURL)) {
    return { dataURL: safeText(testEnv.screenshotDataURL), mode: 'test' };
  }
  if (shouldPreferBrowserSafeBugReportPreview()) {
    return { dataURL: buildFallbackScreenshotDataURL('firefox'), mode: 'fallback-firefox' };
  }
  let timer = 0;
  try {
    const result = await Promise.race([
      captureViewportScreenshotFromClone(),
      new Promise((_, reject) => {
        timer = window.setTimeout(() => reject(new Error('timeout')), BUG_REPORT_CAPTURE_TIMEOUT_MS);
      }),
    ]);
    return result;
  } catch (err) {
    const reason = safeText(err?.message).toLowerCase() === 'timeout' ? 'timeout' : 'render-error';
    return { dataURL: buildFallbackScreenshotDataURL(reason), mode: `fallback-${reason}` };
  } finally {
    if (timer) {
      window.clearTimeout(timer);
    }
  }
}

function exportAnnotatedImageDataURL() {
  if (!pendingReport) {
    return '';
  }
  if (!Array.isArray(pendingReport.strokes) || pendingReport.strokes.length === 0) {
    return pendingReport.screenshotDataURL || '';
  }
  const { preview, ink } = bugReportNodes();
  if (!(preview instanceof HTMLImageElement) || !(ink instanceof HTMLCanvasElement)) return '';
  const width = preview.naturalWidth || ink.width || 1;
  const height = preview.naturalHeight || ink.height || 1;
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(preview, 0, 0, width, height);
  const scaleX = width / Math.max(1, ink.width);
  const scaleY = height / Math.max(1, ink.height);
  ctx.lineJoin = 'round';
  ctx.lineCap = 'round';
  ctx.strokeStyle = BUG_REPORT_STROKE_COLOR;
  ctx.lineWidth = BUG_REPORT_STROKE_WIDTH * ((scaleX + scaleY) / 2);
  pendingReport.strokes.forEach((stroke) => {
    if (!Array.isArray(stroke.points) || stroke.points.length === 0) return;
    ctx.beginPath();
    stroke.points.forEach((point, index) => {
      const x = point.x * scaleX;
      const y = point.y * scaleY;
      if (index === 0) {
        ctx.moveTo(x, y);
        return;
      }
      ctx.lineTo(x, y);
    });
    ctx.stroke();
  });
  return canvas.toDataURL('image/png');
}

function updateVoiceNoteButton() {
  const { record } = bugReportNodes();
  if (!(record instanceof HTMLButtonElement)) return;
  record.textContent = noteRecording ? 'Stop Voice' : 'Voice Note';
  record.classList.toggle('is-recording', Boolean(noteRecording));
}

async function startBugReportVoiceNote() {
  if (noteRecording) return;
  const stream = await acquireMicStream();
  const recorder = newMediaRecorder(stream);
  const mimeType = safeText(recorder?.mimeType) || 'audio/webm';
  await sttStart(mimeType);
  const recording = { recorder, mimeType, stream, cancelled: false };
  noteRecording = recording;
  updateVoiceNoteButton();
  recorder.addEventListener('dataavailable', (event) => {
    if (event.data instanceof Blob && event.data.size > 0) {
      void sttSendBlob(event.data);
    }
  });
  recorder.addEventListener('stop', async () => {
    if (recording.cancelled) {
      noteRecording = null;
      updateVoiceNoteButton();
      return;
    }
    try {
      const result = await sttStop();
      const transcript = safeText(result?.text);
      if (pendingReport) {
        pendingReport.voiceTranscript = transcript;
      }
      const { note } = bugReportNodes();
      if (transcript && note instanceof HTMLTextAreaElement) {
        note.value = note.value.trim()
          ? `${note.value.trim()}\n${transcript}`
          : transcript;
      }
      showStatus(transcript ? 'voice note added' : 'voice note empty');
    } catch (err) {
      showStatus(`voice note failed: ${safeText(err?.message || err) || 'unknown error'}`);
    } finally {
      noteRecording = null;
      updateVoiceNoteButton();
    }
  }, { once: true });
  recorder.start(250);
  showStatus('voice note recording');
}

async function stopBugReportVoiceNote(cancel = false) {
  if (!noteRecording) return;
  const active = noteRecording;
  noteRecording = null;
  updateVoiceNoteButton();
  if (cancel) {
    active.cancelled = true;
    sttCancel();
  }
  try {
    if (active.recorder && active.recorder.state !== 'inactive') {
      active.recorder.stop();
    }
  } catch (_) {}
}

async function snapshotBugReportContext(trigger, runtime) {
  const app = (window as any)._taburaApp;
  const dialogueDiagnostics = typeof app?.getDialogueDiagnostics === 'function'
    ? app.getDialogueDiagnostics()
    : null;
  const meetingDiagnostics = await captureMeetingBugReportDiagnostics();
  const recent = recentEvents.slice();
  const inputMode = resolveBugReportInputMode(trigger, recent);
  return {
    trigger,
    timestamp: formatNow(),
    pageURL: window.location.href,
    version: safeText(runtime?.version),
    bootID: safeText(runtime?.boot_id),
    startedAt: safeText(runtime?.started_at),
    activeMode: inputMode,
    canvasState: buildCanvasState(inputMode),
    recentEvents: recent,
    browserLogs: browserLogs.slice(),
    device: await buildDeviceState(),
    dialogueDiagnostics,
    meetingDiagnostics,
    screenshotDataURL: '',
    strokes: [],
    voiceTranscript: '',
  };
}

async function captureMeetingBugReportDiagnostics() {
  if (safeText(state.livePolicy).toLowerCase() !== 'meeting') return null;
  let participantStatus = null;
  try {
    const resp = await fetch(apiURL('participant/status'));
    if (resp.ok) {
      participantStatus = await resp.json();
    }
  } catch (_) {}
  return {
    live_policy: safeText(state.livePolicy),
    live_session_mode: safeText(state.liveSessionMode),
    companion_runtime_state: safeText(state.companionRuntimeState),
    companion_runtime_reason: safeText(state.companionRuntimeReason),
    turn_policy_profile: safeText(state.turnPolicyProfile),
    participant_status: participantStatus,
  };
}

async function openBugReport(trigger) {
  ensureBugReportUi();
  const runtime = await fetchRuntimeMeta().catch(() => ({}));
  const report = await snapshotBugReportContext(trigger, runtime);
  report.screenshotDataURL = buildFallbackScreenshotDataURL('loading');
  pendingReport = report;
  applyBugReportPreview(report.screenshotDataURL, 'fallback-loading');
  const { preview, note } = bugReportNodes();
  if (preview instanceof HTMLImageElement) {
    preview.onload = () => syncBugReportCanvasSize();
  }
  if (note instanceof HTMLTextAreaElement) {
    note.value = '';
    window.setTimeout(() => note.focus(), 0);
  }
  clearBugReportInk();
  openBugReportSheet();
  window.requestAnimationFrame(() => syncBugReportCanvasSize());
  showStatus('bug report captured');
  const capture = await captureViewportScreenshot() as any;
  if (pendingReport !== report) return;
  report.screenshotDataURL = capture.dataURL;
  applyBugReportPreview(capture.dataURL, capture.mode);
  syncBugReportCanvasSize();
}

export function isInlineBugReportTrigger(text) {
  const clean = safeText(text).toLowerCase();
  return /^(report bug|bug report|report a bug|das ist kaputt)[.!?]*$/.test(clean);
}

export async function maybeHandleInlineBugReport(text, options: Record<string, any> = {}) {
  if (!isInlineBugReportTrigger(text)) return false;
  const trigger = safeText(options?.trigger) || 'voice';
  recordRecentEvent(`bug report trigger ${trigger}`);
  await openBugReport(trigger);
  return true;
}

async function saveBugReport() {
  if (!pendingReport) return;
  const { note, save } = bugReportNodes();
  const body = {
    trigger: pendingReport.trigger,
    timestamp: pendingReport.timestamp,
    page_url: pendingReport.pageURL,
    version: pendingReport.version,
    boot_id: pendingReport.bootID,
    started_at: pendingReport.startedAt,
    active_mode: pendingReport.activeMode,
    canvas_state: pendingReport.canvasState,
    recent_events: pendingReport.recentEvents,
    browser_logs: pendingReport.browserLogs,
    device: pendingReport.device,
    dialogue_diagnostics: pendingReport.dialogueDiagnostics,
    meeting_diagnostics: pendingReport.meetingDiagnostics,
    note: note instanceof HTMLTextAreaElement ? note.value.trim() : '',
    voice_transcript: pendingReport.voiceTranscript,
    screenshot_data_url: pendingReport.screenshotDataURL,
    annotated_data_url: exportAnnotatedImageDataURL(),
  };
  if (save instanceof HTMLButtonElement) {
    save.disabled = true;
  }
  try {
    const resp = await fetch(apiURL('bugs/report'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      const detail = safeText(await resp.text()) || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    const issueNumber = Number(payload?.issue_number || 0);
    const issueError = safeText(payload?.issue_error);
    const filed = issueNumber > 0 || safeText(payload?.issue_url);
    closeBugReportSheet();
    renderCanvas({
      kind: 'text_artifact',
      event_id: `bug-report-${Date.now()}`,
      title: safeText(payload?.issue_title) || (filed ? 'Bug report filed' : 'Bug report saved locally'),
      text: bugReportIssueCanvasMarkdown(payload),
    });
    showCanvasColumn('canvas-text');
    try {
      await refreshBugReportInboxState();
    } catch (refreshErr) {
      console.warn('bug report inbox refresh failed', refreshErr);
    }
    if (issueNumber > 0) {
      showStatus(`bug report filed: #${issueNumber}`);
    } else if (issueError) {
      showStatus(`bug report saved locally: ${safeText(payload?.bundle_path) || 'ok'}`);
    } else {
      showStatus(`bug report saved locally: ${safeText(payload?.bundle_path) || 'ok'}`);
    }
  } catch (err) {
    showStatus(`bug bundle failed: ${safeText(err?.message || err) || 'unknown error'}`);
  } finally {
    if (save instanceof HTMLButtonElement) {
      save.disabled = false;
    }
  }
}

async function refreshBugReportInboxState() {
  if (!String(state.activeWorkspaceId || '').trim()) return false;
  if (state.prReviewDrawerOpen && state.fileSidebarMode === 'items' && String(state.itemSidebarView || '').trim().toLowerCase() === 'inbox') {
    return loadItemSidebarView('inbox');
  }
  return refreshItemSidebarCounts();
}

function onBugReportPointerDown(event) {
  if (!pendingReport) return;
  const { ink } = bugReportNodes();
  if (!(ink instanceof HTMLCanvasElement)) return;
  const rect = ink.getBoundingClientRect();
  const x = event.clientX - rect.left;
  const y = event.clientY - rect.top;
  if (x < 0 || y < 0 || x > rect.width || y > rect.height) return;
  activeStroke = { points: [{ x, y }] };
  ink.setPointerCapture(event.pointerId);
  redrawBugReportInk();
  event.preventDefault();
}

function onBugReportPointerMove(event) {
  if (!activeStroke) return;
  const { ink } = bugReportNodes();
  if (!(ink instanceof HTMLCanvasElement)) return;
  const rect = ink.getBoundingClientRect();
  const x = event.clientX - rect.left;
  const y = event.clientY - rect.top;
  activeStroke.points.push({ x, y });
  redrawBugReportInk();
  event.preventDefault();
}

function finishBugReportStroke() {
  if (!activeStroke || !pendingReport) return;
  if (!Array.isArray(pendingReport.strokes)) {
    pendingReport.strokes = [];
  }
  pendingReport.strokes.push(activeStroke);
  activeStroke = null;
  redrawBugReportInk();
}

function cancelTwoFingerHold() {
  if (!twoFingerHold) return;
  window.clearTimeout(twoFingerHold.timer);
  twoFingerHold = null;
}

function installTwoFingerHold() {
  document.addEventListener('touchstart', (event) => {
    if (document.body.classList.contains('bug-report-open')) return;
    if (event.touches.length !== 2) {
      cancelTwoFingerHold();
      return;
    }
    const points = Array.from(event.touches).map((touch) => ({ x: touch.clientX, y: touch.clientY }));
    const timer = window.setTimeout(() => {
      cancelTwoFingerHold();
      void openBugReport('gesture');
    }, BUG_REPORT_TOUCH_HOLD_MS);
    twoFingerHold = { timer, points };
  }, { passive: true });
  document.addEventListener('touchmove', (event) => {
    if (!twoFingerHold || event.touches.length !== 2) {
      cancelTwoFingerHold();
      return;
    }
    const moved = Array.from(event.touches).some((touch, index) => {
      const start = twoFingerHold.points[index];
      if (!start) return true;
      return Math.abs(touch.clientX - start.x) > 18 || Math.abs(touch.clientY - start.y) > 18;
    });
    if (moved) {
      cancelTwoFingerHold();
    }
  }, { passive: true });
  ['touchend', 'touchcancel'].forEach((type) => {
    document.addEventListener(type, () => cancelTwoFingerHold(), { passive: true });
  });
}

function bindBugReportUi() {
  const { button, preview, ink, record, cancel, save, clear } = bugReportNodes();
  if (!(button instanceof HTMLButtonElement) || !(preview instanceof HTMLImageElement) || !(ink instanceof HTMLCanvasElement)) {
    return;
  }
  button.addEventListener('click', () => {
    recordRecentEvent('bug report button');
    void openBugReport('button');
  });
  preview.addEventListener('load', () => syncBugReportCanvasSize());
  ink.addEventListener('pointerdown', onBugReportPointerDown);
  ink.addEventListener('pointermove', onBugReportPointerMove);
  ink.addEventListener('pointerup', finishBugReportStroke);
  ink.addEventListener('pointercancel', finishBugReportStroke);
  window.addEventListener('resize', () => syncBugReportCanvasSize());
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && document.body.classList.contains('bug-report-open')) {
      event.preventDefault();
      closeBugReportSheet();
      return;
    }
    if (safeText(event.key).toLowerCase() !== BUG_REPORT_SHORTCUT_KEY) return;
    if (!(event.ctrlKey && event.altKey) || event.metaKey) return;
    event.preventDefault();
    recordRecentEvent('bug report shortcut');
    void openBugReport('shortcut');
  }, true);
  if (record instanceof HTMLButtonElement) {
    record.addEventListener('click', () => {
      if (noteRecording) {
        void stopBugReportVoiceNote(false);
        return;
      }
      void startBugReportVoiceNote().catch((err) => {
        noteRecording = null;
        updateVoiceNoteButton();
        showStatus(`voice note failed: ${safeText(err?.message || err) || 'unknown error'}`);
      });
    });
  }
  if (cancel instanceof HTMLButtonElement) {
    cancel.addEventListener('click', () => closeBugReportSheet());
  }
  if (save instanceof HTMLButtonElement) {
    save.addEventListener('click', () => {
      void saveBugReport();
    });
  }
  if (clear instanceof HTMLButtonElement) {
    clear.addEventListener('click', () => clearBugReportInk());
  }
}

export function initBugReportUi() {
  if (bugReportUiReady) return;
  bugReportUiReady = true;
  ensureBugReportUi();
  installBrowserLogCapture();
  installInteractionCapture();
  installTwoFingerHold();
  bindBugReportUi();
}
