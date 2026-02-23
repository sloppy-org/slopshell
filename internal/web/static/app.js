import { marked } from './vendor/marked.esm.js';
import { renderCanvas, clearCanvas, getLocationFromPoint, getLocationFromSelection, showLineHighlight, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveTextEventId } from './canvas.js';
import {
  getZenState, setZenMode,
  showIndicator, hideIndicator,
  showTextInput, hideTextInput,
  showOverlay, hideOverlay, updateOverlay,
  isOverlayVisible, isTextInputVisible, isRecording, setRecording,
  getInputAnchor, setInputAnchor, getAnchorFromPoint,
  buildContextPrefix, getLastInputPosition,
} from './zen.js';

const state = {
  sessionId: 'local',
  canvasWs: null,
  chatWs: null,
  chatWsToken: 0,
  canvasWsToken: 0,
  chatWsHasConnected: false,
  chatSessionId: '',
  chatMode: 'chat',
  hasArtifact: false,
  projects: [],
  defaultProjectId: '',
  serverActiveProjectId: '',
  activeProjectId: '',
  projectsOpen: false,
  projectSwitchInFlight: false,
  pendingByTurn: new Map(),
  pendingQueue: [],
  assistantActiveTurns: new Set(),
  assistantUnknownTurns: 0,
  assistantRemoteActiveCount: 0,
  assistantRemoteQueuedCount: 0,
  assistantCancelInFlight: false,
  assistantLastError: '',
  chatCtrlHoldTimer: null,
  chatVoiceCapture: null,
  contextUsed: 0,
  contextMax: 0,
  // Zen-specific: track if a canvas action happened during this turn
  zenCanvasActionThisTurn: false,
};

export function getState() {
  return state;
}

window._taburaApp = { getState, acquireMicStream, sttStart, sttSendBlob, sttStop, sttCancel };

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_CHAT_MATH_SEGMENT_';
const DEV_UI_RELOAD_POLL_MS = 1500;
const ASSISTANT_ACTIVITY_POLL_MS = 1200;
let localMessageSeq = 0;
const CHAT_CTRL_LONG_PRESS_MS = 180;
const CHAT_SEND_HOLD_MS = 300;
let devReloadBootID = '';
let devReloadTimer = null;
let devReloadInFlight = false;
let devReloadRequested = false;
let assistantActivityTimer = null;
let assistantActivityInFlight = false;

const ACTIVE_PROJECT_STORAGE_KEY = 'tabura.activeProjectId';
const LAST_VIEW_STORAGE_KEY = 'tabura.lastView';

const renderer = new marked.Renderer();
renderer.code = ({ text, lang }) => {
  const safeLang = escapeHtml((lang || 'plaintext').toLowerCase());
  return `<pre><code class="language-${safeLang}">${escapeHtml(text || '')}</code></pre>\n`;
};
marked.setOptions({ breaks: true, renderer });

function extractMathSegments(markdownSource) {
  const source = String(markdownSource || '');
  const stash = [];
  let text = source;
  const patterns = [
    /\$\$[\s\S]+?\$\$/g,
    /\\\[[\s\S]+?\\\]/g,
    /\\\([\s\S]+?\\\)/g,
  ];
  for (const pattern of patterns) {
    text = text.replace(pattern, (segment) => {
      const token = `${MATH_SEGMENT_TOKEN_PREFIX}${stash.length}@@`;
      stash.push(segment);
      return token;
    });
  }
  return { text, stash };
}

function restoreMathSegments(renderedHtml, mathSegments) {
  let output = String(renderedHtml || '');
  for (let i = 0; i < mathSegments.length; i += 1) {
    const token = `${MATH_SEGMENT_TOKEN_PREFIX}${i}@@`;
    output = output.replaceAll(token, escapeHtml(String(mathSegments[i] || '')));
  }
  return output;
}

function typesetMath(root, attempt = 0) {
  if (!(root instanceof Element) || !root.isConnected) return Promise.resolve();
  const mj = window.MathJax;
  if (!mj || typeof mj.typesetPromise !== 'function') {
    if (attempt >= 40) return Promise.resolve();
    return new Promise((resolve) => {
      window.setTimeout(() => {
        void typesetMath(root, attempt + 1).then(resolve);
      }, 75);
    });
  }
  const startupReady = mj.startup?.promise && typeof mj.startup.promise.then === 'function'
    ? mj.startup.promise
    : Promise.resolve();
  return startupReady
    .then(() => {
      if (!root.isConnected) return undefined;
      return mj.typesetPromise([root]);
    })
    .catch(() => {});
}

function showStatus(text) {
  const el = document.getElementById('status-text');
  if (el) el.textContent = text;
  const zenEl = document.getElementById('zen-status');
  if (zenEl) zenEl.textContent = text;
}

function forceUiHardReload() {
  const url = new URL(window.location.href);
  url.searchParams.set('__tabura_reload', Date.now().toString(36));
  window.location.replace(url.toString());
}

async function fetchRuntimeMeta() {
  const resp = await fetch('/api/runtime', {
    cache: 'no-store',
    headers: { 'Cache-Control': 'no-cache' },
  });
  if (!resp.ok) {
    throw new Error(`runtime metadata failed: HTTP ${resp.status}`);
  }
  return resp.json();
}

async function pollRuntimeForDevReload() {
  if (devReloadInFlight || devReloadRequested) return;
  devReloadInFlight = true;
  try {
    const runtime = await fetchRuntimeMeta();
    const isDevMode = Boolean(runtime?.dev_mode);
    const bootID = String(runtime?.boot_id || '').trim();
    if (!isDevMode) return;
    if (!bootID) return;
    if (!devReloadBootID) {
      devReloadBootID = bootID;
      return;
    }
    if (devReloadBootID !== bootID) {
      devReloadRequested = true;
      showStatus('UI changed; reloading...');
      forceUiHardReload();
    }
  } catch (_) {
    // Ignore transient runtime probe errors during service restarts.
  } finally {
    devReloadInFlight = false;
  }
}

function startDevReloadWatcher() {
  if (devReloadTimer !== null) return;
  const tick = () => {
    void pollRuntimeForDevReload();
  };
  devReloadTimer = window.setInterval(tick, DEV_UI_RELOAD_POLL_MS);
  tick();
  window.addEventListener('focus', tick);
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) tick();
  });
}

function isEditableTarget(target) {
  if (!(target instanceof Element)) return false;
  return Boolean(target.closest('input,textarea,select,[contenteditable="true"]'));
}

function activeProject() {
  return state.projects.find((project) => project.id === state.activeProjectId) || null;
}

