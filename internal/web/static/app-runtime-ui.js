import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

let runtimeReloadBootID = '';
let runtimeReloadTimer = null;
let runtimeReloadInFlight = false;
let runtimeReloadRequested = false;
let panelMotionWatchersAttached = false;
let suppressClickUntil = 0;
const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_CHAT_MATH_SEGMENT_';
const renderEdgeTopModelButtons = (...args) => refs.renderEdgeTopModelButtons(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const beginConversationVoiceCapture = (...args) => refs.beginConversationVoiceCapture(...args);
const acquireMicStream = (...args) => refs.acquireMicStream(...args);
const parseOptionalBoolean = (...args) => refs.parseOptionalBoolean(...args);
const readYoloModePreference = (...args) => refs.readYoloModePreference(...args);
const setYoloModeLocal = (...args) => refs.setYoloModeLocal(...args);
const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const renderInkControls = (...args) => refs.renderInkControls(...args);
const showWelcomeForActiveProject = (...args) => refs.showWelcomeForActiveProject(...args);
const appendPlainMessage = (...args) => refs.appendPlainMessage(...args);
const stopTTSPlayback = (...args) => refs.stopTTSPlayback(...args);
const canSpeakTTS = (...args) => refs.canSpeakTTS(...args);
const canStartLiveDialogueListen = (...args) => refs.canStartLiveDialogueListen(...args);
const requestHotwordSync = (...args) => refs.requestHotwordSync(...args);
const applyLiveSessionStateSnapshot = (...args) => refs.applyLiveSessionStateSnapshot(...args);
const syncInputModeBodyState = (...args) => refs.syncInputModeBodyState(...args);
const isLikelyIOS = (...args) => refs.isLikelyIOS(...args);
const shouldStopInUiClick = (...args) => refs.shouldStopInUiClick(...args);

export function mediaQueryMatches(query) {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  try {
    return window.matchMedia(query).matches;
  } catch (_) {
    return false;
  }
}

export function shouldEnablePanelMotion() {
  if (mediaQueryMatches('(prefers-reduced-motion: reduce)')) return false;
  if (mediaQueryMatches('(monochrome)')) return false;
  if (mediaQueryMatches('(update: slow)')) return false;
  return true;
}

export function syncPanelMotionMode() {
  document.body.classList.toggle('panel-motion-enabled', shouldEnablePanelMotion());
}

export function initPanelMotionMode() {
  syncPanelMotionMode();
  if (panelMotionWatchersAttached) return;
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
  panelMotionWatchersAttached = true;
  PANEL_MOTION_WATCH_QUERIES.forEach((query) => {
    let mql = null;
    try {
      mql = window.matchMedia(query);
    } catch (_) {
      mql = null;
    }
    if (!mql) return;
    const onChange = () => syncPanelMotionMode();
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', onChange);
      return;
    }
    if (typeof mql.addListener === 'function') {
      mql.addListener(onChange);
    }
  });
}

export function isMobileSilent() {
  return state.ttsSilent && window.matchMedia('(max-width: 767px)').matches;
}

// iPhone corner-radius profiles for bottom-edge frame rounding.
const IPHONE_CORNER_RADIUS_PROFILES = [
  { shortSide: 375, longSide: 812, dpr: 3, radius: 44 },
  { shortSide: 390, longSide: 844, dpr: 3, radius: 47 },
  { shortSide: 393, longSide: 852, dpr: 3, radius: 55 },
  { shortSide: 402, longSide: 874, dpr: 3, radius: 62 },
  { shortSide: 414, longSide: 896, dpr: 2, radius: 41 },
  { shortSide: 428, longSide: 926, dpr: 3, radius: 53 },
  { shortSide: 430, longSide: 932, dpr: 3, radius: 55 },
  { shortSide: 440, longSide: 956, dpr: 3, radius: 62 },
];

export function isIPhoneStandalone() {
  const ua = String(navigator.userAgent || '').toLowerCase();
  const plat = String(navigator.platform || '').toLowerCase();
  const isIPhone = /iphone/.test(ua) || plat === 'iphone' || (plat === 'macintel' && navigator.maxTouchPoints > 1);
  if (!isIPhone) return false;
  try {
    return navigator.standalone === true || window.matchMedia('(display-mode: standalone)').matches;
  } catch (_) {
    return false;
  }
}

