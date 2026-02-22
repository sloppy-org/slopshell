import { marked } from './vendor/marked.esm.js';
import { renderCanvas, clearCanvas, initCanvasControls } from './canvas.js';

const state = {
  sessionId: 'local',
  canvasWs: null,
  chatWs: null,
  chatWsToken: 0,
  canvasWsToken: 0,
  chatWsHasConnected: false,
  chatSessionId: '',
  chatMode: 'chat',
  activeTab: 'chat',
  projects: [],
  defaultProjectId: '',
  serverActiveProjectId: '',
  activeProjectId: '',
  projectsOpen: false,
  projectSwitchInFlight: false,
  canvasHasUnread: false,
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
};

export function getState() {
  return state;
}

window._tabulaApp = { getState, acquireMicStream, sttStart, sttSendChunk, sttStop, sttCancel };

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABULA_CHAT_MATH_SEGMENT_';
const DEV_UI_RELOAD_POLL_MS = 1500;
const ASSISTANT_ACTIVITY_POLL_MS = 1200;
const ACTIVE_PROJECT_STORAGE_KEY = 'tabula.activeProjectId';
let localMessageSeq = 0;
const CHAT_CTRL_LONG_PRESS_MS = 180;
const CHAT_SEND_HOLD_MS = 300;
let devReloadBootID = '';
let devReloadTimer = null;
let devReloadInFlight = false;
let devReloadRequested = false;
let assistantActivityTimer = null;
let assistantActivityInFlight = false;

const renderer = new marked.Renderer();
renderer.code = ({ text, lang }) => {
  const safeLang = escapeHtml((lang || 'plaintext').toLowerCase());
  return `<pre><code class="language-${safeLang}">${escapeHtml(text || '')}</code></pre>\n`;
};
marked.setOptions({ breaks: true, renderer });

function escapeHtml(text) {
  return String(text || '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function sanitizeHtml(html) {
  const doc = new DOMParser().parseFromString(String(html || ''), 'text/html');
  const dangerous = doc.querySelectorAll('script,iframe,object,embed,link[rel="import"],form,svg,base,style');
  dangerous.forEach((el) => el.remove());
  doc.querySelectorAll('*').forEach((el) => {
    for (const attr of [...el.attributes]) {
      const val = attr.value.trim().toLowerCase();
      const isDangerous = attr.name.startsWith('on')
        || val.startsWith('javascript:')
        || val.startsWith('vbscript:')
        || (val.startsWith('data:') && !val.startsWith('data:image/'));
      if (isDangerous) {
        el.removeAttribute(attr.name);
      }
    }
  });
  return doc.body.innerHTML;
}

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
}

function forceUiHardReload() {
  const url = new URL(window.location.href);
  url.searchParams.set('__tabula_reload', Date.now().toString(36));
  window.location.replace(url.toString());
}

async function fetchRuntimeMeta() {
  const resp = await fetch('/api/runtime', {
    cache: 'no-store',
    headers: {
      'Cache-Control': 'no-cache',
    },
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
    if (!document.hidden) {
      tick();
    }
  });
}

function chatInputEl() {
  const el = document.getElementById('chat-input');
  return el instanceof HTMLTextAreaElement ? el : null;
}

function isEditableTarget(target) {
  if (!(target instanceof Element)) return false;
  return Boolean(target.closest('input,textarea,select,[contenteditable="true"]'));
}

function focusChatInput({ placeCursorAtEnd = false } = {}) {
  if (state.activeTab !== 'chat') return;
  const input = chatInputEl();
  if (!input) return;
  if (document.activeElement === input) return;
  try {
    input.focus({ preventScroll: true });
  } catch (_) {
    input.focus();
  }
  if (placeCursorAtEnd) {
    const end = input.value.length;
    input.setSelectionRange(end, end);
  }
}

function activeProject() {
  return state.projects.find((project) => project.id === state.activeProjectId) || null;
}