function persistActiveProjectID(projectID) {
  if (!projectID) return;
  try {
    window.localStorage.setItem(ACTIVE_PROJECT_STORAGE_KEY, projectID);
  } catch (_) {}
}

function readPersistedProjectID() {
  try {
    return String(window.localStorage.getItem(ACTIVE_PROJECT_STORAGE_KEY) || '').trim();
  } catch (_) {
    return '';
  }
}

function persistLastView(view) {
  try {
    window.localStorage.setItem(LAST_VIEW_STORAGE_KEY, JSON.stringify(view));
  } catch (_) {}
}

function readPersistedLastView() {
  try {
    return JSON.parse(window.localStorage.getItem(LAST_VIEW_STORAGE_KEY) || 'null');
  } catch (_) {
    return null;
  }
}

function setActiveProjectID(projectID) {
  state.activeProjectId = String(projectID || '').trim();
  if (state.activeProjectId) {
    persistActiveProjectID(state.activeProjectId);
  }
  renderEdgeTopProjects();
}


function newMediaRecorder(stream) {
  const candidates = [
    'audio/ogg;codecs=opus',
    'audio/webm;codecs=opus',
  ];
  const isSupported = typeof window.MediaRecorder?.isTypeSupported === 'function'
    ? (t) => window.MediaRecorder.isTypeSupported(t)
    : () => false;
  for (const mt of candidates) {
    if (isSupported(mt)) {
      try {
        return new window.MediaRecorder(stream, { mimeType: mt });
      } catch (_) { /* try next */ }
    }
  }
  return new window.MediaRecorder(stream);
}


function canUseMicrophoneCapture() {
  return Boolean(window.MediaRecorder)
    && Boolean(navigator.mediaDevices)
    && typeof navigator.mediaDevices.getUserMedia === 'function';
}

let _cachedMicStream = null;
let _micStreamPromise = null;

function acquireMicStream() {
  if (_cachedMicStream) {
    const tracks = _cachedMicStream.getAudioTracks();
    if (tracks.length > 0 && tracks[0].readyState === 'live') {
      return Promise.resolve(_cachedMicStream);
    }
    _cachedMicStream = null;
  }
  if (_micStreamPromise) return _micStreamPromise;
  _micStreamPromise = navigator.mediaDevices.getUserMedia({
    audio: { echoCancellation: true, autoGainControl: true, noiseSuppression: true },
  }).then((stream) => {
    _cachedMicStream = stream;
    _micStreamPromise = null;
    return stream;
  }).catch((err) => {
    _micStreamPromise = null;
    throw err;
  });
  return _micStreamPromise;
}

function releaseMicStream() {
  if (!_cachedMicStream) return;
  _cachedMicStream.getTracks().forEach((t) => t.stop());
  _cachedMicStream = null;
}

let _sttResolve = null;
let _sttReject = null;
let _sttActive = false;

function sttStart(mimeType) {
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    return Promise.reject(new Error('chat WebSocket not connected'));
  }
  _sttActive = true;
  ws.send(JSON.stringify({ type: 'stt_start', mime_type: mimeType || 'audio/webm' }));
}

function sttSendBlob(blob) {
  if (!_sttActive) return Promise.resolve();
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) return Promise.resolve();
  if (!blob || blob.size <= 0) return Promise.resolve();
  return blob.arrayBuffer().then((buf) => {
    if (!state.chatWs || state.chatWs.readyState !== WebSocket.OPEN) return;
    state.chatWs.send(buf);
  });
}

function sttStop() {
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    _sttActive = false;
    return Promise.reject(new Error('chat WebSocket not connected'));
  }
  _sttActive = false;
  return new Promise((resolve, reject) => {
    _sttResolve = resolve;
    _sttReject = reject;
    ws.send(JSON.stringify({ type: 'stt_stop' }));
  });
}

function sttCancel() {
  _sttActive = false;
  if (_sttReject) {
    _sttReject(new Error('STT cancelled'));
    _sttResolve = null;
    _sttReject = null;
  }
  const ws = state.chatWs;
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'stt_cancel' }));
  }
}

function handleSTTWSMessage(payload) {
  const type = String(payload?.type || '');
  if (type === 'stt_result') {
    if (_sttResolve) {
      _sttResolve({ text: payload.text || '' });
      _sttResolve = null;
      _sttReject = null;
    }
    return true;
  }
  if (type === 'stt_error') {
    if (_sttReject) {
      _sttReject(new Error(payload.error || 'STT failed'));
      _sttResolve = null;
      _sttReject = null;
    }
    return true;
  }
  if (type === 'stt_started' || type === 'stt_cancelled') {
    return true;
  }
  return false;
}

function stopChatVoiceMedia(capture) {
  if (!capture) return;
  if (capture.mediaRecorder) {
    try {
      if (capture.mediaRecorder.state !== 'inactive') {
        capture.mediaRecorder.stop();
      }
    } catch (_) {}
  }
  capture.mediaRecorder = null;
  capture.mediaStream = null;
  releaseMicStream();
}

function stopChatVoiceMediaAndFlush(capture) {
  if (!capture?.mediaRecorder) {
    stopChatVoiceMedia(capture);
    return Promise.resolve();
  }
  const recorder = capture.mediaRecorder;
  if (recorder.state === 'inactive') {
    stopChatVoiceMedia(capture);
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const onStop = () => {
      recorder.removeEventListener('error', onError);
      stopChatVoiceMedia(capture);
      resolve();
    };
    const onError = () => {
      recorder.removeEventListener('stop', onStop);
      stopChatVoiceMedia(capture);
      resolve();
    };
    recorder.addEventListener('stop', onStop, { once: true });
    recorder.addEventListener('error', onError, { once: true });
    try {
      recorder.stop();
    } catch (_) {
      recorder.removeEventListener('stop', onStop);
      recorder.removeEventListener('error', onError);
      stopChatVoiceMedia(capture);
      resolve();
    }
  });
}

async function beginZenVoiceCapture(x, y, anchor) {
  if (state.chatVoiceCapture) return;
  if (!canUseMicrophoneCapture()) return;
  const capture = {
    active: false,
    stopping: false,
    stopRequested: false,
    autoSend: true,
    mediaStream: null,
    mediaRecorder: null,
    chunks: [],
  };
  state.chatVoiceCapture = capture;
  setRecording(true);
  setInputAnchor(anchor || null);
  showIndicator(x, y);
  showStatus('recording...');
  try {
    const stream = await acquireMicStream();
    if (state.chatVoiceCapture !== capture) return;
    const recorder = newMediaRecorder(stream);
    capture.mimeType = recorder.mimeType || 'audio/webm';
    if (state.chatVoiceCapture !== capture) return;
    capture.mediaStream = stream;
    capture.mediaRecorder = recorder;
    capture.active = true;
    recorder.addEventListener('dataavailable', (ev) => {
      if (!ev?.data || ev.data.size <= 0) return;
      capture.chunks.push(ev.data);
    });
    recorder.start();
    if (capture.stopRequested) {
      void stopZenVoiceCaptureAndSend();
    }
  } catch (err) {
    setRecording(false);
    hideIndicator();
    const message = String(err?.message || err || 'voice capture failed');
    showStatus(`voice capture failed: ${message}`);
    sttCancel();
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
  }
}