export function applyIPhoneFrameCorners() {
  const root = document.documentElement;
  if (!isIPhoneStandalone()) {
    root.style.removeProperty('--cue-corner-radius');
    return;
  }
  const short = Math.min(Math.round(screen.width), Math.round(screen.height));
  const long = Math.max(Math.round(screen.width), Math.round(screen.height));
  const dpr = Math.max(1, Math.round(devicePixelRatio || 1));
  const match = IPHONE_CORNER_RADIUS_PROFILES.find(
    (p) => p.shortSide === short && p.longSide === long && p.dpr === dpr,
  );
  const r = match ? match.radius : (dpr >= 3 ? 55 : 44);
  root.style.setProperty('--cue-corner-radius', `0 0 ${r}px ${r}px`);
}

let syncKeyboardStateNow = null;

export function setSyncKeyboardStateNow(sync) {
  syncKeyboardStateNow = typeof sync === 'function' ? sync : null;
}

export function isFocusedTextInput() {
  const el = document.activeElement;
  if (!el) return false;
  if (el instanceof HTMLTextAreaElement) return true;
  if (el instanceof HTMLInputElement) {
    const type = String(el.type || 'text').toLowerCase();
    return ![
      'button', 'checkbox', 'color', 'file', 'hidden',
      'image', 'radio', 'range', 'reset', 'submit',
    ].includes(type);
  }
  return el instanceof HTMLElement && el.isContentEditable;
}

export function clearKeyboardOpenState() {
  const inputRow = document.querySelector('.chat-pane-input-row');
  if (inputRow) inputRow.classList.remove('keyboard-open');
  document.body.classList.remove('keyboard-open');
  if (isIPhoneStandalone()) applyIPhoneFrameCorners();
}

export function settleKeyboardAfterSubmit() {
  clearKeyboardOpenState();
  const sync = syncKeyboardStateNow;
  if (typeof sync !== 'function') return;
  [0, 100, 220, 380, 600, 900, 1300].forEach((delay) => {
    window.setTimeout(() => {
      if (syncKeyboardStateNow !== sync) return;
      sync();
    }, delay);
  });
}

export function setTTSSilentMode(silent, { persist = true, pinPanel = true } = {}) {
  const next = Boolean(silent);
  state.ttsSilent = next;
  if (next) {
    cancelLiveSessionListen();
    stopTTSPlayback();
    document.body.classList.add('silent-mode');
    if (pinPanel && window.matchMedia('(max-width: 767px)').matches) {
      const edgeRight = document.getElementById('edge-right');
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    }
  } else {
    document.body.classList.remove('silent-mode');
  }
  renderEdgeTopModelButtons();
  requestHotwordSync();
}

export function toggleTTSSilentMode() {
  if (!state.ttsEnabled) return;
  const next = !state.ttsSilent;
  updateRuntimePreferences({ silent_mode: next })
    .then(() => {
      setTTSSilentMode(next, { persist: false });
      showStatus(next ? 'silent mode on' : 'voice mode on');
      void showWelcomeForActiveProject();
    })
    .catch((err) => {
      showStatus(`silent update failed: ${String(err?.message || err || 'unknown error')}`);
      renderEdgeTopModelButtons();
    });
}

// Single shared AudioContext — created once, unlocked via resume() on user
// gesture per Web Audio API best practice (MDN). Safari iOS requires resume()
// to be called from a user-initiated event; once resumed the context stays
// running until the page is closed.
const ttsAudioCtx = new (window.AudioContext || window.webkitAudioContext)();
setHotwordAudioContext(ttsAudioCtx);
export function getTTSAudioContext() {
  return ttsAudioCtx;
}
export function unlockAudioContext() {
  if (ttsAudioCtx.state === 'suspended') {
    ttsAudioCtx.resume().catch(() => {}).finally(() => {
      requestHotwordSync();
    });
    return;
  }
  requestHotwordSync();
}
['touchstart', 'touchend', 'mousedown', 'keydown'].forEach(evt =>
  document.body.addEventListener(evt, unlockAudioContext, { once: false })
);

