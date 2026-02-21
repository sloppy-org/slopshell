import { marked } from './vendor/marked.esm.js';
import { renderCanvas, clearCanvas, initCanvasControls } from './canvas.js';

const state = {
  sessionId: 'local',
  canvasWs: null,
  chatWs: null,
  chatSessionId: '',
  chatMode: 'chat',
  activeTab: 'chat',
  canvasHasUnread: false,
  pendingByTurn: new Map(),
  pendingQueue: [],
  chatCtrlHoldTimer: null,
  chatVoiceCapture: null,
};

export function getState() {
  return state;
}

window._tabulaApp = { getState };

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABULA_CHAT_MATH_SEGMENT_';
let localMessageSeq = 0;
const CHAT_CTRL_LONG_PRESS_MS = 180;
const sttActionStart = 'start';
const sttActionAppend = 'append';
const sttActionStop = 'stop';
const sttActionCancel = 'cancel';

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

function createPushToPromptSessionID() {
  const rand = Math.random().toString(36).slice(2, 10);
  return `ptp-chat-${Date.now().toString(36)}-${rand}`;
}

function base64FromBytes(bytes) {
  if (!bytes || !bytes.length) return '';
  const chunkSize = 0x8000;
  let out = '';
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    out += String.fromCharCode(...chunk);
  }
  return btoa(out);
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