function setProjectOverviewVisible(open) {
  state.projectsOpen = Boolean(open);
  const root = document.getElementById('view-main');
  const overview = document.getElementById('project-overview');
  if (root) {
    root.classList.toggle('projects-open', state.projectsOpen);
  }
  if (overview) {
    overview.classList.toggle('is-hidden', !state.projectsOpen);
  }
}

function persistActiveProjectID(projectID) {
  if (!projectID) return;
  try {
    window.localStorage.setItem(ACTIVE_PROJECT_STORAGE_KEY, projectID);
  } catch (_) {
    // noop
  }
}

function readPersistedProjectID() {
  try {
    return String(window.localStorage.getItem(ACTIVE_PROJECT_STORAGE_KEY) || '').trim();
  } catch (_) {
    return '';
  }
}

function setActiveProjectID(projectID) {
  state.activeProjectId = String(projectID || '').trim();
  if (state.activeProjectId) {
    persistActiveProjectID(state.activeProjectId);
  }
  renderProjectTabs();
  renderProjectCards();
}


function newMediaRecorder(stream) {
  let recorder = null;
  try {
    const preferredType = 'audio/webm;codecs=opus';
    if (typeof window.MediaRecorder?.isTypeSupported === 'function'
      && window.MediaRecorder.isTypeSupported(preferredType)) {
      recorder = new window.MediaRecorder(stream, { mimeType: preferredType });
    } else {
      recorder = new window.MediaRecorder(stream);
    }
  } catch (_) {
    recorder = new window.MediaRecorder(stream);
  }
  return recorder;
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
  _micStreamPromise = navigator.mediaDevices.getUserMedia({ audio: true }).then((stream) => {
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

function sttSendChunk(blob) {
  if (!_sttActive) return;
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  if (!blob || typeof blob.arrayBuffer !== 'function' || blob.size <= 0) return;
  blob.arrayBuffer().then((buf) => {
    if (!_sttActive) return;
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

function setSendButtonRecording(active) {
  const btn = document.getElementById('btn-chat-send');
  if (!btn) return;
  if (active) {
    btn.classList.add('is-recording');
    btn.textContent = 'Rec';
  } else {
    btn.classList.remove('is-recording');
    btn.textContent = 'Send';
  }
}

function stopChatVoiceMedia(capture) {
  if (!capture) return;
  if (capture.mediaRecorder) {
    try {
      if (capture.mediaRecorder.state !== 'inactive') {
        capture.mediaRecorder.stop();
      }
    } catch (_) {
      // noop
    }
  }
  capture.mediaRecorder = null;
  capture.mediaStream = null;
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

async function beginChatVoiceCapture(opts) {
  if (state.activeTab !== 'chat') return;
  if (state.chatVoiceCapture) return;
  if (!canUseMicrophoneCapture()) return;
  const capture = {
    active: false,
    stopping: false,
    stopRequested: false,
    autoSend: Boolean(opts?.autoSend),
    mediaStream: null,
    mediaRecorder: null,
  };
  state.chatVoiceCapture = capture;
  showStatus('push-to-prompt recording...');
  try {
    const stream = await acquireMicStream();
    if (state.chatVoiceCapture !== capture) return;
    sttStart('audio/webm');
    if (state.chatVoiceCapture !== capture) return;
    const recorder = newMediaRecorder(stream);
    capture.mediaStream = stream;
    capture.mediaRecorder = recorder;
    capture.active = true;
    setSendButtonRecording(true);
    recorder.addEventListener('dataavailable', (ev) => {
      if (!ev?.data || ev.data.size <= 0) return;
      sttSendChunk(ev.data);
    });
    recorder.start(300);
    if (capture.stopRequested) {
      void stopChatVoiceCaptureAndApply();
    }
  } catch (err) {
    setSendButtonRecording(false);
    const message = String(err?.message || err || 'push-to-prompt failed');
    showStatus(`push-to-prompt failed: ${message}`);
    sttCancel();
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
  }
}

async function stopChatVoiceCaptureAndApply() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  capture.stopRequested = true;
  if (!capture.active) return;
  capture.stopping = true;
  let remoteStopped = false;
  try {
    await stopChatVoiceMediaAndFlush(capture);
    const result = await sttStop();
    remoteStopped = true;
    const transcript = String(result?.text || '').trim();
    if (!transcript) {
      throw new Error('speech recognizer returned empty text');
    }
    const input = chatInputEl();
    if (!input) return;
    const needsSpace = input.value.trim() && !/[ \n]$/.test(input.value);
    input.value = `${input.value}${needsSpace ? ' ' : ''}${transcript}`;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    if (capture.autoSend) {
      showStatus('sending...');
      void sendChatMessage();
      return;
    }
    focusChatInput({ placeCursorAtEnd: true });
    showStatus('dictation ready (press Enter to send)');
  } catch (err) {
    const message = String(err?.message || err || 'push-to-prompt failed');
    showStatus(`push-to-prompt failed: ${message}`);
  } finally {
    setSendButtonRecording(false);
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
  setSendButtonRecording(false);
  sttCancel();
  stopChatVoiceMedia(capture);
  state.chatVoiceCapture = null;
}

function setActiveTab(tab) {
  const workspace = document.getElementById('workspace');
  const chatBtn = document.getElementById('tab-chat');
  const canvasBtn = document.getElementById('tab-canvas');
  if (!workspace || !chatBtn || !canvasBtn) return;
  state.activeTab = tab === 'canvas' ? 'canvas' : 'chat';
  workspace.classList.toggle('workspace-chat', state.activeTab === 'chat');
  workspace.classList.toggle('workspace-canvas', state.activeTab === 'canvas');
  chatBtn.classList.toggle('is-active', state.activeTab === 'chat');
  canvasBtn.classList.toggle('is-active', state.activeTab === 'canvas');
  if (state.activeTab === 'canvas') {
    state.canvasHasUnread = false;
    updateCanvasIndicator();
  } else {
    window.setTimeout(() => focusChatInput({ placeCursorAtEnd: true }), 0);
  }
}

function updateCanvasIndicator() {
  const dot = document.getElementById('tab-canvas-indicator');
  if (!dot) return;
  dot.style.display = state.canvasHasUnread ? 'inline-block' : 'none';
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
  const el = document.getElementById('chat-assistant-state');
  if (!(el instanceof HTMLElement)) return;
  const working = isAssistantWorking();
  const stopping = state.assistantCancelInFlight;
  const failed = !working && !stopping && Boolean(state.assistantLastError);
  if (stopping) {
    el.textContent = 'Assistant stopping...';
  } else if (failed) {
    el.textContent = 'Assistant error';
  } else if (!hasLocalAssistantWork() && state.assistantRemoteActiveCount <= 0 && state.assistantRemoteQueuedCount > 0) {
    el.textContent = `Assistant queued (${state.assistantRemoteQueuedCount})...`;
  } else {
    el.textContent = working ? 'Assistant is working...' : 'Assistant idle';
  }
  el.title = failed ? String(state.assistantLastError) : '';
  el.classList.toggle('is-working', working && !stopping);
  el.classList.toggle('is-stopping', stopping);
  el.classList.toggle('is-error', failed);
  el.classList.toggle('is-idle', !working && !stopping && !failed);
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
  if (host) {
    host.innerHTML = '';
  }
}

async function fetchProjects() {
  const resp = await fetch('/api/projects', { cache: 'no-store' });
  if (!resp.ok) {
    throw new Error(`projects list failed: HTTP ${resp.status}`);
  }
  const payload = await resp.json();
  const projects = Array.isArray(payload?.projects) ? payload.projects : [];
  state.projects = projects.map((project) => ({
    ...project,
    id: String(project?.id || ''),
  })).filter((project) => project.id);
  state.defaultProjectId = String(payload?.default_project_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_project_id || '').trim();
  renderProjectTabs();
  renderProjectCards();
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

function renderProjectTabs() {
  const strip = document.getElementById('project-tab-strip');
  if (!(strip instanceof HTMLElement)) return;
  strip.innerHTML = '';
  for (const project of state.projects) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'project-tab-btn';
    if (project.id === state.activeProjectId) {
      button.classList.add('is-active');
    }
    button.textContent = String(project.name || project.id || 'Project');
    button.title = String(project.root_path || '');
    button.addEventListener('click', () => {
      if (project.id === state.activeProjectId) return;
      void switchProject(project.id);
    });
    strip.appendChild(button);
  }
}

function renderProjectCards() {
  const host = document.getElementById('project-cards');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  for (const project of state.projects) {
    const card = document.createElement('article');
    card.className = 'project-card';
    if (project.id === state.activeProjectId) {
      card.classList.add('is-active');
    }

    const head = document.createElement('div');
    head.className = 'project-card-head';
    const name = document.createElement('div');
    name.className = 'project-card-name';
    name.textContent = String(project.name || project.id || 'Project');
    const kind = document.createElement('span');
    kind.className = 'project-kind-pill';
    kind.textContent = String(project.kind || 'managed');
    head.append(name, kind);

    const path = document.createElement('div');
    path.className = 'project-card-path';
    path.textContent = String(project.root_path || '');

    const actions = document.createElement('div');
    actions.className = 'project-card-actions';
    const openBtn = document.createElement('button');
    openBtn.type = 'button';
    openBtn.textContent = project.id === state.activeProjectId ? 'Active' : 'Open';
    openBtn.disabled = project.id === state.activeProjectId;
    openBtn.addEventListener('click', () => {
      void switchProject(project.id);
    });
    actions.appendChild(openBtn);

    card.append(head, path, actions);
    host.appendChild(card);
  }
}

async function createProjectFromForm() {
  const nameInput = document.getElementById('project-create-name');
  const kindInput = document.getElementById('project-create-kind');
  const pathInput = document.getElementById('project-create-path');
  const req = {
    name: nameInput instanceof HTMLInputElement ? nameInput.value : '',
    kind: kindInput instanceof HTMLSelectElement ? kindInput.value : 'managed',
    path: pathInput instanceof HTMLInputElement ? pathInput.value : '',
  };
  const resp = await fetch('/api/projects', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const project = payload?.project;
  if (!project || !project.id) {
    throw new Error('project create returned invalid project');
  }
  upsertProject(project);
  renderProjectTabs();
  renderProjectCards();
  if (nameInput instanceof HTMLInputElement) nameInput.value = '';
  if (pathInput instanceof HTMLInputElement) pathInput.value = '';
  return project;
}

async function activateProject(projectID) {
  const resp = await fetch(`/api/projects/${encodeURIComponent(projectID)}/activate`, {
    method: 'POST',
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const project = payload?.project || {};
  state.chatSessionId = String(project.chat_session_id || '');
  state.sessionId = String(project.canvas_session_id || 'local');
  setChatMode(project.chat_mode || 'chat');
  if (!state.chatSessionId) {
    throw new Error('chat session ID missing');
  }
  upsertProject(project);
  return project;
}

async function loadChatHistory() {
  if (!state.chatSessionId) return;
  const host = chatHistoryEl();
  if (!host) return;
  host.innerHTML = '';
  const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/history`);
  if (!resp.ok) {
    throw new Error(`chat history failed: HTTP ${resp.status}`);
  }
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
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(targetSessionID)}/activity`, {
      cache: 'no-store',
    });
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
    // Ignore transient probes while reconnecting.
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
    if (!document.hidden) {
      tick();
    }
  });
}

function closeChatWs() {
  state.chatWsToken += 1;
  if (state.chatWs) {
    try {
      state.chatWs.close();
    } catch (_) {
      // noop
    }
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
    showStatus('chat connected');
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
    try {
      payload = JSON.parse(event.data);
    } catch (_) {
      return;
    }
    if (handleSTTWSMessage(payload)) return;
    handleChatEvent(payload);
  };

  ws.onclose = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    state.chatWs = null;
    showStatus('reconnecting chat...');
    window.setTimeout(() => {
      if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
      openChatWs();
    }, 1200);
  };
}

function closeCanvasWs() {
  state.canvasWsToken += 1;
  if (state.canvasWs) {
    try {
      state.canvasWs.close();
    } catch (_) {
      // noop
    }
  }
  state.canvasWs = null;
}

function handleChatEvent(payload) {
  const type = String(payload?.type || '').trim();
  if (!type) return;

  if (type === 'mode_changed') {
    setChatMode(payload.mode || 'chat');
    const message = String(payload.message || '').trim();
    if (message) {
      appendPlainMessage('system', message);
    }
    return;
  }

  if (type === 'action') {
    const action = String(payload.action || '').trim();
    if (action === 'open_canvas') {
      setActiveTab('canvas');
    } else if (action === 'open_chat') {
      setActiveTab('chat');
    } else if (action === 'commit_canvas') {
      void commitCanvasFromChat();
    }
    return;
  }

  if (type === 'turn_started') {
    trackAssistantTurnStarted(payload.turn_id);
    ensurePendingForTurn(payload.turn_id);
    return;
  }

  if (type === 'assistant_message') {
    const turnID = String(payload.turn_id || '').trim();
    trackAssistantTurnStarted(turnID);
    const row = ensurePendingForTurn(turnID);
    updateAssistantRow(row, String(payload.message || ''), true);
    return;
  }

  if (type === 'message_persisted') {
    if (String(payload.role || '') !== 'assistant') return;
    const turnID = String(payload.turn_id || '').trim();
    const row = takePendingRow(turnID);
    if (row) {
      updateAssistantRow(row, String(payload.message || ''), false);
    } else {
      appendRenderedAssistant(String(payload.message || ''));
    }
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus('ready');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    return;
  }

  if (type === 'turn_cancelled') {
    const turnID = String(payload.turn_id || '').trim();
    const row = takePendingRow(turnID);
    if (row) {
      updateAssistantRow(row, '_Stopped._', false);
    }
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus('assistant stopped');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
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
    showStatus('assistant queue cleared');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    return;
  }

  if (type === 'error') {
    const turnID = String(payload.turn_id || '').trim();
    const row = takePendingRow(turnID);
    if (row) {
      row.classList.remove('is-pending');
    }
    trackAssistantTurnFinished(turnID);
    const errText = String(payload.error || 'assistant request failed');
    state.assistantLastError = errText;
    appendPlainMessage('system', errText);
    showStatus(errText);
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
  }
}

async function switchProject(projectID) {
  const nextProjectID = String(projectID || '').trim();
  if (!nextProjectID) return;
  if (state.projectSwitchInFlight) return;
  if (nextProjectID === state.activeProjectId && state.chatSessionId) {
    setProjectOverviewVisible(false);
    return;
  }

  state.projectSwitchInFlight = true;
  showStatus('switching project...');
  cancelChatVoiceCapture();
  closeChatWs();
  closeCanvasWs();
  clearChatHistory();
  clearCanvas();
  resetAssistantTurnTracking({ clearError: true });
  setActiveProjectID(nextProjectID);
  try {
    const project = await activateProject(nextProjectID);
    state.chatWsHasConnected = false;
    upsertProject(project);
    renderProjectTabs();
    renderProjectCards();
    openCanvasWs();
    await loadChatHistory();
    await refreshAssistantActivity();
    openChatWs();
    setProjectOverviewVisible(false);
    showStatus(`ready · ${String(project.name || 'project')}`);
    if (state.activeTab === 'chat') {
      focusChatInput({ placeCursorAtEnd: true });
    }
  } catch (err) {
    const message = String(err?.message || err || 'project switch failed');
    appendPlainMessage('system', `Project switch failed: ${message}`);
    showStatus(`project switch failed: ${message}`);
  } finally {
    state.projectSwitchInFlight = false;
  }
}

async function sendChatMessage() {
  const input = document.getElementById('chat-input');
  if (!(input instanceof HTMLTextAreaElement)) return;
  const text = input.value.trim();
  if (!text || !state.chatSessionId) return;
  state.assistantLastError = '';
  updateAssistantActivityIndicator();
  input.value = '';
  input.style.height = 'auto';
  focusChatInput({ placeCursorAtEnd: true });
  appendPlainMessage('user', text);

  if (!text.startsWith('/')) {
    const pending = appendRenderedAssistant('_Thinking..._', { pending: true, localId: nextLocalMessageId() });
    state.pendingQueue.push(pending);
    updateAssistantActivityIndicator();
  }

  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text }),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      appendPlainMessage('system', `Send failed: ${detail}`);
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
  }
  focusChatInput({ placeCursorAtEnd: true });
}

async function cancelActiveAssistantTurn() {
  if (!state.chatSessionId || state.assistantCancelInFlight) return;
  await refreshAssistantActivity();
  if (!isAssistantWorking()) {
    showStatus(state.assistantLastError ? state.assistantLastError : 'assistant idle');
    updateAssistantActivityIndicator();
    return;
  }
  state.assistantCancelInFlight = true;
  updateAssistantActivityIndicator();
  showStatus('stopping assistant...');
  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/cancel`, {
      method: 'POST',
    });
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
        showStatus(state.assistantLastError ? state.assistantLastError : 'assistant idle');
      }
    }
  } catch (err) {
    showStatus(`stop failed: ${String(err?.message || err)}`);
  } finally {
    state.assistantCancelInFlight = false;
    updateAssistantActivityIndicator();
    window.setTimeout(() => {
      void refreshAssistantActivity();
    }, 120);
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
      if (state.activeTab !== 'canvas') {
        state.canvasHasUnread = true;
        updateCanvasIndicator();
      }
    } catch (_) {
      // ignore malformed payloads
    }
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
    if (!resp.ok) {
      clearCanvas();
      return;
    }
    const payload = await resp.json();
    if (payload?.event) {
      renderCanvas(payload.event);
      return;
    }
    clearCanvas();
  } catch (_) {
    clearCanvas();
  }
}

async function runChatCommand(command) {
  if (!state.chatSessionId) return;
  const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/commands`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ command }),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    appendPlainMessage('system', detail);
    return;
  }
  const payload = await resp.json();
  const message = String(payload?.result?.message || '').trim();
  if (message) {
    appendPlainMessage('system', message);
  }
}