async function stopZenVoiceCaptureAndSend() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  capture.stopRequested = true;
  if (!capture.active) return;
  capture.stopping = true;
  let remoteStopped = false;
  try {
    await stopChatVoiceMediaAndFlush(capture);
    const mimeType = capture.mimeType || 'audio/webm';
    sttStart(mimeType);
    if (capture.chunks.length > 0) {
      const blob = new Blob(capture.chunks, { type: mimeType });
      capture.chunks = [];
      await sttSendBlob(blob);
    }
    const result = await sttStop();
    remoteStopped = true;
    const transcript = String(result?.text || '').trim();
    if (!transcript) {
      throw new Error('speech recognizer returned empty text');
    }
    showStatus('sending...');
    void zenSubmitMessage(transcript);
  } catch (err) {
    const message = String(err?.message || err || 'voice capture failed');
    showStatus(`voice capture failed: ${message}`);
  } finally {
    setRecording(false);
    hideIndicator();
    if (!remoteStopped) {
      sttCancel();
    }
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
  }
}

function cancelChatVoiceCapture() {
  const capture = state.chatVoiceCapture;
  if (!capture) return;
  setRecording(false);
  hideIndicator();
  sttCancel();
  stopChatVoiceMedia(capture);
  state.chatVoiceCapture = null;
}

function showCanvasColumn(paneId) {
  const col = document.getElementById('canvas-column');
  if (!col) return;
  const viewport = col.querySelector('#canvas-viewport');
  if (viewport) {
    viewport.querySelectorAll('.canvas-pane').forEach((p) => {
      p.style.display = 'none';
      p.classList.remove('is-active');
    });
    const target = document.getElementById(paneId);
    if (target) {
      target.style.display = '';
      target.classList.add('is-active');
    }
  }
  state.hasArtifact = true;
  setZenMode('artifact');
  persistLastView({ mode: 'artifact' });
}

function hideCanvasColumn() {
  state.hasArtifact = false;
  setZenMode('rasa');
  clearLineHighlight();
  persistLastView({ mode: 'rasa' });
  // In zen mode, hide all panes to show blank canvas
  const viewport = document.getElementById('canvas-viewport');
  if (viewport) {
    viewport.querySelectorAll('.canvas-pane').forEach((p) => {
      p.style.display = 'none';
      p.classList.remove('is-active');
    });
  }
}

function chatHistoryEl() {
  return document.getElementById('chat-history');
}

function scrollChatToBottom(host) {
  if (!(host instanceof HTMLElement)) return;
  host.scrollTop = host.scrollHeight;
}

function syncChatScroll(host) {
  if (!(host instanceof HTMLElement)) return;
  scrollChatToBottom(host);
  window.requestAnimationFrame(() => scrollChatToBottom(host));
}

function setChatMode(mode) {
  state.chatMode = String(mode || 'chat').toLowerCase() === 'plan' ? 'plan' : 'chat';
  const pill = document.getElementById('chat-mode-pill');
  if (pill) {
    pill.textContent = state.chatMode;
    pill.className = `badge ${state.chatMode === 'plan' ? 'review' : ''}`;
  }
}

function hasLocalAssistantWork() {
  return state.pendingQueue.length > 0
    || state.pendingByTurn.size > 0
    || state.assistantActiveTurns.size > 0
    || state.assistantUnknownTurns > 0;
}

function isAssistantWorking() {
  return hasLocalAssistantWork()
    || state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0
    || state.assistantCancelInFlight;
}

function updateAssistantActivityIndicator() {
  if (!hasLocalAssistantWork() && state.assistantRemoteActiveCount <= 0 && state.assistantRemoteQueuedCount <= 0) {
    state.assistantUnknownTurns = 0;
    state.assistantActiveTurns.clear();
  }
}

function trackAssistantTurnStarted(turnID) {
  state.assistantLastError = '';
  const key = String(turnID || '').trim();
  if (key) {
    state.assistantActiveTurns.add(key);
  } else {
    state.assistantUnknownTurns += 1;
  }
  updateAssistantActivityIndicator();
}

function trackAssistantTurnFinished(turnID) {
  const key = String(turnID || '').trim();
  if (key) {
    if (!state.assistantActiveTurns.delete(key) && state.assistantUnknownTurns > 0) {
      state.assistantUnknownTurns -= 1;
    }
  } else if (state.assistantUnknownTurns > 0) {
    state.assistantUnknownTurns -= 1;
  }
  updateAssistantActivityIndicator();
}

function takePendingRow(turnID) {
  const key = String(turnID || '').trim();
  if (key && state.pendingByTurn.has(key)) {
    const row = state.pendingByTurn.get(key);
    state.pendingByTurn.delete(key);
    updateAssistantActivityIndicator();
    return row;
  }
  const row = state.pendingQueue.shift() || null;
  updateAssistantActivityIndicator();
  return row;
}

function nextLocalMessageId() {
  localMessageSeq += 1;
  return `local-msg-${Date.now()}-${localMessageSeq}`;
}

// Chat history log (diagnostics pane)
function appendPlainMessage(role, text, options = {}) {
  const host = chatHistoryEl();
  if (!host) return null;
  const row = document.createElement('div');
  row.className = `chat-message chat-${role}`;
  if (options.pending) row.classList.add('is-pending');
  row.dataset.role = role;
  if (options.turnId) row.dataset.turnId = options.turnId;
  if (options.localId) row.dataset.localId = options.localId;

  const meta = document.createElement('div');
  meta.className = 'chat-message-meta';
  meta.textContent = role;

  const bubble = document.createElement('div');
  bubble.className = 'chat-bubble';
  bubble.textContent = String(text || '');

  row.appendChild(meta);
  row.appendChild(bubble);
  host.appendChild(row);
  syncChatScroll(host);
  return row;
}

