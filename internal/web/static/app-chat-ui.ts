import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const openWorkspaceSidebarFile = (...args) => refs.openWorkspaceSidebarFile(...args);
const switchProject = (...args) => refs.switchProject(...args);
const showCanvasColumn = (...args) => refs.showCanvasColumn(...args);
const chatHistoryEl = (...args) => refs.chatHistoryEl(...args);
const syncChatScroll = (...args) => refs.syncChatScroll(...args);
const setTTSSilentMode = (...args) => refs.setTTSSilentMode(...args);
const sendChatWsJSON = (...args) => refs.sendChatWsJSON(...args);
const parseOptionalBoolean = (...args) => refs.parseOptionalBoolean(...args);
const updateRuntimePreferences = (...args) => refs.updateRuntimePreferences(...args);

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_CHAT_MATH_SEGMENT_';
let localMessageSeq = 0;
const renderer = new marked.Renderer();
renderer.code = ({ text, lang }) => {
  const safeLang = escapeHtml((lang || 'plaintext').toLowerCase());
  return `<pre><code class="language-${safeLang}">${escapeHtml(text || '')}</code></pre>\n`;
};
marked.setOptions({ breaks: true, renderer });

export function extractMathSegments(markdownSource) {
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

export function restoreMathSegments(renderedHtml, mathSegments) {
  let output = String(renderedHtml || '');
  for (let i = 0; i < mathSegments.length; i += 1) {
    const token = `${MATH_SEGMENT_TOKEN_PREFIX}${i}@@`;
    output = output.replaceAll(token, escapeHtml(String(mathSegments[i] || '')));
  }
  return output;
}

export function typesetMath(root, attempt = 0) {
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

export function trackAssistantTurnStarted(turnID) {
  state.assistantLastError = '';
  state.assistantLastStartedAt = Date.now();
  const key = String(turnID || '').trim();
  if (key) {
    state.assistantActiveTurns.add(key);
  } else {
    state.assistantUnknownTurns += 1;
  }
  updateAssistantActivityIndicator();
}

export function trackAssistantTurnFinished(turnID) {
  const key = String(turnID || '').trim();
  if (key) {
    state.voiceTurns.delete(key);
    if (!state.assistantActiveTurns.delete(key) && state.assistantUnknownTurns > 0) {
      state.assistantUnknownTurns -= 1;
    }
  } else if (state.assistantUnknownTurns > 0) {
    state.assistantUnknownTurns -= 1;
  } else if (state.assistantActiveTurns.size > 0) {
    if ((Date.now() - state.assistantLastStartedAt) < ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS) {
      updateAssistantActivityIndicator();
      return;
    }
    // Some cancel/error events can arrive without turn_id. In that case, clear
    // one active local turn so the stop indicator cannot get stuck indefinitely.
    const firstActiveTurn = state.assistantActiveTurns.values().next().value;
    if (firstActiveTurn) {
      state.voiceTurns.delete(firstActiveTurn);
      state.assistantActiveTurns.delete(firstActiveTurn);
    }
  }
  updateAssistantActivityIndicator();
}

export function takePendingRow(turnID) {
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

export function takeAnyPendingRow() {
  if (state.pendingByTurn.size > 0) {
    const first = state.pendingByTurn.entries().next().value;
    if (Array.isArray(first) && first.length >= 2) {
      const key = String(first[0] || '').trim();
      const row = first[1] || null;
      if (key) state.pendingByTurn.delete(key);
      updateAssistantActivityIndicator();
      return row;
    }
  }
  const row = state.pendingQueue.shift() || null;
  updateAssistantActivityIndicator();
  return row;
}

export function nextLocalMessageId() {
  localMessageSeq += 1;
  return `local-msg-${Date.now()}-${localMessageSeq}`;
}

// Chat history log (diagnostics pane)
export function appendPlainMessage(role, text, options: Record<string, any> = {}) {
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

export function approvalDecisionLabel(decision) {
  const value = String(decision || '').trim().toLowerCase();
  if (value === 'accept' || value === 'approve') return 'Approved';
  if (value === 'decline' || value === 'reject') return 'Rejected';
  return 'Cancelled';
}

export function setApprovalButtonsDisabled(row, disabled) {
  if (!(row instanceof HTMLElement)) return;
  row.querySelectorAll('button[data-approval-decision]').forEach((button) => {
    if (button instanceof HTMLButtonElement) {
      button.disabled = disabled;
    }
  });
}

export function renderApprovalRequestCard(payload) {
  const requestID = String(payload?.request_id || '').trim();
  if (!requestID) return null;
  let row = state.pendingApprovals.get(requestID) || null;
  if (!(row instanceof HTMLElement)) {
    row = appendPlainMessage('system', '', { localId: requestID });
    if (!(row instanceof HTMLElement)) return null;
    row.classList.add('chat-approval-request');
    state.pendingApprovals.set(requestID, row);
  }
  const bubble = row.querySelector('.chat-bubble');
  if (!(bubble instanceof HTMLElement)) return row;
  const description = String(payload?.description || 'Approval required').trim();
  const action = String(payload?.action || payload?.request_kind || '').trim().replace(/_/g, ' ');
  const reason = String(payload?.reason || '').trim();
  const grantRoot = String(payload?.grant_root || '').trim();
  bubble.innerHTML = '';

  const title = document.createElement('div');
  title.className = 'chat-approval-title';
  title.textContent = description;
  bubble.appendChild(title);

  if (action) {
    const meta = document.createElement('div');
    meta.className = 'chat-approval-meta';
    meta.textContent = action;
    bubble.appendChild(meta);
  }
  if (reason) {
    const detail = document.createElement('div');
    detail.className = 'chat-approval-detail';
    detail.textContent = reason;
    bubble.appendChild(detail);
  }
  if (grantRoot) {
    const scope = document.createElement('div');
    scope.className = 'chat-approval-detail';
    scope.textContent = `scope: ${grantRoot}`;
    bubble.appendChild(scope);
  }

  const actions = document.createElement('div');
  actions.className = 'chat-approval-actions';
  [
    ['Approve', 'accept'],
    ['Reject', 'decline'],
    ['Cancel', 'cancel'],
  ].forEach(([label, decision]) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'chat-approval-btn';
    button.dataset.approvalDecision = decision;
    button.textContent = label;
    button.addEventListener('click', () => {
      setApprovalButtonsDisabled(row, true);
      if (!sendChatWsJSON({ type: 'approval_response', request_id: requestID, decision })) {
        setApprovalButtonsDisabled(row, false);
        showStatus('approval send failed');
      }
    });
    actions.appendChild(button);
  });
  bubble.appendChild(actions);
  syncChatScroll(chatHistoryEl());
  return row;
}

export function resolveApprovalRequestCard(requestID, decision) {
  const key = String(requestID || '').trim();
  if (!key) return;
  const row = state.pendingApprovals.get(key);
  if (!(row instanceof HTMLElement)) return;
  setApprovalButtonsDisabled(row, true);
  row.classList.add('is-resolved');
  const bubble = row.querySelector('.chat-bubble');
  if (!(bubble instanceof HTMLElement)) return;
  let status = row.querySelector('.chat-approval-status');
  if (!(status instanceof HTMLElement)) {
    status = document.createElement('div');
    status.className = 'chat-approval-status';
    bubble.appendChild(status);
  }
  status.textContent = approvalDecisionLabel(decision);
}

function normalizeAssistantProvider(provider) {
  const value = String(provider || '').trim().toLowerCase();
  if (value === 'local' || value === 'cerebras' || value === 'google' || value === 'openai') return value;
  return '';
}

function assistantProviderLabel(provider, explicitLabel = '') {
  const label = String(explicitLabel || '').trim();
  if (label) return label;
  switch (normalizeAssistantProvider(provider)) {
    case 'local':
      return 'Local';
    case 'cerebras':
      return 'Cerebras';
    case 'google':
      return 'Google';
    case 'openai':
      return 'OpenAI';
    default:
      return 'Assistant';
  }
}

function setAssistantRowProvider(row, options: Record<string, any> = {}) {
  if (!(row instanceof HTMLElement)) return;
  const meta = row.querySelector('.chat-message-meta');
  if (meta instanceof HTMLElement) meta.textContent = '';
  const label = row.querySelector('.chat-assistant-label');
  if (!(label instanceof HTMLElement)) return;
  const provider = normalizeAssistantProvider(options.provider);
  const display = assistantProviderLabel(provider, options.providerLabel);
  label.textContent = display;
  label.dataset.provider = provider || 'assistant';
  row.dataset.provider = provider;
  const model = String(options.providerModel || '').trim();
  if (model) {
    label.title = model;
  } else {
    label.removeAttribute('title');
  }
}

export function appendRenderedAssistant(markdownText, options: Record<string, any> = {}) {
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

  const bubble = document.createElement('div');
  bubble.className = 'chat-bubble markdown';
  const progress = document.createElement('div');
  progress.className = 'chat-bubble-progress';
  const body = document.createElement('div');
  body.className = 'chat-bubble-body';
  const label = document.createElement('div');
  label.className = 'chat-assistant-label';
  const content = document.createElement('div');
  content.className = 'chat-assistant-content';
  const { text: markdownBody, stash: mathSegments } = extractMathSegments(markdownText);
  const rendered = marked.parse(markdownBody || '');
  content.innerHTML = restoreMathSegments(sanitizeHtml(rendered), mathSegments);
  body.appendChild(label);
  body.appendChild(content);
  bubble.appendChild(progress);
  bubble.appendChild(body);
  row.appendChild(meta);
  row.appendChild(bubble);
  host.appendChild(row);
  setAssistantRowProvider(row, options);
  syncChatScroll(host);
  void typesetMath(content).finally(() => syncChatScroll(host));
  return row;
}

export function assistantRowBodyEl(row) {
  if (!(row instanceof HTMLElement)) return null;
  const content = row.querySelector('.chat-assistant-content');
  if (content instanceof HTMLElement) return content;
  const body = row.querySelector('.chat-bubble-body');
  if (body instanceof HTMLElement) return body;
  const bubble = row.querySelector('.chat-bubble');
  return bubble instanceof HTMLElement ? bubble : null;
}

export function ensureAssistantProgressEl(row) {
  if (!(row instanceof HTMLElement)) return null;
  const bubble = row.querySelector('.chat-bubble');
  if (!(bubble instanceof HTMLElement)) return null;
  let progress = bubble.querySelector('.chat-bubble-progress');
  if (progress instanceof HTMLElement) return progress;
  progress = document.createElement('div');
  progress.className = 'chat-bubble-progress';
  const body = assistantRowBodyEl(row);
  if (body && body !== bubble && body.parentElement === bubble) {
    bubble.insertBefore(progress, body);
  } else {
    bubble.prepend(progress);
  }
  return progress;
}

export function appendAssistantProgressLine(row, text) {
  if (!(row instanceof HTMLElement)) return;
  const lineText = String(text || '').trim();
  if (!lineText) return;
  const progress = ensureAssistantProgressEl(row);
  if (!(progress instanceof HTMLElement)) return;
  const line = document.createElement('div');
  line.className = 'chat-bubble-progress-line';
  line.textContent = lineText;
  progress.appendChild(line);
  const host = chatHistoryEl();
  syncChatScroll(host);
}

export function findAssistantRowForTurn(turnID) {
  const key = String(turnID || '').trim();
  if (key && state.pendingByTurn.has(key)) {
    return state.pendingByTurn.get(key);
  }
  const host = chatHistoryEl();
  if (!host) return null;
  const rows = host.querySelectorAll('.chat-message.chat-assistant');
  for (let i = rows.length - 1; i >= 0; i -= 1) {
    const row = rows[i];
    if (!(row instanceof HTMLElement)) continue;
    if (key && row.dataset.turnId === key) return row;
    if (!key && row.classList.contains('is-pending')) return row;
  }
  return null;
}

export function humanizeItemTypeLabel(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  return value
    .replace(/[._-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

export function formatItemCompletedLabel(payload) {
  const label = humanizeItemTypeLabel(payload?.item_type);
  const detail = String(payload?.detail || '').trim();
  if (!label && !detail) return '';
  if (!label) return detail;
  if (!detail) return label;
  return `${label}: ${detail}`;
}

export function appendAssistantProgressForTurn(turnID, text) {
  const line = String(text || '').trim();
  if (!line) return;
  const existing = findAssistantRowForTurn(turnID);
  const row = existing || ensurePendingForTurn(turnID);
  if (!(row instanceof HTMLElement)) return;
  appendAssistantProgressLine(row, line);
}

export function updateAssistantRow(row, markdownText, pending = true, options: Record<string, any> = {}) {
  if (!row) return;
  const host = chatHistoryEl();
  row.classList.toggle('is-pending', pending);
  setAssistantRowProvider(row, options);
  const body = assistantRowBodyEl(row);
  if (!(body instanceof HTMLElement)) return;
  const { text: markdownBody, stash: mathSegments } = extractMathSegments(markdownText);
  const rendered = marked.parse(markdownBody || '');
  body.innerHTML = restoreMathSegments(sanitizeHtml(rendered), mathSegments);
  syncChatScroll(host);
  void typesetMath(body).finally(() => syncChatScroll(host));
}

export function ensurePendingForTurn(turnID) {
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

export function resetAssistantTurnTracking({ clearError = false } = {}) {
  state.pendingByTurn.clear();
  state.pendingApprovals.clear();
  state.pendingQueue = [];
  state.voiceTurns.clear();
  state.assistantActiveTurns.clear();
  state.assistantUnknownTurns = 0;
  state.assistantRemoteActiveCount = 0;
  state.assistantRemoteQueuedCount = 0;
  state.assistantCancelInFlight = false;
  state.voiceTranscriptSubmitInFlight = false;
  state.voiceAwaitingTurn = false;
  state.indicatorSuppressedByCanvasUpdate = false;
  if (clearError) {
    state.assistantLastError = '';
  }
  updateAssistantActivityIndicator();
}

export function clearChatHistory() {
  const host = chatHistoryEl();
  if (host) host.innerHTML = '';
  state.pendingApprovals.clear();
}

export function clearWelcomeSurface() {
  state.welcomeSurface = null;
  const canvasText = document.getElementById('canvas-text');
  if (canvasText instanceof HTMLElement) {
    canvasText.classList.remove('welcome-surface');
  }
}

export function activeWelcomeProjectID() {
  if (state.welcomeSurface && typeof state.welcomeSurface === 'object') {
    return String(state.welcomeSurface.project_id || '').trim();
  }
  return '';
}

export async function handleWelcomeAction(action) {
  const type = String(action?.type || '').trim();
  if (!type) return;
  if (type === 'switch_project') {
    const projectID = String(action?.project_id || '').trim();
    if (!projectID) return;
    await switchProject(projectID);
    return;
  }
  if (type === 'open_file') {
    const filePath = String(action?.path || '').trim();
    if (filePath) {
      await openWorkspaceSidebarFile(filePath);
    }
    return;
  }
  if (type === 'set_silent_mode') {
    const next = parseOptionalBoolean(action?.silent_mode);
    if (next !== null) {
      await updateRuntimePreferences({ silent_mode: next });
      setTTSSilentMode(next, { persist: false });
    }
    return;
  }
  if (type === 'set_startup_behavior') {
    await updateRuntimePreferences({ startup_behavior: 'resume_active' });
  }
}

export function renderWelcomeSurface(payload) {
  const canvasText = document.getElementById('canvas-text');
  if (!(canvasText instanceof HTMLElement)) return;
  const sections = Array.isArray(payload?.sections) ? payload.sections : [];
  const title = String(payload?.title || 'Welcome').trim() || 'Welcome';
  const subtitle = 'Pick up a recent file, open docs, or switch modes before asking.';
  const normalizedSections = sections.map((section, index) => ({
    ...section,
    _sectionIndex: index,
  }));
  const sectionHtml = normalizedSections.map((section) => {
    const cards = Array.isArray(section?.cards) ? section.cards : [];
    const cardsHtml = cards.map((card, index) => `
      <button
        type="button"
        class="welcome-card"
        data-section-index="${Number(section?._sectionIndex ?? 0)}"
        data-card-index="${index}"
      >
        <span class="welcome-card-title">${escapeHtml(String(card?.title || 'Open'))}</span>
        ${card?.subtitle ? `<span class="welcome-card-subtitle">${escapeHtml(String(card.subtitle || ''))}</span>` : ''}
        ${card?.description ? `<span class="welcome-card-description">${escapeHtml(String(card.description || ''))}</span>` : ''}
      </button>
    `).join('');
    return `
      <section class="welcome-section">
        <div class="welcome-section-title">${escapeHtml(String(section?.title || 'Section'))}</div>
        <div class="welcome-card-grid">${cardsHtml}</div>
      </section>
    `;
  }).join('');
  state.welcomeSurface = {
    ...payload,
    sections: normalizedSections,
  };
  canvasText.classList.add('welcome-surface');
  canvasText.innerHTML = `
    <div class="welcome-surface-root">
      <div>
        <div class="welcome-surface-title">${escapeHtml(title)}</div>
        <div class="welcome-surface-subtitle">${escapeHtml(subtitle)}</div>
      </div>
      ${sectionHtml}
    </div>
  `;
  canvasText.querySelectorAll('.welcome-card').forEach((node) => {
    node.addEventListener('click', (event) => {
      const target = event.currentTarget;
      if (!(target instanceof HTMLElement)) return;
      const sectionIndex = Number.parseInt(target.dataset.sectionIndex || '', 10);
      const cardIndex = Number.parseInt(target.dataset.cardIndex || '', 10);
      if (!Number.isFinite(sectionIndex) || !Number.isFinite(cardIndex)) return;
      const section = state.welcomeSurface?.sections?.[sectionIndex];
      const card = section?.cards?.[cardIndex];
      if (!card?.action) return;
      void handleWelcomeAction(card.action);
    });
  });
  showCanvasColumn('canvas-text');
}

export async function fetchProjectWelcome(projectID = 'active') {
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(projectID)}/welcome`), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  return resp.json();
}

export async function showWelcomeForActiveProject(force = false) {
  void force;
  clearWelcomeSurface();
}

export function shouldUseBottomComposer() {
  return window.matchMedia('(max-width: 767px)').matches;
}

export function openComposerAt(x, y, anchor = null, initialText = '') {
  const text = String(initialText || '');
  if (shouldUseBottomComposer()) {
    const edgeRight = document.getElementById('edge-right');
    const input = document.getElementById('chat-pane-input');
    setInputAnchor(anchor);
    if (edgeRight instanceof HTMLElement) {
      edgeRight.classList.add('edge-active', 'edge-pinned');
    }
    if (input instanceof HTMLTextAreaElement) {
      input.focus();
      input.value = text;
      const caret = text.length;
      input.setSelectionRange(caret, caret);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return;
  }
  showTextInput(x, y, anchor);
  if (!text) return;
  const input = document.getElementById('floating-input');
  if (input instanceof HTMLTextAreaElement) {
    input.value = text;
    const caret = text.length;
    input.setSelectionRange(caret, caret);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }
}