export function initRuntimeUi() {
  configureLiveSession({
    canStartDialogueListen: canStartLiveDialogueListen,
    onStateChange: (snapshot) => {
      applyLiveSessionStateSnapshot(snapshot);
      renderEdgeTopModelButtons();
      updateAssistantActivityIndicator();
    },
    onDialogueListenTimeout: () => {
      requestHotwordSync();
      updateAssistantActivityIndicator();
    },
    onDialogueSpeechDetected: () => {
      beginConversationVoiceCapture();
    },
    onDialogueListenCancelled: () => {
      requestHotwordSync();
      updateAssistantActivityIndicator();
    },
    onMeetingError: (message) => {
      showStatus(`meeting failed: ${String(message || 'unknown error')}`);
    },
    getAudioContext: () => ttsAudioCtx,
    acquireMicStream,
  });
  applyLiveSessionStateSnapshot();
}

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

export function showStatus(text) {
  const el = document.getElementById('status-text');
  if (el) el.textContent = text;
  const statusEl = document.getElementById('status-label');
  if (statusEl) statusEl.textContent = text;
}

export function suppressSyntheticClick() {
  const ms = isLikelyIOS() ? 1200 : 700;
  suppressClickUntil = Math.max(suppressClickUntil, Date.now() + ms);
}

export function isSuppressedClick() {
  return Date.now() < suppressClickUntil;
}

let lastVoiceCaptureNoticeText = '';
let lastVoiceCaptureNoticeAt = 0;

export function showVoiceCaptureNotice(message, x = null, y = null) {
  const text = String(message || '').trim();
  if (!text) return;
  showStatus(text);
  const now = Date.now();
  if (text !== lastVoiceCaptureNoticeText || now - lastVoiceCaptureNoticeAt > 2000) {
    appendPlainMessage('system', text);
    lastVoiceCaptureNoticeText = text;
    lastVoiceCaptureNoticeAt = now;
  }
  const px = Number.isFinite(x) ? x : Math.floor(window.innerWidth / 2);
  const py = Number.isFinite(y) ? y : Math.floor(window.innerHeight / 2);
  showOverlay(px, py + 20);
  updateOverlay(text);
}

export function microphoneUnavailableMessage() {
  if (!window.isSecureContext) {
    return 'Microphone unavailable on insecure HTTP. Open this site through your HTTPS URL (including reverse-proxy HTTPS) and allow microphone access.';
  }
  if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
    return 'Microphone API unavailable in this browser context. Use Safari/Chrome with microphone access enabled.';
  }
  return 'Microphone unavailable. Check browser microphone permissions and audio input availability.';
}

export function persistRuntimeReloadContext(reason = '') {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  const chatHistory = document.getElementById('chat-history');
  const context = {
    reason: String(reason || '').trim().toLowerCase(),
    activeProjectId: String(state.activeProjectId || '').trim(),
    edgeTopPinned: edgeTop?.classList.contains('edge-pinned') === true,
    edgeRightPinned: edgeRight?.classList.contains('edge-pinned') === true,
    chatScrollTop: chatHistory instanceof HTMLElement ? chatHistory.scrollTop : 0,
    windowScrollX: Number.isFinite(window.scrollX) ? window.scrollX : 0,
    windowScrollY: Number.isFinite(window.scrollY) ? window.scrollY : 0,
    capturedAt: Date.now(),
  };
  try {
    window.sessionStorage.setItem(RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, JSON.stringify(context));
  } catch (_) {}
}

export function consumeRuntimeReloadContext() {
  try {
    const raw = window.sessionStorage.getItem(RUNTIME_RELOAD_CONTEXT_STORAGE_KEY);
    if (!raw) return null;
    window.sessionStorage.removeItem(RUNTIME_RELOAD_CONTEXT_STORAGE_KEY);
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? parsed : null;
  } catch (_) {
    return null;
  }
}