async function commitCanvasFromChat() {
  try {
    const commitRequestTimeoutMs = 45000;
    const controller = new AbortController();
    const timeoutID = window.setTimeout(() => controller.abort(), commitRequestTimeoutMs);
    let resp;
    try {
      resp = await fetch(`/api/canvas/${encodeURIComponent(state.sessionId)}/commit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ include_draft: true }),
        signal: controller.signal,
      });
    } finally {
      window.clearTimeout(timeoutID);
    }
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      appendPlainMessage('system', `Commit failed: ${detail}`);
      return;
    }
    appendPlainMessage('system', 'Draft annotations committed.');
  } catch (err) {
    if (err && err.name === 'AbortError') {
      appendPlainMessage('system', 'Commit failed: request timed out after 45s');
      return;
    }
    appendPlainMessage('system', `Commit failed: ${String(err?.message || err)}`);
  }
}

function bindUi() {
  document.getElementById('tab-chat')?.addEventListener('click', () => setActiveTab('chat'));
  document.getElementById('tab-canvas')?.addEventListener('click', () => setActiveTab('canvas'));
  document.getElementById('btn-projects')?.addEventListener('click', () => {
    setProjectOverviewVisible(!state.projectsOpen);
  });
  document.getElementById('btn-projects-close')?.addEventListener('click', () => {
    setProjectOverviewVisible(false);
  });

  document.getElementById('btn-plan-toggle')?.addEventListener('click', () => {
    void runChatCommand('/plan');
  });
  document.getElementById('btn-open-canvas')?.addEventListener('click', () => setActiveTab('canvas'));
  document.getElementById('btn-commit-from-chat')?.addEventListener('click', () => {
    void commitCanvasFromChat();
  });

  const form = document.getElementById('chat-form');
  form?.addEventListener('submit', (ev) => {
    ev.preventDefault();
    void sendChatMessage();
  });

  const sendBtn = document.getElementById('btn-chat-send');
  if (sendBtn) {
    let sendHoldTimer = null;
    let sendHoldActive = false;
    let sendHoldIsTouch = false;
    const startHold = (ev, isTouch) => {
      if (state.chatVoiceCapture) {
        if (isTouch) ev.preventDefault();
        void stopChatVoiceCaptureAndApply();
        return;
      }
      if (isTouch) ev.preventDefault();
      sendHoldActive = false;
      sendHoldIsTouch = isTouch;
      sendHoldTimer = window.setTimeout(() => {
        sendHoldTimer = null;
        sendHoldActive = true;
        void beginChatVoiceCapture({ autoSend: true });
      }, CHAT_SEND_HOLD_MS);
    };
    const endHold = () => {
      if (sendHoldTimer) {
        clearTimeout(sendHoldTimer);
        sendHoldTimer = null;
        sendHoldIsTouch = false;
        return;
      }
      if (sendHoldActive || state.chatVoiceCapture) {
        sendHoldActive = false;
        sendHoldIsTouch = false;
        void stopChatVoiceCaptureAndApply();
      }
    };
    sendBtn.addEventListener('touchstart', (ev) => startHold(ev, true), { passive: false });
    window.addEventListener('touchend', (ev) => {
      if (!sendHoldIsTouch) return;
      if (sendHoldTimer || sendHoldActive || state.chatVoiceCapture) ev.preventDefault();
      endHold();
    }, { passive: false });
    window.addEventListener('touchcancel', () => {
      if (!sendHoldIsTouch) return;
      endHold();
    });
    sendBtn.addEventListener('mousedown', (ev) => {
      if (ev.button !== 0) return;
      if (sendHoldIsTouch) return;
      startHold(ev, false);
    });
    window.addEventListener('mouseup', () => {
      if (sendHoldIsTouch) return;
      endHold();
    });
    sendBtn.addEventListener('click', (ev) => {
      if (sendHoldActive || state.chatVoiceCapture) {
        ev.preventDefault();
        ev.stopImmediatePropagation();
      }
    }, true);
  }

  const projectCreateForm = document.getElementById('project-create-form');
  projectCreateForm?.addEventListener('submit', (ev) => {
    ev.preventDefault();
    if (state.projectSwitchInFlight) return;
    showStatus('creating project...');
    void createProjectFromForm()
      .then((project) => switchProject(project.id))
      .catch((err) => {
        const message = String(err?.message || err || 'project create failed');
        showStatus(`project create failed: ${message}`);
      });
  });

  const projectKindInput = document.getElementById('project-create-kind');
  if (projectKindInput instanceof HTMLSelectElement) {
    const syncProjectPathField = () => {
      const wrap = document.getElementById('project-create-path-wrap');
      if (!(wrap instanceof HTMLElement)) return;
      const isLinked = projectKindInput.value === 'linked';
      wrap.style.opacity = isLinked ? '1' : '0.92';
    };
    projectKindInput.addEventListener('change', syncProjectPathField);
    syncProjectPathField();
  }

  const input = document.getElementById('chat-input');
  if (input instanceof HTMLTextAreaElement) {
    input.addEventListener('keydown', (ev) => {
      const isEnter = ev.key === 'Enter';
      if (!isEnter) return;
      if (ev.shiftKey) return;
      ev.preventDefault();
      void sendChatMessage();
    });
    input.addEventListener('input', () => {
      input.style.height = 'auto';
      input.style.height = `${Math.min(input.scrollHeight, 240)}px`;
    });
  }

  document.addEventListener('keydown', (ev) => {
    if (ev.key === 'Escape' && state.projectsOpen) {
      ev.preventDefault();
      setProjectOverviewVisible(false);
      return;
    }
    if (state.activeTab !== 'chat') return;

    if (ev.key === 'Escape' && !ev.metaKey && !ev.ctrlKey && !ev.altKey) {
      ev.preventDefault();
      void cancelActiveAssistantTurn();
      return;
    }

    if (ev.key === 'Control' && !ev.repeat) {
      if (state.chatCtrlHoldTimer || state.chatVoiceCapture) return;
      state.chatCtrlHoldTimer = window.setTimeout(() => {
        state.chatCtrlHoldTimer = null;
        void beginChatVoiceCapture();
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

    const inputEl = chatInputEl();
    if (!inputEl) return;

    if (ev.key.length === 1) {
      ev.preventDefault();
      focusChatInput({ placeCursorAtEnd: true });
      const start = inputEl.selectionStart ?? inputEl.value.length;
      const end = inputEl.selectionEnd ?? inputEl.value.length;
      inputEl.value = `${inputEl.value.slice(0, start)}${ev.key}${inputEl.value.slice(end)}`;
      const caret = start + ev.key.length;
      inputEl.setSelectionRange(caret, caret);
      inputEl.dispatchEvent(new Event('input', { bubbles: true }));
      return;
    }

    if (ev.key === 'Enter') {
      ev.preventDefault();
      if (inputEl.value.trim()) {
        void sendChatMessage();
      } else {
        focusChatInput({ placeCursorAtEnd: true });
      }
    }
  }, true);

  document.addEventListener('keyup', (ev) => {
    if (ev.key !== 'Control') return;
    if (state.chatCtrlHoldTimer) {
      clearTimeout(state.chatCtrlHoldTimer);
      state.chatCtrlHoldTimer = null;
      return;
    }
    if (state.activeTab === 'chat' && state.chatVoiceCapture) {
      void stopChatVoiceCaptureAndApply();
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

  window.addEventListener('focus', () => {
    if (state.activeTab === 'chat') {
      window.setTimeout(() => focusChatInput({ placeCursorAtEnd: true }), 20);
    }
  });

  const panelChat = document.getElementById('panel-chat');
  panelChat?.addEventListener('click', (ev) => {
    if (state.activeTab !== 'chat') return;
    const target = ev.target;
    if (target instanceof Element) {
      if (target.closest('button,a,input,textarea,select,[contenteditable="true"]')) return;
      const selection = window.getSelection();
      if (selection && !selection.isCollapsed) return;
    }
    focusChatInput({ placeCursorAtEnd: true });
  });
}

function warmMicStream() {
  if (!canUseMicrophoneCapture()) return;
  acquireMicStream().catch(() => {});
}

async function init() {
  bindUi();
  warmMicStream();
  updateAssistantActivityIndicator();
  startDevReloadWatcher();
  startAssistantActivityWatcher();
  initCanvasControls();
  setProjectOverviewVisible(false);
  clearCanvas();
  setActiveTab('chat');
  showStatus('starting...');

  await fetchProjects();
  const initialProjectID = resolveInitialProjectID();
  if (!initialProjectID) {
    throw new Error('no projects available');
  }
  await switchProject(initialProjectID);
}

init().catch((err) => {
  showStatus('failed');
  appendPlainMessage('system', `Initialization failed: ${String(err?.message || err)}`);
});