function appendRenderedAssistant(markdownText, options = {}) {
  const host = chatHistoryEl();
  if (!host) return null;
  const row = document.createElement('div');
  row.className = 'chat-message chat-assistant';
  if (options.pending) row.classList.add('is-pending');
  row.dataset.role = 'assistant';
  if (options.turnId) row.dataset.turnId = options.turnId;
  if (options.localId) row.dataset.localId = options.localId;

  const meta = document.createElement('div');
  meta.className = 'chat-message-meta';
  meta.textContent = 'assistant';

  const bubble = document.createElement('div');
  bubble.className = 'chat-bubble markdown';
  const { text: markdownBody, stash: mathSegments } = extractMathSegments(markdownText);
  const rendered = marked.parse(markdownBody || '');
  bubble.innerHTML = restoreMathSegments(sanitizeHtml(rendered), mathSegments);
  row.appendChild(meta);
  row.appendChild(bubble);
  host.appendChild(row);
  syncChatScroll(host);
  void typesetMath(bubble).finally(() => syncChatScroll(host));
  return row;
}

function updateAssistantRow(row, markdownText, pending = true) {
  if (!row) return;
  const host = chatHistoryEl();
  row.classList.toggle('is-pending', pending);
  const bubble = row.querySelector('.chat-bubble');
  if (!(bubble instanceof HTMLElement)) return;
  const { text: markdownBody, stash: mathSegments } = extractMathSegments(markdownText);
  const rendered = marked.parse(markdownBody || '');
  bubble.innerHTML = restoreMathSegments(sanitizeHtml(rendered), mathSegments);
  syncChatScroll(host);
  void typesetMath(bubble).finally(() => syncChatScroll(host));
}

function ensurePendingForTurn(turnID) {
  const key = String(turnID || '').trim();
  if (key && state.pendingByTurn.has(key)) {
    return state.pendingByTurn.get(key);
  }
  let row = state.pendingQueue.shift() || null;
  if (!row) {
    row = appendRenderedAssistant('_Thinking..._', { pending: true, localId: nextLocalMessageId() });
  }
  if (key) {
    row.dataset.turnId = key;
    state.pendingByTurn.set(key, row);
  }
  updateAssistantActivityIndicator();
  return row;
}

function resetAssistantTurnTracking({ clearError = false } = {}) {
  state.pendingByTurn.clear();
  state.pendingQueue = [];
  state.assistantActiveTurns.clear();
  state.assistantUnknownTurns = 0;
  state.assistantRemoteActiveCount = 0;
  state.assistantRemoteQueuedCount = 0;
  state.assistantCancelInFlight = false;
  if (clearError) {
    state.assistantLastError = '';
  }
  updateAssistantActivityIndicator();
}

function clearChatHistory() {
  const host = chatHistoryEl();
  if (host) host.innerHTML = '';
}

async function fetchProjects() {
  const resp = await fetch('/api/projects', { cache: 'no-store' });
  if (!resp.ok) throw new Error(`projects list failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const projects = Array.isArray(payload?.projects) ? payload.projects : [];
  state.projects = projects.map((project) => ({
    ...project,
    id: String(project?.id || ''),
  })).filter((project) => project.id);
  state.defaultProjectId = String(payload?.default_project_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_project_id || '').trim();
  renderEdgeTopProjects();
}

function upsertProject(project) {
  if (!project || !project.id) return;
  const index = state.projects.findIndex((item) => item.id === project.id);
  if (index >= 0) {
    state.projects[index] = project;
  } else {
    state.projects.push(project);
  }
}

function resolveInitialProjectID() {
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

function renderEdgeTopProjects() {
  const host = document.getElementById('edge-top-projects');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  for (const project of state.projects) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-project-btn';
    if (project.id === state.activeProjectId) {
      button.classList.add('is-active');
    }
    button.textContent = String(project.name || project.id || 'Project');
    button.title = String(project.root_path || '');
    button.addEventListener('click', () => {
      if (project.id === state.activeProjectId) return;
      void switchProject(project.id);
    });
    host.appendChild(button);
  }
}

async function activateProject(projectID) {
  const resp = await fetch(`/api/projects/${encodeURIComponent(projectID)}/activate`, { method: 'POST' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const project = payload?.project || {};
  state.chatSessionId = String(project.chat_session_id || '');
  state.sessionId = String(project.canvas_session_id || 'local');
  setChatMode(project.chat_mode || 'chat');
  if (!state.chatSessionId) throw new Error('chat session ID missing');
  upsertProject(project);
  return project;
}

async function loadChatHistory() {
  if (!state.chatSessionId) return;
  const host = chatHistoryEl();
  if (!host) return;
  host.innerHTML = '';
  const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/history`);
  if (!resp.ok) throw new Error(`chat history failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const session = payload?.session || {};
  setChatMode(session.mode || state.chatMode);
  const messages = Array.isArray(payload?.messages) ? payload.messages : [];
  for (const msg of messages) {
    const role = String(msg.role || 'assistant').toLowerCase();
    const markdown = String(msg.content_markdown || '');
    const plain = String(msg.content_plain || markdown);
    if (role === 'assistant') {
      appendRenderedAssistant(markdown || plain);
    } else {
      appendPlainMessage(role, plain);
    }
  }
  scrollChatToBottom(host);
  updateAssistantActivityIndicator();
}

async function refreshAssistantActivity() {
  if (!state.chatSessionId || assistantActivityInFlight) return;
  const targetSessionID = state.chatSessionId;
  assistantActivityInFlight = true;
  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(targetSessionID)}/activity`, { cache: 'no-store' });
    if (!resp.ok) return;
    if (targetSessionID !== state.chatSessionId) return;
    const payload = await resp.json();
    const activeTurns = Number(payload?.active_turns || 0);
    const queuedTurns = Number(payload?.queued_turns || 0);
    if (!Number.isFinite(activeTurns) || activeTurns < 0) return;
    if (!Number.isFinite(queuedTurns) || queuedTurns < 0) return;
    state.assistantRemoteActiveCount = activeTurns;
    state.assistantRemoteQueuedCount = queuedTurns;
    updateAssistantActivityIndicator();
  } catch (_) {
  } finally {
    assistantActivityInFlight = false;
  }
}

function startAssistantActivityWatcher() {
  if (assistantActivityTimer !== null) return;
  const tick = () => {
    if (document.hidden) return;
    void refreshAssistantActivity();
  };
  assistantActivityTimer = window.setInterval(tick, ASSISTANT_ACTIVITY_POLL_MS);
  tick();
  window.addEventListener('focus', tick);
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) tick();
  });
}

function closeChatWs() {
  state.chatWsToken += 1;
  if (state.chatWs) {
    try { state.chatWs.close(); } catch (_) {}
  }
  state.chatWs = null;
}