export function restoreRuntimeReloadContext() {
  const context = state.pendingRuntimeReloadContext;
  state.pendingRuntimeReloadContext = null;
  if (!context || typeof context !== 'object') return;
  const edgeTop = document.getElementById('edge-top');
  if (edgeTop instanceof HTMLElement) {
    edgeTop.classList.toggle('edge-pinned', context.edgeTopPinned === true);
  }
  const edgeRight = document.getElementById('edge-right');
  if (edgeRight instanceof HTMLElement) {
    edgeRight.classList.toggle('edge-pinned', context.edgeRightPinned === true);
  }
  const chatHistory = document.getElementById('chat-history');
  if (chatHistory instanceof HTMLElement) {
    const top = Number(context.chatScrollTop);
    chatHistory.scrollTop = Number.isFinite(top) ? top : 0;
  }
  const scrollX = Number(context.windowScrollX);
  const scrollY = Number(context.windowScrollY);
  if (Number.isFinite(scrollX) || Number.isFinite(scrollY)) {
    window.scrollTo(
      Number.isFinite(scrollX) ? scrollX : 0,
      Number.isFinite(scrollY) ? scrollY : 0,
    );
  }
  if (String(context.reason || '').trim().toLowerCase() === 'deployment') {
    state.pendingRuntimeReloadStatus = 'Bug fix applied.';
    showStatus(state.pendingRuntimeReloadStatus);
  }
}

export function forceUiHardReload(reason = 'deployment') {
  persistRuntimeReloadContext(reason);
  const url = new URL(window.location.href);
  url.searchParams.set('__tabura_reload', Date.now().toString(36));
  window.location.replace(url.toString());
}

export function normalizeInputMode(modeRaw) {
  const mode = String(modeRaw || '').trim().toLowerCase();
  if (mode === 'voice') return 'voice';
  if (mode === 'keyboard' || mode === 'typing' || mode === 'text') return 'keyboard';
  return 'pen';
}

export function isPenInputMode() {
  return state.inputMode === 'pen';
}

export function isKeyboardInputMode() {
  return state.inputMode === 'keyboard' || state.inputMode === 'typing';
}

export function renderToolPalette() {
  const host = document.getElementById('tool-palette');
  if (!(host instanceof HTMLElement)) return;
  host.replaceChildren();
  const disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  for (const mode of TOOL_PALETTE_MODES) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'tool-palette-btn';
    button.dataset.mode = mode.id;
    button.setAttribute('aria-label', mode.label);
    button.setAttribute('title', mode.label);
    button.setAttribute('aria-pressed', state.inputMode === mode.id ? 'true' : 'false');
    if (state.inputMode === mode.id) {
      button.classList.add('is-active');
    }
    button.disabled = disabled;
    button.innerHTML = mode.icon;
    button.addEventListener('click', () => {
      updateRuntimePreferences({ input_mode: mode.id })
        .then(() => {
          if (mode.id !== 'pen') {
            clearInkDraft();
          }
          renderInkControls();
          showStatus(`${mode.id} mode on`);
        })
        .catch((err) => {
          showStatus(`input mode failed: ${String(err?.message || err || 'unknown error')}`);
        });
    });
    host.appendChild(button);
  }
}

export async function fetchRuntimeMeta() {
  const resp = await fetch(apiURL('runtime'), {
    cache: 'no-store',
    headers: { 'Cache-Control': 'no-cache' },
  });
  if (!resp.ok) {
    throw new Error(`runtime metadata failed: HTTP ${resp.status}`);
  }
  return resp.json();
}

export function applyRuntimePreferences(runtime) {
  const runtimeYolo = parseOptionalBoolean(runtime?.safety_yolo_mode);
  if (runtimeYolo !== null) {
    setYoloModeLocal(runtimeYolo, { persist: true, render: false });
  } else {
    setYoloModeLocal(readYoloModePreference(), { persist: false, render: false });
  }
  const runtimeSilent = parseOptionalBoolean(runtime?.silent_mode);
  state.ttsSilent = runtimeSilent === true;
  state.inputMode = normalizeInputMode(runtime?.input_mode || 'pen');
  syncInputModeBodyState();
  renderToolPalette();
  state.startupBehavior = String(runtime?.startup_behavior || 'hub_first').trim().toLowerCase() || 'hub_first';
  state.disclaimerVersion = String(runtime?.disclaimer_version || '').trim();
  state.disclaimerAckRequired = Boolean(runtime?.disclaimer_ack_required);
}