async function callPushToPromptAction(action, actionPayload = {}) {
  const req = {
    action,
    ...actionPayload,
  };
  const customMCPURL = String(window.__TABULA_VOXTYPE_MCP_URL || '').trim();
  if (customMCPURL) {
    req.voxtype_mcp_url = customMCPURL;
  }
  const resp = await fetch('/api/stt/push-to-prompt', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  let responsePayload = {};
  const raw = await resp.text();
  if (raw) {
    try {
      responsePayload = JSON.parse(raw);
    } catch (_) {
      if (!resp.ok) {
        throw new Error(raw);
      }
    }
  }
  if (!resp.ok) {
    throw new Error(typeof responsePayload === 'object' && responsePayload !== null && responsePayload.error
      ? responsePayload.error
      : raw || 'push-to-prompt request failed');
  }
  if (typeof responsePayload !== 'object' || responsePayload === null) {
    throw new Error('push-to-prompt request returned invalid response');
  }
  return responsePayload;
}

function canUseMicrophoneCapture() {
  return Boolean(window.MediaRecorder)
    && Boolean(navigator.mediaDevices)
    && typeof navigator.mediaDevices.getUserMedia === 'function';
}

async function appendChatVoiceChunk(capture, chunkBlob) {
  if (!capture?.sttSessionID) return;
  if (!chunkBlob || typeof chunkBlob.arrayBuffer !== 'function' || chunkBlob.size <= 0) return;
  const bytes = new Uint8Array(await chunkBlob.arrayBuffer());
  if (!bytes.length) return;
  const seq = Number(capture.appendSeq || 0);
  capture.appendSeq = seq + 1;
  const payload = {
    session_id: capture.sttSessionID,
    seq,
    audio_base64: base64FromBytes(bytes),
  };
  const chain = capture.appendChain || Promise.resolve();
  capture.appendChain = chain.then(() => callPushToPromptAction(sttActionAppend, payload));
  await capture.appendChain;
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
  if (capture.mediaStream && typeof capture.mediaStream.getTracks === 'function') {
    capture.mediaStream.getTracks().forEach((track) => {
      if (track && typeof track.stop === 'function') {
        track.stop();
      }
    });
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

async function beginChatVoiceCapture() {
  if (state.activeTab !== 'chat') return;
  if (state.chatVoiceCapture) return;
  const capture = {
    active: false,
    stopping: false,
    stopRequested: false,
    sttSessionID: createPushToPromptSessionID(),
    appendSeq: 0,
    appendChain: Promise.resolve(),
    appendError: '',
    captureBackend: '',
    mediaStream: null,
    mediaRecorder: null,
  };
  state.chatVoiceCapture = capture;
  showStatus('push-to-prompt recording...');
  try {
    const startResp = await callPushToPromptAction(sttActionStart, {
      session_id: capture.sttSessionID,
      mime_type: 'audio/webm',
    });
    capture.active = true;
    capture.captureBackend = String(startResp?.capture_backend || '').trim().toLowerCase() || 'buffered';
    if (capture.captureBackend === 'daemon') {
      return;
    }
    if (!canUseMicrophoneCapture()) {
      throw new Error('Microphone capture is unavailable in this browser.');
    }
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    if (state.chatVoiceCapture !== capture) {
      if (typeof stream.getTracks === 'function') {
        stream.getTracks().forEach((track) => {
          if (track && typeof track.stop === 'function') track.stop();
        });
      }
      return;
    }
    const recorder = newMediaRecorder(stream);
    capture.mediaStream = stream;
    capture.mediaRecorder = recorder;
    recorder.addEventListener('dataavailable', (ev) => {
      if (!ev?.data || ev.data.size <= 0) return;
      const chain = appendChatVoiceChunk(capture, ev.data);
      capture.appendChain = chain.catch((err) => {
        capture.appendError = String(err?.message || err || 'audio chunk append failed');
        throw err;
      });
      void capture.appendChain.catch(() => {});
    });
    recorder.start(300);
    if (capture.stopRequested) {
      await stopChatVoiceMediaAndFlush(capture);
    }
  } catch (err) {
    const message = String(err?.message || err || 'push-to-prompt failed');
    showStatus(`push-to-prompt failed: ${message}`);
    if (capture.sttSessionID) {
      void callPushToPromptAction(sttActionCancel, { session_id: capture.sttSessionID }).catch(() => {});
    }
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
  }
}

async function stopChatVoiceCaptureAndApply() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  capture.stopping = true;
  capture.stopRequested = true;
  let remoteStopped = false;
  try {
    if (!capture.active) {
      return;
    }
    await stopChatVoiceMediaAndFlush(capture);
    await (capture.appendChain || Promise.resolve());
    if (capture.appendError) {
      throw new Error(capture.appendError);
    }
    const stt = await callPushToPromptAction(sttActionStop, { session_id: capture.sttSessionID });
    remoteStopped = true;
    const transcript = String(stt?.text || '').trim();
    if (!transcript) {
      throw new Error('speech recognizer returned empty text');
    }
    const input = chatInputEl();
    if (!input) return;
    const needsSpace = input.value.trim() && !/[ \n]$/.test(input.value);
    input.value = `${input.value}${needsSpace ? ' ' : ''}${transcript}`;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    focusChatInput({ placeCursorAtEnd: true });
    showStatus('dictation ready (press Enter to send)');
  } catch (err) {
    const message = String(err?.message || err || 'push-to-prompt failed');
    showStatus(`push-to-prompt failed: ${message}`);
  } finally {
    if (!remoteStopped && capture?.sttSessionID) {
      void callPushToPromptAction(sttActionCancel, { session_id: capture.sttSessionID }).catch(() => {});
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
  if (capture.sttSessionID) {
    void callPushToPromptAction(sttActionCancel, { session_id: capture.sttSessionID }).catch(() => {});
  }
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
  return row;
}

async function ensureChatSession() {
  const resp = await fetch('/api/chat/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  });
  if (!resp.ok) {
    throw new Error(`chat session create failed: HTTP ${resp.status}`);
  }
  const payload = await resp.json();
  state.chatSessionId = String(payload.session_id || '');
  setChatMode(payload.mode || 'chat');
  if (!state.chatSessionId) {
    throw new Error('chat session ID missing');
  }
  showStatus('ready');
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
}

function openChatWs() {
  if (!state.chatSessionId) return;
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/chat/${encodeURIComponent(state.chatSessionId)}`;
  const ws = new WebSocket(wsUrl);
  state.chatWs = ws;

  ws.onopen = () => {
    showStatus('chat connected');
  };

  ws.onmessage = (event) => {
    let payload = null;
    try {
      payload = JSON.parse(event.data);
    } catch (_) {
      return;
    }
    handleChatEvent(payload);
  };

  ws.onclose = () => {
    state.chatWs = null;
    showStatus('reconnecting chat...');
    window.setTimeout(() => openChatWs(), 1200);
  };
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
    ensurePendingForTurn(payload.turn_id);
    return;
  }

  if (type === 'assistant_message') {
    const turnID = String(payload.turn_id || '').trim();
    const row = ensurePendingForTurn(turnID);
    updateAssistantRow(row, String(payload.message || ''), true);
    return;
  }

  if (type === 'message_persisted') {
    if (String(payload.role || '') !== 'assistant') return;
    const turnID = String(payload.turn_id || '').trim();
    let row = null;
    if (turnID && state.pendingByTurn.has(turnID)) {
      row = state.pendingByTurn.get(turnID);
      state.pendingByTurn.delete(turnID);
    } else {
      row = state.pendingQueue.shift() || null;
    }
    if (row) {
      updateAssistantRow(row, String(payload.message || ''), false);
    } else {
      appendRenderedAssistant(String(payload.message || ''));
    }
    return;
  }

  if (type === 'error') {
    const errText = String(payload.error || 'assistant request failed');
    appendPlainMessage('system', errText);
  }
}

async function sendChatMessage() {
  const input = document.getElementById('chat-input');
  if (!(input instanceof HTMLTextAreaElement)) return;
  const text = input.value.trim();
  if (!text || !state.chatSessionId) return;
  input.value = '';
  input.style.height = 'auto';
  focusChatInput({ placeCursorAtEnd: true });
  appendPlainMessage('user', text);

  if (!text.startsWith('/')) {
    const pending = appendRenderedAssistant('_Thinking..._', { pending: true, localId: nextLocalMessageId() });
    state.pendingQueue.push(pending);
  }

  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text }),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      appendPlainMessage('system', `Send failed: ${detail}`);
      return;
    }
    const payload = await resp.json();
    if (payload?.kind === 'command' && payload?.result?.message) {
      appendPlainMessage('system', String(payload.result.message));
    }
  } catch (err) {
    appendPlainMessage('system', `Send failed: ${String(err?.message || err)}`);
  }
  focusChatInput({ placeCursorAtEnd: true });
}

function openCanvasWs() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/canvas/${encodeURIComponent(state.sessionId)}`;
  const ws = new WebSocket(wsUrl);
  state.canvasWs = ws;

  ws.onopen = () => {
    void loadCanvasSnapshot();
  };

  ws.onmessage = (event) => {
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
    state.canvasWs = null;
    window.setTimeout(() => openCanvasWs(), 1200);
  };
}

async function loadCanvasSnapshot() {
  try {
    const resp = await fetch(`/api/canvas/${encodeURIComponent(state.sessionId)}/snapshot`);
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
    const resp = await fetch(`/api/canvas/${encodeURIComponent(state.sessionId)}/commit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ include_draft: true }),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      appendPlainMessage('system', `Commit failed: ${detail}`);
      return;
    }
    appendPlainMessage('system', 'Draft annotations committed.');
  } catch (err) {
    appendPlainMessage('system', `Commit failed: ${String(err?.message || err)}`);
  }
}

function bindUi() {
  document.getElementById('tab-chat')?.addEventListener('click', () => setActiveTab('chat'));
  document.getElementById('tab-canvas')?.addEventListener('click', () => setActiveTab('canvas'));

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
    if (state.activeTab !== 'chat') return;

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

async function init() {
  bindUi();
  initCanvasControls();
  clearCanvas();
  setActiveTab('chat');
  showStatus('starting...');

  openCanvasWs();
  await ensureChatSession();
  await loadChatHistory();
  openChatWs();
  focusChatInput({ placeCursorAtEnd: true });
}

init().catch((err) => {
  showStatus('failed');
  appendPlainMessage('system', `Initialization failed: ${String(err?.message || err)}`);
});