function openChatWs() {
  if (!state.chatSessionId) return;
  const turnToken = state.chatWsToken + 1;
  state.chatWsToken = turnToken;
  const targetSessionID = state.chatSessionId;
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/chat/${encodeURIComponent(targetSessionID)}`;
  const ws = new WebSocket(wsUrl);
  state.chatWs = ws;

  ws.onopen = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    const isReconnect = state.chatWsHasConnected;
    state.chatWsHasConnected = true;
    showStatus('connected');
    void refreshAssistantActivity();
    if (isReconnect) {
      resetAssistantTurnTracking();
      void loadChatHistory().catch((err) => {
        appendPlainMessage('system', `History sync failed: ${String(err?.message || err)}`);
      });
    }
  };

  ws.onmessage = (event) => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    if (typeof event.data !== 'string') return;
    let payload = null;
    try { payload = JSON.parse(event.data); } catch (_) { return; }
    if (handleSTTWSMessage(payload)) return;
    handleChatEvent(payload);
  };

  ws.onclose = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    state.chatWs = null;
    showStatus('reconnecting...');
    window.setTimeout(() => {
      if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
      openChatWs();
    }, 1200);
  };
}

function closeCanvasWs() {
  state.canvasWsToken += 1;
  if (state.canvasWs) {
    try { state.canvasWs.close(); } catch (_) {}
  }
  state.canvasWs = null;
}

function isShortResponse(text) {
  if (!text) return true;
  if (text.length > 500) return false;
  if (/```/.test(text)) return false;
  return true;
}

function handleChatEvent(payload) {
  const type = String(payload?.type || '').trim();
  if (!type) return;

  if (type === 'mode_changed') {
    setChatMode(payload.mode || 'chat');
    const message = String(payload.message || '').trim();
    if (message) appendPlainMessage('system', message);
    return;
  }

  if (type === 'action') {
    const action = String(payload.action || '').trim();
    if (action === 'open_canvas') {
      showCanvasColumn('canvas-text');
      state.zenCanvasActionThisTurn = true;
    } else if (action === 'open_chat') {
      // In zen mode, this just means "no more canvas" - stay on rasa
    }
    return;
  }

  if (type === 'turn_started') {
    trackAssistantTurnStarted(payload.turn_id);
    ensurePendingForTurn(payload.turn_id);
    state.zenCanvasActionThisTurn = false;
    // Show overlay for streaming response
    const pos = getLastInputPosition();
    showOverlay(pos.x, pos.y + 24);
    updateOverlay('_Thinking..._');
    getZenState().overlayTurnId = payload.turn_id || null;
    return;
  }

  if (type === 'assistant_message') {
    const turnID = String(payload.turn_id || '').trim();
    trackAssistantTurnStarted(turnID);
    const row = ensurePendingForTurn(turnID);
    const md = String(payload.message || '');
    updateAssistantRow(row, md, true);
    // Stream into overlay
    updateOverlay(md);
    return;
  }

  if (type === 'message_persisted') {
    if (String(payload.role || '') !== 'assistant') return;
    const turnID = String(payload.turn_id || '').trim();
    const md = String(payload.message || '');
    const row = takePendingRow(turnID);
    if (row) {
      updateAssistantRow(row, md, false);
    } else {
      appendRenderedAssistant(md);
    }
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus('ready');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    // Route response
    if (state.zenCanvasActionThisTurn) {
      // Canvas updated in-place, auto-dismiss overlay
      hideOverlay();
    } else if (isShortResponse(md)) {
      // Keep in overlay, finalize it
      updateOverlay(md);
    } else {
      // Long standalone result - keep in overlay for now
      updateOverlay(md);
    }
    state.zenCanvasActionThisTurn = false;
    return;
  }

  if (type === 'turn_cancelled') {
    const turnID = String(payload.turn_id || '').trim();
    const row = takePendingRow(turnID);
    if (row) updateAssistantRow(row, '_Stopped._', false);
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus('stopped');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    updateOverlay('_Stopped._');
    window.setTimeout(() => hideOverlay(), 1000);
    return;
  }

  if (type === 'turn_queue_cleared') {
    const count = Number(payload?.count || 0);
    const limit = Number.isFinite(count) && count > 0 ? Math.floor(count) : state.pendingQueue.length;
    for (let i = 0; i < limit; i += 1) {
      const row = takePendingRow('');
      if (!row) break;
      updateAssistantRow(row, '_Stopped._', false);
      trackAssistantTurnFinished('');
    }
    showStatus('queue cleared');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    return;
  }

  if (type === 'context_usage') {
    state.contextUsed = Number(payload.context_used) || 0;
    state.contextMax = Number(payload.context_max) || 0;
    return;
  }

  if (type === 'context_compact') {
    appendPlainMessage('system', 'Context auto-compacted to free space.');
    state.contextUsed = 0;
    state.contextMax = 0;
    return;
  }

  if (type === 'chat_cleared') {
    clearChatHistory();
    resetAssistantTurnTracking({ clearError: true });
    appendPlainMessage('system', 'Chat cleared.');
    state.contextUsed = 0;
    state.contextMax = 0;
    return;
  }

  if (type === 'chat_compacted') {
    void loadChatHistory().catch(() => {});
    const message = String(payload.message || 'Chat compacted.').trim();
    appendPlainMessage('system', message);
    return;
  }

  if (type === 'error') {
    const turnID = String(payload.turn_id || '').trim();
    const row = takePendingRow(turnID);
    if (row) row.classList.remove('is-pending');
    trackAssistantTurnFinished(turnID);
    const errText = String(payload.error || 'assistant request failed');
    state.assistantLastError = errText;
    appendPlainMessage('system', errText);
    showStatus(errText);
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    updateOverlay(`**Error:** ${errText}`);
    window.setTimeout(() => hideOverlay(), 2000);
  }
}

async function switchProject(projectID) {
  const nextProjectID = String(projectID || '').trim();
  if (!nextProjectID) return;
  if (state.projectSwitchInFlight) return;
  if (nextProjectID === state.activeProjectId && state.chatSessionId) return;

  state.projectSwitchInFlight = true;
  showStatus('switching project...');
  cancelChatVoiceCapture();
  closeChatWs();
  closeCanvasWs();
  clearChatHistory();
  clearCanvas();
  hideCanvasColumn();
  hideOverlay();
  hideTextInput();
  resetAssistantTurnTracking({ clearError: true });
  setActiveProjectID(nextProjectID);
  try {
    const project = await activateProject(nextProjectID);
    state.chatWsHasConnected = false;
    upsertProject(project);
    renderEdgeTopProjects();
    openCanvasWs();
    await loadChatHistory();
    await refreshAssistantActivity();
    openChatWs();
    showStatus(`ready`);
  } catch (err) {
    const message = String(err?.message || err || 'project switch failed');
    appendPlainMessage('system', `Project switch failed: ${message}`);
    showStatus(`project switch failed: ${message}`);
  } finally {
    state.projectSwitchInFlight = false;
  }
}