export async function updateRuntimePreferences(patch) {
  const resp = await fetch(apiURL('runtime/preferences'), {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch || {}),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const silent = parseOptionalBoolean(payload?.silent_mode);
  if (silent !== null) {
    state.ttsSilent = silent;
  }
  state.inputMode = normalizeInputMode(payload?.input_mode || state.inputMode || 'pen');
  syncInputModeBodyState();
  renderToolPalette();
  state.startupBehavior = String(payload?.startup_behavior || state.startupBehavior || 'hub_first').trim().toLowerCase() || 'hub_first';
  renderEdgeTopModelButtons();
  return payload;
}

export async function acknowledgeDisclaimer(version) {
  const payload = {};
  if (String(version || '').trim()) {
    payload.version = String(version || '').trim();
  }
  const resp = await fetch(apiURL('runtime/disclaimer-ack'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
}

export function closeDisclaimerModal() {
  const node = document.getElementById('liability-modal');
  if (node && node.parentElement) node.parentElement.removeChild(node);
}

export function showDisclaimerModal() {
  if (!state.disclaimerAckRequired) return Promise.resolve();
  closeDisclaimerModal();
  return new Promise((resolve, reject) => {
    const root = document.createElement('div');
    root.id = 'liability-modal';
    root.className = 'liability-modal';
    root.innerHTML = `
      <div class=\"liability-modal-card\" role=\"dialog\" aria-modal=\"true\" aria-label=\"Liability notice\">
        <h2>Liability Notice</h2>
        <p>Tabura is provided as-is. You are solely responsible for backups, verification, and safe operation.</p>
        <p>No warranties or liability are assumed to the maximum extent permitted by applicable law.</p>
        <button id=\"liability-ack-btn\" type=\"button\" class=\"edge-project-btn\">I understand</button>
      </div>
    `;
    document.body.appendChild(root);
    const btn = document.getElementById('liability-ack-btn');
    if (!(btn instanceof HTMLButtonElement)) {
      reject(new Error('liability acknowledgement button unavailable'));
      return;
    }
    btn.addEventListener('click', () => {
      btn.disabled = true;
      acknowledgeDisclaimer(state.disclaimerVersion)
        .then(() => {
          state.disclaimerAckRequired = false;
          closeDisclaimerModal();
          resolve();
        })
        .catch((err) => {
          btn.disabled = false;
          showStatus(`disclaimer acknowledgement failed: ${String(err?.message || err || 'unknown error')}`);
          reject(err);
        });
    });
  });
}

export async function pollRuntimeForRuntimeReload() {
  if (runtimeReloadInFlight || runtimeReloadRequested) return;
  runtimeReloadInFlight = true;
  try {
    const runtime = await fetchRuntimeMeta();
    const bootID = String(runtime?.boot_id || '').trim();
    if (!bootID) return;
    if (!runtimeReloadBootID) {
      runtimeReloadBootID = bootID;
      return;
    }
    if (runtimeReloadBootID !== bootID) {
      runtimeReloadBootID = bootID;
      runtimeReloadRequested = true;
      showStatus('Bug fix applied, refreshing...');
      forceUiHardReload('deployment');
    }
  } catch (_) {
    // Ignore transient runtime probe errors during service restarts.
  } finally {
    runtimeReloadInFlight = false;
  }
}

export function startRuntimeReloadWatcher() {
  if (runtimeReloadTimer !== null) return;
  const tick = () => {
    void pollRuntimeForRuntimeReload();
  };
  runtimeReloadTimer = window.setInterval(tick, DEV_UI_RELOAD_POLL_MS);
  tick();
  window.addEventListener('focus', tick);
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) tick();
  });
}

export function isEditableTarget(target) {
  if (!(target instanceof Element)) return false;
  return Boolean(target.closest('input,textarea,select,[contenteditable="true"]'));
}

export function artifactEditorEl() {
  const el = document.getElementById('artifact-editor');
  return el instanceof HTMLTextAreaElement ? el : null;
}

export function ensureArtifactEditor() {
  const existing = artifactEditorEl();
  if (existing) return existing;
  const viewport = document.getElementById('canvas-viewport');
  if (!(viewport instanceof HTMLElement)) return null;
  const el = document.createElement('textarea');
  el.id = 'artifact-editor';
  el.className = 'artifact-editor';
  el.style.display = 'none';
  el.setAttribute('aria-label', 'Artifact editor');
  el.spellcheck = false;
  el.wrap = 'off';
  viewport.appendChild(el);
  return el;
}

export function isTextArtifactPaneActive() {
  if (!state.hasArtifact) return false;
  const pane = document.getElementById('canvas-text');
  return pane instanceof HTMLElement
    && pane.classList.contains('is-active')
    && window.getComputedStyle(pane).display !== 'none';
}

export function canEnterArtifactEditModeFromTarget(target) {
  if (!isTextArtifactPaneActive()) return false;
  if (state.prReviewMode) return false;
  if (!(target instanceof Element)) return false;
  if (!target.closest('#canvas-text')) return false;
  if (target.closest('a,button,input,textarea,select,[contenteditable="true"]')) return false;
  if (isRecording() || shouldStopInUiClick()) return false;
  return true;
}

export function parseCssPx(value, fallback = 0) {
  const n = Number.parseFloat(String(value || ''));
  return Number.isFinite(n) ? n : fallback;
}

export function measureEditorCharWidth(editor) {
  const probe = document.createElement('span');
  probe.textContent = 'M';
  probe.style.position = 'fixed';
  probe.style.visibility = 'hidden';
  probe.style.whiteSpace = 'pre';
  probe.style.font = window.getComputedStyle(editor).font;
  document.body.appendChild(probe);
  const width = probe.getBoundingClientRect().width;
  probe.remove();
  return width > 0 ? width : 8;
}

export function offsetFromLineAndColumn(text, targetLine, targetCol) {
  const lines = String(text || '').split('\n');
  if (lines.length === 0) return 0;
  const line = Math.max(0, Math.min(lines.length - 1, targetLine));
  const col = Math.max(0, Math.min(lines[line].length, targetCol));
  let offset = 0;
  for (let i = 0; i < line; i += 1) {
    offset += lines[i].length + 1;
  }
  return offset + col;
}

export function placeArtifactEditorCaretFromPoint(editor, clientX, clientY) {
  if (!Number.isFinite(clientX) || !Number.isFinite(clientY)) return;
  const rect = editor.getBoundingClientRect();
  const cs = window.getComputedStyle(editor);
  const padL = parseCssPx(cs.paddingLeft, 0);
  const padT = parseCssPx(cs.paddingTop, 0);
  const lineHeight = parseCssPx(cs.lineHeight, parseCssPx(cs.fontSize, 16) * 1.4);
  const charWidth = measureEditorCharWidth(editor);
  const localX = Math.max(0, clientX - rect.left + editor.scrollLeft - padL);
  const localY = Math.max(0, clientY - rect.top + editor.scrollTop - padT);
  const line = Math.max(0, Math.floor(localY / Math.max(1, lineHeight)));
  const col = Math.max(0, Math.floor(localX / Math.max(1, charWidth)));
  const offset = offsetFromLineAndColumn(editor.value, line, col);
  editor.setSelectionRange(offset, offset);
}

export function applyArtifactEditorText(text) {
  if (!isTextArtifactPaneActive()) return;
  const nextText = String(text || '');
  if (nextText === String(getPreviousArtifactText() || '')) return;
  const pane = document.getElementById('canvas-text');
  const scrollTop = pane instanceof HTMLElement ? pane.scrollTop : 0;
  renderCanvas({
    event_id: getActiveTextEventId() || undefined,
    kind: 'text_artifact',
    title: getActiveArtifactTitle() || '',
    text: nextText,
  });
  const nextPane = document.getElementById('canvas-text');
  if (nextPane instanceof HTMLElement) {
    const maxTop = Math.max(0, nextPane.scrollHeight - nextPane.clientHeight);
    nextPane.scrollTop = Math.min(scrollTop, maxTop);
  }
}

export function exitArtifactEditMode(options = {}) {
  const applyChanges = options.applyChanges !== false;
  const editor = artifactEditorEl();
  if (!editor || !state.artifactEditMode) return false;
  const nextText = editor.value;
  editor.style.display = 'none';
  if (document.activeElement === editor) {
    try { editor.blur(); } catch (_) {}
  }
  state.artifactEditMode = false;
  document.body.classList.remove('artifact-edit-mode');
  if (applyChanges) {
    applyArtifactEditorText(nextText);
  }
  return true;
}