async function zenSubmitMessage(text) {
  const trimmed = String(text || '').trim();
  if (!trimmed || !state.chatSessionId) return;
  let finalText = trimmed;
  const anchor = getInputAnchor();
  if (anchor) {
    const prefix = buildContextPrefix(anchor);
    if (prefix) finalText = `${prefix} ${finalText}`;
    setInputAnchor(null);
    clearLineHighlight();
  }
  state.assistantLastError = '';
  updateAssistantActivityIndicator();
  appendPlainMessage('user', finalText);

  if (!finalText.startsWith('/')) {
    const pending = appendRenderedAssistant('_Thinking..._', { pending: true, localId: nextLocalMessageId() });
    state.pendingQueue.push(pending);
    updateAssistantActivityIndicator();
  }

  const body = { text: finalText };
  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      appendPlainMessage('system', `Send failed: ${detail}`);
      updateOverlay(`**Send failed:** ${detail}`);
      return;
    }
    const payload = await resp.json();
    if (payload?.kind === 'command' && payload?.result?.message) {
      appendPlainMessage('system', String(payload.result.message));
    }
  } catch (err) {
    const pending = takePendingRow('');
    pending?.remove();
    trackAssistantTurnFinished('');
    appendPlainMessage('system', `Send failed: ${String(err?.message || err)}`);
    updateOverlay(`**Send failed:** ${String(err?.message || err)}`);
  }
}

async function cancelActiveAssistantTurn() {
  if (!state.chatSessionId || state.assistantCancelInFlight) return;
  await refreshAssistantActivity();
  if (!isAssistantWorking()) {
    showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
    updateAssistantActivityIndicator();
    return;
  }
  state.assistantCancelInFlight = true;
  updateAssistantActivityIndicator();
  showStatus('stopping...');
  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/cancel`, { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      showStatus(`stop failed: ${detail}`);
      return;
    }
    const payload = await resp.json();
    const canceled = Number(payload?.canceled || 0);
    if (canceled <= 0) {
      await refreshAssistantActivity();
      if (!isAssistantWorking()) {
        showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
      }
    }
  } catch (err) {
    showStatus(`stop failed: ${String(err?.message || err)}`);
  } finally {
    state.assistantCancelInFlight = false;
    updateAssistantActivityIndicator();
    window.setTimeout(() => { void refreshAssistantActivity(); }, 120);
  }
}

function openCanvasWs() {
  const turnToken = state.canvasWsToken + 1;
  state.canvasWsToken = turnToken;
  const targetSessionID = String(state.sessionId || 'local');
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/canvas/${encodeURIComponent(targetSessionID)}`;
  const ws = new WebSocket(wsUrl);
  state.canvasWs = ws;

  ws.onopen = () => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    void loadCanvasSnapshot(targetSessionID);
  };

  ws.onmessage = (event) => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    try {
      const payload = JSON.parse(event.data);
      renderCanvas(payload);
      if (payload.event_id && payload.kind && payload.kind !== 'clear_canvas') {
        const paneId = payload.kind === 'image_artifact' ? 'canvas-image'
          : payload.kind === 'pdf_artifact' ? 'canvas-pdf'
          : 'canvas-text';
        showCanvasColumn(paneId);
        state.zenCanvasActionThisTurn = true;
      }
      if (payload.kind === 'clear_canvas') {
        hideCanvasColumn();
      }
    } catch (_) {}
  };

  ws.onclose = () => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    state.canvasWs = null;
    window.setTimeout(() => {
      if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
      openCanvasWs();
    }, 1200);
  };
}

async function loadCanvasSnapshot(sessionID = state.sessionId) {
  try {
    const resp = await fetch(`/api/canvas/${encodeURIComponent(sessionID)}/snapshot`);
    if (!resp.ok) { clearCanvas(); return; }
    const payload = await resp.json();
    if (payload?.event) {
      renderCanvas(payload.event);
      const ev = payload.event;
      if (ev.event_id && ev.kind && ev.kind !== 'clear_canvas') {
        const paneId = ev.kind === 'image_artifact' ? 'canvas-image'
          : ev.kind === 'pdf_artifact' ? 'canvas-pdf'
          : 'canvas-text';
        showCanvasColumn(paneId);
      }
      return;
    }
    clearCanvas();
  } catch (_) {
    clearCanvas();
  }
}

// Edge panel logic
let edgeTopTimer = null;
let edgeRightTimer = null;
let edgeTouchStart = null;