export function enterArtifactEditMode(clientX, clientY) {
  if (!isTextArtifactPaneActive()) return false;
  const editor = ensureArtifactEditor();
  if (!editor) return false;
  cancelLiveSessionListen();
  hideTextInput();
  editor.value = String(getPreviousArtifactText() || '');
  editor.style.display = '';
  state.artifactEditMode = true;
  document.body.classList.add('artifact-edit-mode');
  editor.focus();
  placeArtifactEditorCaretFromPoint(editor, clientX, clientY);
  return true;
}

export function activeProject() {
  return state.projects.find((project) => project.id === state.activeProjectId) || null;
}

export function activeProjectKey() {
  return String(activeProject()?.project_key || '').trim();
}

export function normalizeCompanionIdleSurface(raw) {
  return String(raw || '').trim().toLowerCase() === COMPANION_IDLE_SURFACES.BLACK
    ? COMPANION_IDLE_SURFACES.BLACK
    : COMPANION_IDLE_SURFACES.ROBOT;
}

export function normalizeCompanionRuntimeState(raw) {
  const stateName = String(raw || '').trim().toLowerCase();
  if (stateName === COMPANION_RUNTIME_STATES.LISTENING) return COMPANION_RUNTIME_STATES.LISTENING;
  if (stateName === COMPANION_RUNTIME_STATES.THINKING) return COMPANION_RUNTIME_STATES.THINKING;
  if (stateName === COMPANION_RUNTIME_STATES.TALKING) return COMPANION_RUNTIME_STATES.TALKING;
  if (stateName === COMPANION_RUNTIME_STATES.ERROR) return COMPANION_RUNTIME_STATES.ERROR;
  return COMPANION_RUNTIME_STATES.IDLE;
}

export function companionIdleSurfaceEl() {
  return document.getElementById('companion-idle-surface');
}

export function companionStatusCopy(runtimeState) {
  switch (normalizeCompanionRuntimeState(runtimeState)) {
    case COMPANION_RUNTIME_STATES.LISTENING:
      return { label: 'Listening', detail: 'Ambient capture is live.' };
    case COMPANION_RUNTIME_STATES.THINKING:
      return { label: 'Thinking', detail: 'Working through the current request.' };
    case COMPANION_RUNTIME_STATES.TALKING:
      return { label: 'Talking', detail: 'Speaking the current response.' };
    case COMPANION_RUNTIME_STATES.ERROR:
      return { label: 'Error', detail: 'Meeting mode hit a runtime error.' };
    default:
      return { label: 'Idle', detail: 'Ready in the background.' };
  }
}

export function hasVisibleCanvasArtifact() {
  const activePane = document.querySelector('#canvas-viewport .canvas-pane.is-active');
  if (!(activePane instanceof HTMLElement)) return false;
  return window.getComputedStyle(activePane).display !== 'none';
}

export function shouldShowCompanionIdleSurface() {
  return Boolean(state.companionEnabled) && !state.liveSessionActive && !hasVisibleCanvasArtifact() && !isHubActive();
}

export function updateCompanionIdleSurface() {
  const surface = companionIdleSurfaceEl();
  if (!(surface instanceof HTMLElement)) return;
  const visible = shouldShowCompanionIdleSurface();
  const runtimeState = normalizeCompanionRuntimeState(state.companionRuntimeState);
  const idleSurface = normalizeCompanionIdleSurface(state.companionIdleSurface);
  const copy = companionStatusCopy(runtimeState);
  surface.dataset.state = runtimeState;
  surface.dataset.surface = idleSurface;
  surface.setAttribute('aria-hidden', visible ? 'false' : 'true');
  surface.style.display = visible ? 'block' : 'none';
  const statusNode = surface.querySelector('.companion-idle-status');
  if (statusNode) statusNode.textContent = copy.label;
  const detailNode = surface.querySelector('.companion-idle-detail');
  if (detailNode) {
    const runtimeDetail = String(state.companionRuntimeReason || '').trim();
    detailNode.textContent = runtimeDetail && runtimeState !== COMPANION_RUNTIME_STATES.IDLE
      ? runtimeDetail.replaceAll('_', ' ')
      : copy.detail;
  }
}

export function syncCompanionIdleSurface() {
  updateAssistantActivityIndicator();
}

export function applyCompanionState(payload = {}) {
  const config = payload?.config && typeof payload.config === 'object' ? payload.config : {};
  state.companionEnabled = Boolean(
    payload?.companion_enabled ?? config?.companion_enabled ?? state.companionEnabled,
  );
  state.companionIdleSurface = normalizeCompanionIdleSurface(
    payload?.idle_surface ?? config?.idle_surface ?? state.companionIdleSurface,
  );
  state.companionRuntimeState = normalizeCompanionRuntimeState(
    payload?.state ?? payload?.runtime?.state ?? state.companionRuntimeState,
  );
  state.companionRuntimeReason = String(
    payload?.reason ?? payload?.runtime?.reason ?? state.companionRuntimeReason ?? '',
  ).trim();
  state.companionProjectKey = String(payload?.project_key || activeProjectKey()).trim();
  updateCompanionIdleSurface();
}

export function resetCompanionState() {
  state.companionEnabled = false;
  state.companionIdleSurface = COMPANION_IDLE_SURFACES.ROBOT;
  state.companionRuntimeState = COMPANION_RUNTIME_STATES.IDLE;
  state.companionRuntimeReason = 'idle';
  state.companionProjectKey = '';
  updateCompanionIdleSurface();
}

export function isHubProject(project) {
  if (!project || typeof project !== 'object') return false;
  const kind = String(project.kind || '').trim().toLowerCase();
  if (kind === 'hub') return true;
  const key = String(project.project_key || '').trim();
  return key === '__hub__';
}

export function hubProject() {
  return state.projects.find((project) => isHubProject(project)) || null;
}

export function isHubActive() {
  return isHubProject(activeProject());
}

export function isTemporaryProjectKind(kind) {
  const normalized = String(kind || '').trim().toLowerCase();
  return normalized === 'meeting' || normalized === 'task';
}

export function normalizeReasoningEffortOptions(rawEfforts) {
  const raw = Array.isArray(rawEfforts) ? rawEfforts : [];
  const clean = [];
  const seen = new Set();
  for (const rawEffort of raw) {
    const effort = String(rawEffort || '').trim().toLowerCase();
    if (!effort || seen.has(effort)) continue;
    seen.add(effort);
    clean.push(effort);
  }
  return clean;
}

export function normalizeReasoningEffortOptionsByAlias(rawEfforts) {
  const source = rawEfforts && typeof rawEfforts === 'object' ? rawEfforts : {};
  const out = {};
  for (const alias of PROJECT_CHAT_MODEL_ALIASES) {
    const configured = normalizeReasoningEffortOptions(source[alias]);
    if (configured.length > 0) {
      out[alias] = configured;
      continue;
    }
    const defaults = PROJECT_CHAT_MODEL_REASONING_EFFORTS[alias];
    out[alias] = Array.isArray(defaults) && defaults.length > 0 ? defaults.slice() : ['low', 'medium', 'high'];
  }
  return out;
}

export function applyRuntimeReasoningEffortOptions(rawEfforts) {
  state.reasoningEffortsByAlias = normalizeReasoningEffortOptionsByAlias(rawEfforts);
}

export function normalizeProjectChatModelAlias(value) {
  const clean = String(value || '').trim().toLowerCase();
  if (PROJECT_CHAT_MODEL_ALIASES.includes(clean)) {
    return clean;
  }
  return '';
}

export function reasoningEffortOptionsForAlias(alias) {
  const cleanAlias = normalizeProjectChatModelAlias(alias);
  const configured = Array.isArray(state.reasoningEffortsByAlias?.[cleanAlias]) ? state.reasoningEffortsByAlias[cleanAlias] : [];
  if (configured.length > 0) {
    return configured.slice();
  }
  const defaults = PROJECT_CHAT_MODEL_REASONING_EFFORTS[cleanAlias];
  return Array.isArray(defaults) && defaults.length > 0 ? defaults.slice() : ['low', 'medium', 'high'];
}

export function defaultReasoningEffortForAlias(alias) {
  const options = reasoningEffortOptionsForAlias(alias);
  return options.length > 0 ? options[0] : 'low';
}

export function normalizeProjectChatModelReasoningEffort(value, alias) {
  let effort = String(value || '').trim().toLowerCase();
  if (effort === 'extra_high') {
    effort = 'xhigh';
  }
  const options = reasoningEffortOptionsForAlias(alias);
  if (options.includes(effort)) {
    return effort;
  }
  return defaultReasoningEffortForAlias(alias);
}