function initEdgePanels() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');

  // Desktop: hover near edge
  document.addEventListener('mousemove', (ev) => {
    // Top edge
    if (ev.clientY < 20 && edgeTop && !edgeTop.classList.contains('edge-pinned')) {
      edgeTop.classList.add('edge-active');
      if (edgeTopTimer) { clearTimeout(edgeTopTimer); edgeTopTimer = null; }
    }
    // Right edge
    if (ev.clientX > window.innerWidth - 20 && edgeRight && !edgeRight.classList.contains('edge-pinned')) {
      edgeRight.classList.add('edge-active');
      if (edgeRightTimer) { clearTimeout(edgeRightTimer); edgeRightTimer = null; }
    }
  });

  // Leave panels
  if (edgeTop) {
    edgeTop.addEventListener('mouseleave', () => {
      if (edgeTop.classList.contains('edge-pinned')) return;
      edgeTopTimer = setTimeout(() => {
        edgeTop.classList.remove('edge-active');
        edgeTopTimer = null;
      }, 300);
    });
    edgeTop.addEventListener('mouseenter', () => {
      if (edgeTopTimer) { clearTimeout(edgeTopTimer); edgeTopTimer = null; }
    });
  }

  if (edgeRight) {
    edgeRight.addEventListener('mouseleave', () => {
      if (edgeRight.classList.contains('edge-pinned')) return;
      edgeRightTimer = setTimeout(() => {
        edgeRight.classList.remove('edge-active');
        edgeRightTimer = null;
      }, 300);
    });
    edgeRight.addEventListener('mouseenter', () => {
      if (edgeRightTimer) { clearTimeout(edgeRightTimer); edgeRightTimer = null; }
    });
  }

  // Click to pin
  if (edgeTop) {
    edgeTop.addEventListener('click', (ev) => {
      if (ev.target instanceof Element && ev.target.closest('button')) return;
      edgeTop.classList.add('edge-pinned');
    });
  }
  if (edgeRight) {
    edgeRight.addEventListener('click', (ev) => {
      if (ev.target instanceof Element && ev.target.closest('button')) return;
      edgeRight.classList.add('edge-pinned');
    });
  }

  // Tabula Rasa button
  const rasaBtn = document.getElementById('btn-edge-rasa');
  if (rasaBtn) {
    rasaBtn.addEventListener('click', () => {
      clearCanvas();
      hideCanvasColumn();
      if (edgeTop) {
        edgeTop.classList.remove('edge-active', 'edge-pinned');
      }
    });
  }

  // Mobile: swipe from edge
  document.addEventListener('touchstart', (ev) => {
    if (ev.touches.length !== 1) return;
    const t = ev.touches[0];
    if (t.clientX > window.innerWidth - 20 || t.clientY < 20 || t.clientX < 20) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: null };
      if (t.clientX > window.innerWidth - 20) edgeTouchStart.edge = 'right';
      else if (t.clientY < 20) edgeTouchStart.edge = 'top';
    }
  }, { passive: true });

  document.addEventListener('touchmove', (ev) => {
    if (!edgeTouchStart || ev.touches.length !== 1) return;
    const t = ev.touches[0];
    const dx = t.clientX - edgeTouchStart.x;
    const dy = t.clientY - edgeTouchStart.y;
    if (edgeTouchStart.edge === 'right' && dx < -30 && edgeRight) {
      edgeRight.classList.add('edge-active');
    } else if (edgeTouchStart.edge === 'top' && dy > 30 && edgeTop) {
      edgeTop.classList.add('edge-active');
    }
  }, { passive: true });

  document.addEventListener('touchend', () => {
    edgeTouchStart = null;
  }, { passive: true });
}

function closeEdgePanels() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  if (edgeTop) edgeTop.classList.remove('edge-active', 'edge-pinned');
  if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
}

function bindUi() {
  const canvasText = document.getElementById('canvas-text');
  const canvasViewport = document.getElementById('canvas-viewport');

  // Zen: Left-click/tap on canvas -> toggle voice recording
  const zenClickTarget = canvasViewport || document.getElementById('workspace');
  if (zenClickTarget) {
    zenClickTarget.addEventListener('click', (ev) => {
      // Ignore clicks on interactive elements
      if (ev.target instanceof Element && ev.target.closest('button,a,input,textarea,select,[contenteditable="true"],.mail-triage-table,.mail-detail-view,.zen-overlay,.zen-input,.edge-panel')) return;
      // Ignore if right-click
      if (ev.button !== 0) return;
      // Ignore text selection
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;

      const x = ev.clientX;
      const y = ev.clientY;

      if (isRecording()) {
        void stopZenVoiceCaptureAndSend();
        return;
      }

      // Get anchor if on artifact
      let anchor = null;
      if (state.hasArtifact && canvasText) {
        anchor = getAnchorFromPoint(x, y);
        if (anchor) {
          showLineHighlight(x, y);
        }
      }

      void beginZenVoiceCapture(x, y, anchor);
    });
  }

  // Zen: Right-click -> text input
  if (zenClickTarget) {
    zenClickTarget.addEventListener('contextmenu', (ev) => {
      if (ev.target instanceof Element && ev.target.closest('.mail-triage-table,.mail-detail-view,.edge-panel')) return;
      ev.preventDefault();
      let anchor = null;
      if (state.hasArtifact && canvasText) {
        anchor = getAnchorFromPoint(ev.clientX, ev.clientY);
        if (anchor) {
          showLineHighlight(ev.clientX, ev.clientY);
        }
      }
      showTextInput(ev.clientX, ev.clientY, anchor);
    });
  }

  // Zen: Text input Enter -> send
  const zenInput = document.getElementById('zen-input');
  if (zenInput instanceof HTMLTextAreaElement) {
    zenInput.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        const text = zenInput.value.trim();
        if (text) {
          zenInput.value = '';
          hideTextInput();
          void zenSubmitMessage(text);
        }
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        hideTextInput();
      }
    });
    zenInput.addEventListener('input', () => {
      zenInput.style.height = 'auto';
      zenInput.style.height = `${Math.min(zenInput.scrollHeight, 240)}px`;
    });
  }

  // Zen: Click outside overlay/input -> dismiss
  document.addEventListener('mousedown', (ev) => {
    if (!(ev.target instanceof Element)) return;
    // Dismiss overlay on click outside
    if (isOverlayVisible()) {
      const overlay = document.getElementById('zen-overlay');
      if (overlay && !overlay.contains(ev.target)) {
        hideOverlay();
      }
    }
    // Dismiss text input on click outside
    if (isTextInputVisible()) {
      const input = document.getElementById('zen-input');
      if (input && !input.contains(ev.target) && ev.button === 0) {
        hideTextInput();
      }
    }
  });

  // Zen: Keyboard typing auto-activates text input (rasa mode)
  document.addEventListener('keydown', (ev) => {
    // Escape handling
    if (ev.key === 'Escape' && !ev.metaKey && !ev.ctrlKey && !ev.altKey) {
      if (isRecording()) {
        cancelChatVoiceCapture();
        showStatus('ready');
        return;
      }
      if (isOverlayVisible()) {
        hideOverlay();
        return;
      }
      if (isTextInputVisible()) {
        hideTextInput();
        return;
      }
      closeEdgePanels();
      if (state.hasArtifact) {
        clearCanvas();
        hideCanvasColumn();
        return;
      }
      void cancelActiveAssistantTurn();
      return;
    }

    // Enter stops recording
    if (ev.key === 'Enter' && isRecording()) {
      ev.preventDefault();
      void stopZenVoiceCaptureAndSend();
      return;
    }

    // Control long-press for PTT
    if (ev.key === 'Control' && !ev.repeat) {
      if (state.chatCtrlHoldTimer || state.chatVoiceCapture) return;
      state.chatCtrlHoldTimer = window.setTimeout(() => {
        state.chatCtrlHoldTimer = null;
        const cx = window.innerWidth / 2;
        const cy = window.innerHeight / 2;
        void beginZenVoiceCapture(cx, cy, null);
      }, CHAT_CTRL_LONG_PRESS_MS);
      return;
    }

    if (ev.ctrlKey && ev.key !== 'Control') {
      if (state.chatCtrlHoldTimer) {
        clearTimeout(state.chatCtrlHoldTimer);
        state.chatCtrlHoldTimer = null;
      }
      if (state.chatVoiceCapture) {
        cancelChatVoiceCapture();
        showStatus('ready');
      }
      return;
    }

    if (ev.metaKey || ev.ctrlKey || ev.altKey) return;
    if (isEditableTarget(ev.target)) return;

    // Auto-activate text input on printable key
    if (ev.key.length === 1 && !isTextInputVisible()) {
      const cx = window.innerWidth / 2 - 130;
      const cy = window.innerHeight / 2;
      showTextInput(cx, cy, null);
      // Forward the keystroke
      const input = document.getElementById('zen-input');
      if (input instanceof HTMLTextAreaElement) {
        input.value = ev.key;
        const caret = ev.key.length;
        input.setSelectionRange(caret, caret);
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
      ev.preventDefault();
      return;
    }

    // Enter when text input is NOT visible but could send
    if (ev.key === 'Enter' && !isTextInputVisible()) {
      ev.preventDefault();
    }
  }, true);

  document.addEventListener('keyup', (ev) => {
    if (ev.key !== 'Control') return;
    if (state.chatCtrlHoldTimer) {
      clearTimeout(state.chatCtrlHoldTimer);
      state.chatCtrlHoldTimer = null;
      return;
    }
    if (state.chatVoiceCapture) {
      void stopZenVoiceCaptureAndSend();
    }
  }, true);

  window.addEventListener('blur', () => {
    if (state.chatCtrlHoldTimer) {
      clearTimeout(state.chatCtrlHoldTimer);
      state.chatCtrlHoldTimer = null;
    }
    if (state.chatVoiceCapture) {
      cancelChatVoiceCapture();
      showStatus('ready');
    }
  });

  // Text selection on artifact sets anchor
  if (canvasText) {
    canvasText.addEventListener('mouseup', () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) return;
      const loc = getLocationFromSelection();
      if (loc) {
        setInputAnchor({ line: loc.line, title: loc.title, selectedText: loc.selectedText });
      }
    });
  }

  // Touch long-press for PTT on artifact
  if (canvasText) {
    let artHoldTimer = null;
    let artHoldActive = false;
    let artHoldX = 0;
    let artHoldY = 0;
    const ART_HOLD_MOVE_THRESHOLD = 5;

    canvasText.addEventListener('touchstart', (ev) => {
      if (ev.touches.length !== 1) return;
      const t = ev.touches[0];
      artHoldActive = false;
      artHoldX = t.clientX;
      artHoldY = t.clientY;
      artHoldTimer = window.setTimeout(() => {
        artHoldTimer = null;
        artHoldActive = true;
        const anchor = getAnchorFromPoint(artHoldX, artHoldY);
        if (anchor) {
          showLineHighlight(artHoldX, artHoldY);
        }
        void beginZenVoiceCapture(artHoldX, artHoldY, anchor);
      }, CHAT_SEND_HOLD_MS);
    }, { passive: true });

    canvasText.addEventListener('touchmove', (ev) => {
      if (!artHoldTimer) return;
      if (ev.touches.length !== 1) return;
      const t = ev.touches[0];
      const dx = t.clientX - artHoldX;
      const dy = t.clientY - artHoldY;
      if (Math.sqrt(dx * dx + dy * dy) > ART_HOLD_MOVE_THRESHOLD) {
        if (artHoldTimer) { clearTimeout(artHoldTimer); artHoldTimer = null; }
      }
    }, { passive: true });

    window.addEventListener('touchend', () => {
      if (artHoldTimer) { clearTimeout(artHoldTimer); artHoldTimer = null; return; }
      if (artHoldActive || state.chatVoiceCapture) {
        artHoldActive = false;
        void stopZenVoiceCaptureAndSend();
      }
    }, { passive: true });

    window.addEventListener('touchcancel', () => {
      if (artHoldTimer) { clearTimeout(artHoldTimer); artHoldTimer = null; }
      artHoldActive = false;
    });
  }

  initEdgePanels();
}

function showSplash() {
  const project = activeProject();
  const name = project?.name || '';
  if (!name) return;
  const splash = document.createElement('div');
  splash.className = 'zen-splash';
  splash.textContent = name;
  document.getElementById('view-main')?.appendChild(splash);
  window.setTimeout(() => splash.classList.add('fade-out'), 100);
  window.setTimeout(() => splash.remove(), 1700);
}

function warmMicStream() {
  if (!canUseMicrophoneCapture()) return;
  acquireMicStream().then(() => releaseMicStream()).catch(() => {});
}

async function init() {
  bindUi();
  warmMicStream();
  updateAssistantActivityIndicator();
  startDevReloadWatcher();
  startAssistantActivityWatcher();
  clearCanvas();
  hideCanvasColumn();
  showStatus('starting...');

  await fetchProjects();
  const initialProjectID = resolveInitialProjectID();
  if (!initialProjectID) throw new Error('no projects available');
  await switchProject(initialProjectID);
  showSplash();
}

async function authGate() {
  const resp = await fetch('/api/setup');
  const data = await resp.json();
  if (data.authenticated) return;

  const loginView = document.getElementById('view-login');
  const mainView = document.getElementById('view-main');
  const loginForm = document.getElementById('login-form');
  const loginPassword = document.getElementById('login-password');
  const loginError = document.getElementById('login-error');
  const loginPrompt = document.getElementById('login-prompt');
  const loginBtn = document.getElementById('btn-login');

  if (!data.has_password) {
    loginPrompt.textContent = 'No password set. Run "tabura set-password" on the server.';
    loginBtn.style.display = 'none';
    loginPassword.style.display = 'none';
    loginView.style.display = '';
    mainView.style.display = 'none';
    return new Promise(() => {});
  }

  loginPrompt.textContent = 'Enter your password.';
  loginView.style.display = '';
  mainView.style.display = 'none';

  await new Promise((resolve) => {
    loginForm.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      loginError.textContent = '';
      const pw = loginPassword.value;
      if (!pw) return;
      try {
        const r = await fetch('/api/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ password: pw }),
        });
        if (!r.ok) {
          const msg = (await r.text()).trim();
          loginError.textContent = msg || `Error ${r.status}`;
          return;
        }
        resolve();
      } catch (err) {
        loginError.textContent = String(err?.message || err);
      }
    });
  });

  loginView.style.display = 'none';
  mainView.style.display = '';
}

authGate()
  .then(() => {
    document.getElementById('view-main').style.display = '';
    return init();
  })
  .catch((err) => {
    showStatus('failed');
    appendPlainMessage('system', `Initialization failed: ${String(err?.message || err)}`);
  });
