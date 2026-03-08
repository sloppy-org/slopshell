import { marked } from './vendor/marked.esm.js';
import { apiURL, wsURL } from './paths.js';
import {
  renderCanvas,
  clearCanvas,
  getLocationFromSelection,
  clearLineHighlight,
  escapeHtml,
  sanitizeHtml,
  getActiveArtifactTitle,
  getActiveTextEventId,
  getPreviousArtifactText,
} from './canvas.js';
import {
  getUiState, setUiMode,
  showIndicatorMode, hideIndicator,
  showTextInput, hideTextInput,
  showOverlay, hideOverlay, updateOverlay,
  isOverlayVisible, isTextInputVisible, isRecording, setRecording,
  getInputAnchor, setInputAnchor, getAnchorFromPoint,
  buildContextPrefix, getLastInputPosition, setLastInputPosition,
} from './ui.js';
import {
  configureLiveSession,
  getLiveSessionSnapshot,
  handleLiveSessionMessage,
  isLiveSessionListenActive,
  LIVE_SESSION_HOTWORD_DEFAULT,
  LIVE_SESSION_MODE_DIALOGUE,
  LIVE_SESSION_MODE_MEETING,
  onLiveSessionTTSPlaybackComplete,
  cancelLiveSessionListen,
  startLiveSession,
  stopLiveSession,
} from './live-session.js';
import {
  initHotword,
  startHotwordMonitor,
  stopHotwordMonitor,
  isHotwordActive,
  onHotwordDetected,
  setHotwordThreshold,
  setHotwordAudioContext,
  getPreRollAudio,
  getHotwordMicStream,
} from './hotword.js';
import { initVAD, ensureVADLoaded, float32ToWav } from './vad.js';

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
  projectModelSwitchInFlight: false,
  inputMode: 'pen',
  startupBehavior: 'hub_first',
  ttsSilent: false,
  yoloMode: false,
  disclaimerAckRequired: false,
  disclaimerVersion: '',
  welcomeSurface: null,
  pendingByTurn: new Map(),
  pendingQueue: [],
  assistantActiveTurns: new Set(),
  assistantUnknownTurns: 0,
  assistantRemoteActiveCount: 0,
  assistantRemoteQueuedCount: 0,
  assistantLastStartedAt: 0,
  assistantCancelInFlight: false,
  assistantLastError: '',
  ttsPlaying: false,
  liveSessionActive: false,
  liveSessionMode: '',
  liveSessionHotword: LIVE_SESSION_HOTWORD_DEFAULT,
  liveSessionDialogueListenActive: false,
  liveSessionDialogueListenTimer: null,
  hotwordEnabled: false,
  hotwordActive: false,
  voiceTranscriptSubmitInFlight: false,
  voiceAwaitingTurn: false,
  voiceTurns: new Set(),
  voiceLifecycle: 'idle',
  voiceLifecycleSeq: 0,
  voiceLifecycleReason: '',
  indicatorSuppressedByCanvasUpdate: false,
  chatCtrlHoldTimer: null,
  chatVoiceCapture: null,
  reasoningEffortsByAlias: {
    codex: ['low', 'medium', 'high', 'xhigh'],
    gpt: ['low', 'medium', 'high', 'xhigh'],
    spark: ['low', 'medium', 'high', 'xhigh'],
  },
  contextUsed: 0,
  contextMax: 0,
  // Track if a canvas action happened during this turn
  canvasActionThisTurn: false,
  turnFirstResponseShown: false,
  lastInputOrigin: 'text',
  pendingSubmitController: null,
  pendingSubmitKind: '',
  prReviewMode: false,
  prReviewFiles: [],
  prReviewActiveIndex: 0,
  prReviewTitle: '',
  prReviewPRNumber: '',
  prReviewDrawerOpen: false,
  fileSidebarMode: 'items',
  workspaceBrowserPath: '',
  workspaceBrowserEntries: [],
  workspaceBrowserLoading: false,
  workspaceBrowserError: '',
  workspaceOpenFilePath: '',
  workspaceStepInFlight: false,
  itemSidebarView: 'inbox',
  itemSidebarItems: [],
  itemSidebarCounts: defaultItemSidebarCounts(),
  itemSidebarLoading: false,
  itemSidebarError: '',
  itemSidebarActiveItemID: 0,
  prReviewAwaitingArtifact: false,
  artifactEditMode: false,
  inkDraft: {
    strokes: [],
    activePointerId: null,
    activePointerType: '',
    activePath: null,
    dirty: false,
  },
  inkSubmitInFlight: false,
  companionEnabled: false,
  companionIdleSurface: 'robot',
  companionRuntimeState: 'idle',
  companionRuntimeReason: 'idle',
  companionProjectKey: '',
};

const TOOL_PALETTE_MODES = [
  {
    id: 'voice',
    label: 'Voice mode',
    icon: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4v8"/><path d="M8.5 8.5a3.5 3.5 0 0 1 7 0V12a3.5 3.5 0 0 1-7 0Z"/><path d="M6 11.5a6 6 0 0 0 12 0"/><path d="M12 17.5V21"/><path d="M9 21h6"/></svg>',
  },
  {
    id: 'pen',
    label: 'Pen mode',
    icon: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 20 4.5-1 9-9a2.1 2.1 0 0 0-3-3l-9 9Z"/><path d="m13 7 4 4"/><path d="M4 20h5"/></svg>',
  },
  {
    id: 'keyboard',
    label: 'Keyboard mode',
    icon: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="6" width="18" height="12" rx="2"/><path d="M6 10h.01"/><path d="M9 10h.01"/><path d="M12 10h.01"/><path d="M15 10h.01"/><path d="M18 10h.01"/><path d="M6 14h12"/></svg>',
  },
];

export function getState() {
  return state;
}

function isVoiceTurn() {
  return state.lastInputOrigin === 'voice';
}

window._taburaApp = {
  getState,
  acquireMicStream,
  sttStart,
  sttSendBlob,
  sttStop,
  sttCancel,
  refreshCompanionState,
  syncCompanionIdleSurface,
};

void ensureVADLoaded();

let bootstrapErrorShown = false;

function showBootstrapError(message) {
  const text = String(message || 'Unknown error');
  if (bootstrapErrorShown) return;
  bootstrapErrorShown = true;
  const loginErr = document.getElementById('login-error');
  if (loginErr) loginErr.textContent = `Initialization failed: ${text}`;
  const loginView = document.getElementById('view-login');
  if (loginView) loginView.style.display = '';
  const mainView = document.getElementById('view-main');
  if (mainView) mainView.style.display = 'none';
}

window.addEventListener('error', (event) => {
  const msg = String(event?.error?.message || event?.message || '').trim();
  if (!msg) return;
  if (msg.includes('ResizeObserver loop limit exceeded')) return;
  showBootstrapError(msg);
});

window.addEventListener('unhandledrejection', (event) => {
  const reason = event?.reason;
  const msg = String(reason?.message || reason || '').trim();
  if (!msg) return;
  showBootstrapError(msg);
});

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_CHAT_MATH_SEGMENT_';
const DEV_UI_RELOAD_POLL_MS = 1500;
const ASSISTANT_ACTIVITY_POLL_MS = 1200;
const CHAT_WS_STALE_THRESHOLD_MS = 20000;
const COMPANION_VIEW_PATH_PREFIX = '__tabura_companion__';
const COMPANION_TRANSCRIPT_VIEW_PATH = `${COMPANION_VIEW_PATH_PREFIX}/transcript`;
const COMPANION_SUMMARY_VIEW_PATH = `${COMPANION_VIEW_PATH_PREFIX}/summary`;
const COMPANION_REFERENCES_VIEW_PATH = `${COMPANION_VIEW_PATH_PREFIX}/references`;
const MEETING_TRANSCRIPT_LABEL = 'Meeting Transcript';
const MEETING_SUMMARY_LABEL = 'Meeting Summary';
const MEETING_REFERENCES_LABEL = 'Meeting References';
let localMessageSeq = 0;
const CHAT_CTRL_LONG_PRESS_MS = 180;
const ARTIFACT_EDIT_LONG_TAP_MS = 420;
const ITEM_SIDEBAR_VIEWS = ['inbox', 'waiting', 'someday', 'done'];
// Frontend end-of-utterance policy:
// - start/end speech from local mic energy
// - pure VAD commit (no semantic EOU sidecar)
// - no-speech timeout + relaxed max duration to avoid hanging capture
const VOICE_VAD_AUTO_SEND_DEFAULT = true;
const VOICE_VAD_AUTO_SEND_STORAGE_KEY = 'tabura.voiceVadAutoSend';
const VOICE_VAD_AUTO_SEND_QUERY_PARAM = 'voice_vad_auto_send';
const VOICE_VAD_NO_SPEECH_MS = 4000;
const VOICE_VAD_MAX_RECORDING_HARD_MS = 240000;
const HOTWORD_VAD_NO_SPEECH_MS = 7000;
const VOICE_VAD_RECORDER_CHUNK_MS = 250;
const VOICE_CAPTURE_STOP_FLUSH_TIMEOUT_MS = 1500;
const STOP_REQUEST_TIMEOUT_MS = 3500;
const VOICE_TRANSCRIPT_SUBMIT_GUARD_MS = 220;
const ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS = 1500;
const ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS = 450;
const VOICE_LIFECYCLE = Object.freeze({
  IDLE: 'idle',
  LISTENING: 'listening',
  RECORDING: 'recording',
  STOPPING_RECORDING: 'stopping_recording',
  AWAITING_TURN: 'awaiting_turn',
  ASSISTANT_WORKING: 'assistant_working',
  TTS_PLAYING: 'tts_playing',
});
const COMPANION_IDLE_SURFACES = Object.freeze({
  ROBOT: 'robot',
  BLACK: 'black',
});
const COMPANION_RUNTIME_STATES = Object.freeze({
  IDLE: 'idle',
  LISTENING: 'listening',
  THINKING: 'thinking',
  TALKING: 'talking',
  ERROR: 'error',
});
let runtimeReloadBootID = '';
let runtimeReloadTimer = null;
let runtimeReloadInFlight = false;
let runtimeReloadRequested = false;
let pendingRuntimeReloadContext = null;
let pendingRuntimeReloadStatus = '';
let assistantActivityTimer = null;
let assistantActivityInFlight = false;
let projectRunStatesInFlight = false;
let assistantSilentCancelInFlight = false;
let chatWsLastMessageAt = 0;
let suppressClickUntil = 0;

const ACTIVE_PROJECT_STORAGE_KEY = 'tabura.activeProjectId';
const LAST_VIEW_STORAGE_KEY = 'tabura.lastView';
const RUNTIME_RELOAD_CONTEXT_STORAGE_KEY = 'tabura.runtimeReloadContext';
const PROJECT_CHAT_MODEL_ALIASES = ['codex', 'gpt', 'spark'];
const PROJECT_CHAT_MODEL_REASONING_EFFORTS = {
  codex: ['low', 'medium', 'high', 'xhigh'],
  gpt: ['low', 'medium', 'high', 'xhigh'],
  spark: ['low', 'medium', 'high', 'xhigh'],
};
const TTS_SILENT_STORAGE_KEY = 'tabura.ttsSilent';
const YOLO_MODE_STORAGE_KEY = 'tabura.yoloMode';
const SIDEBAR_IMAGE_EXTENSIONS = new Set(['.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.svg', '.ico', '.avif']);
const PANEL_MOTION_WATCH_QUERIES = [
  '(monochrome)',
  '(update: slow)',
  '(prefers-reduced-motion: reduce)',
];
let panelMotionWatchersAttached = false;

// --- Block stripping & TTS infrastructure ---

const _canvasFileBlockRe = /:::\s*file\s*\{[^}]*\}\s*[\s\S]*?:::/gi;
const _partialBlockRe = /:::\s*file\s*\{[^}]*\}[\s\S]*$/gi;
const _canvasFileMarkerRefRe = /\[file:[^\]]*\]/g;
const _canvasDirectiveOpenRe = /^\s*:::\s*file\s*\{[^}]*\}\s*$/gim;
const _canvasDirectiveCloseRe = /^\s*:::\s*$/gm;
const _langTagRe = /\[lang:([a-z]{2})\]/gi;
const _codeFenceRe = /```[\s\S]*?```/g;
const _inlineCodeRe = /`([^`]+)`/g;
const _inlineLinkRe = /\[([^\]]+)\]\([^)]*\)/g;
const _inlineImageRe = /!\[([^\]]*)\]\([^)]*\)/g;
const _headingRe = /^\s{0,3}#{1,6}\s+/gm;
const _blockquoteRe = /^\s*>\s?/gm;
const _listMarkerRe = /^\s*(?:[-*+]\s+|\d+\.\s+)/gm;
const _boldAsteriskRe = /\*\*([^*]+)\*\*/g;
const _italicAsteriskRe = /\*([^*\s][^*]*?)\*/g;
const _boldUnderscoreRe = /__([^_]+)__/g;
const _italicUnderscoreRe = /_([^_\s][^_]*?)_/g;
const _strikethroughRe = /~~([^~]+)~~/g;
const _htmlTagRe = /<[^>]+>/g;

// Strip complete and partial :::file{} blocks from text.
function stripBlocks(text) {
  text = text.replace(_canvasFileBlockRe, ' ');
  text = text.replace(_partialBlockRe, ' ');
  text = text.replace(_canvasFileMarkerRefRe, ' ');
  text = text.replace(_canvasDirectiveOpenRe, ' ');
  text = text.replace(_canvasDirectiveCloseRe, ' ');
  return text;
}

function stripMarkdownForSpeech(text) {
  text = text.replace(_codeFenceRe, (m) => m.replace(/```/g, ''));
  text = text.replace(_inlineCodeRe, '$1');
  text = text.replace(_inlineImageRe, '$1');
  text = text.replace(_inlineLinkRe, '$1');
  text = text.replace(_headingRe, '');
  text = text.replace(_blockquoteRe, '');
  text = text.replace(_listMarkerRe, '');
  text = text.replace(_strikethroughRe, '$1');
  text = text.replace(_boldAsteriskRe, '$1');
  text = text.replace(_italicAsteriskRe, '$1');
  text = text.replace(_boldUnderscoreRe, '$1');
  text = text.replace(_italicUnderscoreRe, '$1');
  text = text.replace(_htmlTagRe, '');
  text = text.replace(/\|/g, ' ');
  text = text.replace(/[ \t]+\n/g, '\n');
  text = text.replace(/\n+/g, ' ');
  text = text.replace(/\s{2,}/g, ' ');
  return text.trim();
}

// Clean markdown for overlay display: strip blocks and lang tags.
function cleanForOverlay(markdown) {
  return stripBlocks(markdown).replace(_langTagRe, '').trim();
}

function inferTTSLanguage(text) {
  const sample = String(text || '').trim();
  if (!sample) return '';
  if (/[äöüßÄÖÜ]/.test(sample)) return 'de';
  const tokens = sample
    .toLowerCase()
    .replace(/[^a-zA-Z\u00c0-\u017f\s]/g, ' ')
    .split(/\s+/)
    .filter(Boolean);
  if (tokens.length === 0) return '';
  const germanHints = new Set([
    'und', 'ist', 'nicht', 'ich', 'du', 'wir', 'sie', 'mit', 'fuer', 'für',
    'auf', 'das', 'der', 'die', 'den', 'dem', 'ein', 'eine', 'bitte', 'danke',
  ]);
  let hits = 0;
  for (const token of tokens) {
    if (germanHints.has(token)) hits += 1;
  }
  if (hits >= 2 && hits / tokens.length >= 0.08) return 'de';
  return '';
}

// Extract speakable text for TTS (everything except blocks).
function extractTTSText(markdown) {
  let text = stripBlocks(markdown);
  let lang = '';
  text = text.replace(_langTagRe, (_, l) => { if (!lang) lang = l.toLowerCase(); return ''; });
  text = stripMarkdownForSpeech(text);
  if (!lang) {
    lang = inferTTSLanguage(text);
  }
  text = text.trim();
  return { ttsText: text, ttsLang: lang };
}


class SentenceChunker {
  constructor(onSentence) {
    this._buffer = '';
    this._onSentence = onSentence;
    this._timer = null;
  }
  add(text) {
    this._buffer += text;
    this._tryEmit();
  }
  _tryEmit() {
    if (this._timer) { clearTimeout(this._timer); this._timer = null; }
    const boundaries = /([.!?])\s+/g;
    let lastIndex = 0;
    let match;
    while ((match = boundaries.exec(this._buffer)) !== null) {
      const end = match.index + match[1].length;
      const sentence = this._buffer.slice(lastIndex, end).trim();
      if (sentence) this._onSentence(sentence);
      lastIndex = end;
    }
    if (lastIndex > 0) {
      this._buffer = this._buffer.slice(lastIndex).trimStart();
    }
    if (this._buffer.length > 0) {
      this._timer = setTimeout(() => {
        this._timer = null;
        this.flush();
      }, 300);
    }
  }
  flush() {
    if (this._timer) { clearTimeout(this._timer); this._timer = null; }
    const sentence = this._buffer.trim();
    this._buffer = '';
    if (sentence) this._onSentence(sentence);
  }
  reset() {
    if (this._timer) { clearTimeout(this._timer); this._timer = null; }
    this._buffer = '';
  }
}

class TTSPlayer {
  constructor() {
    this._queue = [];
    this._playing = false;
    this._stopped = false;
    this._ctx = null;
    this._currentSource = null;
    this._nextStartTime = 0;
  }
  _ensureCtx() {
    if (!this._ctx) {
      this._ctx = ttsAudioCtx;
    }
    return this._ctx;
  }
  enqueue(wavArrayBuffer) {
    if (this._stopped) return;
    this._queue.push(wavArrayBuffer);
    if (!this._playing) this._playNext();
  }
  stop() {
    this._stopped = true;
    this._queue = [];
    if (this._currentSource) {
      try { this._currentSource.stop(); } catch (_) {}
      this._currentSource = null;
    }
    this._playing = false;
    this._nextStartTime = 0;
    if (state.ttsPlaying) {
      state.ttsPlaying = false;
      updateAssistantActivityIndicator();
    }
  }
  async _playNext() {
    const playbackCompleted = !this._stopped && this._queue.length === 0;
    if (this._stopped || this._queue.length === 0) {
      this._playing = false;
      this._nextStartTime = 0;
      if (state.ttsPlaying) {
        state.ttsPlaying = false;
        updateAssistantActivityIndicator();
      }
      if (playbackCompleted) {
        onLiveSessionTTSPlaybackComplete();
      }
      return;
    }
    this._playing = true;
    if (!state.ttsPlaying) {
      cancelLiveSessionListen();
      state.ttsPlaying = true;
      updateAssistantActivityIndicator();
    }
    const wavData = this._queue.shift();
    try {
      const ctx = this._ensureCtx();
      if (ctx.state === 'suspended') await ctx.resume();
      const audioBuffer = await ctx.decodeAudioData(wavData.slice(0));
      if (this._stopped) return;
      const source = ctx.createBufferSource();
      source.buffer = audioBuffer;
      source.playbackRate.value = 1.1;
      source.connect(ctx.destination);
      this._currentSource = source;
      const now = ctx.currentTime;
      const startAt = this._nextStartTime > now ? this._nextStartTime : now;
      this._nextStartTime = startAt + audioBuffer.duration / 1.1;
      source.start(startAt);
      source.onended = () => {
        this._currentSource = null;
        if (!this._stopped) this._playNext();
      };
    } catch (err) {
      console.warn('TTS playback error:', err);
      this._currentSource = null;
      if (!this._stopped) this._playNext();
    }
  }
}

let ttsPlayer = null;
let ttsSentenceChunker = null;
let ttsEnabled = false;
let ttsLastSpeakText = '';
let ttsSpeakLang = 'en';
let hotwordSyncInFlight = false;
let hotwordResyncQueued = false;
let hotwordInitAttempted = false;
let hotwordUnsubscribe = null;
let hotwordRetryTimer = null;
const HOTWORD_RETRY_MS = 800;
function readTTSSilentPreference() {
  try {
    const value = window.localStorage.getItem(TTS_SILENT_STORAGE_KEY);
    const parsed = parseOptionalBoolean(value);
    return parsed === true;
  } catch (_) {
    return false;
  }
}

function persistTTSSilentPreference(silent) {
  try {
    window.localStorage.setItem(TTS_SILENT_STORAGE_KEY, silent ? 'true' : 'false');
  } catch (_) {}
}

function readYoloModePreference() {
  try {
    const value = window.localStorage.getItem(YOLO_MODE_STORAGE_KEY);
    const parsed = parseOptionalBoolean(value);
    return parsed === true;
  } catch (_) {
    return false;
  }
}

function persistYoloModePreference(enabled) {
  try {
    window.localStorage.setItem(YOLO_MODE_STORAGE_KEY, enabled ? 'true' : 'false');
  } catch (_) {}
}

function setYoloModeLocal(enabled, { persist = true, render = true } = {}) {
  const next = Boolean(enabled);
  if (state.yoloMode === next) return;
  state.yoloMode = next;
  if (persist) persistYoloModePreference(next);
  if (render) renderEdgeTopModelButtons();
}

async function setYoloMode(enabled) {
  const next = Boolean(enabled);
  const resp = await fetch(apiURL('runtime/yolo'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled: next }),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  setYoloModeLocal(next, { persist: true, render: true });
}

function toggleYoloMode() {
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  const next = !Boolean(state.yoloMode);
  setYoloMode(next)
    .then(() => {
      showStatus(next ? 'yolo mode on' : 'yolo mode off');
    })
    .catch((err) => {
      showStatus(`yolo update failed: ${String(err?.message || err || 'unknown error')}`);
      renderEdgeTopModelButtons();
    });
}

function canSpeakTTS() {
  return Boolean(ttsEnabled) && !Boolean(state.ttsSilent);
}

function clearHotwordRetry() {
  if (hotwordRetryTimer !== null) {
    window.clearTimeout(hotwordRetryTimer);
    hotwordRetryTimer = null;
  }
}

function scheduleHotwordRetry() {
  if (hotwordRetryTimer !== null) return;
  hotwordRetryTimer = window.setTimeout(() => {
    hotwordRetryTimer = null;
    requestHotwordSync();
  }, HOTWORD_RETRY_MS);
}

function canStartHotwordMonitor() {
  const mode = syncVoiceLifecycle('can-start-hotword');
  if (!state.hotwordEnabled) return false;
  if (!state.liveSessionActive) return false;
  if (!canSpeakTTS()) return false;
  if (mode === VOICE_LIFECYCLE.RECORDING || mode === VOICE_LIFECYCLE.STOPPING_RECORDING) return false;
  if (mode === VOICE_LIFECYCLE.TTS_PLAYING) return false;
  if (state.chatVoiceCapture) return false;
  if (isStopCapableLifecycle(mode)) return false;
  return true;
}

async function syncHotwordMonitor() {
  if (!state.hotwordEnabled || !canStartHotwordMonitor()) {
    clearHotwordRetry();
    if (isHotwordActive()) {
      stopHotwordMonitor();
    }
    state.hotwordActive = false;
    return;
  }
  if (isHotwordActive()) {
    clearHotwordRetry();
    state.hotwordActive = true;
    return;
  }
  let startErr = null;
  try {
    const stream = await acquireMicStream();
    await startHotwordMonitor(stream);
  } catch (err) {
    startErr = err;
  }
  state.hotwordActive = isHotwordActive();
  if (state.hotwordActive) {
    clearHotwordRetry();
    return;
  }
  const errName = String(startErr?.name || '').toLowerCase();
  const errMsg = String(startErr?.message || '').toLowerCase();
  const permissionDenied = errName.includes('notallowed')
    || errName.includes('permission')
    || errMsg.includes('permission denied')
    || errMsg.includes('notallowederror');
  if (!permissionDenied) {
    scheduleHotwordRetry();
  }
}

function requestHotwordSync() {
  if (hotwordSyncInFlight) {
    hotwordResyncQueued = true;
    return;
  }
  hotwordSyncInFlight = true;
  void syncHotwordMonitor().finally(() => {
    hotwordSyncInFlight = false;
    if (hotwordResyncQueued) {
      hotwordResyncQueued = false;
      requestHotwordSync();
    }
  });
}

function configureHotwordLifecycle() {
  if (typeof hotwordUnsubscribe === 'function') return;
  hotwordUnsubscribe = onHotwordDetected(() => {
    if (!canStartHotwordMonitor()) return;
    stopHotwordMonitor();
    state.hotwordActive = false;
    beginConversationVoiceCapture();
    updateAssistantActivityIndicator();
  });
}

async function initHotwordLifecycle() {
  return initHotwordLifecycleWithOptions();
}

async function initHotwordLifecycleWithOptions(options = {}) {
  const force = Boolean(options && options.force);
  if (hotwordInitAttempted && !force) return state.hotwordEnabled;
  if (force) {
    stopHotwordMonitor();
    state.hotwordActive = false;
    hotwordInitAttempted = false;
  }
  hotwordInitAttempted = true;
  try {
    const enabled = await initHotword({ force });
    state.hotwordEnabled = Boolean(enabled);
    if (state.hotwordEnabled) {
      setHotwordThreshold(0.5);
      configureHotwordLifecycle();
    } else {
      console.warn('Hotword unavailable; continuing without wake-word activation.');
    }
  } catch (err) {
    state.hotwordEnabled = false;
    console.warn('Hotword initialization error:', err);
  }
  requestHotwordSync();
  return state.hotwordEnabled;
}

function applyLiveSessionStateSnapshot(snapshot = null) {
  const nextSnapshot = snapshot && typeof snapshot === 'object'
    ? snapshot
    : getLiveSessionSnapshot();
  state.liveSessionActive = Boolean(nextSnapshot.liveSessionActive);
  state.liveSessionMode = String(nextSnapshot.liveSessionMode || '').trim().toLowerCase();
  state.liveSessionHotword = String(nextSnapshot.liveSessionHotword || LIVE_SESSION_HOTWORD_DEFAULT).trim() || LIVE_SESSION_HOTWORD_DEFAULT;
  state.liveSessionDialogueListenActive = Boolean(nextSnapshot.liveSessionDialogueListenActive);
  state.liveSessionDialogueListenTimer = nextSnapshot.liveSessionDialogueListenTimer ?? null;
  requestHotwordSync();
}

function isDialogueLiveSession() {
  return state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_DIALOGUE;
}

function isMeetingLiveSession() {
  return state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_MEETING;
}

function liveSessionStatusSummary() {
  if (!state.liveSessionActive) return '';
  if (isDialogueLiveSession()) {
    const lifecycle = syncVoiceLifecycle('live-dialogue-summary');
    if (lifecycle === VOICE_LIFECYCLE.TTS_PLAYING) return 'Dialogue • Talking';
    if (lifecycle === VOICE_LIFECYCLE.ASSISTANT_WORKING || lifecycle === VOICE_LIFECYCLE.AWAITING_TURN) return 'Dialogue • Thinking';
    return 'Dialogue • Listening';
  }
  const runtimeState = normalizeCompanionRuntimeState(state.companionRuntimeState);
  if (runtimeState === COMPANION_RUNTIME_STATES.TALKING) return 'Meeting • Talking';
  if (runtimeState === COMPANION_RUNTIME_STATES.THINKING) return 'Meeting • Thinking';
  if (runtimeState === COMPANION_RUNTIME_STATES.ERROR) return 'Meeting • Error';
  if (runtimeState === COMPANION_RUNTIME_STATES.LISTENING) return 'Meeting • Listening';
  return 'Meeting • Quiet';
}

function isMobileSilent() {
  return state.ttsSilent && window.matchMedia('(max-width: 767px)').matches;
}

function mediaQueryMatches(query) {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  try {
    return window.matchMedia(query).matches;
  } catch (_) {
    return false;
  }
}

function shouldEnablePanelMotion() {
  if (mediaQueryMatches('(prefers-reduced-motion: reduce)')) return false;
  if (mediaQueryMatches('(monochrome)')) return false;
  if (mediaQueryMatches('(update: slow)')) return false;
  return true;
}

function syncPanelMotionMode() {
  document.body.classList.toggle('panel-motion-enabled', shouldEnablePanelMotion());
}

function initPanelMotionMode() {
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

function isIPhoneStandalone() {
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

function applyIPhoneFrameCorners() {
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

function isFocusedTextInput() {
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

function clearKeyboardOpenState() {
  const inputRow = document.querySelector('.chat-pane-input-row');
  if (inputRow) inputRow.classList.remove('keyboard-open');
  document.body.classList.remove('keyboard-open');
  if (isIPhoneStandalone()) applyIPhoneFrameCorners();
}

function settleKeyboardAfterSubmit() {
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

function setTTSSilentMode(silent, { persist = true, pinPanel = true } = {}) {
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

function toggleTTSSilentMode() {
  if (!ttsEnabled) return;
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
function unlockAudioContext() {
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

function stopTTSPlayback() {
  if (ttsPlayer) { ttsPlayer.stop(); ttsPlayer = null; }
  if (ttsSentenceChunker) { ttsSentenceChunker.reset(); ttsSentenceChunker = null; }
  ttsLastSpeakText = '';
  ttsSpeakLang = 'en';
  if (state.ttsPlaying) {
    state.ttsPlaying = false;
    updateAssistantActivityIndicator();
  }
  requestHotwordSync();
}

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

function ensureTTSChunker() {
  if (!ttsPlayer) {
    ttsPlayer = new TTSPlayer();
  }
  if (ttsSentenceChunker) return;
  ttsSentenceChunker = new SentenceChunker((sentence) => {
    const ws = state.chatWs;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'tts_speak', text: sentence, lang: ttsSpeakLang }));
    }
  });
}

function queueTTSDiff(diffText) {
  if (!canSpeakTTS()) return;
  const fragment = String(diffText || '').trim();
  if (!fragment) return;
  ensureTTSChunker();
  ttsSentenceChunker.add(fragment);
}

function computeTTSDiff(nextFullText, hintedDeltaText = '') {
  const next = String(nextFullText || '');
  const hinted = String(hintedDeltaText || '');

  if (hinted.trim()) {
    ttsLastSpeakText = next;
    return hinted;
  }
  if (!next || next === ttsLastSpeakText) {
    ttsLastSpeakText = next;
    return '';
  }
  if (next.startsWith(ttsLastSpeakText)) {
    const suffix = next.slice(ttsLastSpeakText.length);
    ttsLastSpeakText = next;
    return suffix;
  }
  if (ttsLastSpeakText.startsWith(next)) {
    // Model backtracked to a shorter snapshot; wait for next stream update.
    ttsLastSpeakText = next;
    return '';
  }
  // Non-prefix rewrite: queue full updated snapshot so speech does not drop.
  ttsLastSpeakText = next;
  return next;
}

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
  const statusEl = document.getElementById('status-label');
  if (statusEl) statusEl.textContent = text;
}

function suppressSyntheticClick() {
  const ms = isLikelyIOS() ? 1200 : 700;
  suppressClickUntil = Math.max(suppressClickUntil, Date.now() + ms);
}

function isSuppressedClick() {
  return Date.now() < suppressClickUntil;
}

let lastVoiceCaptureNoticeText = '';
let lastVoiceCaptureNoticeAt = 0;

function showVoiceCaptureNotice(message, x = null, y = null) {
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

function microphoneUnavailableMessage() {
  if (!window.isSecureContext) {
    return 'Microphone unavailable on insecure HTTP. Open this site through your HTTPS URL (including reverse-proxy HTTPS) and allow microphone access.';
  }
  if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
    return 'Microphone API unavailable in this browser context. Use Safari/Chrome with microphone access enabled.';
  }
  return 'Microphone unavailable. Check browser microphone permissions and audio input availability.';
}

function persistRuntimeReloadContext(reason = '') {
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

function consumeRuntimeReloadContext() {
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

function restoreRuntimeReloadContext() {
  const context = pendingRuntimeReloadContext;
  pendingRuntimeReloadContext = null;
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
    pendingRuntimeReloadStatus = 'Bug fix applied.';
    showStatus(pendingRuntimeReloadStatus);
  }
}

function forceUiHardReload(reason = 'deployment') {
  persistRuntimeReloadContext(reason);
  const url = new URL(window.location.href);
  url.searchParams.set('__tabura_reload', Date.now().toString(36));
  window.location.replace(url.toString());
}

function normalizeInputMode(modeRaw) {
  const mode = String(modeRaw || '').trim().toLowerCase();
  if (mode === 'voice') return 'voice';
  if (mode === 'keyboard' || mode === 'typing' || mode === 'text') return 'keyboard';
  return 'pen';
}

function isPenInputMode() {
  return state.inputMode === 'pen';
}

function isKeyboardInputMode() {
  return state.inputMode === 'keyboard' || state.inputMode === 'typing';
}

function renderToolPalette() {
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

async function fetchRuntimeMeta() {
  const resp = await fetch(apiURL('runtime'), {
    cache: 'no-store',
    headers: { 'Cache-Control': 'no-cache' },
  });
  if (!resp.ok) {
    throw new Error(`runtime metadata failed: HTTP ${resp.status}`);
  }
  return resp.json();
}

function applyRuntimePreferences(runtime) {
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
  state.startupBehavior = String(runtime?.startup_behavior || 'hub_first').trim().toLowerCase() || 'hub_first';
  state.disclaimerVersion = String(runtime?.disclaimer_version || '').trim();
  state.disclaimerAckRequired = Boolean(runtime?.disclaimer_ack_required);
}

async function updateRuntimePreferences(patch) {
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
  state.startupBehavior = String(payload?.startup_behavior || state.startupBehavior || 'hub_first').trim().toLowerCase() || 'hub_first';
  renderEdgeTopModelButtons();
  return payload;
}

async function acknowledgeDisclaimer(version) {
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

function closeDisclaimerModal() {
  const node = document.getElementById('liability-modal');
  if (node && node.parentElement) node.parentElement.removeChild(node);
}

function showDisclaimerModal() {
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

async function pollRuntimeForRuntimeReload() {
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

function startRuntimeReloadWatcher() {
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

function isEditableTarget(target) {
  if (!(target instanceof Element)) return false;
  return Boolean(target.closest('input,textarea,select,[contenteditable="true"]'));
}

function artifactEditorEl() {
  const el = document.getElementById('artifact-editor');
  return el instanceof HTMLTextAreaElement ? el : null;
}

function ensureArtifactEditor() {
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

function isTextArtifactPaneActive() {
  if (!state.hasArtifact) return false;
  const pane = document.getElementById('canvas-text');
  return pane instanceof HTMLElement
    && pane.classList.contains('is-active')
    && window.getComputedStyle(pane).display !== 'none';
}

function canEnterArtifactEditModeFromTarget(target) {
  if (!isTextArtifactPaneActive()) return false;
  if (state.prReviewMode) return false;
  if (!(target instanceof Element)) return false;
  if (!target.closest('#canvas-text')) return false;
  if (target.closest('a,button,input,textarea,select,[contenteditable="true"]')) return false;
  if (isRecording() || shouldStopInUiClick()) return false;
  return true;
}

function parseCssPx(value, fallback = 0) {
  const n = Number.parseFloat(String(value || ''));
  return Number.isFinite(n) ? n : fallback;
}

function measureEditorCharWidth(editor) {
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

function offsetFromLineAndColumn(text, targetLine, targetCol) {
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

function placeArtifactEditorCaretFromPoint(editor, clientX, clientY) {
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

function applyArtifactEditorText(text) {
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

function exitArtifactEditMode(options = {}) {
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

function enterArtifactEditMode(clientX, clientY) {
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

function activeProject() {
  return state.projects.find((project) => project.id === state.activeProjectId) || null;
}

function activeProjectKey() {
  return String(activeProject()?.project_key || '').trim();
}

function normalizeCompanionIdleSurface(raw) {
  return String(raw || '').trim().toLowerCase() === COMPANION_IDLE_SURFACES.BLACK
    ? COMPANION_IDLE_SURFACES.BLACK
    : COMPANION_IDLE_SURFACES.ROBOT;
}

function normalizeCompanionRuntimeState(raw) {
  const stateName = String(raw || '').trim().toLowerCase();
  if (stateName === COMPANION_RUNTIME_STATES.LISTENING) return COMPANION_RUNTIME_STATES.LISTENING;
  if (stateName === COMPANION_RUNTIME_STATES.THINKING) return COMPANION_RUNTIME_STATES.THINKING;
  if (stateName === COMPANION_RUNTIME_STATES.TALKING) return COMPANION_RUNTIME_STATES.TALKING;
  if (stateName === COMPANION_RUNTIME_STATES.ERROR) return COMPANION_RUNTIME_STATES.ERROR;
  return COMPANION_RUNTIME_STATES.IDLE;
}

function companionIdleSurfaceEl() {
  return document.getElementById('companion-idle-surface');
}

function companionStatusCopy(runtimeState) {
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

function hasVisibleCanvasArtifact() {
  const activePane = document.querySelector('#canvas-viewport .canvas-pane.is-active');
  if (!(activePane instanceof HTMLElement)) return false;
  return window.getComputedStyle(activePane).display !== 'none';
}

function shouldShowCompanionIdleSurface() {
  return Boolean(state.companionEnabled) && !state.liveSessionActive && !hasVisibleCanvasArtifact() && !isHubActive();
}

function updateCompanionIdleSurface() {
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

function syncCompanionIdleSurface() {
  updateAssistantActivityIndicator();
}

function applyCompanionState(payload = {}) {
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

function resetCompanionState() {
  state.companionEnabled = false;
  state.companionIdleSurface = COMPANION_IDLE_SURFACES.ROBOT;
  state.companionRuntimeState = COMPANION_RUNTIME_STATES.IDLE;
  state.companionRuntimeReason = 'idle';
  state.companionProjectKey = '';
  updateCompanionIdleSurface();
}

function isHubProject(project) {
  if (!project || typeof project !== 'object') return false;
  const kind = String(project.kind || '').trim().toLowerCase();
  if (kind === 'hub') return true;
  const key = String(project.project_key || '').trim();
  return key === '__hub__';
}

function hubProject() {
  return state.projects.find((project) => isHubProject(project)) || null;
}

function isHubActive() {
  return isHubProject(activeProject());
}

function isTemporaryProjectKind(kind) {
  const normalized = String(kind || '').trim().toLowerCase();
  return normalized === 'meeting' || normalized === 'task';
}

function normalizeReasoningEffortOptions(rawEfforts) {
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

function normalizeReasoningEffortOptionsByAlias(rawEfforts) {
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

function applyRuntimeReasoningEffortOptions(rawEfforts) {
  state.reasoningEffortsByAlias = normalizeReasoningEffortOptionsByAlias(rawEfforts);
}

function normalizeProjectChatModelAlias(value) {
  const clean = String(value || '').trim().toLowerCase();
  if (PROJECT_CHAT_MODEL_ALIASES.includes(clean)) {
    return clean;
  }
  return '';
}

function reasoningEffortOptionsForAlias(alias) {
  const cleanAlias = normalizeProjectChatModelAlias(alias);
  const configured = Array.isArray(state.reasoningEffortsByAlias?.[cleanAlias]) ? state.reasoningEffortsByAlias[cleanAlias] : [];
  if (configured.length > 0) {
    return configured.slice();
  }
  const defaults = PROJECT_CHAT_MODEL_REASONING_EFFORTS[cleanAlias];
  return Array.isArray(defaults) && defaults.length > 0 ? defaults.slice() : ['low', 'medium', 'high'];
}

function defaultReasoningEffortForAlias(alias) {
  const options = reasoningEffortOptionsForAlias(alias);
  return options.length > 0 ? options[0] : 'low';
}

function normalizeProjectChatModelReasoningEffort(value, alias) {
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

function activeProjectChatModelAlias() {
  const alias = normalizeProjectChatModelAlias(activeProject()?.chat_model);
  return alias || 'spark';
}

function activeProjectChatModelReasoningEffort() {
  const alias = activeProjectChatModelAlias();
  return normalizeProjectChatModelReasoningEffort(activeProject()?.chat_model_reasoning_effort, alias);
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
  setFileSidebarAvailability();
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
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

function isLikelyIOS() {
  const ua = String(navigator.userAgent || '').toLowerCase();
  return /iphone|ipad|ipod/.test(ua)
    || (ua.includes('macintosh') && navigator.maxTouchPoints > 1);
}

function firstNonEmptyChunkMimeType(chunks) {
  if (!Array.isArray(chunks)) return '';
  for (const chunk of chunks) {
    const mt = String(chunk?.type || '').trim();
    if (mt) return mt;
  }
  return '';
}


function canUseMicrophoneCapture() {
  return Boolean(window.MediaRecorder)
    && Boolean(navigator.mediaDevices)
    && typeof navigator.mediaDevices.getUserMedia === 'function';
}

const MIC_CAPTURE_CONSTRAINTS = {
  echoCancellation: true,
  autoGainControl: true,
  noiseSuppression: true,
};

let _cachedMicStream = null;
let _micStreamPromise = null;
let _cachedMicStreamCleanup = null;
let _micRefreshRequested = false;

function detachCachedMicStreamObservers() {
  if (typeof _cachedMicStreamCleanup === 'function') {
    try {
      _cachedMicStreamCleanup();
    } catch (_) {}
  }
  _cachedMicStreamCleanup = null;
}

function requestMicRefresh() {
  _micRefreshRequested = true;
}

function streamHasLiveAudioTrack(stream) {
  if (!stream || typeof stream.getAudioTracks !== 'function') return false;
  if (typeof stream.active === 'boolean' && !stream.active) return false;
  const tracks = stream.getAudioTracks();
  if (!Array.isArray(tracks) || tracks.length === 0) return false;
  return tracks.every((track) => {
    if (!track) return false;
    if (String(track.readyState || '').toLowerCase() !== 'live') return false;
    if (typeof track.enabled === 'boolean' && !track.enabled) return false;
    if (typeof track.muted === 'boolean' && track.muted) return false;
    return true;
  });
}

function invalidateCachedMicStream({ stopTracks = false } = {}) {
  const stream = _cachedMicStream;
  detachCachedMicStreamObservers();
  _cachedMicStream = null;
  if (!stream || !stopTracks || typeof stream.getTracks !== 'function') return;
  try {
    stream.getTracks().forEach((track) => {
      try {
        if (track?.readyState !== 'ended') track.stop();
      } catch (_) {}
    });
  } catch (_) {}
}

function observeCachedMicStream(stream) {
  if (!stream || typeof stream.getAudioTracks !== 'function') return;
  const tracks = stream.getAudioTracks();
  const disposers = [];
  const invalidate = () => {
    requestMicRefresh();
    if (_cachedMicStream === stream) {
      const activeCapture = state.chatVoiceCapture;
      if (activeCapture && activeCapture.mediaStream === stream && !activeCapture.stopping) {
        return;
      }
      invalidateCachedMicStream({ stopTracks: false });
    }
  };

  if (typeof stream.addEventListener === 'function') {
    const onInactive = () => invalidate();
    try {
      stream.addEventListener('inactive', onInactive, { once: true });
      disposers.push(() => {
        try {
          stream.removeEventListener('inactive', onInactive);
        } catch (_) {}
      });
    } catch (_) {}
  }

  tracks.forEach((track) => {
    if (!track || typeof track.addEventListener !== 'function') return;
    const onEnded = () => invalidate();
    const onMute = () => invalidate();
    try {
      track.addEventListener('ended', onEnded, { once: true });
      track.addEventListener('mute', onMute, { once: true });
      disposers.push(() => {
        try { track.removeEventListener('ended', onEnded); } catch (_) {}
        try { track.removeEventListener('mute', onMute); } catch (_) {}
      });
    } catch (_) {}
  });

  _cachedMicStreamCleanup = () => {
    for (const dispose of disposers) {
      try { dispose(); } catch (_) {}
    }
  };
}

function acquireMicStream() {
  if (_cachedMicStream && !_micRefreshRequested && streamHasLiveAudioTrack(_cachedMicStream)) {
    return Promise.resolve(_cachedMicStream);
  }
  if (_cachedMicStream) invalidateCachedMicStream({ stopTracks: false });
  if (_micStreamPromise) return _micStreamPromise;
  _micStreamPromise = navigator.mediaDevices.getUserMedia({
    audio: { ...MIC_CAPTURE_CONSTRAINTS },
  }).then((stream) => {
    _micRefreshRequested = false;
    _cachedMicStream = stream;
    observeCachedMicStream(stream);
    _micStreamPromise = null;
    return stream;
  }).catch((err) => {
    _micStreamPromise = null;
    throw err;
  });
  return _micStreamPromise;
}

function releaseMicStream({ force = false } = {}) {
  if (!_cachedMicStream) return;
  const activeCapture = state.chatVoiceCapture;
  if (!force && activeCapture && activeCapture.mediaStream === _cachedMicStream && !activeCapture.stopping) {
    return;
  }
  invalidateCachedMicStream({ stopTracks: true });
}

function parseOptionalBoolean(value) {
  if (typeof value === 'boolean') return value;
  const normalized = String(value || '').trim().toLowerCase();
  if (!normalized) return null;
  if (normalized === '1' || normalized === 'true' || normalized === 'on' || normalized === 'yes') return true;
  if (normalized === '0' || normalized === 'false' || normalized === 'off' || normalized === 'no') return false;
  return null;
}

function isVoiceVADAutoSendEnabled() {
  try {
    const queryValue = new URL(window.location.href).searchParams.get(VOICE_VAD_AUTO_SEND_QUERY_PARAM);
    const queryFlag = parseOptionalBoolean(queryValue);
    if (queryFlag !== null) return queryFlag;
  } catch (_) {}
  try {
    const storedValue = window.localStorage.getItem(VOICE_VAD_AUTO_SEND_STORAGE_KEY);
    const storedFlag = parseOptionalBoolean(storedValue);
    if (storedFlag !== null) return storedFlag;
  } catch (_) {}
  return VOICE_VAD_AUTO_SEND_DEFAULT;
}

let _sttMimeType = '';
let _sttParts = [];
let _sttActive = false;
let _sttAbortController = null;

function recordHarnessSTTAction(action, payload = {}) {
  if (!Array.isArray(window.__harnessLog)) return;
  window.__harnessLog.push({ type: 'stt', action, ...payload });
}

function recordHarnessPrintAction(action, payload = {}) {
  if (!Array.isArray(window.__harnessLog)) return;
  window.__harnessLog.push({ type: 'print', action, ...payload });
}

function openPrintView(url) {
  const target = String(url || '').trim();
  if (!target) return;
  let frame = document.getElementById('print-frame');
  if (!(frame instanceof HTMLIFrameElement)) {
    frame = document.createElement('iframe');
    frame.id = 'print-frame';
    frame.style.display = 'none';
    document.body.appendChild(frame);
  }
  const separator = target.includes('?') ? '&' : '?';
  const nextURL = `${target}${separator}__tabura_print=${Date.now()}`;
  frame.setAttribute('src', nextURL);
  recordHarnessPrintAction('open', { url: nextURL });
  showStatus('print view opened');
}

function sttStart(mimeType) {
  if (_sttAbortController) {
    try { _sttAbortController.abort(); } catch (_) {}
    _sttAbortController = null;
  }
  _sttMimeType = String(mimeType || '').trim();
  _sttParts = [];
  _sttActive = true;
  recordHarnessSTTAction('start', { mime_type: _sttMimeType || 'application/octet-stream' });
  return Promise.resolve();
}

function sttSendBlob(blob) {
  if (!_sttActive) return Promise.resolve();
  if (!blob || blob.size <= 0) return Promise.resolve();
  _sttParts.push(blob);
  recordHarnessSTTAction('append', { bytes: Number(blob.size) || 0 });
  return Promise.resolve();
}

function sttStop() {
  if (!_sttActive) return Promise.reject(new Error('no active STT session'));
  _sttActive = false;
  recordHarnessSTTAction('stop');
  const mimeType = String(_sttMimeType || '').trim() || 'application/octet-stream';
  _sttMimeType = '';
  const parts = Array.isArray(_sttParts) ? _sttParts.slice() : [];
  _sttParts = [];
  if (parts.length === 0) {
    return Promise.resolve({ text: '', reason: 'recording_too_short' });
  }
  const audioBlob = new Blob(parts, { type: mimeType });
  if (!(audioBlob instanceof Blob) || audioBlob.size <= 0) {
    return Promise.resolve({ text: '', reason: 'recording_too_short' });
  }
  const form = new FormData();
  form.append('file', audioBlob, 'recording.audio');
  form.append('mime_type', mimeType);
  const controller = new AbortController();
  _sttAbortController = controller;
  return fetch(apiURL('stt/transcribe'), {
    method: 'POST',
    body: form,
    signal: controller.signal,
  }).then(async (resp) => {
    const raw = await resp.text();
    let payload = null;
    if (raw) {
      try {
        payload = JSON.parse(raw);
      } catch (_) {}
    }
    if (!resp.ok) {
      let detail = '';
      if (payload && typeof payload === 'object') {
        if (typeof payload.error === 'string') detail = payload.error;
        if (!detail && typeof payload.message === 'string') detail = payload.message;
      }
      if (!detail) detail = raw || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    if (!payload || typeof payload !== 'object') {
      throw new Error('invalid STT response');
    }
    return {
      text: String(payload.text || ''),
      reason: String(payload.reason || ''),
    };
  }).catch((err) => {
    if (String(err?.name || '') === 'AbortError') {
      throw new Error('STT cancelled');
    }
    throw err;
  }).finally(() => {
    if (_sttAbortController === controller) {
      _sttAbortController = null;
    }
  });
}

function sttCancel() {
  _sttActive = false;
  _sttMimeType = '';
  _sttParts = [];
  if (_sttAbortController) {
    try { _sttAbortController.abort(); } catch (_) {}
    _sttAbortController = null;
  }
  recordHarnessSTTAction('cancel');
}

function handleSTTWSMessage(payload) {
  const type = String(payload?.type || '');
  if (type.startsWith('stt_')) {
    return true;
  }
  if (type === 'tts_error') {
    console.warn('TTS error:', payload.error);
    return true;
  }
  return false;
}

function stopChatVoiceMedia(capture) {
  if (!capture) return;
  if (capture.vadState?.isRunning) {
    stopVADMonitor(capture);
  }
  if (capture.mediaRecorder) {
    try {
      if (capture.mediaRecorder.state !== 'inactive') {
        capture.mediaRecorder.stop();
      }
    } catch (_) {}
  }
  capture.mediaRecorder = null;
  capture.mediaStream = null;
  if (capture._sileroDeferred) {
    try { capture._sileroDeferred.destroy(); } catch (_) {}
    capture._sileroDeferred = null;
  }
  if (capture._vadStream) {
    for (const track of capture._vadStream.getTracks()) { track.stop(); }
    capture._vadStream = null;
  }
  if (capture._vadAudioContext) {
    try { capture._vadAudioContext.close(); } catch (_) {}
    capture._vadAudioContext = null;
  }
}

function handleVADNoSpeechTimeout(capture) {
  stopVADMonitor(capture);
  state.indicatorSuppressedByCanvasUpdate = false;
  showStatus('no speech detected');
  setRecording(false);
  setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-vad-no-speech');
  sttCancel();
  stopChatVoiceMedia(capture);
  if (state.chatVoiceCapture === capture) {
    state.chatVoiceCapture = null;
  }
  updateAssistantActivityIndicator();
  window.setTimeout(() => {
    if (isUiReadyForStatus()) {
      showStatus('ready');
    }
  }, 800);
}

function startVADMonitor(capture) {
  if (!isVoiceVADAutoSendEnabled()) return;
  if (!capture || capture.vadState) return;
  if (!capture.mediaStream) return;
  void startSileroVADMonitor(capture);
}

async function startSileroVADMonitor(capture) {
  const isHotwordCapture = Boolean(capture?.hotwordTriggered);
  const vadNoSpeechMs = isHotwordCapture ? HOTWORD_VAD_NO_SPEECH_MS : VOICE_VAD_NO_SPEECH_MS;
  const redemptionMs = isHotwordCapture ? 1200 : 600;
  const minSpeechMs = isHotwordCapture ? 400 : 250;

  const vadState = {
    sileroInstance: null,
    noSpeechTimer: null,
    maxDurationTimer: null,
    committed: false,
    isRunning: true,
  };
  capture.vadState = vadState;

  vadState.maxDurationTimer = window.setTimeout(() => {
    if (!vadState.isRunning || vadState.committed) return;
    vadState.committed = true;
    stopVADMonitor(capture);
    void stopVoiceCaptureAndSend();
  }, VOICE_VAD_MAX_RECORDING_HARD_MS);

  try {
    // Clone the stream so MicVAD's AudioContext/AudioWorklet cannot interfere
    // with the MediaRecorder consuming the original stream (Safari bug).
    const vadStream = capture.mediaStream.clone();
    capture._vadStream = vadStream;

    const instance = await initVAD({
      stream: vadStream,
      audioContext: capture._vadAudioContext || undefined,
      positiveSpeechThreshold: 0.6,
      negativeSpeechThreshold: 0.35,
      redemptionMs,
      minSpeechMs,
      preSpeechPadMs: 300,
      onSpeechStart() {
        if (!vadState.isRunning || vadState.committed) return;
        if (vadState.noSpeechTimer) {
          window.clearTimeout(vadState.noSpeechTimer);
          vadState.noSpeechTimer = null;
        }
      },
      onSpeechEnd(audio) {
        if (!vadState.isRunning || vadState.committed) return;
        vadState.committed = true;
        if (audio instanceof Float32Array && audio.length > 0) {
          capture._vadAudioBlob = float32ToWav(audio, 16000);
        }
        stopVADMonitor(capture);
        void stopVoiceCaptureAndSend();
      },
    });

    if (!vadState.isRunning) {
      if (instance) instance.destroy();
      return;
    }

    vadState.sileroInstance = instance;
    if (instance) instance.start();

    // Start the no-speech timer only after the VAD is running. On Safari,
    // model + AudioWorklet init can exceed 4s; starting the timer before
    // init would fire handleVADNoSpeechTimeout and tear down the capture
    // before the VAD ever processed a frame.
    vadState.noSpeechTimer = window.setTimeout(() => {
      if (!vadState.isRunning || vadState.committed) return;
      handleVADNoSpeechTimeout(capture);
    }, vadNoSpeechMs);
  } catch (err) {
    console.warn('Silero VAD init failed:', err);
    if (vadState.isRunning) {
      handleVADNoSpeechTimeout(capture);
    }
  }
}

function stopVADMonitor(capture) {
  if (!capture || !capture.vadState) return;
  const vs = capture.vadState;
  capture.vadState = null;
  vs.isRunning = false;

  if (vs.noSpeechTimer) window.clearTimeout(vs.noSpeechTimer);
  if (vs.maxDurationTimer) window.clearTimeout(vs.maxDurationTimer);
  if (vs.sileroInstance) {
    try { vs.sileroInstance.pause(); } catch (_) {}
    capture._sileroDeferred = vs.sileroInstance;
  }
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
    let done = false;
    let timeoutId = null;
    const finish = () => {
      if (done) return;
      done = true;
      recorder.removeEventListener('stop', onStop);
      recorder.removeEventListener('error', onError);
      if (timeoutId !== null) {
        window.clearTimeout(timeoutId);
        timeoutId = null;
      }
      stopChatVoiceMedia(capture);
      resolve();
    };
    const onStop = () => {
      finish();
    };
    const onError = () => {
      finish();
    };
    recorder.addEventListener('stop', onStop, { once: true });
    recorder.addEventListener('error', onError, { once: true });
    try {
      // Avoid requestData() before stop(): Safari/WebKit has had fetch-data
      // races when requestData and stop are queued back-to-back, which can
      // drop the final audio payload on iOS.
      recorder.stop();
    } catch (_) {
      finish();
      return;
    }
    timeoutId = window.setTimeout(finish, VOICE_CAPTURE_STOP_FLUSH_TIMEOUT_MS);
  });
}

async function beginVoiceCapture(x, y, anchor, options = {}) {
  if (state.chatVoiceCapture) return;
  if (!canUseMicrophoneCapture()) {
    showVoiceCaptureNotice(microphoneUnavailableMessage(), x, y);
    return;
  }
  cancelLiveSessionListen();
  // Interrupt TTS playback when starting recording
  stopTTSPlayback();

  // Pre-create AudioContext during the user gesture (synchronous, before
  // any await) so iOS Safari allows it to enter "running" state.  Without
  // this, vad.MicVAD.new() creates its own AudioContext deep in an async
  // chain where iOS Safari considers the gesture expired and suspends it,
  // causing the AudioWorklet to never process frames.
  let vadAudioContext = null;
  if (isVoiceVADAutoSendEnabled() && typeof AudioContext !== 'undefined') {
    try {
      vadAudioContext = new AudioContext();
      if (vadAudioContext.state === 'suspended') vadAudioContext.resume();
    } catch (_) {}
  }

  const capture = {
    active: false,
    stopping: false,
    stopRequested: false,
    autoSend: true,
    hotwordTriggered: Boolean(options && options.hotwordTriggered),
    mediaStream: null,
    mediaRecorder: null,
    chunks: [],
  };
  state.chatVoiceCapture = capture;
  state.lastInputOrigin = 'voice';
  state.voiceAwaitingTurn = false;
  state.indicatorSuppressedByCanvasUpdate = false;
  startVoiceLifecycleOp('voice-capture-begin');
  setVoiceLifecycle(VOICE_LIFECYCLE.RECORDING, 'voice-capture-begin');
  setLastInputPosition(x, y);
  setRecording(true);
  setInputAnchor(anchor || null);
  updateAssistantActivityIndicator();
  showStatus('recording...');
  try {
    const stream = await acquireMicStream();
    if (state.chatVoiceCapture !== capture) {
      if (vadAudioContext) { try { vadAudioContext.close(); } catch (_) {} }
      return;
    }
    const recorder = newMediaRecorder(stream);
    capture.mimeType = String(recorder?.mimeType || '').trim();
    if (state.chatVoiceCapture !== capture) {
      if (vadAudioContext) { try { vadAudioContext.close(); } catch (_) {} }
      return;
    }
    capture.mediaStream = stream;
    capture.mediaRecorder = recorder;
    capture._vadAudioContext = vadAudioContext;
    vadAudioContext = null;
    capture.active = true;
    recorder.addEventListener('dataavailable', (ev) => {
      if (!ev?.data || ev.data.size <= 0) return;
      capture.chunks.push(ev.data);
    });
    recorder.start(VOICE_VAD_RECORDER_CHUNK_MS);
    if (!capture.stopRequested) {
      startVADMonitor(capture);
    }
    if (capture.stopRequested) {
      void stopVoiceCaptureAndSend();
    }
  } catch (err) {
    setRecording(false);
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-capture-start-failed');
    updateAssistantActivityIndicator();
    const message = String(err?.message || err || 'voice capture failed');
    showVoiceCaptureNotice(`voice capture failed: ${message}`, x, y);
    sttCancel();
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
    if (vadAudioContext) { try { vadAudioContext.close(); } catch (_) {} }
  }
}

function voiceCaptureEmptyReasonMessage(reason) {
  const normalized = String(reason || '').trim().toLowerCase();
  if (normalized === 'recording_too_short') {
    return 'recording too short; hold to talk for a bit longer';
  }
  if (normalized === 'likely_noise' || normalized === 'no_speech_detected') {
    return 'no clear speech detected; try again in a quieter environment';
  }
  if (normalized === 'empty_transcript') {
    return 'speech recognizer returned no transcript';
  }
  return 'speech recognizer returned empty text';
}

async function stopVoiceCaptureAndSend() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  const opSeq = startVoiceLifecycleOp('voice-capture-stop-send');
  const isHotwordCapture = Boolean(capture?.hotwordTriggered);
  capture.stopRequested = true;
  if (!capture.active) return;
  capture.stopping = true;
  setRecording(false);
  setVoiceLifecycle(VOICE_LIFECYCLE.STOPPING_RECORDING, 'voice-capture-stop-send');
  state.voiceAwaitingTurn = true;
  setVoiceLifecycle(VOICE_LIFECYCLE.AWAITING_TURN, 'voice-awaiting-turn');
  state.indicatorSuppressedByCanvasUpdate = false;
  updateAssistantActivityIndicator();
  showStatus('transcribing...');
  let remoteStopped = false;
  let reopenDialogueListen = false;
  try {
    let sttBlob = null;
    let mimeType = '';
    if (capture._vadAudioBlob) {
      // VAD auto-stop: use speech audio directly, skip MediaRecorder flush
      // so Safari cannot interfere via its broken stop/dataavailable ordering.
      sttBlob = capture._vadAudioBlob;
      mimeType = 'audio/wav';
      capture._vadAudioBlob = null;
    } else {
      // Manual stop / timeout: flush MediaRecorder and use its chunks.
      await stopChatVoiceMediaAndFlush(capture);
      mimeType = String(capture.mimeType || '').trim();
      if (!mimeType) {
        mimeType = firstNonEmptyChunkMimeType(capture.chunks);
      }
      if (capture.chunks.length > 0) {
        sttBlob = mimeType
          ? new Blob(capture.chunks, { type: mimeType })
          : new Blob(capture.chunks);
        if (!mimeType) {
          mimeType = String(sttBlob?.type || '').trim();
        }
        capture.chunks = [];
      }
      if (!mimeType) {
        mimeType = isLikelyIOS() ? 'audio/mp4' : 'audio/webm';
      }
    }
    sttStart(mimeType);
    if (sttBlob) {
      await sttSendBlob(sttBlob);
    }
    const result = await sttStop();
    remoteStopped = true;
    const transcript = String(result?.text || '').trim();
    if (!transcript) {
      if (isDialogueLiveSession() && isHotwordCapture) {
        state.voiceAwaitingTurn = false;
        reopenDialogueListen = true;
        return;
      }
      throw new Error(voiceCaptureEmptyReasonMessage(result?.reason));
    }
    showStatus('sending...');
    state.voiceTranscriptSubmitInFlight = true;
    void submitMessage(transcript, { kind: 'voice_transcript' });
  } catch (err) {
    if (opSeq !== state.voiceLifecycleSeq) return;
    state.voiceAwaitingTurn = false;
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-capture-stop-failed');
    updateAssistantActivityIndicator();
    const message = String(err?.message || err || 'voice capture failed');
    const pos = getLastInputPosition();
    const x = Number.isFinite(pos?.x) ? Number(pos.x) : null;
    const y = Number.isFinite(pos?.y) ? Number(pos.y) : null;
    showVoiceCaptureNotice(`voice capture failed: ${message}`, x, y);
    if (isDialogueLiveSession()) {
      reopenDialogueListen = true;
    }
  } finally {
    if (!remoteStopped) {
      sttCancel();
    }
    if (state.liveSessionActive) {
      stopHotwordMonitor();
      state.hotwordActive = false;
    }
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
    if (opSeq === state.voiceLifecycleSeq) {
      syncVoiceLifecycle('voice-capture-stop-finished');
    }
    updateAssistantActivityIndicator();
    if (reopenDialogueListen && isDialogueLiveSession()) {
      // Re-open follow-up listen only after capture teardown has settled.
      window.setTimeout(() => {
        if (!isDialogueLiveSession()) return;
        onLiveSessionTTSPlaybackComplete();
      }, 0);
    }
  }
}

function cancelChatVoiceCapture() {
  const capture = state.chatVoiceCapture;
  if (!capture) return;
  setRecording(false);
  state.voiceTranscriptSubmitInFlight = false;
  state.voiceAwaitingTurn = false;
  abortPendingSubmit('voice_transcript');
  sttCancel();
  if (state.liveSessionActive) {
    stopHotwordMonitor();
    state.hotwordActive = false;
  }
  stopChatVoiceMedia(capture);
  state.chatVoiceCapture = null;
  setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-capture-cancelled');
  updateAssistantActivityIndicator();
}

function showCanvasColumn(paneId) {
  const col = document.getElementById('canvas-column');
  if (!col) return;
  if (paneId !== 'canvas-text' && state.artifactEditMode) {
    exitArtifactEditMode({ applyChanges: true });
  }
  if (paneId !== 'canvas-text') {
    exitPrReviewMode();
  }
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
  setUiMode('artifact');
  persistLastView({ mode: 'artifact' });
  if (!isVoiceTurn() && isDirectAssistantWorking()) {
    hideOverlay();
  }
  updateAssistantActivityIndicator();
}

function hideCanvasColumn() {
  if (state.artifactEditMode) {
    exitArtifactEditMode({ applyChanges: true });
  }
  exitPrReviewMode();
  clearInkDraft();
  state.hasArtifact = false;
  state.workspaceOpenFilePath = '';
  state.workspaceStepInFlight = false;
  setUiMode('rasa');
  clearLineHighlight();
  persistLastView({ mode: 'rasa' });
  // Hide all panes to show blank canvas
  const viewport = document.getElementById('canvas-viewport');
  if (viewport) {
    viewport.querySelectorAll('.canvas-pane').forEach((p) => {
      p.style.display = 'none';
      p.classList.remove('is-active');
    });
  }
  updateAssistantActivityIndicator();
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
  const normalized = String(mode || 'chat').toLowerCase();
  state.chatMode = normalized === 'plan' || normalized === 'review' ? normalized : 'chat';
  const pill = document.getElementById('chat-mode-pill');
  if (pill) {
    pill.textContent = state.chatMode;
    pill.className = `badge ${state.chatMode === 'plan' || state.chatMode === 'review' ? 'review' : ''}`;
  }
}

function hasLocalAssistantWork() {
  return state.pendingQueue.length > 0
    || state.pendingByTurn.size > 0
    || state.assistantActiveTurns.size > 0
    || state.assistantUnknownTurns > 0;
}

function hasRemoteAssistantWork() {
  return state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0;
}

function hasLocalStopCapableWork() {
  return state.assistantActiveTurns.size > 0
    || state.assistantUnknownTurns > 0
    || state.assistantCancelInFlight;
}

function isVoiceTranscriptSubmitPending() {
  return Boolean(state.pendingSubmitController) && state.pendingSubmitKind === 'voice_transcript';
}

function hasPendingOverlayTurn() {
  const ui = getUiState();
  if (!ui || !ui.overlayVisible) return false;
  return Boolean(String(ui.overlayTurnId || '').trim());
}

function isDirectAssistantWorking() {
  return hasLocalStopCapableWork()
    || state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0;
}

function isAssistantWorking() {
  return isDirectAssistantWorking();
}

function isTTSSpeaking() {
  return state.ttsPlaying;
}

function startVoiceLifecycleOp(reason = '') {
  state.voiceLifecycleSeq += 1;
  state.voiceLifecycleReason = String(reason || '');
  return state.voiceLifecycleSeq;
}

function setVoiceLifecycle(next, reason = '') {
  const normalized = Object.values(VOICE_LIFECYCLE).includes(next)
    ? next
    : VOICE_LIFECYCLE.IDLE;
  state.voiceLifecycle = normalized;
  if (reason) {
    state.voiceLifecycleReason = String(reason);
  }
  return state.voiceLifecycle;
}

function deriveVoiceLifecycle() {
  if (isRecording()) return VOICE_LIFECYCLE.RECORDING;
  if (state.chatVoiceCapture?.stopping) return VOICE_LIFECYCLE.STOPPING_RECORDING;
  if (state.voiceAwaitingTurn) return VOICE_LIFECYCLE.AWAITING_TURN;
  if (isLiveSessionListenActive()) return VOICE_LIFECYCLE.LISTENING;
  if (hasLocalStopCapableWork()) return VOICE_LIFECYCLE.ASSISTANT_WORKING;
  if (isTTSSpeaking()) return VOICE_LIFECYCLE.TTS_PLAYING;
  return VOICE_LIFECYCLE.IDLE;
}

function syncVoiceLifecycle(reason = '') {
  return setVoiceLifecycle(deriveVoiceLifecycle(), reason);
}

function isStopCapableLifecycle(mode = state.voiceLifecycle) {
  return mode === VOICE_LIFECYCLE.LISTENING
    || mode === VOICE_LIFECYCLE.STOPPING_RECORDING
    || mode === VOICE_LIFECYCLE.AWAITING_TURN
    || mode === VOICE_LIFECYCLE.ASSISTANT_WORKING;
}

function isUiReadyForStatus() {
  const mode = syncVoiceLifecycle('ready-check');
  return mode === VOICE_LIFECYCLE.IDLE;
}

function canStartLiveDialogueListen() {
  if (!canSpeakTTS()) return false;
  if (!isDialogueLiveSession()) return false;
  const mode = syncVoiceLifecycle('can-start-live-dialogue');
  if (mode === VOICE_LIFECYCLE.RECORDING || mode === VOICE_LIFECYCLE.STOPPING_RECORDING) return false;
  if (mode === VOICE_LIFECYCLE.TTS_PLAYING) return false;
  if (state.chatVoiceCapture) return false;
  if (mode !== VOICE_LIFECYCLE.LISTENING && isStopCapableLifecycle(mode)) return false;
  return true;
}

function beginConversationVoiceCapture() {
  const x = Math.floor(window.innerWidth / 2);
  const y = Math.floor(window.innerHeight / 2);
  void beginVoiceCapture(x, y, null, { hotwordTriggered: true });
}

function currentIndicatorMode() {
  if (shouldShowCompanionIdleSurface()) return '';
  const mode = state.voiceLifecycle;
  if (mode === VOICE_LIFECYCLE.RECORDING) return 'recording';
  if (mode === VOICE_LIFECYCLE.LISTENING) return 'listening';
  if (isStopCapableLifecycle(mode)) return 'play';
  if (state.liveSessionActive && state.hotwordActive) return 'paused';
  if (state.indicatorSuppressedByCanvasUpdate) return '';
  return '';
}

function shouldStopInUiClick() {
  return isStopCapableLifecycle(syncVoiceLifecycle('ui-stop-check'));
}

function isUiStopGestureActive() {
  return shouldStopInUiClick()
    || isVoiceTranscriptSubmitPending()
    || state.voiceTranscriptSubmitInFlight
    || hasPendingOverlayTurn();
}

function updateAssistantActivityIndicator() {
  if (!hasLocalAssistantWork() && state.assistantRemoteActiveCount <= 0 && state.assistantRemoteQueuedCount <= 0) {
    state.assistantUnknownTurns = 0;
    state.assistantActiveTurns.clear();
  }
  syncVoiceLifecycle('indicator-update');
  state.hotwordActive = isHotwordActive();
  updateCompanionIdleSurface();
  const pos = getLastInputPosition();
  const px = Number.isFinite(pos?.x) && pos.x > 0 ? pos.x : Math.floor(window.innerWidth / 2);
  const py = Number.isFinite(pos?.y) && pos.y > 0 ? pos.y : Math.floor(window.innerHeight / 2);
  const mode = currentIndicatorMode();
  if (mode) {
    showIndicatorMode(mode, px, py);
  } else {
    hideIndicator();
  }
  requestHotwordSync();
}

function paneIdForCanvasKind(kind) {
  const normalized = String(kind || '').trim().toLowerCase();
  if (normalized === 'image_artifact' || normalized === 'image') return 'canvas-image';
  if (normalized === 'pdf_artifact' || normalized === 'pdf') return 'canvas-pdf';
  if (normalized === 'text_artifact' || normalized === 'text') return 'canvas-text';
  return '';
}

function isTemporaryCanvasArtifactTitle(title) {
  const normalized = String(title || '')
    .trim()
    .replaceAll('\\', '/')
    .replace(/^\.\//, '')
    .toLowerCase();
  return normalized.startsWith('.tabura/artifacts/tmp/')
    || normalized.startsWith('tabura/artifacts/tmp/');
}

function isRealCanvasArtifactEvent(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (!kind || kind === 'clear_canvas') return false;
  if (kind === 'image_artifact' || kind === 'image' || kind === 'pdf_artifact' || kind === 'pdf') {
    return true;
  }
  if (kind !== 'text_artifact' && kind !== 'text') return false;

  const meta = payload?.meta;
  if (meta && typeof meta === 'object' && typeof meta.real_artifact === 'boolean') {
    return meta.real_artifact;
  }

  const title = String(payload?.title || '').trim();
  if (!title) return false;
  return !isTemporaryCanvasArtifactTitle(title);
}

function isMobileViewport() {
  return window.matchMedia('(max-width: 767px)').matches;
}

function statusBadgeForDiffFile(statusRaw) {
  const normalized = String(statusRaw || '').trim().toLowerCase();
  if (normalized === 'added') return 'A';
  if (normalized === 'deleted') return 'D';
  if (normalized === 'renamed') return 'R';
  return 'M';
}

function parseUnifiedDiffFiles(diffText) {
  const text = String(diffText || '').replaceAll('\r\n', '\n');
  if (!text.trim()) return [];
  const lines = text.split('\n');
  const files = [];
  let current = null;

  const pushCurrent = () => {
    if (!current) return;
    const diff = current.lines.join('\n').trimEnd();
    if (!diff) return;
    files.push({
      path: String(current.path || '(patch)'),
      status: String(current.status || 'modified'),
      diff,
    });
  };

  const parsePathFromHeader = (line) => {
    const match = /^diff --git a\/(.+?) b\/(.+)$/.exec(line);
    if (!match) return '';
    const right = String(match[2] || '').trim();
    const left = String(match[1] || '').trim();
    if (right && right !== '/dev/null') return right;
    return left;
  };

  const parsePathFromMarker = (line, marker) => {
    if (!line.startsWith(marker)) return '';
    const raw = String(line.slice(marker.length)).trim();
    if (!raw || raw === '/dev/null') return '';
    return raw.startsWith('a/') || raw.startsWith('b/') ? raw.slice(2) : raw;
  };

  for (const line of lines) {
    if (line.startsWith('diff --git ')) {
      pushCurrent();
      current = {
        path: parsePathFromHeader(line) || '(patch)',
        status: 'modified',
        lines: [line],
      };
      continue;
    }

    if (!current) {
      continue;
    }

    current.lines.push(line);
    if (line.startsWith('new file mode ')) {
      current.status = 'added';
      continue;
    }
    if (line.startsWith('deleted file mode ')) {
      current.status = 'deleted';
      continue;
    }
    if (line.startsWith('rename from ')) {
      current.status = 'renamed';
      continue;
    }
    if (line.startsWith('rename to ')) {
      const renamedTo = String(line.slice('rename to '.length)).trim();
      if (renamedTo) current.path = renamedTo;
      current.status = 'renamed';
      continue;
    }
    const plusPath = parsePathFromMarker(line, '+++ ');
    if (plusPath && current.path === '(patch)') {
      current.path = plusPath;
      continue;
    }
    const minusPath = parsePathFromMarker(line, '--- ');
    if (minusPath && current.path === '(patch)') {
      current.path = minusPath;
    }
  }
  pushCurrent();

  if (files.length > 0) return files;
  return [{
    path: '(patch)',
    status: 'modified',
    diff: text.trimEnd(),
  }];
}

let sidebarEdgeTapAt = 0;
function setPrReviewDrawerOpen(open) {
  const shouldOpen = Boolean(open) && (state.prReviewMode || Boolean(state.activeProjectId));
  state.prReviewDrawerOpen = shouldOpen;
  document.body.classList.toggle('file-sidebar-open', shouldOpen);
  const pane = document.getElementById('pr-file-pane');
  const backdrop = document.getElementById('pr-file-drawer-backdrop');
  if (pane) pane.classList.toggle('is-open', shouldOpen);
  if (backdrop) backdrop.classList.toggle('is-open', shouldOpen);
}

function setFileSidebarAvailability() {
  const enabled = state.prReviewMode || Boolean(state.activeProjectId);
  document.body.classList.toggle('file-sidebar-enabled', enabled);
  if (!enabled) {
    setPrReviewDrawerOpen(false);
  }
}

function normalizeWorkspaceBrowserPath(rawPath) {
  const cleaned = String(rawPath || '').replaceAll('\\', '/').trim();
  if (!cleaned) return '';
  const pieces = cleaned.split('/').filter((piece) => piece && piece !== '.' && piece !== '..');
  return pieces.join('/');
}

function parentWorkspaceBrowserPath(path) {
  const cleaned = normalizeWorkspaceBrowserPath(path);
  if (!cleaned) return '';
  const pieces = cleaned.split('/');
  pieces.pop();
  return pieces.join('/');
}

function workspaceNavigableFilePaths() {
  const entries = Array.isArray(state.workspaceBrowserEntries) ? state.workspaceBrowserEntries : [];
  const files = [];
  entries.forEach((entry) => {
    if (Boolean(entry?.is_dir)) return;
    const path = normalizeWorkspaceBrowserPath(entry?.path || '');
    if (!path) return;
    files.push(path);
  });
  return files;
}

function resolveWorkspaceSteppingCurrentFile() {
  const fromState = normalizeWorkspaceBrowserPath(state.workspaceOpenFilePath);
  if (fromState) return fromState;
  const activeTitle = normalizeWorkspaceBrowserPath(getActiveArtifactTitle());
  if (activeTitle) return activeTitle;
  return '';
}

function sidebarFileKindForPath(path) {
  const lower = String(path || '').toLowerCase();
  if (lower.endsWith('.pdf')) return 'pdf_artifact';
  for (const ext of SIDEBAR_IMAGE_EXTENSIONS) {
    if (lower.endsWith(ext)) return 'image_artifact';
  }
  return 'text_artifact';
}

function companionViewKindForPath(path) {
  const normalized = normalizeWorkspaceBrowserPath(path);
  if (normalized === COMPANION_TRANSCRIPT_VIEW_PATH) return 'transcript';
  if (normalized === COMPANION_SUMMARY_VIEW_PATH) return 'summary';
  if (normalized === COMPANION_REFERENCES_VIEW_PATH) return 'references';
  return '';
}

function workspaceCompanionEntries() {
  return [
    { name: MEETING_TRANSCRIPT_LABEL, path: COMPANION_TRANSCRIPT_VIEW_PATH, is_dir: false },
    { name: MEETING_SUMMARY_LABEL, path: COMPANION_SUMMARY_VIEW_PATH, is_dir: false },
    { name: MEETING_REFERENCES_LABEL, path: COMPANION_REFERENCES_VIEW_PATH, is_dir: false },
  ];
}

function defaultItemSidebarCounts() {
  return {
    inbox: 0,
    waiting: 0,
    someday: 0,
    done: 0,
  };
}

function normalizeItemSidebarView(rawView) {
  const value = String(rawView || '').trim().toLowerCase();
  if (ITEM_SIDEBAR_VIEWS.includes(value)) return value;
  return 'inbox';
}

function itemSidebarEndpoint(view) {
  const normalized = normalizeItemSidebarView(view);
  if (normalized === 'done') return `items/${normalized}?limit=50`;
  return `items/${normalized}`;
}

function normalizeItemSidebarCounts(rawCounts) {
  const counts = defaultItemSidebarCounts();
  if (!rawCounts || typeof rawCounts !== 'object') return counts;
  ITEM_SIDEBAR_VIEWS.forEach((view) => {
    const value = Number(rawCounts[view] ?? 0);
    counts[view] = Number.isFinite(value) && value > 0 ? Math.trunc(value) : 0;
  });
  return counts;
}

function setInboxTriggerCount(count) {
  const edgeLeftTap = document.getElementById('edge-left-tap');
  if (!(edgeLeftTap instanceof HTMLElement)) return;
  const value = Math.max(0, Number(count) || 0);
  edgeLeftTap.dataset.inboxCount = value > 0 ? String(value) : '';
  edgeLeftTap.classList.toggle('has-inbox-count', value > 0);
}

function applyItemSidebarCounts(rawCounts) {
  state.itemSidebarCounts = normalizeItemSidebarCounts(rawCounts);
  setInboxTriggerCount(state.itemSidebarCounts.inbox);
}

async function refreshItemSidebarCounts() {
  const projectID = String(state.activeProjectId || '').trim();
  if (!projectID) {
    applyItemSidebarCounts(defaultItemSidebarCounts());
    return false;
  }
  const resp = await fetch(apiURL('items/counts'), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  if (projectID !== String(state.activeProjectId || '').trim()) return false;
  applyItemSidebarCounts(payload?.counts);
  return true;
}

async function loadItemSidebarView(view = state.itemSidebarView) {
  const normalizedView = normalizeItemSidebarView(view);
  const projectID = String(state.activeProjectId || '').trim();
  state.itemSidebarView = normalizedView;
  state.itemSidebarLoading = true;
  state.itemSidebarError = '';
  if (!state.prReviewMode) {
    state.fileSidebarMode = 'items';
  }
  renderPrReviewFileList();
  if (!projectID) {
    state.itemSidebarItems = [];
    state.itemSidebarLoading = false;
    applyItemSidebarCounts(defaultItemSidebarCounts());
    renderPrReviewFileList();
    return false;
  }
  try {
    const [itemsResp, countsResp] = await Promise.all([
      fetch(apiURL(itemSidebarEndpoint(normalizedView)), { cache: 'no-store' }),
      fetch(apiURL('items/counts'), { cache: 'no-store' }),
    ]);
    if (!itemsResp.ok) {
      const detail = (await itemsResp.text()).trim() || `HTTP ${itemsResp.status}`;
      throw new Error(detail);
    }
    if (!countsResp.ok) {
      const detail = (await countsResp.text()).trim() || `HTTP ${countsResp.status}`;
      throw new Error(detail);
    }
    const [itemsPayload, countsPayload] = await Promise.all([itemsResp.json(), countsResp.json()]);
    if (projectID !== String(state.activeProjectId || '').trim()) return false;
    state.itemSidebarItems = Array.isArray(itemsPayload?.items) ? itemsPayload.items : [];
    state.itemSidebarLoading = false;
    state.itemSidebarError = '';
    applyItemSidebarCounts(countsPayload?.counts);
    renderPrReviewFileList();
    return true;
  } catch (err) {
    if (projectID !== String(state.activeProjectId || '').trim()) return false;
    state.itemSidebarItems = [];
    state.itemSidebarLoading = false;
    state.itemSidebarError = String(err?.message || err || 'item list unavailable');
    renderPrReviewFileList();
    return false;
  }
}

function sidebarTabLabel(view) {
  if (view === 'waiting') return 'Waiting';
  if (view === 'someday') return 'Someday';
  if (view === 'done') return 'Done';
  return 'Inbox';
}

function normalizeDisplayText(raw) {
  return String(raw || '')
    .trim()
    .replace(/[._-]+/g, ' ')
    .replace(/\s+/g, ' ');
}

function itemSourceLabel(item) {
  const source = normalizeDisplayText(item?.source);
  if (source) return source.toLowerCase();
  const sourceRef = String(item?.source_ref || '').trim();
  if (sourceRef.includes('#PR-')) return 'github';
  return '';
}

function itemKindLabel(item) {
  const artifactKind = String(item?.artifact_kind || '').trim().toLowerCase();
  if (artifactKind === 'idea_note') return 'idea';
  if (artifactKind === 'email') return 'email';
  if (artifactKind === 'github_pr') return 'review';
  if (artifactKind === 'github_issue') return 'task';
  if (artifactKind === 'plan_note') return 'task';
  const source = itemSourceLabel(item);
  if (source === 'github') return 'task';
  return 'task';
}

function itemIconForRow(item) {
  const artifactKind = String(item?.artifact_kind || '').trim().toLowerCase();
  const source = itemSourceLabel(item);
  if (artifactKind === 'github_pr') return { icon: 'symbol', text: 'R' };
  if (artifactKind === 'email') return { icon: 'symbol', text: '@' };
  if (artifactKind === 'idea_note') return { icon: 'symbol', text: 'I' };
  if (source === 'github') return { icon: 'symbol', text: '#' };
  return { icon: 'symbol', text: 'T' };
}

function parseSidebarTimestamp(value) {
  const text = String(value || '').trim();
  if (!text) return null;
  let normalized = text;
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(normalized)) {
    normalized = `${normalized.replace(' ', 'T')}Z`;
  }
  const parsed = Date.parse(normalized);
  return Number.isFinite(parsed) ? parsed : null;
}

function formatSidebarAge(value) {
  const parsed = parseSidebarTimestamp(value);
  if (parsed === null) return '';
  const seconds = Math.max(0, Math.floor((Date.now() - parsed) / 1000));
  if (seconds < 60) return 'now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

function buildItemSidebarSubtitle(item) {
  const parts = [];
  const artifactTitle = String(item?.artifact_title || '').trim();
  if (artifactTitle) parts.push(artifactTitle);
  const actorName = String(item?.actor_name || '').trim();
  if (actorName) parts.push(actorName);
  return parts.join(' · ');
}

function buildItemSidebarBadges(item) {
  const badges = [];
  const kind = itemKindLabel(item);
  if (kind) badges.push(kind);
  const source = itemSourceLabel(item);
  if (source) badges.push(source);
  const artifactKind = normalizeDisplayText(item?.artifact_kind).toLowerCase();
  if (artifactKind && artifactKind !== kind) badges.push(artifactKind);
  return badges.filter((badge, index, all) => badge && all.indexOf(badge) === index);
}

function renderSidebarTabs(list) {
  const tabs = document.createElement('div');
  tabs.className = 'sidebar-tabs';
  ITEM_SIDEBAR_VIEWS.forEach((view) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'sidebar-tab';
    if (state.fileSidebarMode !== 'workspace' && state.itemSidebarView === view) {
      button.classList.add('is-active');
    }
    button.textContent = sidebarTabLabel(view);
    const count = Number(state.itemSidebarCounts?.[view] || 0);
    if (count > 0) {
      const badge = document.createElement('span');
      badge.className = 'sidebar-tab-count';
      badge.textContent = String(count);
      button.appendChild(badge);
    }
    button.addEventListener('click', () => {
      state.fileSidebarMode = 'items';
      state.itemSidebarView = view;
      void loadItemSidebarView(view);
    });
    tabs.appendChild(button);
  });
  const filesButton = document.createElement('button');
  filesButton.type = 'button';
  filesButton.className = 'sidebar-tab';
  if (state.fileSidebarMode === 'workspace') {
    filesButton.classList.add('is-active');
  }
  filesButton.textContent = 'Files';
  filesButton.addEventListener('click', () => {
    state.fileSidebarMode = 'workspace';
    renderPrReviewFileList();
    if (!state.workspaceBrowserLoading && state.workspaceBrowserEntries.length === 0 && !state.workspaceBrowserError) {
      void refreshWorkspaceBrowser(false);
    }
  });
  tabs.appendChild(filesButton);
  list.appendChild(tabs);
}

function renderSidebarRow({ icon, iconText = '', label, active = false, meta = '', subtitle = '', badges = [], onClick }) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'pr-file-item';
  if (active) {
    button.classList.add('is-active');
  }

  const iconEl = document.createElement('span');
  iconEl.className = `chooser-icon icon-${icon}`;
  iconEl.textContent = String(iconText || '');

  const bodyEl = document.createElement('span');
  bodyEl.className = 'sidebar-row-main';

  const labelEl = document.createElement('span');
  labelEl.className = 'pr-file-name';
  labelEl.textContent = String(label || '');

  bodyEl.appendChild(labelEl);
  if (subtitle || badges.length > 0) {
    const secondaryEl = document.createElement('span');
    secondaryEl.className = 'sidebar-row-secondary';
    if (subtitle) {
      const subtitleEl = document.createElement('span');
      subtitleEl.className = 'sidebar-row-subtitle';
      subtitleEl.textContent = String(subtitle);
      secondaryEl.appendChild(subtitleEl);
    }
    if (badges.length > 0) {
      const badgesEl = document.createElement('span');
      badgesEl.className = 'sidebar-row-badges';
      badges.forEach((badgeText) => {
        const badgeEl = document.createElement('span');
        badgeEl.className = 'sidebar-badge';
        badgeEl.textContent = String(badgeText);
        badgesEl.appendChild(badgeEl);
      });
      secondaryEl.appendChild(badgesEl);
    }
    bodyEl.appendChild(secondaryEl);
  }

  button.appendChild(iconEl);
  button.appendChild(bodyEl);
  if (meta) {
    const metaEl = document.createElement('span');
    metaEl.className = 'pr-file-status';
    metaEl.textContent = String(meta);
    button.appendChild(metaEl);
  }
  let lastTouchAt = 0;
  let touchStartY = 0;
  button.addEventListener('touchstart', (ev) => {
    const t = ev.touches && ev.touches[0];
    if (t) touchStartY = t.clientY;
  }, { passive: true });
  button.addEventListener('touchend', (ev) => {
    const t = ev.changedTouches && ev.changedTouches[0];
    if (t && Math.abs(t.clientY - touchStartY) > 10) return;
    ev.preventDefault();
    ev.stopPropagation();
    lastTouchAt = Date.now();
    onClick(ev);
  }, { passive: false });
  button.addEventListener('click', (ev) => {
    if (Date.now() - lastTouchAt < 700) {
      ev.preventDefault();
      return;
    }
    if (Date.now() - sidebarEdgeTapAt < 600) return;
    onClick(ev);
  });
  return button;
}

function renderItemSidebarList(list) {
  if (state.itemSidebarLoading) {
    list.appendChild(renderSidebarRow({
      icon: 'symbol',
      iconText: '…',
      label: 'Loading items...',
      onClick: () => {},
    }));
    return;
  }
  if (state.itemSidebarError) {
    list.appendChild(renderSidebarRow({
      icon: 'symbol',
      iconText: '!',
      label: `Error: ${state.itemSidebarError}`,
      onClick: () => {},
    }));
    return;
  }
  const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
  if (items.length === 0) {
    list.appendChild(renderSidebarRow({
      icon: 'symbol',
      iconText: '0',
      label: `No ${sidebarTabLabel(state.itemSidebarView).toLowerCase()} items.`,
      onClick: () => {},
    }));
    return;
  }
  items.forEach((item) => {
    const icon = itemIconForRow(item);
    list.appendChild(renderSidebarRow({
      icon: icon.icon,
      iconText: icon.text,
      label: String(item?.title || 'Untitled item'),
      subtitle: buildItemSidebarSubtitle(item),
      badges: buildItemSidebarBadges(item),
      meta: formatSidebarAge(item?.updated_at || item?.created_at),
      active: Number(item?.id || 0) === Number(state.itemSidebarActiveItemID || 0),
      onClick: () => {
        state.itemSidebarActiveItemID = Number(item?.id || 0);
        renderPrReviewFileList();
      },
    }));
  });
}

function renderWorkspaceFileList(list) {
  if (state.workspaceBrowserLoading) {
    list.appendChild(renderSidebarRow({
      icon: 'folder',
      label: 'Loading...',
      onClick: () => {},
    }));
    return;
  }
  if (state.workspaceBrowserError) {
    list.appendChild(renderSidebarRow({
      icon: 'file',
      label: `Error: ${state.workspaceBrowserError}`,
      onClick: () => {},
    }));
    return;
  }
  const currentPath = normalizeWorkspaceBrowserPath(state.workspaceBrowserPath);
  const activeWorkspaceFilePath = normalizeWorkspaceBrowserPath(state.workspaceOpenFilePath);
  if (currentPath) {
    list.appendChild(renderSidebarRow({
      icon: 'parent',
      label: '..',
      onClick: () => {
        void loadWorkspaceBrowserPath(parentWorkspaceBrowserPath(currentPath));
      },
    }));
  }
  const entries = Array.isArray(state.workspaceBrowserEntries) ? state.workspaceBrowserEntries : [];
  const rows = currentPath ? entries : workspaceCompanionEntries().concat(entries);
  rows.forEach((entry) => {
    const isDir = Boolean(entry?.is_dir);
    const entryPath = normalizeWorkspaceBrowserPath(entry?.path || '');
    const entryName = String(entry?.name || entryPath || '(item)');
    list.appendChild(renderSidebarRow({
      icon: isDir ? 'folder' : 'file',
      label: entryName,
      active: !isDir && activeWorkspaceFilePath && entryPath === activeWorkspaceFilePath,
      onClick: () => {
        if (isDir) {
          void loadWorkspaceBrowserPath(entryPath);
          return;
        }
        void openWorkspaceSidebarFile(entryPath);
      },
    }));
  });
}

function resetPrReviewUi() {
  document.body.classList.remove('pr-review-mode');
  state.fileSidebarMode = 'items';
  setFileSidebarAvailability();
  renderPrReviewFileList();
}

function renderPrReviewFileList() {
  const list = document.getElementById('pr-file-list');
  if (!(list instanceof HTMLElement)) return;
  setFileSidebarAvailability();
  if (state.prReviewMode) {
    state.fileSidebarMode = 'pr';
  }
  const mode = state.fileSidebarMode === 'pr' && state.prReviewMode ? 'pr' : state.fileSidebarMode;
  list.innerHTML = '';
  if (mode === 'pr') {
    const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
    files.forEach((file, index) => {
      const statusName = String(file?.status || 'modified').toLowerCase();
      list.appendChild(renderSidebarRow({
        icon: 'file',
        label: String(file?.path || `(file ${index + 1})`),
        active: index === state.prReviewActiveIndex,
        meta: statusBadgeForDiffFile(statusName),
        onClick: () => {
          setPrReviewActiveFile(index);
          if (isMobileViewport()) {
            setPrReviewDrawerOpen(false);
            closeEdgePanels();
          }
        },
      }));
    });
    return;
  }
  renderSidebarTabs(list);
  if (mode === 'workspace') {
    renderWorkspaceFileList(list);
    return;
  }
  renderItemSidebarList(list);
}

async function loadWorkspaceBrowserPath(path = '') {
  const projectID = String(state.activeProjectId || '').trim();
  if (!projectID) {
    state.workspaceBrowserPath = '';
    state.workspaceBrowserEntries = [];
    state.workspaceBrowserLoading = false;
    state.workspaceBrowserError = '';
    renderPrReviewFileList();
    return false;
  }
  const requestedPath = normalizeWorkspaceBrowserPath(path);
  state.workspaceBrowserLoading = true;
  state.workspaceBrowserError = '';
  renderPrReviewFileList();
  try {
    const urls = [
      apiURL(`projects/${encodeURIComponent(projectID)}/files?path=${encodeURIComponent(requestedPath)}`),
    ];
    if (projectID.toLowerCase() !== 'active') {
      urls.push(apiURL(`projects/active/files?path=${encodeURIComponent(requestedPath)}`));
    }

    let payload = null;
    let lastError = '';
    for (let i = 0; i < urls.length; i += 1) {
      const resp = await fetch(urls[i], { cache: 'no-store' });
      if (resp.ok) {
        payload = await resp.json();
        break;
      }
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      if (resp.status !== 404) {
        throw new Error(detail);
      }
      lastError = detail;
    }
    if (!payload) {
      throw new Error(lastError || 'file list unavailable');
    }
    if (projectID !== String(state.activeProjectId || '')) return false;
    state.workspaceBrowserPath = normalizeWorkspaceBrowserPath(payload?.path || requestedPath);
    const entriesRaw = Array.isArray(payload?.entries) ? payload.entries : [];
    state.workspaceBrowserEntries = entriesRaw.map((entry) => ({
      name: String(entry?.name || ''),
      path: normalizeWorkspaceBrowserPath(entry?.path || ''),
      is_dir: Boolean(entry?.is_dir),
    }));
    state.workspaceBrowserLoading = false;
    state.workspaceBrowserError = '';
    renderPrReviewFileList();
    return true;
  } catch (err) {
    if (projectID !== String(state.activeProjectId || '')) return false;
    state.workspaceBrowserLoading = false;
    state.workspaceBrowserError = String(err?.message || err || 'file list unavailable');
    state.workspaceBrowserEntries = [];
    renderPrReviewFileList();
    return false;
  }
}

async function openWorkspaceSidebarFile(path) {
  const filePath = normalizeWorkspaceBrowserPath(path);
  if (!filePath) return false;
  state.fileSidebarMode = 'workspace';
  clearWelcomeSurface();
  const companionViewKind = companionViewKindForPath(filePath);
  if (companionViewKind) {
    return openCompanionWorkspaceView(companionViewKind, filePath);
  }
  const kind = sidebarFileKindForPath(filePath);
  if (kind === 'image_artifact') {
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'image_artifact',
      event_id: `workspace-file-${Date.now()}`,
      title: filePath,
      path: filePath,
    });
    showCanvasColumn('canvas-image');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  }
  if (kind === 'pdf_artifact') {
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'pdf_artifact',
      event_id: `workspace-file-${Date.now()}`,
      title: filePath,
      path: filePath,
    });
    showCanvasColumn('canvas-pdf');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  }

  const sid = String(state.sessionId || 'local');
  try {
    const resp = await fetch(apiURL(`files/${encodeURIComponent(sid)}/${encodeURIComponent(filePath)}`), { cache: 'no-store' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const contentType = String(resp.headers.get('content-type') || '').toLowerCase();
    if (contentType.startsWith('image/')) {
      state.workspaceOpenFilePath = filePath;
      renderPrReviewFileList();
      renderCanvas({
        kind: 'image_artifact',
        event_id: `workspace-file-${Date.now()}`,
        title: filePath,
        path: filePath,
      });
      showCanvasColumn('canvas-image');
      if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
      return true;
    }
    if (contentType.includes('application/pdf')) {
      state.workspaceOpenFilePath = filePath;
      renderPrReviewFileList();
      renderCanvas({
        kind: 'pdf_artifact',
        event_id: `workspace-file-${Date.now()}`,
        title: filePath,
        path: filePath,
      });
      showCanvasColumn('canvas-pdf');
      if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
      return true;
    }
    const text = await resp.text();
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'text_artifact',
      event_id: `workspace-file-${Date.now()}`,
      title: filePath,
      text,
    });
    showCanvasColumn('canvas-text');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  } catch (err) {
    showStatus(`open failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

async function openCompanionWorkspaceView(viewKind, filePath) {
  const projectID = String(state.activeProjectId || '').trim();
  if (!projectID) return false;
  const titles = {
    transcript: MEETING_TRANSCRIPT_LABEL,
    summary: MEETING_SUMMARY_LABEL,
    references: MEETING_REFERENCES_LABEL,
  };
  const endpoint = viewKind === 'transcript' || viewKind === 'summary' || viewKind === 'references'
    ? viewKind
    : '';
  if (!endpoint) return false;
  try {
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(projectID)}/${endpoint}?format=md`), { cache: 'no-store' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const text = await resp.text();
    state.workspaceOpenFilePath = filePath;
    renderPrReviewFileList();
    renderCanvas({
      kind: 'text_artifact',
      event_id: `workspace-companion-${viewKind}-${Date.now()}`,
      title: titles[viewKind] || filePath,
      text,
    });
    showCanvasColumn('canvas-text');
    if (isMobileViewport()) { setPrReviewDrawerOpen(false); closeEdgePanels(); }
    return true;
  } catch (err) {
    appendPlainMessage('system', `${titles[viewKind] || 'Meeting view'} failed: ${String(err?.message || err)}`);
    return false;
  }
}

async function refreshWorkspaceBrowser(resetPath = false) {
  const nextPath = resetPath ? '' : state.workspaceBrowserPath;
  return loadWorkspaceBrowserPath(nextPath);
}

function stepWorkspaceFile(delta) {
  if (state.prReviewMode) return false;
  if (state.workspaceStepInFlight) return false;
  const shift = Number(delta);
  if (!Number.isFinite(shift) || shift === 0) return false;
  const files = workspaceNavigableFilePaths();
  if (files.length <= 1) return false;
  const currentFile = resolveWorkspaceSteppingCurrentFile();
  if (!currentFile) return false;
  const currentIndex = files.indexOf(currentFile);
  if (currentIndex < 0) return false;
  const nextIndex = ((currentIndex + Math.trunc(shift)) % files.length + files.length) % files.length;
  if (nextIndex === currentIndex) return false;
  const nextFile = files[nextIndex];
  if (!nextFile) return false;
  state.workspaceStepInFlight = true;
  void openWorkspaceSidebarFile(nextFile).finally(() => {
    state.workspaceStepInFlight = false;
  });
  return true;
}

function renderActivePrReviewFile() {
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (!state.prReviewMode || files.length === 0) return false;
  if (state.prReviewActiveIndex < 0 || state.prReviewActiveIndex >= files.length) {
    state.prReviewActiveIndex = 0;
  }
  const file = files[state.prReviewActiveIndex];
  if (!file) return false;
  clearWelcomeSurface();
  renderCanvas({
    kind: 'text_artifact',
    event_id: `pr-review-${Date.now()}-${state.prReviewActiveIndex}`,
    title: String(file.path || ''),
    text: String(file.diff || ''),
  });
  showCanvasColumn('canvas-text');
  renderPrReviewFileList();
  return true;
}

function setPrReviewActiveFile(index) {
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (!state.prReviewMode || files.length === 0) return false;
  const total = files.length;
  let next = Number(index);
  if (!Number.isFinite(next)) return false;
  next = ((Math.trunc(next) % total) + total) % total;
  if (next === state.prReviewActiveIndex) {
    renderPrReviewFileList();
    return false;
  }
  state.prReviewActiveIndex = next;
  return renderActivePrReviewFile();
}

function stepPrReviewFile(delta) {
  if (!state.prReviewMode) return false;
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (files.length <= 1) return false;
  const shift = Number(delta);
  if (!Number.isFinite(shift) || shift === 0) return false;
  return setPrReviewActiveFile(state.prReviewActiveIndex + shift);
}

function stepCanvasFile(delta) {
  if (state.prReviewMode) {
    return stepPrReviewFile(delta);
  }
  return stepWorkspaceFile(delta);
}

function exitPrReviewMode() {
  if (!state.prReviewMode && (!state.prReviewFiles || state.prReviewFiles.length === 0)) {
    return;
  }
  state.prReviewMode = false;
  state.prReviewFiles = [];
  state.prReviewActiveIndex = 0;
  state.prReviewTitle = '';
  state.prReviewPRNumber = '';
  resetPrReviewUi();
}

function maybeEnterPrReviewModeFromTextArtifact(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind !== 'text_artifact' && kind !== 'text') return false;
  const title = String(payload?.title || '').trim();
  const text = String(payload?.text || '');
  if (!text.trim()) return false;
  const titleHint = /\.diff$|\.patch$/i.test(title);
  const hasDiffHeader = text.includes('\ndiff --git ') || text.startsWith('diff --git ');
  if (!titleHint && !hasDiffHeader) return false;
  const files = parseUnifiedDiffFiles(text);
  if (files.length === 0) return false;
  if (!titleHint && files.length < 2) return false;

  state.prReviewMode = true;
  state.prReviewFiles = files;
  state.prReviewActiveIndex = 0;
  state.prReviewTitle = title;
  const numberMatch = /(?:^|[^0-9])pr[-_]?(\d+)(?:[^0-9]|$)/i.exec(title);
  state.prReviewPRNumber = numberMatch ? String(numberMatch[1]) : '';
  document.body.classList.add('pr-review-mode');
  setPrReviewDrawerOpen(false);
  renderPrReviewFileList();
  return renderActivePrReviewFile();
}

function isLikelyPrReviewArtifact(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind !== 'text_artifact' && kind !== 'text') return false;
  const title = String(payload?.title || '').trim().toLowerCase();
  if (!title) return false;
  return /(?:^|\/)\.tabura\/artifacts\/pr\/pr-\d+\.(?:diff|patch)$/.test(title)
    || /(?:^|\/)artifacts\/pr\/pr-\d+\.(?:diff|patch)$/.test(title);
}

function trackAssistantTurnStarted(turnID) {
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

function trackAssistantTurnFinished(turnID) {
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

function takeAnyPendingRow() {
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
  const progress = document.createElement('div');
  progress.className = 'chat-bubble-progress';
  const body = document.createElement('div');
  body.className = 'chat-bubble-body';
  const { text: markdownBody, stash: mathSegments } = extractMathSegments(markdownText);
  const rendered = marked.parse(markdownBody || '');
  body.innerHTML = restoreMathSegments(sanitizeHtml(rendered), mathSegments);
  bubble.appendChild(progress);
  bubble.appendChild(body);
  row.appendChild(meta);
  row.appendChild(bubble);
  host.appendChild(row);
  syncChatScroll(host);
  void typesetMath(body).finally(() => syncChatScroll(host));
  return row;
}

function assistantRowBodyEl(row) {
  if (!(row instanceof HTMLElement)) return null;
  const body = row.querySelector('.chat-bubble-body');
  if (body instanceof HTMLElement) return body;
  const bubble = row.querySelector('.chat-bubble');
  return bubble instanceof HTMLElement ? bubble : null;
}

function ensureAssistantProgressEl(row) {
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

function appendAssistantProgressLine(row, text) {
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

function findAssistantRowForTurn(turnID) {
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

function humanizeItemTypeLabel(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  return value
    .replace(/[._-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

function formatItemCompletedLabel(payload) {
  const label = humanizeItemTypeLabel(payload?.item_type);
  const detail = String(payload?.detail || '').trim();
  if (!label && !detail) return '';
  if (!label) return detail;
  if (!detail) return label;
  return `${label}: ${detail}`;
}

function appendAssistantProgressForTurn(turnID, text) {
  const line = String(text || '').trim();
  if (!line) return;
  const existing = findAssistantRowForTurn(turnID);
  const row = existing || ensurePendingForTurn(turnID);
  if (!(row instanceof HTMLElement)) return;
  appendAssistantProgressLine(row, line);
}

function updateAssistantRow(row, markdownText, pending = true) {
  if (!row) return;
  const host = chatHistoryEl();
  row.classList.toggle('is-pending', pending);
  const body = assistantRowBodyEl(row);
  if (!(body instanceof HTMLElement)) return;
  const { text: markdownBody, stash: mathSegments } = extractMathSegments(markdownText);
  const rendered = marked.parse(markdownBody || '');
  body.innerHTML = restoreMathSegments(sanitizeHtml(rendered), mathSegments);
  syncChatScroll(host);
  void typesetMath(body).finally(() => syncChatScroll(host));
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

function clearChatHistory() {
  const host = chatHistoryEl();
  if (host) host.innerHTML = '';
}

function clearWelcomeSurface() {
  state.welcomeSurface = null;
  const canvasText = document.getElementById('canvas-text');
  if (canvasText instanceof HTMLElement) {
    canvasText.classList.remove('welcome-surface');
  }
}

function activeWelcomeProjectID() {
  if (state.welcomeSurface && typeof state.welcomeSurface === 'object') {
    return String(state.welcomeSurface.project_id || '').trim();
  }
  return '';
}

async function handleWelcomeAction(action) {
  const type = String(action?.type || '').trim();
  if (!type) return;
  if (type === 'switch_project') {
    const projectID = String(action?.project_id || '').trim();
    if (!projectID || projectID === 'hub') {
      const hub = hubProject();
      if (hub?.id) {
        await switchProject(hub.id);
      }
      return;
    }
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
  if (type === 'set_input_mode') {
    const next = normalizeInputMode(action?.input_mode || 'pen');
    await updateRuntimePreferences({ input_mode: next });
    return;
  }
  if (type === 'set_startup_behavior') {
    await updateRuntimePreferences({ startup_behavior: 'hub_first' });
  }
}

function renderWelcomeSurface(payload) {
  const canvasText = document.getElementById('canvas-text');
  if (!(canvasText instanceof HTMLElement)) return;
  const sections = Array.isArray(payload?.sections) ? payload.sections : [];
  const title = String(payload?.title || 'Welcome').trim() || 'Welcome';
  const subtitle = isHubActive()
    ? 'Choose a project or change a global runtime preference.'
    : 'Pick up a recent file, open docs, or switch modes before asking.';
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

async function fetchProjectWelcome(projectID = 'active') {
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(projectID)}/welcome`), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  return resp.json();
}

async function showWelcomeForActiveProject(force = false) {
  void force;
  clearWelcomeSurface();
}

function shouldUseBottomComposer() {
  return window.matchMedia('(max-width: 767px)').matches;
}

function openComposerAt(x, y, anchor = null, initialText = '') {
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

function activeArtifactKindForInk() {
  const activePane = document.querySelector('#canvas-viewport .canvas-pane.is-active');
  if (!(activePane instanceof HTMLElement)) return 'text';
  if (activePane.id === 'canvas-pdf') return 'pdf';
  if (activePane.id === 'canvas-image') return 'image';
  return 'text';
}

function resetInkDraftState() {
  state.inkDraft.activePointerId = null;
  state.inkDraft.activePointerType = '';
  state.inkDraft.activePath = null;
}

function inkLayerEl() {
  const node = document.getElementById('ink-layer');
  return node instanceof SVGSVGElement ? node : null;
}

function renderInkControls() {
  const controls = document.getElementById('ink-controls');
  if (!(controls instanceof HTMLElement)) return;
  const visible = isPenInputMode() && state.inkDraft.dirty;
  controls.style.display = visible ? '' : 'none';
  const submit = document.getElementById('ink-submit');
  const clear = document.getElementById('ink-clear');
  if (submit instanceof HTMLButtonElement) submit.disabled = state.inkSubmitInFlight;
  if (clear instanceof HTMLButtonElement) clear.disabled = state.inkSubmitInFlight;
}

function syncInputModeBodyState() {
  document.body.classList.toggle('pen-input-mode', isPenInputMode());
}

function setPenInkingState(active) {
  document.body.classList.toggle('pen-inking', Boolean(active));
}

function clearInkDraft() {
  const layer = inkLayerEl();
  if (layer) layer.innerHTML = '';
  state.inkDraft.strokes = [];
  state.inkDraft.dirty = false;
  resetInkDraftState();
  setPenInkingState(false);
  renderInkControls();
}

function syncInkLayerSize() {
  const layer = inkLayerEl();
  const viewport = document.getElementById('canvas-viewport');
  if (!(layer instanceof SVGSVGElement) || !(viewport instanceof HTMLElement)) return;
  const rect = viewport.getBoundingClientRect();
  const width = Math.max(1, Math.round(rect.width));
  const height = Math.max(1, Math.round(rect.height));
  layer.setAttribute('viewBox', `0 0 ${width} ${height}`);
  layer.setAttribute('width', `${width}`);
  layer.setAttribute('height', `${height}`);
}

function pointForViewportEvent(clientX, clientY) {
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

function appendInkPointToPath(pathEl, stroke) {
  if (!(pathEl instanceof SVGPathElement) || !stroke || !Array.isArray(stroke.points) || stroke.points.length === 0) return;
  const d = stroke.points.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x.toFixed(2)} ${point.y.toFixed(2)}`).join(' ');
  pathEl.setAttribute('d', d);
}

function beginInkStroke(pointerEvent) {
  const layer = inkLayerEl();
  if (!(layer instanceof SVGSVGElement)) return false;
  syncInkLayerSize();
  const point = pointForViewportEvent(pointerEvent.clientX, pointerEvent.clientY);
  const stroke = {
    pointer_type: String(pointerEvent.pointerType || 'pen').trim().toLowerCase() || 'pen',
    width: Math.max(1.5, Number(pointerEvent.pressure) > 0 ? 1.8 + Number(pointerEvent.pressure) * 2.8 : 2.4),
    points: [{
      x: point.x,
      y: point.y,
      pressure: Number(pointerEvent.pressure) || 0,
    }],
  };
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('stroke-width', stroke.width.toFixed(2));
  appendInkPointToPath(path, stroke);
  layer.appendChild(path);
  state.inkDraft.strokes.push(stroke);
  state.inkDraft.activePointerId = pointerEvent.pointerId;
  state.inkDraft.activePointerType = stroke.pointer_type;
  state.inkDraft.activePath = path;
  state.inkDraft.dirty = true;
  renderInkControls();
  return true;
}

function extendInkStroke(pointerEvent) {
  if (state.inkDraft.activePointerId !== pointerEvent.pointerId) return false;
  const stroke = state.inkDraft.strokes[state.inkDraft.strokes.length - 1];
  const path = state.inkDraft.activePath;
  if (!stroke || !(path instanceof SVGPathElement)) return false;
  const point = pointForViewportEvent(pointerEvent.clientX, pointerEvent.clientY);
  stroke.points.push({
    x: point.x,
    y: point.y,
    pressure: Number(pointerEvent.pressure) || 0,
  });
  appendInkPointToPath(path, stroke);
  return true;
}

function buildInkSVGMarkup() {
  const layer = inkLayerEl();
  if (!(layer instanceof SVGSVGElement)) return '';
  syncInkLayerSize();
  const viewBox = layer.getAttribute('viewBox') || '0 0 1 1';
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}">${layer.innerHTML}</svg>`;
}

function buildInkPNGBase64() {
  syncInkLayerSize();
  const layer = inkLayerEl();
  if (!(layer instanceof SVGSVGElement)) return '';
  const viewBox = String(layer.getAttribute('viewBox') || '').trim();
  const parts = viewBox.split(/\s+/).map((part) => Number(part));
  const width = Math.max(1, Math.round(parts[2] || Number(layer.getAttribute('width')) || 1));
  const height = Math.max(1, Math.round(parts[3] || Number(layer.getAttribute('height')) || 1));
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.strokeStyle = '#111827';
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

async function submitInkDraft() {
  if (state.inkSubmitInFlight || state.inkDraft.strokes.length === 0) return false;
  const project = activeProject();
  if (!project?.id) return false;
  const wasBlankCanvas = !state.hasArtifact;
  state.inkSubmitInFlight = true;
  renderInkControls();
  try {
    const payload = {
      project_id: project.id,
      artifact_kind: activeArtifactKindForInk(),
      artifact_title: String(getActiveArtifactTitle() || ''),
      artifact_path: String(state.workspaceOpenFilePath || ''),
      strokes: state.inkDraft.strokes.map((stroke) => ({
        pointer_type: stroke.pointer_type,
        width: stroke.width,
        points: stroke.points.map((point) => ({
          x: point.x,
          y: point.y,
          pressure: point.pressure,
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

async function fetchProjects() {
  const resp = await fetch(apiURL('projects'), { cache: 'no-store' });
  if (!resp.ok) throw new Error(`projects list failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const projects = Array.isArray(payload?.projects) ? payload.projects : [];
  state.projects = projects.map((project) => ({
    ...project,
    id: String(project?.id || ''),
    chat_mode: String(project?.chat_mode || 'chat'),
    chat_model_reasoning_effort: String(project?.chat_model_reasoning_effort || '').trim().toLowerCase(),
    run_state: normalizeProjectRunState(project?.run_state),
    unread: Boolean(project?.unread),
    review_pending: Boolean(project?.review_pending),
  })).filter((project) => project.id);
  state.defaultProjectId = String(payload?.default_project_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_project_id || '').trim();
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
}

function normalizeProjectRunState(runState) {
  const activeTurns = Math.max(0, Number(runState?.active_turns || 0) || 0);
  const queuedTurns = Math.max(0, Number(runState?.queued_turns || 0) || 0);
  let status = String(runState?.status || '').trim().toLowerCase();
  if (status !== 'running' && status !== 'queued' && status !== 'idle') {
    status = activeTurns > 0 ? 'running' : (queuedTurns > 0 ? 'queued' : 'idle');
  }
  return {
    active_turns: activeTurns,
    queued_turns: queuedTurns,
    is_working: Boolean(runState?.is_working) || activeTurns > 0 || queuedTurns > 0,
    status,
    active_turn_id: String(runState?.active_turn_id || '').trim(),
  };
}

function projectRunStateSummary(project) {
  const runState = normalizeProjectRunState(project?.run_state);
  if (runState.status === 'running') {
    return `${runState.active_turns} active, ${runState.queued_turns} queued`;
  }
  if (runState.status === 'queued') {
    return `${runState.queued_turns} queued`;
  }
  return 'idle';
}

function upsertProject(project) {
  if (!project || !project.id) return;
  project.chat_mode = String(project.chat_mode || 'chat');
  if (project.chat_model_reasoning_effort !== undefined) {
    project.chat_model_reasoning_effort = String(project.chat_model_reasoning_effort || '').trim().toLowerCase();
  }
  project.run_state = normalizeProjectRunState(project.run_state);
  project.unread = Boolean(project.unread);
  project.review_pending = Boolean(project.review_pending);
  const index = state.projects.findIndex((item) => item.id === project.id);
  if (index >= 0) {
    state.projects[index] = project;
  } else {
    state.projects.push(project);
  }
  renderEdgeTopModelButtons();
}

async function refreshCompanionState(projectID = state.activeProjectId) {
  const project = state.projects.find((item) => item.id === String(projectID || '').trim()) || null;
  if (!project || isHubProject(project)) {
    resetCompanionState();
    return null;
  }
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(project.id)}/companion/state`), { cache: 'no-store' });
  if (!resp.ok) {
    resetCompanionState();
    throw new Error(`companion state failed: HTTP ${resp.status}`);
  }
  const payload = await resp.json();
  applyCompanionState(payload);
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  return payload;
}

async function updateCompanionConfig(patch) {
  const project = activeProject();
  if (!project || !project.id || isHubProject(project)) return null;
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(project.id)}/companion/config`), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch || {}),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  applyCompanionState({
    project_key: activeProjectKey(),
    companion_enabled: payload?.companion_enabled,
    idle_surface: payload?.idle_surface,
    state: state.companionRuntimeState,
    reason: state.companionRuntimeReason,
  });
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  return payload;
}

async function toggleCompanionIdleSurfacePreference() {
  const nextSurface = state.companionIdleSurface === COMPANION_IDLE_SURFACES.BLACK
    ? COMPANION_IDLE_SURFACES.ROBOT
    : COMPANION_IDLE_SURFACES.BLACK;
  try {
    await updateCompanionConfig({ idle_surface: nextSurface });
    showStatus(nextSurface === COMPANION_IDLE_SURFACES.BLACK ? 'black mode on' : 'black mode off');
  } catch (err) {
    const message = String(err?.message || err || 'idle surface update failed');
    appendPlainMessage('system', `Idle surface update failed: ${message}`);
    showStatus(`idle surface failed: ${message}`);
  }
}

async function activateLiveSession(mode) {
  const normalized = String(mode || '').trim().toLowerCase();
  if (normalized !== LIVE_SESSION_MODE_DIALOGUE && normalized !== LIVE_SESSION_MODE_MEETING) return false;
  if (!activeProject() || isHubActive()) return false;
  const wasMeeting = isMeetingLiveSession();
  if (state.liveSessionActive) {
    stopLiveSession();
    applyLiveSessionStateSnapshot();
  }
  if (wasMeeting && normalized !== LIVE_SESSION_MODE_MEETING) {
    await updateCompanionConfig({ companion_enabled: false }).catch(() => {});
  }
  if (normalized === LIVE_SESSION_MODE_MEETING) {
    await updateCompanionConfig({ companion_enabled: true });
    try {
      const started = await startLiveSession(LIVE_SESSION_MODE_MEETING, state.chatWs);
      applyLiveSessionStateSnapshot();
      return started;
    } catch (err) {
      await updateCompanionConfig({ companion_enabled: false }).catch(() => {});
      applyLiveSessionStateSnapshot();
      throw err;
    }
  }
  const started = await startLiveSession(LIVE_SESSION_MODE_DIALOGUE, state.chatWs);
  applyLiveSessionStateSnapshot();
  return started;
}

async function deactivateLiveSession(options = {}) {
  const silent = Boolean(options?.silent);
  const disableMeetingConfig = Boolean(options?.disableMeetingConfig);
  const wasMeeting = isMeetingLiveSession();
  stopLiveSession();
  applyLiveSessionStateSnapshot();
  if (disableMeetingConfig && wasMeeting) {
    await updateCompanionConfig({ companion_enabled: false }).catch(() => {});
  }
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
  if (!silent) {
    showStatus('live off');
  }
}

function resolveInitialProjectID() {
  const reloadProjectID = String(pendingRuntimeReloadContext?.activeProjectId || '').trim();
  if (reloadProjectID && state.projects.some((project) => project.id === reloadProjectID)) {
    return reloadProjectID;
  }
  if (state.startupBehavior === 'hub_first') {
    const hub = hubProject();
    if (hub?.id) return hub.id;
  }
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
    const runState = normalizeProjectRunState(project.run_state);
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-project-btn';
    if (isHubProject(project)) {
      button.classList.add('edge-hub-btn');
    }
    if (project.id === state.activeProjectId) {
      button.classList.add('is-active');
    }
    if (runState.is_working) {
      button.classList.add('is-working');
    }
    if (project.unread) {
      button.classList.add('is-unread');
    }
    if (runState.status === 'running') {
      button.classList.add('is-running');
    }
    if (runState.status === 'queued') {
      button.classList.add('is-queued');
    }
    button.dataset.runState = runState.status;
    button.textContent = String(project.name || project.id || 'Project');
    const summary = projectRunStateSummary(project);
    const rootPath = String(project.root_path || '').trim();
    button.title = rootPath ? `${summary} | ${rootPath}` : summary;
    button.setAttribute('aria-label', `${String(project.name || project.id || 'Project')}: ${summary}`);
    button.addEventListener('click', () => {
      if (isHubProject(project)) {
        void switchToHub();
        return;
      }
      if (project.id === state.activeProjectId) return;
      void switchProject(project.id);
    });
    host.appendChild(button);
  }
}

function renderEdgeTopModelButtons() {
  const host = document.getElementById('edge-top-models');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  const project = activeProject();
  const hubActive = isHubActive();
  const selectedAlias = activeProjectChatModelAlias();
  const selectedEffort = activeProjectChatModelReasoningEffort();
  const effortOptions = reasoningEffortOptionsForAlias(selectedAlias);
  for (const alias of PROJECT_CHAT_MODEL_ALIASES) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'edge-project-btn edge-model-btn';
    button.textContent = alias;
    if (alias === selectedAlias) {
      button.classList.add('is-active');
    }
    button.disabled = !project || hubActive || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    button.addEventListener('click', () => {
      void switchProjectChatModel(alias);
    });
    host.appendChild(button);
  }

  const effortWrap = document.createElement('div');
  effortWrap.className = 'edge-model-effort-wrap';
  const effortSelect = document.createElement('select');
  effortSelect.className = 'edge-model-select edge-reasoning-effort-select';
  effortSelect.setAttribute('aria-label', 'Reasoning effort');
  for (const effort of effortOptions) {
    const option = document.createElement('option');
    option.value = effort;
    option.textContent = effort === 'xhigh' || effort === 'extra_high' ? 'xhigh' : effort.replace(/_/g, ' ');
    effortSelect.appendChild(option);
  }
  effortSelect.value = effortOptions.includes(selectedEffort) ? selectedEffort : (effortOptions[0] || '');
  effortSelect.disabled = !project || hubActive || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  effortSelect.addEventListener('change', () => {
    const nextEffort = normalizeProjectChatModelReasoningEffort(effortSelect.value, selectedAlias);
    void switchProjectChatModel(selectedAlias, nextEffort);
  });
  effortWrap.appendChild(effortSelect);
  host.appendChild(effortWrap);

  const liveLabel = document.createElement('span');
  liveLabel.className = 'edge-project-btn edge-model-btn edge-live-label';
  liveLabel.textContent = 'Live';
  host.appendChild(liveLabel);

  const liveDisabled = !project || hubActive || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  if (state.liveSessionActive) {
    const liveStatus = document.createElement('span');
    liveStatus.className = 'edge-project-btn edge-model-btn edge-live-status';
    liveStatus.textContent = liveSessionStatusSummary();
    host.appendChild(liveStatus);

    if (state.hotwordEnabled) {
      const hotwordBadge = document.createElement('span');
      hotwordBadge.className = 'edge-project-btn edge-model-btn edge-live-hotword';
      hotwordBadge.textContent = state.liveSessionHotword || LIVE_SESSION_HOTWORD_DEFAULT;
      host.appendChild(hotwordBadge);
    }

    const stopButton = document.createElement('button');
    stopButton.type = 'button';
    stopButton.className = 'edge-project-btn edge-model-btn edge-live-stop-btn';
    stopButton.textContent = 'Stop';
    stopButton.disabled = liveDisabled;
    stopButton.addEventListener('click', () => {
      void deactivateLiveSession({ disableMeetingConfig: true });
    });
    host.appendChild(stopButton);
  } else {
    const dialogueButton = document.createElement('button');
    dialogueButton.type = 'button';
    dialogueButton.className = 'edge-project-btn edge-model-btn edge-live-dialogue-btn';
    dialogueButton.textContent = 'Dialogue';
    dialogueButton.disabled = liveDisabled || !ttsEnabled;
    dialogueButton.addEventListener('click', () => {
      void activateLiveSession(LIVE_SESSION_MODE_DIALOGUE)
        .then((started) => {
          renderEdgeTopModelButtons();
          updateAssistantActivityIndicator();
          if (started) {
            showStatus('live dialogue on');
          }
        })
        .catch((err) => {
          const message = String(err?.message || err || 'live dialogue failed');
          showStatus(`live dialogue failed: ${message}`);
        });
    });
    host.appendChild(dialogueButton);

    const meetingButton = document.createElement('button');
    meetingButton.type = 'button';
    meetingButton.className = 'edge-project-btn edge-model-btn edge-live-meeting-btn';
    meetingButton.textContent = 'Meeting';
    meetingButton.disabled = liveDisabled;
    meetingButton.addEventListener('click', () => {
      void activateLiveSession(LIVE_SESSION_MODE_MEETING)
        .then((started) => {
          renderEdgeTopModelButtons();
          updateAssistantActivityIndicator();
          if (started) {
            showStatus('live meeting on');
          }
        })
        .catch((err) => {
          const message = String(err?.message || err || 'live meeting failed');
          appendPlainMessage('system', `Live meeting failed: ${message}`);
          showStatus(`live meeting failed: ${message}`);
        });
    });
    host.appendChild(meetingButton);
  }

  const yoloButton = document.createElement('button');
  yoloButton.type = 'button';
  yoloButton.className = 'edge-project-btn edge-model-btn edge-yolo-btn';
  yoloButton.textContent = 'yolo';
  yoloButton.setAttribute('aria-pressed', state.yoloMode ? 'true' : 'false');
  if (state.yoloMode) {
    yoloButton.classList.add('is-active');
  }
  yoloButton.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  yoloButton.addEventListener('click', () => {
    toggleYoloMode();
  });
  host.appendChild(yoloButton);

  const silentButton = document.createElement('button');
  silentButton.type = 'button';
  silentButton.className = 'edge-project-btn edge-model-btn edge-silent-btn';
  silentButton.textContent = 'silent';
  silentButton.setAttribute('aria-pressed', state.ttsSilent ? 'true' : 'false');
  if (state.ttsSilent) {
    silentButton.classList.add('is-active');
  }
  silentButton.disabled = !ttsEnabled || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  silentButton.addEventListener('click', () => {
    toggleTTSSilentMode();
  });
  host.appendChild(silentButton);

  const blackButton = document.createElement('button');
  blackButton.type = 'button';
  blackButton.className = 'edge-project-btn edge-model-btn edge-companion-surface-btn';
  blackButton.textContent = 'black';
  blackButton.setAttribute('aria-pressed', state.companionIdleSurface === COMPANION_IDLE_SURFACES.BLACK ? 'true' : 'false');
  if (state.companionIdleSurface === COMPANION_IDLE_SURFACES.BLACK) {
    blackButton.classList.add('is-active');
  }
  blackButton.disabled = !project || hubActive || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  blackButton.addEventListener('click', () => {
    void toggleCompanionIdleSurfacePreference();
  });
  host.appendChild(blackButton);

  const temporarySourceProjectID = project && !isHubProject(project) ? String(project.id || '').trim() : '';
  const temporaryButtons = isTemporaryProjectKind(project?.kind)
    ? [
        {
          className: 'edge-temp-persist-btn',
          label: 'keep',
          onClick: () => { void persistTemporaryProject(String(project?.id || '').trim()); },
        },
        {
          className: 'edge-temp-discard-btn',
          label: 'discard',
          onClick: () => { void discardTemporaryProject(String(project?.id || '').trim()); },
        },
      ]
    : [
        {
          className: 'edge-temp-meeting-btn',
          label: 'meeting',
          onClick: () => { void createTemporaryProject('meeting', temporarySourceProjectID); },
        },
        {
          className: 'edge-temp-task-btn',
          label: 'task',
          onClick: () => { void createTemporaryProject('task', temporarySourceProjectID); },
        },
      ];
  for (const action of temporaryButtons) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `edge-project-btn edge-model-btn ${action.className}`;
    button.textContent = action.label;
    button.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    button.addEventListener('click', action.onClick);
    host.appendChild(button);
  }
  renderToolPalette();
}

async function switchProjectChatModel(modelAlias, reasoningEffort = '') {
  const project = activeProject();
  if (!project || !project.id) return;
  const nextAlias = normalizeProjectChatModelAlias(modelAlias);
  if (!nextAlias) return;
  const currentAlias = activeProjectChatModelAlias();
  const rawEffort = String(reasoningEffort || '').trim().toLowerCase();
  const includeEffort = rawEffort !== '';
  const nextEffort = includeEffort ? normalizeProjectChatModelReasoningEffort(rawEffort, nextAlias) : '';
  const currentEffort = activeProjectChatModelReasoningEffort();
  if (nextAlias === currentAlias && (!includeEffort || nextEffort === currentEffort)) return;
  if (state.projectModelSwitchInFlight || state.projectSwitchInFlight) return;

  state.projectModelSwitchInFlight = true;
  renderEdgeTopModelButtons();
  showStatus(`switching model to ${nextAlias}...`);
  try {
    const payload = { model: nextAlias };
    if (includeEffort) {
      payload.reasoning_effort = nextEffort;
    }
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(project.id)}/chat-model`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const responsePayload = await resp.json();
    const updatedProject = responsePayload?.project || {};
    upsertProject(updatedProject);
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus('ready');
  } catch (err) {
    const message = String(err?.message || err || 'model switch failed');
    appendPlainMessage('system', `Model switch failed: ${message}`);
    showStatus(`model switch failed: ${message}`);
  } finally {
    state.projectModelSwitchInFlight = false;
    renderEdgeTopModelButtons();
  }
}

async function createTemporaryProject(kind, sourceProjectID = '') {
  const projectKind = String(kind || '').trim().toLowerCase();
  if (!isTemporaryProjectKind(projectKind)) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus(`starting ${projectKind}...`);
  const payload = {
    kind: projectKind,
    activate: true,
  };
  const sourceID = String(sourceProjectID || '').trim();
  if (sourceID) {
    payload.source_project_id = sourceID;
  }
  try {
    const resp = await fetch(apiURL('projects'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const responsePayload = await resp.json();
    const project = responsePayload?.project || {};
    const projectID = String(project?.id || '').trim();
    await fetchProjects();
    if (projectID) {
      await switchProject(projectID);
      return;
    }
    showStatus(`${projectKind} ready`);
  } catch (err) {
    const message = String(err?.message || err || `${projectKind} start failed`);
    appendPlainMessage('system', `${projectKind} start failed: ${message}`);
    showStatus(`${projectKind} start failed: ${message}`);
  }
}

async function persistTemporaryProject(projectID) {
  const id = String(projectID || '').trim();
  if (!id) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus('saving session...');
  try {
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(id)}/persist`), { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    if (payload?.project) {
      upsertProject(payload.project);
    }
    await fetchProjects();
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus('session saved');
  } catch (err) {
    const message = String(err?.message || err || 'session save failed');
    appendPlainMessage('system', `Session save failed: ${message}`);
    showStatus(`session save failed: ${message}`);
  }
}

async function discardTemporaryProject(projectID) {
  const id = String(projectID || '').trim();
  if (!id) return;
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  showStatus('discarding session...');
  try {
    const resp = await fetch(apiURL(`projects/${encodeURIComponent(id)}/discard`), { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      throw new Error(detail);
    }
    const payload = await resp.json();
    const nextProjectID = String(payload?.active_project_id || '').trim() || state.defaultProjectId || hubProject()?.id || '';
    await fetchProjects();
    if (nextProjectID) {
      await switchProject(nextProjectID);
      return;
    }
    renderEdgeTopProjects();
    renderEdgeTopModelButtons();
    showStatus('session discarded');
  } catch (err) {
    const message = String(err?.message || err || 'session discard failed');
    appendPlainMessage('system', `Session discard failed: ${message}`);
    showStatus(`session discard failed: ${message}`);
  }
}

async function activateProject(projectID) {
  const resp = await fetch(apiURL(`projects/${encodeURIComponent(projectID)}/activate`), { method: 'POST' });
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
  clearWelcomeSurface();
  return project;
}

async function loadChatHistory() {
  if (!state.chatSessionId) return;
  const host = chatHistoryEl();
  if (!host) return;
  host.innerHTML = '';
  const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(state.chatSessionId)}/history`));
  if (!resp.ok) throw new Error(`chat history failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const session = payload?.session || {};
  setChatMode(session.mode || state.chatMode);
  const messages = Array.isArray(payload?.messages) ? payload.messages : [];
  for (const msg of messages) {
    const role = String(msg.role || 'assistant').toLowerCase();
    const renderFormat = String(msg.render_format || '').toLowerCase();
    const markdown = String(msg.content_markdown || '');
    const plain = String(msg.content_plain || markdown);
    if (role === 'assistant') {
      if (!shouldRenderAssistantHistoryInChat(renderFormat, markdown, plain)) continue;
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
    const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(targetSessionID)}/activity`), { cache: 'no-store' });
    if (!resp.ok) {
      if (!hasLocalAssistantWork() && !state.assistantCancelInFlight) {
        state.assistantRemoteActiveCount = 0;
        state.assistantRemoteQueuedCount = 0;
        updateAssistantActivityIndicator();
      }
      return;
    }
    if (targetSessionID !== state.chatSessionId) return;
    const payload = await resp.json();
    const activeTurns = Number(payload?.active_turns || 0);
    const queuedTurns = Number(payload?.queued_turns || 0);
    if (!Number.isFinite(activeTurns) || activeTurns < 0) return;
    if (!Number.isFinite(queuedTurns) || queuedTurns < 0) return;
    state.assistantRemoteActiveCount = activeTurns;
    state.assistantRemoteQueuedCount = queuedTurns;
    const project = activeProject();
    if (project?.id) {
      upsertProject({
        ...project,
        run_state: payload,
      });
      renderEdgeTopProjects();
    }
    const recentlyStarted = (Date.now() - state.assistantLastStartedAt) < ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS;
    if (activeTurns <= 0
      && queuedTurns <= 0
      && !state.assistantCancelInFlight
      && !state.voiceAwaitingTurn
      && !state.voiceTranscriptSubmitInFlight
      && !isVoiceTranscriptSubmitPending()
      && !recentlyStarted) {
      state.assistantActiveTurns.clear();
      state.assistantUnknownTurns = 0;
    }
    updateAssistantActivityIndicator();
  } catch (_) {
    if (!hasLocalAssistantWork() && !state.assistantCancelInFlight) {
      state.assistantRemoteActiveCount = 0;
      state.assistantRemoteQueuedCount = 0;
      updateAssistantActivityIndicator();
    }
  } finally {
    assistantActivityInFlight = false;
  }
}

async function refreshProjectRunStates() {
  if (projectRunStatesInFlight) return;
  projectRunStatesInFlight = true;
  try {
    const resp = await fetch(apiURL('projects/activity'), { cache: 'no-store' });
    if (!resp.ok) return;
    const payload = await resp.json();
    const items = Array.isArray(payload?.projects) ? payload.projects : [];
    for (const item of items) {
      const projectID = String(item?.project_id || '').trim();
      if (!projectID) continue;
      const existing = state.projects.find((project) => project.id === projectID);
      if (!existing) continue;
      upsertProject({
        ...existing,
        chat_mode: item?.chat_mode || existing.chat_mode,
        run_state: item?.run_state,
        unread: item?.unread,
        review_pending: item?.review_pending,
      });
    }
    renderEdgeTopProjects();
  } finally {
    projectRunStatesInFlight = false;
  }
}

function startAssistantActivityWatcher() {
  if (assistantActivityTimer !== null) return;
  const tick = () => {
    if (document.hidden) return;
    if (hasLocalStopCapableWork() && state.chatWs && chatWsLastMessageAt > 0) {
      const elapsed = Date.now() - chatWsLastMessageAt;
      if (elapsed > CHAT_WS_STALE_THRESHOLD_MS) {
        chatWsLastMessageAt = 0;
        closeChatWs();
        openChatWs();
        return;
      }
    }
    void refreshAssistantActivity();
    void refreshProjectRunStates();
  };
  assistantActivityTimer = window.setInterval(tick, ASSISTANT_ACTIVITY_POLL_MS);
  tick();
  window.addEventListener('focus', () => {
    tick();
    if (document.hidden || state.chatVoiceCapture) return;
    requestMicRefresh();
    releaseMicStream({ force: true });
    requestHotwordSync();
  });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      requestMicRefresh();
      stopHotwordMonitor();
      state.hotwordActive = false;
      cancelLiveSessionListen();
      releaseMicStream({ force: true });
      if (state.chatVoiceCapture) {
        cancelChatVoiceCapture();
      }
      if (state.voiceAwaitingTurn) {
        sttCancel();
        state.voiceAwaitingTurn = false;
        updateAssistantActivityIndicator();
      }
      return;
    }
    tick();
    requestHotwordSync();
  });
  window.addEventListener('pageshow', () => {
    if (state.chatVoiceCapture) return;
    requestMicRefresh();
    releaseMicStream({ force: true });
    requestHotwordSync();
  });
  window.addEventListener('pagehide', () => {
    requestMicRefresh();
    stopHotwordMonitor();
    state.hotwordActive = false;
    releaseMicStream({ force: true });
  });
  const mediaDevices = navigator.mediaDevices;
  if (mediaDevices && typeof mediaDevices.addEventListener === 'function') {
    mediaDevices.addEventListener('devicechange', () => {
      requestMicRefresh();
      releaseMicStream({ force: true });
      requestHotwordSync();
    });
  }
}

function closeChatWs() {
  state.chatWsToken += 1;
  chatWsLastMessageAt = 0;
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
  const ws = new WebSocket(wsURL(`chat/${encodeURIComponent(targetSessionID)}`));
  ws.binaryType = 'arraybuffer';
  state.chatWs = ws;

  ws.onopen = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    const isReconnect = state.chatWsHasConnected;
    state.chatWsHasConnected = true;
    showStatus('connected');
    if (pendingRuntimeReloadStatus) {
      showStatus(pendingRuntimeReloadStatus);
      pendingRuntimeReloadStatus = '';
    }
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
    chatWsLastMessageAt = Date.now();
    if (event.data instanceof ArrayBuffer) {
      if (!canSpeakTTS()) return;
      if (!ttsPlayer) ttsPlayer = new TTSPlayer();
      ttsPlayer.enqueue(event.data);
      return;
    }
    if (event.data instanceof Blob) {
      if (!canSpeakTTS()) return;
      event.data.arrayBuffer()
        .then((audioBuffer) => {
          if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
          if (!canSpeakTTS()) return;
          if (!ttsPlayer) ttsPlayer = new TTSPlayer();
          ttsPlayer.enqueue(audioBuffer);
        })
        .catch((err) => {
          console.warn('TTS blob decode error:', err);
        });
      return;
    }
    if (typeof event.data !== 'string') return;
    let payload = null;
    try { payload = JSON.parse(event.data); } catch (_) { return; }
    if (handleSTTWSMessage(payload)) return;
    try {
      handleChatEvent(payload);
    } catch (err) {
      console.error('handleChatEvent error:', err);
      const turnID = String(payload?.turn_id || '').trim();
      if (turnID) trackAssistantTurnFinished(turnID);
      state.voiceAwaitingTurn = false;
      appendPlainMessage('system', `Internal error: ${String(err?.message || err)}`);
      showStatus('error');
      updateAssistantActivityIndicator();
    }
  };

  ws.onclose = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    cancelLiveSessionListen();
    if (isMeetingLiveSession()) {
      stopLiveSession();
      applyLiveSessionStateSnapshot();
    }
    if (state.chatVoiceCapture || state.voiceAwaitingTurn) {
      cancelChatVoiceCapture();
      sttCancel();
      state.voiceAwaitingTurn = false;
      updateAssistantActivityIndicator();
    }
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

function assistantMessageUsesCanvasBlocks(text) {
  const lower = String(text || '').toLowerCase();
  return lower.includes(':::file{');
}

function shouldRenderAssistantHistoryInChat(_renderFormat, markdown, plain) {
  return Boolean(String(markdown || plain || '').trim());
}

function isVoiceOutputModePayload(payload) {
  return String(payload?.output_mode || '').trim().toLowerCase() === 'voice';
}

function handleChatEvent(payload) {
  const type = String(payload?.type || '').trim();
  if (!type) return;

  if (handleLiveSessionMessage(payload)) {
    applyLiveSessionStateSnapshot();
    renderEdgeTopModelButtons();
    updateAssistantActivityIndicator();
    return;
  }

  if (type === 'companion_state') {
    const projectKey = String(payload?.project_key || '').trim();
    const currentProjectKey = activeProjectKey();
    if (!projectKey || !currentProjectKey || projectKey === currentProjectKey) {
      applyCompanionState(payload);
      updateAssistantActivityIndicator();
    }
    return;
  }

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
      state.canvasActionThisTurn = true;
    } else if (action === 'open_chat') {
      // No more canvas - stay on rasa
    }
    return;
  }

  if (type === 'system_action') {
    const action = payload && typeof payload.action === 'object' ? payload.action : {};
    const actionType = String(action?.type || '').trim();
    if (actionType === 'switch_project') {
      const projectID = String(action?.project_id || '').trim();
      if (projectID) {
        void switchProject(projectID);
      }
    } else if (actionType === 'switch_model') {
      const projectID = String(action?.project_id || '').trim();
      const alias = normalizeProjectChatModelAlias(action?.alias);
      const effortRaw = String(action?.effort || '').trim().toLowerCase();
      if (projectID && alias) {
        const existing = state.projects.find((item) => item.id === projectID);
        if (existing) {
          const nextEffort = normalizeProjectChatModelReasoningEffort(
            effortRaw || existing.chat_model_reasoning_effort || '',
            alias,
          );
          upsertProject({
            ...existing,
            chat_model: alias,
            chat_model_reasoning_effort: nextEffort,
          });
          renderEdgeTopProjects();
          renderEdgeTopModelButtons();
          showStatus(`model set to ${alias}`);
          return;
        }
      }
      if (alias) {
        const effort = effortRaw ? normalizeProjectChatModelReasoningEffort(effortRaw, alias) : '';
        void switchProjectChatModel(alias, effort);
      }
    } else if (actionType === 'toggle_silent') {
      toggleTTSSilentMode();
    } else if (actionType === 'print_item') {
      openPrintView(String(action?.url || '').trim());
    } else if (actionType === 'toggle_live_dialogue' || actionType === 'toggle_conversation') {
      const next = state.liveSessionActive ? '' : LIVE_SESSION_MODE_DIALOGUE;
      const action = next
        ? activateLiveSession(next)
        : deactivateLiveSession({ disableMeetingConfig: true });
      Promise.resolve(action)
        .then(() => {
          renderEdgeTopModelButtons();
          updateAssistantActivityIndicator();
          showStatus(next ? 'live dialogue on' : 'live off');
        })
        .catch((err) => {
          const message = String(err?.message || err || 'live toggle failed');
          showStatus(`live toggle failed: ${message}`);
        });
    }
    return;
  }

  if (type === 'system_action_confirmation_required') {
    const action = payload && typeof payload.action === 'object' ? payload.action : {};
    const summary = String(action?.summary || '').trim();
    if (summary) {
      showStatus('confirmation required');
      appendPlainMessage('system', `Confirmation required: ${summary}`);
    }
    return;
  }

  if (type === 'turn_started') {
    const turnID = String(payload.turn_id || '').trim();
    const turnIsVoice = isVoiceOutputModePayload(payload) || state.voiceAwaitingTurn || isVoiceTurn();
    if (turnID) {
      if (turnIsVoice) state.voiceTurns.add(turnID);
      else state.voiceTurns.delete(turnID);
    }
    trackAssistantTurnStarted(turnID);
    state.voiceAwaitingTurn = false;
    state.indicatorSuppressedByCanvasUpdate = false;
    ensurePendingForTurn(turnID);
    // A previous canvas update can suppress indicator rendering. Re-sync after
    // clearing suppression so stop control is available immediately on turn start.
    updateAssistantActivityIndicator();
    if (isMobileSilent()) {
      const edgeRight = document.getElementById('edge-right');
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    }
    state.canvasActionThisTurn = false;
    state.turnFirstResponseShown = false;
    // Reset TTS state for new turn
    stopTTSPlayback();
    const pos = getLastInputPosition();
    if (isVoiceTurn() || state.hasArtifact) {
      hideOverlay();
    } else if (isMobileSilent()) {
      hideOverlay();
    } else {
      showOverlay(pos.x, pos.y + 24);
      updateOverlay('_Thinking..._');
      getUiState().overlayTurnId = payload.turn_id || null;
    }
    return;
  }

  if (type === 'assistant_message') {
    const turnID = String(payload.turn_id || '').trim();
    trackAssistantTurnStarted(turnID);
    const md = String(payload.message || '');
    const autoCanvas = Boolean(payload.auto_canvas);
    const renderOnCanvas = Boolean(payload.render_on_canvas) || autoCanvas || assistantMessageUsesCanvasBlocks(md);
    const row = ensurePendingForTurn(turnID);
    if (String(md || '').trim()) {
      updateAssistantRow(row, md, true);
    } else if (!renderOnCanvas) {
      updateAssistantRow(row, '_Thinking..._', true);
    }

    if (autoCanvas) {
      state.indicatorSuppressedByCanvasUpdate = true;
      updateAssistantActivityIndicator();
      if (!isVoiceTurn()) {
        hideOverlay();
      }
    }

    // First non-empty response: show on canvas (silent) / speak (voice)
    const trimmedMd = String(md || '').trim();
    const shouldSpeakStreaming = isVoiceOutputModePayload(payload) || (turnID ? state.voiceTurns.has(turnID) : false) || isVoiceTurn();
    if (trimmedMd && !state.turnFirstResponseShown) {
      state.turnFirstResponseShown = true;
      if (isMobileSilent()) {
        renderCanvas({ kind: 'text_artifact', title: '', text: md });
      }
      if (shouldSpeakStreaming && canSpeakTTS()) {
        const { ttsText, ttsLang } = extractTTSText(md);
        if (ttsLang) ttsSpeakLang = ttsLang;
        const diff = computeTTSDiff(ttsText);
        queueTTSDiff(diff);
      }
    }

    if (!isVoiceTurn() && !isMobileSilent() && !state.hasArtifact) {
      const cleaned = cleanForOverlay(md);
      if (cleaned) updateOverlay(cleaned);
    } else if (!isVoiceTurn()) {
      hideOverlay();
    }
    return;
  }

  if (type === 'assistant_output' || type === 'message_persisted') {
    if (String(payload.role || '') !== 'assistant') return;
    const turnID = String(payload.turn_id || '').trim();
    const md = String(payload.message || '');
    const autoCanvas = Boolean(payload.auto_canvas);
    const inferredText = md || ttsLastSpeakText;
    const renderOnCanvas = Boolean(payload.render_on_canvas) || autoCanvas || assistantMessageUsesCanvasBlocks(inferredText);
    // Persisted text may be empty for voice-only responses; fall back to TTS text.
    const displayMd = md || (ttsLastSpeakText ? `_${ttsLastSpeakText}_` : '');
    const hasDisplayMd = Boolean(String(displayMd || '').trim());
    const mobileSilent = isMobileSilent();
    const row = takePendingRow(turnID);
    if (row && hasDisplayMd) {
      updateAssistantRow(row, displayMd, false);
    } else if (row) {
      row.classList.remove('is-pending');
    } else if (hasDisplayMd) {
      appendRenderedAssistant(displayMd);
    }
    const shouldSpeakTurn = isVoiceOutputModePayload(payload) || (turnID ? state.voiceTurns.has(turnID) : false) || isVoiceTurn();
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus('ready');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();

    if (shouldSpeakTurn && canSpeakTTS() && md.trim()) {
      const { ttsText, ttsLang } = extractTTSText(md);
      if (ttsLang) ttsSpeakLang = ttsLang;
      const diff = computeTTSDiff(ttsText);
      queueTTSDiff(diff);
    } else if (autoCanvas) {
      state.indicatorSuppressedByCanvasUpdate = true;
      updateAssistantActivityIndicator();
    }

    if (ttsSentenceChunker) {
      ttsSentenceChunker.flush();
    }
    if (mobileSilent) {
      if (state.canvasActionThisTurn) {
        // LLM touched the canvas this turn — keep showing the document.
        const edgeRight = document.getElementById('edge-right');
        if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
      } else if (hasDisplayMd) {
        // Mirror final answer on canvas while keeping chat in focus.
        renderCanvas({
          kind: 'text_artifact',
          title: '',
          text: displayMd,
        });
      }
      hideOverlay();
      state.canvasActionThisTurn = false;
      return;
    }
    if (!isVoiceTurn()) {
      if (autoCanvas || state.hasArtifact) {
        hideOverlay();
        state.canvasActionThisTurn = false;
        return;
      }
      const cleaned = cleanForOverlay(md);
      if (state.canvasActionThisTurn && !cleaned) {
        hideOverlay();
      } else if (cleaned) {
        updateOverlay(cleaned);
      } else {
        hideOverlay();
      }
    }
    state.canvasActionThisTurn = false;
    // If live dialogue is active but no TTS was queued (e.g. TTS error,
    // empty md, or all text already spoken during streaming), kick the listen
    // cycle so the hands-free loop does not stall.
    if (isDialogueLiveSession() && !ttsPlayer && shouldSpeakTurn && canSpeakTTS()) {
      onLiveSessionTTSPlaybackComplete();
    }
    return;
  }

  if (type === 'item_completed') {
    const turnID = String(payload.turn_id || '').trim();
    const line = formatItemCompletedLabel(payload);
    appendAssistantProgressForTurn(turnID, line);
    return;
  }

  if (type === 'turn_completed') {
    void refreshAssistantActivity();
    return;
  }

  if (type === 'turn_cancelled') {
    state.voiceAwaitingTurn = false;
    const turnID = String(payload.turn_id || '').trim();
    let row = takePendingRow(turnID);
    if (!row && !turnID) {
      row = takeAnyPendingRow();
    }
    if (row) updateAssistantRow(row, '_Stopped._', false);
    trackAssistantTurnFinished(turnID);
    state.indicatorSuppressedByCanvasUpdate = false;
    state.assistantLastError = '';
    showStatus('stopped');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();
    hideOverlay();
    window.setTimeout(() => {
      hideOverlay();
      void refreshAssistantActivity();
    }, 180);
    return;
  }

  if (type === 'turn_queue_cleared') {
    state.voiceAwaitingTurn = false;
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
    stopTTSPlayback();
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
    state.voiceAwaitingTurn = false;
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
    if (isDialogueLiveSession() && canSpeakTTS()) {
      onLiveSessionTTSPlaybackComplete();
    }
  }
}

async function switchProject(projectID) {
  const nextProjectID = String(projectID || '').trim();
  if (!nextProjectID) return;
  if (state.projectSwitchInFlight) return;
  if (nextProjectID === state.activeProjectId && state.chatSessionId) return;

  state.projectSwitchInFlight = true;
  showStatus('switching project...');
  await deactivateLiveSession({ silent: true, disableMeetingConfig: true });
  cancelChatVoiceCapture();
  closeChatWs();
  closeCanvasWs();
  clearChatHistory();
  clearCanvas();
  clearWelcomeSurface();
  resetCompanionState();
  state.fileSidebarMode = 'items';
  state.workspaceOpenFilePath = '';
  state.workspaceStepInFlight = false;
  state.itemSidebarItems = [];
  state.itemSidebarCounts = defaultItemSidebarCounts();
  state.itemSidebarLoading = false;
  state.itemSidebarError = '';
  state.itemSidebarActiveItemID = 0;
  setInboxTriggerCount(0);
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
    await refreshWorkspaceBrowser(true);
    await loadItemSidebarView(state.itemSidebarView).catch(() => {});
    openCanvasWs();
    await showWelcomeForActiveProject(true);
    await loadChatHistory();
    await refreshAssistantActivity();
    await refreshCompanionState(project.id).catch(() => {});
    openChatWs();
    showStatus(`ready`);
  } catch (err) {
    const message = String(err?.message || err || 'project switch failed');
    appendPlainMessage('system', `Project switch failed: ${message}`);
    showStatus(`project switch failed: ${message}`);
  } finally {
    state.projectSwitchInFlight = false;
    renderEdgeTopModelButtons();
  }
}

async function switchToHub() {
  const project = hubProject();
  if (!project || !project.id) return;
  await switchProject(project.id);
}

function setPendingSubmit(controller, kind = '') {
  state.pendingSubmitController = controller || null;
  state.pendingSubmitKind = String(kind || '').trim();
}

function clearPendingSubmit(controller = null) {
  if (controller && state.pendingSubmitController !== controller) return;
  state.pendingSubmitController = null;
  state.pendingSubmitKind = '';
}

function abortPendingSubmit(kind = '') {
  const controller = state.pendingSubmitController;
  if (!controller) return false;
  const requiredKind = String(kind || '').trim();
  if (requiredKind && state.pendingSubmitKind !== requiredKind) return false;
  clearPendingSubmit(controller);
  try { controller.abort(); } catch (_) {}
  return true;
}

function abortError() {
  try {
    return new DOMException('aborted', 'AbortError');
  } catch (_) {
    const err = new Error('aborted');
    err.name = 'AbortError';
    return err;
  }
}

function waitWithAbort(delayMs, signal) {
  const ms = Number(delayMs);
  if (!Number.isFinite(ms) || ms <= 0) return Promise.resolve();
  if (!signal) {
    return new Promise((resolve) => window.setTimeout(resolve, ms));
  }
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer);
      signal.removeEventListener('abort', onAbort);
      reject(abortError());
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

async function submitMessage(text, options = {}) {
  const trimmed = String(text || '').trim();
  const submitKind = String(options?.kind || '').trim();
  if (!trimmed || !state.chatSessionId) {
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = false;
    }
    return;
  }
  cancelLiveSessionListen();
  startVoiceLifecycleOp('submit-message');
  let submitController = null;
  if (submitKind) {
    submitController = new AbortController();
    setPendingSubmit(submitController, submitKind);
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = true;
    }
  }
  state.indicatorSuppressedByCanvasUpdate = false;
  // Interrupt TTS playback when sending a new message
  if (ttsPlayer) { ttsPlayer.stop(); ttsPlayer = null; }
  if (ttsSentenceChunker) { ttsSentenceChunker.reset(); ttsSentenceChunker = null; }
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

  if (!finalText.startsWith('/') && (isVoiceTurn() || isMobileSilent())) {
    const pending = appendRenderedAssistant('_Thinking..._', { pending: true, localId: nextLocalMessageId() });
    state.pendingQueue.push(pending);
    updateAssistantActivityIndicator();
  }

  const body = {
    text: finalText,
    output_mode: state.ttsSilent ? 'silent' : 'voice',
    input_mode: submitKind === 'voice_transcript' ? 'voice' : 'text',
  };
  try {
    if (submitKind === 'voice_transcript' && submitController) {
      await waitWithAbort(VOICE_TRANSCRIPT_SUBMIT_GUARD_MS, submitController.signal);
      if (submitController.signal.aborted) {
        throw abortError();
      }
    }
    const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(state.chatSessionId)}/messages`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal: submitController ? submitController.signal : undefined,
    });
    if (!resp.ok) {
      state.voiceAwaitingTurn = false;
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      appendPlainMessage('system', `Send failed: ${detail}`);
      updateOverlay(`**Send failed:** ${detail}`);
      updateAssistantActivityIndicator();
      return;
    }
    const payload = await resp.json();
    if (payload?.kind === 'command') {
      const commandName = String(payload?.result?.name || '').trim().toLowerCase();
      if (commandName === 'pr') {
        state.prReviewAwaitingArtifact = true;
      }
      if (payload?.result?.message) {
        appendPlainMessage('system', String(payload.result.message));
      }
    }
  } catch (err) {
    if (err && (err.name === 'AbortError' || String(err?.message || '').toLowerCase().includes('aborted'))) {
      state.voiceAwaitingTurn = false;
      const pending = takePendingRow('');
      pending?.remove();
      trackAssistantTurnFinished('');
      showStatus('stopped');
      updateAssistantActivityIndicator();
      return;
    }
    state.voiceAwaitingTurn = false;
    const pending = takePendingRow('');
    pending?.remove();
    trackAssistantTurnFinished('');
    appendPlainMessage('system', `Send failed: ${String(err?.message || err)}`);
    updateOverlay(`**Send failed:** ${String(err?.message || err)}`);
    updateAssistantActivityIndicator();
  } finally {
    clearPendingSubmit(submitController);
    if (submitKind === 'voice_transcript') {
      state.voiceTranscriptSubmitInFlight = false;
    }
  }
}

function forceVoiceLifecycleIdle(statusText = 'stopped') {
  cancelLiveSessionListen();
  state.voiceTranscriptSubmitInFlight = false;
  abortPendingSubmit('voice_transcript');
  sttCancel();
  stopTTSPlayback();
  if (state.chatVoiceCapture) {
    stopChatVoiceMedia(state.chatVoiceCapture);
    state.chatVoiceCapture = null;
  }
  setRecording(false);
  state.voiceAwaitingTurn = false;
  state.indicatorSuppressedByCanvasUpdate = false;
  state.assistantCancelInFlight = false;
  state.assistantActiveTurns.clear();
  state.assistantUnknownTurns = 0;
  state.voiceTurns.clear();
  for (const row of state.pendingByTurn.values()) {
    if (row instanceof HTMLElement) updateAssistantRow(row, '_Stopped._', false);
  }
  for (const row of state.pendingQueue) {
    if (row instanceof HTMLElement) updateAssistantRow(row, '_Stopped._', false);
  }
  state.pendingByTurn.clear();
  state.pendingQueue = [];
  hideOverlay();
  showStatus(statusText);
  setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'force-idle');
  updateAssistantActivityIndicator();
}

async function cancelActiveAssistantTurn(options = null) {
  const force = Boolean(options && options.force);
  const silent = Boolean(options && options.silent);
  if (!state.chatSessionId || state.assistantCancelInFlight || (silent && assistantSilentCancelInFlight)) return false;
  if (!force) {
    await refreshAssistantActivity();
    if (!isAssistantWorking()) {
      if (!silent) {
        showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
        updateAssistantActivityIndicator();
      }
      return false;
    }
  }
  if (!silent) {
    state.assistantCancelInFlight = true;
    updateAssistantActivityIndicator();
    showStatus('stopping...');
  } else {
    assistantSilentCancelInFlight = true;
  }
  let canceled = 0;
  let timeoutId = null;
  try {
    const controller = new AbortController();
    timeoutId = window.setTimeout(() => {
      controller.abort();
    }, STOP_REQUEST_TIMEOUT_MS);
    const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(state.chatSessionId)}/cancel`), {
      method: 'POST',
      signal: controller.signal,
    });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      if (!silent) showStatus(`stop failed: ${detail}`);
      return false;
    }
    const payload = await resp.json();
    canceled = Number(payload?.canceled || 0);
    if (canceled <= 0) {
      await refreshAssistantActivity();
      if (!silent && !isAssistantWorking()) {
        showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
      }
    }
  } catch (err) {
    if (!silent) {
      if (String(err?.name || '') === 'AbortError') {
        showStatus('stop request timed out');
      } else {
        showStatus(`stop failed: ${String(err?.message || err)}`);
      }
    }
    return false;
  } finally {
    if (timeoutId !== null) {
      window.clearTimeout(timeoutId);
      timeoutId = null;
    }
    if (!silent) {
      state.assistantCancelInFlight = false;
      updateAssistantActivityIndicator();
    } else {
      assistantSilentCancelInFlight = false;
    }
    window.setTimeout(() => { void refreshAssistantActivity(); }, 120);
  }
  return canceled > 0;
}

async function cancelActiveAssistantTurnWithRetry(maxAttempts = 3, options = null) {
  const silent = Boolean(options && options.silent);
  const attempts = Number.isFinite(maxAttempts) ? Math.max(1, Math.floor(maxAttempts)) : 1;
  for (let i = 0; i < attempts; i += 1) {
    const canceled = await cancelActiveAssistantTurn({ force: true, silent });
    if (canceled) return true;
    await refreshAssistantActivity();
    if (!isAssistantWorking()) return false;
    if (i + 1 < attempts) {
      await new Promise((resolve) => window.setTimeout(resolve, 140));
    }
  }
  return false;
}

async function handleStopAction() {
  startVoiceLifecycleOp('stop-action');
  if (isLiveSessionListenActive()) {
    cancelLiveSessionListen();
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'stop-listening');
    showStatus('ready');
    updateAssistantActivityIndicator();
    return;
  }
  if (state.liveSessionActive && !state.chatVoiceCapture && !isAssistantWorking()) {
    await deactivateLiveSession({ disableMeetingConfig: true });
    return;
  }

  const capture = state.chatVoiceCapture;
  if (capture && capture.stopping) {
    // Duplicate stop gestures can arrive while recorder.stop()/STT stop is
    // already in flight (notably delayed synthetic clicks on iOS). Treat this
    // as idempotent unless voice transcript submit is now pending.
    if (!isVoiceTranscriptSubmitPending() && !state.voiceTranscriptSubmitInFlight) {
      return;
    }
  }
  const isCaptureActive = Boolean(capture && !capture.stopping);
  if (isCaptureActive) {
    await stopVoiceCaptureAndSend();
    return;
  }

  if (isTTSSpeaking()) {
    stopTTSPlayback();
  }

  const localStopCapable = shouldStopInUiClick()
    || hasLocalStopCapableWork()
    || state.voiceAwaitingTurn
    || state.voiceTranscriptSubmitInFlight
    || isVoiceTranscriptSubmitPending()
    || hasPendingOverlayTurn();
  if (!localStopCapable && !hasRemoteAssistantWork()) return;
  forceVoiceLifecycleIdle('stopped');
  void cancelActiveAssistantTurnWithRetry(3, { silent: true }).finally(() => {
    void refreshAssistantActivity();
  });
}

function applyCanvasArtifactEvent(payload) {
  clearWelcomeSurface();
  clearInkDraft();
  if (state.artifactEditMode) {
    exitArtifactEditMode({ applyChanges: false });
  }
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind === 'clear_canvas') {
    state.prReviewAwaitingArtifact = false;
    state.workspaceOpenFilePath = '';
    state.workspaceStepInFlight = false;
    exitPrReviewMode();
    renderCanvas(payload);
    hideCanvasColumn();
    void showWelcomeForActiveProject(true);
    return;
  }

  let handledByPrReview = false;
  const textArtifact = kind === 'text_artifact' || kind === 'text';
  if (textArtifact && (state.prReviewAwaitingArtifact || state.prReviewMode || isLikelyPrReviewArtifact(payload))) {
    handledByPrReview = maybeEnterPrReviewModeFromTextArtifact(payload);
  }
  if (state.prReviewAwaitingArtifact) {
    state.prReviewAwaitingArtifact = false;
  }
  if (!handledByPrReview) {
    state.workspaceOpenFilePath = '';
    state.workspaceStepInFlight = false;
    exitPrReviewMode();
  }

  if (!handledByPrReview && state.prReviewMode) {
    exitPrReviewMode();
  }

  if (!handledByPrReview) {
    renderCanvas(payload);
  }

  if (kind) {
    state.indicatorSuppressedByCanvasUpdate = true;
    updateAssistantActivityIndicator();
  }

  const paneId = paneIdForCanvasKind(payload.kind);
  if (!paneId) return;
  const realCanvasArtifact = isRealCanvasArtifactEvent(payload);
  showCanvasColumn(paneId);
  state.canvasActionThisTurn = state.canvasActionThisTurn || realCanvasArtifact;
  if (isMobileSilent() && realCanvasArtifact) {
    const edgeRight = document.getElementById('edge-right');
    if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
  }
}

function openCanvasWs() {
  const turnToken = state.canvasWsToken + 1;
  state.canvasWsToken = turnToken;
  const targetSessionID = String(state.sessionId || 'local');
  const ws = new WebSocket(wsURL(`canvas/${encodeURIComponent(targetSessionID)}`));
  state.canvasWs = ws;

  ws.onopen = () => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    void loadCanvasSnapshot(targetSessionID);
  };

  ws.onmessage = (event) => {
    if (turnToken !== state.canvasWsToken || targetSessionID !== state.sessionId) return;
    try {
      const payload = JSON.parse(event.data);
      applyCanvasArtifactEvent(payload);
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
    const resp = await fetch(apiURL(`canvas/${encodeURIComponent(sessionID)}/snapshot`));
    if (!resp.ok) {
      if (!state.hasArtifact) {
        exitPrReviewMode();
        clearCanvas();
        await showWelcomeForActiveProject(true);
      }
      return;
    }
    const payload = await resp.json();
    if (payload?.event) {
      applyCanvasArtifactEvent(payload.event);
      return;
    }
    if (!state.hasArtifact) {
      exitPrReviewMode();
      clearCanvas();
      await showWelcomeForActiveProject(true);
    }
  } catch (_) {
    if (!state.hasArtifact) {
      exitPrReviewMode();
      clearCanvas();
      await showWelcomeForActiveProject(true);
    }
  }
}

// Edge panel logic
let edgeTopTimer = null;
let edgeRightTimer = null;
let edgeTouchStart = null;
const EDGE_TAP_SIZE_PX = 30;
const EDGE_TAP_SIZE_SMALL_PX = 30;
const EDGE_TOP_TAP_SIZE_PX = 56;
const EDGE_TOP_TAP_SIZE_SMALL_PX = 52;
const EDGE_TAP_SIZE_SMALL_MEDIA_QUERY = '(max-width: 768px)';

function getEdgeTapSizePx() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return EDGE_TAP_SIZE_PX;
  }
  try {
    return window.matchMedia(EDGE_TAP_SIZE_SMALL_MEDIA_QUERY).matches
      ? EDGE_TAP_SIZE_SMALL_PX
      : EDGE_TAP_SIZE_PX;
  } catch (_) {
    return EDGE_TAP_SIZE_PX;
  }
}

function getTopEdgeTapSizePx() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return EDGE_TOP_TAP_SIZE_PX;
  }
  try {
    return window.matchMedia(EDGE_TAP_SIZE_SMALL_MEDIA_QUERY).matches
      ? EDGE_TOP_TAP_SIZE_SMALL_PX
      : EDGE_TOP_TAP_SIZE_PX;
  } catch (_) {
    return EDGE_TOP_TAP_SIZE_PX;
  }
}

function edgePanelsAreOpen() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  const topOpen = Boolean(edgeTop && (edgeTop.classList.contains('edge-active') || edgeTop.classList.contains('edge-pinned')));
  const rightOpen = Boolean(edgeRight && (edgeRight.classList.contains('edge-active') || edgeRight.classList.contains('edge-pinned')));
  return topOpen || rightOpen || state.prReviewDrawerOpen;
}

function toggleFileSidebarFromEdge() {
  if (!state.prReviewMode && !state.activeProjectId) return;
  if (!state.prReviewMode) {
    if (state.fileSidebarMode === 'workspace') {
      if (!state.workspaceBrowserLoading && state.workspaceBrowserEntries.length === 0 && !state.workspaceBrowserError) {
        void refreshWorkspaceBrowser(false);
      }
    } else if (!state.itemSidebarLoading && state.itemSidebarItems.length === 0 && !state.itemSidebarError) {
      void loadItemSidebarView(state.itemSidebarView);
    }
  }
  setPrReviewDrawerOpen(!state.prReviewDrawerOpen);
  renderPrReviewFileList();
}

function toggleRightEdgeDrawer(edgeRight) {
  if (!(edgeRight instanceof HTMLElement)) return;
  if (edgeRight.classList.contains('edge-pinned')) {
    edgeRight.classList.remove('edge-pinned', 'edge-active');
    return;
  }
  edgeRight.classList.add('edge-active', 'edge-pinned');
}

function handleRasaEdgeTap() {
  const hadOpenPanels = edgePanelsAreOpen();
  closeEdgePanels();
  if (hadOpenPanels) return;
  if (state.hasArtifact) {
    clearCanvas();
    hideCanvasColumn();
  }
}

function isLeftEdgeTapCoordinate(clientX) {
  const edgeTapSize = getEdgeTapSizePx();
  if (!state.prReviewDrawerOpen) {
    return clientX < edgeTapSize;
  }
  const pane = document.getElementById('pr-file-pane');
  if (!(pane instanceof HTMLElement) || !pane.classList.contains('is-open')) {
    return clientX < edgeTapSize;
  }
  const rect = pane.getBoundingClientRect();
  const zoneStart = Math.max(0, rect.right - edgeTapSize);
  const zoneEnd = Math.min(window.innerWidth, rect.right);
  return clientX >= zoneStart && clientX <= zoneEnd;
}

function initEdgePanels() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  const edgeLeftTap = document.getElementById('edge-left-tap');

  // Desktop: hover near edge
  document.addEventListener('mousemove', (ev) => {
    const edgeTapSize = getEdgeTapSizePx();
    const topEdgeTapSize = getTopEdgeTapSizePx();
    // Top edge
    if (ev.clientY < topEdgeTapSize && edgeTop && !edgeTop.classList.contains('edge-pinned')) {
      edgeTop.classList.add('edge-active');
      if (edgeTopTimer) { clearTimeout(edgeTopTimer); edgeTopTimer = null; }
    }
    // Right edge
    if (ev.clientX > window.innerWidth - edgeTapSize && edgeRight && !edgeRight.classList.contains('edge-pinned')) {
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
      clearInkDraft();
      clearCanvas();
      hideCanvasColumn();
      if (edgeTop) {
        edgeTop.classList.remove('edge-active', 'edge-pinned');
      }
    });
  }

  // Desktop: button clicks for left/right/bottom edge taps
  if (edgeLeftTap) {
    let edgeLeftLastTouchAt = 0;
    let edgeLeftTouchStartX = 0;
    let edgeLeftTouchStartY = 0;
    let edgeLeftTouchFlipHandled = false;
    edgeLeftTap.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (Date.now() - edgeLeftLastTouchAt < 700) return;
      toggleFileSidebarFromEdge();
    });
    edgeLeftTap.addEventListener('touchstart', (ev) => {
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      edgeLeftTouchStartX = touch.clientX;
      edgeLeftTouchStartY = touch.clientY;
      edgeLeftTouchFlipHandled = false;
    }, { passive: true });
    edgeLeftTap.addEventListener('touchmove', (ev) => {
      if (!state.hasArtifact || edgeLeftTouchFlipHandled) return;
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      const dx = touch.clientX - edgeLeftTouchStartX;
      const dy = touch.clientY - edgeLeftTouchStartY;
      const absDx = Math.abs(dx);
      const absDy = Math.abs(dy);
      if (dx > 30 && absDx > absDy * 1.1) {
        if (stepCanvasFile(-1)) {
          edgeLeftTouchFlipHandled = true;
          edgeLeftLastTouchAt = Date.now();
          ev.preventDefault();
        }
      }
    }, { passive: false });
    edgeLeftTap.addEventListener('touchend', (ev) => {
      if (edgeLeftTouchFlipHandled) {
        ev.preventDefault();
        edgeLeftTouchFlipHandled = false;
        return;
      }
      ev.preventDefault();
      edgeLeftLastTouchAt = Date.now();
      sidebarEdgeTapAt = Date.now();
      toggleFileSidebarFromEdge();
    }, { passive: false });
  }

  const edgeRightTap = document.getElementById('edge-right-tap');
  if (edgeRightTap) {
    let edgeRightLastTouchAt = 0;
    let edgeRightTouchStartX = 0;
    let edgeRightTouchStartY = 0;
    let edgeRightTouchFlipHandled = false;
    edgeRightTap.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (Date.now() - edgeRightLastTouchAt < 700) return;
      toggleRightEdgeDrawer(edgeRight);
    });
    edgeRightTap.addEventListener('touchstart', (ev) => {
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      edgeRightTouchStartX = touch.clientX;
      edgeRightTouchStartY = touch.clientY;
      edgeRightTouchFlipHandled = false;
    }, { passive: true });
    edgeRightTap.addEventListener('touchmove', (ev) => {
      if (!state.hasArtifact || edgeRightTouchFlipHandled) return;
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      const dx = touch.clientX - edgeRightTouchStartX;
      const dy = touch.clientY - edgeRightTouchStartY;
      const absDx = Math.abs(dx);
      const absDy = Math.abs(dy);
      if (dx < -30 && absDx > absDy * 1.1) {
        if (stepCanvasFile(1)) {
          edgeRightTouchFlipHandled = true;
          edgeRightLastTouchAt = Date.now();
          ev.preventDefault();
        }
      }
    }, { passive: false });
    // Direct touch handler: iOS system gesture recognizer can intercept
    // document-level touch events near screen edges. Handle on the button
    // itself with touch-action:manipulation to bypass system gestures.
    edgeRightTap.addEventListener('touchend', (ev) => {
      if (edgeRightTouchFlipHandled) {
        ev.preventDefault();
        edgeRightTouchFlipHandled = false;
        return;
      }
      ev.preventDefault();
      edgeRightLastTouchAt = Date.now();
      toggleRightEdgeDrawer(edgeRight);
    }, { passive: false });
  }

  const prDrawerBackdrop = document.getElementById('pr-file-drawer-backdrop');
  if (prDrawerBackdrop) {
    prDrawerBackdrop.addEventListener('click', () => {
      setPrReviewDrawerOpen(false);
    });
  }
  // Mobile: touch tap and swipe from edges and open panels.
  // Buttons don't reliably fire click on iOS, so handle everything here.
  let edgeTouchHandled = false;
  document.addEventListener('touchstart', (ev) => {
    if (ev.touches.length !== 1) return;
    if (ev.target instanceof Element && ev.target.closest('#edge-left-tap,#edge-right-tap')) {
      edgeTouchStart = null;
      return;
    }
    const target = ev.target instanceof Element ? ev.target : null;
    const t = ev.touches[0];
    const edgeTapSize = getEdgeTapSizePx();
    const topEdgeTapSize = getTopEdgeTapSizePx();
    edgeTouchHandled = false;
    const startsInCanvasViewport = Boolean(target && target.closest('#canvas-viewport'));
    // When a canvas artifact is visible, prioritize horizontal swipe-to-flip
    // over left/right edge-open gestures.
    const preserveCanvasHorizontalSwipe = Boolean(state.hasArtifact && startsInCanvasViewport);
    const topOpen = Boolean(edgeTop && (edgeTop.classList.contains('edge-active') || edgeTop.classList.contains('edge-pinned')));
    const rightOpen = Boolean(edgeRight && (edgeRight.classList.contains('edge-active') || edgeRight.classList.contains('edge-pinned')));
    const leftOpen = Boolean(state.prReviewDrawerOpen);
    if (leftOpen && target && target.closest('#pr-file-pane')) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'left-open' };
    } else if (rightOpen && target && target.closest('#edge-right')) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'right-open' };
    } else if (topOpen && target && target.closest('#edge-top')) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'top-open' };
    } else if (!preserveCanvasHorizontalSwipe && isLeftEdgeTapCoordinate(t.clientX)) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'left' };
    } else if (!preserveCanvasHorizontalSwipe && t.clientX > window.innerWidth - edgeTapSize) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'right' };
    } else if (t.clientY < topEdgeTapSize) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'top' };
    } else if (t.clientY > window.innerHeight - edgeTapSize) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'bottom' };
    } else {
      edgeTouchStart = null;
    }
  }, { passive: true });

  document.addEventListener('touchmove', (ev) => {
    if (!edgeTouchStart || edgeTouchHandled || ev.touches.length !== 1) return;
    const t = ev.touches[0];
    const dx = t.clientX - edgeTouchStart.x;
    const dy = t.clientY - edgeTouchStart.y;
    const absDx = Math.abs(dx);
    const absDy = Math.abs(dy);
    if (edgeTouchStart.edge === 'right' && dx < -30 && absDx > absDy * 1.1 && edgeRight) {
      edgeRight.classList.add('edge-active');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'top' && dy > 30 && absDy > absDx * 1.1 && edgeTop) {
      edgeTop.classList.add('edge-active');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'left-open' && dx < -30 && absDx > absDy * 1.1 && state.prReviewDrawerOpen) {
      setPrReviewDrawerOpen(false);
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'right-open' && dx > 30 && absDx > absDy * 1.1 && edgeRight) {
      edgeRight.classList.remove('edge-active', 'edge-pinned');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'top-open' && dy < -30 && absDy > absDx * 1.1 && edgeTop) {
      edgeTop.classList.remove('edge-active', 'edge-pinned');
      edgeTouchHandled = true;
    }
  }, { passive: true });

  document.addEventListener('touchend', (ev) => {
    if (!edgeTouchStart || edgeTouchHandled) {
      edgeTouchStart = null;
      return;
    }
    // Tap (not swipe): small movement from start point
    const touch = ev.changedTouches && ev.changedTouches[0];
    if (touch) {
      const dx = Math.abs(touch.clientX - edgeTouchStart.x);
      const dy = Math.abs(touch.clientY - edgeTouchStart.y);
      if (dx < 20 && dy < 20) {
        let handledTapAction = false;
        switch (edgeTouchStart.edge) {
          case 'left':
            toggleFileSidebarFromEdge();
            handledTapAction = true;
            break;
          case 'bottom':
            handleRasaEdgeTap();
            handledTapAction = true;
            break;
          case 'right':
            toggleRightEdgeDrawer(edgeRight);
            handledTapAction = true;
            break;
          case 'top':
            if (edgeTop) {
              edgeTop.classList.add('edge-pinned');
              handledTapAction = true;
            }
            break;
        }
        if (handledTapAction) {
          // Prevent iOS from synthesizing a click after edge tap — the
          // panel pin above can cause the click to land inside the
          // newly-visible panel (e.g. chatHistory) and start recording.
          ev.preventDefault();
          suppressSyntheticClick();
        }
      }
    }
    edgeTouchStart = null;
  }, { passive: false });

  // Blur chat input when app goes to background so iOS does not
  // restore keyboard focus on resume.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      const cpInput = document.getElementById('chat-pane-input');
      if (cpInput && document.activeElement === cpInput) {
        cpInput.blur();
      }
    }
  });

  // Toggle safe-area bottom padding and keyboard state on mobile.
  // iOS can report changing viewport metrics while the keyboard opens;
  // keep a baseline "fully open" viewport and restore frame corners
  // once the keyboard is dismissed.
  if (window.visualViewport) {
    const inputRow = document.querySelector('.chat-pane-input-row');
    if (inputRow) {
      const root = document.documentElement;

      const setKeyboardOpen = (keyboardOpen) => {
        inputRow.classList.toggle('keyboard-open', keyboardOpen);
        document.body.classList.toggle('keyboard-open', keyboardOpen);
        if (!isIPhoneStandalone()) return;
        if (keyboardOpen) {
          root.style.setProperty('--cue-corner-radius', '0 0 0 0');
        } else {
          applyIPhoneFrameCorners();
        }
      };

      let baselineHeight = Math.max(
        window.innerHeight,
        window.visualViewport.height + Math.max(0, window.visualViewport.offsetTop || 0),
      );
      const syncKeyboardState = () => {
        const vv = window.visualViewport;
        if (!vv) return;
        const offsetTop = Math.max(0, Number(vv.offsetTop) || 0);
        const viewportExtent = vv.height + offsetTop;
        if (viewportExtent > baselineHeight) baselineHeight = viewportExtent;
        const focused = isFocusedTextInput();
        const shifted = offsetTop > 1;
        const shrunkenWhileFocused = focused && viewportExtent < baselineHeight - 100;
        const keyboardOpen = shifted || shrunkenWhileFocused;
        setKeyboardOpen(keyboardOpen);
        if (!keyboardOpen) {
          baselineHeight = Math.max(window.innerHeight, viewportExtent);
        }
      };

      window.visualViewport.addEventListener('resize', syncKeyboardState);
      window.visualViewport.addEventListener('scroll', syncKeyboardState);
      window.addEventListener('orientationchange', () => {
        baselineHeight = Math.max(
          window.innerHeight,
          window.visualViewport
            ? (window.visualViewport.height + Math.max(0, window.visualViewport.offsetTop || 0))
            : window.innerHeight,
        );
        window.setTimeout(syncKeyboardState, 80);
      });
      document.addEventListener('focusin', syncKeyboardState, true);
      document.addEventListener('focusout', () => {
        window.setTimeout(syncKeyboardState, 80);
        window.setTimeout(syncKeyboardState, 260);
      }, true);
      syncKeyboardStateNow = syncKeyboardState;
      syncKeyboardState();
    }
  }
}

function closeEdgePanels() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  if (edgeTop) edgeTop.classList.remove('edge-active', 'edge-pinned');
  if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
  if (state.prReviewDrawerOpen) {
    setPrReviewDrawerOpen(false);
  }
}

function bindUi() {
  const canvasText = document.getElementById('canvas-text');
  const canvasViewport = document.getElementById('canvas-viewport');
  const artifactEditor = ensureArtifactEditor();
  const indicatorNode = document.getElementById('indicator');
  if (indicatorNode && indicatorNode.parentElement !== document.body) {
    document.body.appendChild(indicatorNode);
  }
  if (artifactEditor) {
    artifactEditor.addEventListener('keydown', (ev) => {
      if (ev.key !== 'Escape') return;
      ev.preventDefault();
      ev.stopPropagation();
      exitArtifactEditMode({ applyChanges: true });
    }, true);
  }
  let lastMouseX = Math.floor(window.innerWidth / 2);
  let lastMouseY = Math.floor(window.innerHeight / 2);
  let hasLastMousePosition = false;
  const isInEdgeZone = (x, y) => {
    const s = getEdgeTapSizePx();
    const top = getTopEdgeTapSizePx();
    return x < s || x > window.innerWidth - s || y < top || y > window.innerHeight - s;
  };
  const isVoiceInteractionTarget = (target, x, y) => (
    isInEdgeZone(x, y)
    || (target instanceof Element
      && target.closest('button,a,input,textarea,select,[contenteditable="true"],.overlay,.floating-input,.edge-panel,#canvas-pdf .canvas-pdf-page,#canvas-pdf .textLayer,#canvas-pdf .annotationLayer'))
  );
  const rememberMousePosition = (x, y) => {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return;
    lastMouseX = Number(x);
    lastMouseY = Number(y);
    hasLastMousePosition = true;
  };
  const getCtrlVoiceCapturePoint = () => {
    if (hasLastMousePosition) {
      return { x: lastMouseX, y: lastMouseY };
    }
    const lastPos = getLastInputPosition();
    if (Number.isFinite(lastPos?.x) && Number.isFinite(lastPos?.y)) {
      return { x: Number(lastPos.x), y: Number(lastPos.y) };
    }
    return {
      x: Math.floor(window.innerWidth / 2),
      y: Math.floor(window.innerHeight / 2),
    };
  };
  const beginVoiceCaptureFromPoint = (x, y) => {
    let anchor = null;
    if (state.hasArtifact && canvasText) {
      anchor = getAnchorFromPoint(x, y);
    }
    return beginVoiceCapture(x, y, anchor);
  };

  document.addEventListener('mousemove', (ev) => {
    rememberMousePosition(ev.clientX, ev.clientY);
  }, { passive: true });
  document.addEventListener('pointerdown', (ev) => {
    if (ev.pointerType !== 'mouse') return;
    rememberMousePosition(ev.clientX, ev.clientY);
  }, true);

  if (indicatorNode) {
    const isIndicatorArmed = () => (
      indicatorNode.classList.contains('is-working')
      || indicatorNode.classList.contains('is-recording')
      || indicatorNode.classList.contains('is-listening')
    );
    const pointHitsIndicatorChip = (x, y) => {
      const chips = indicatorNode.querySelectorAll('.record-dot, .stop-square');
      for (const chip of chips) {
        if (!(chip instanceof HTMLElement)) continue;
        const style = window.getComputedStyle(chip);
        if (style.display === 'none' || style.visibility === 'hidden') continue;
        const rect = chip.getBoundingClientRect();
        if (x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom) {
          return true;
        }
      }
      return false;
    };
    const isTapOnInteractiveUi = (ev) => {
      const t = ev.target;
      if (!(t instanceof Element)) return false;
      return Boolean(t.closest('button, a, input, textarea, select, #edge-left-tap, #edge-right-tap, #edge-top, #edge-right, #pr-file-pane, #pr-file-drawer-backdrop'));
    };
    const handleIndicatorTap = (ev, x, y, isTouch = false) => {
      if (!isIndicatorArmed()) return;
      if (!isTouch && isSuppressedClick()) return;
      const stopGestureActive = isUiStopGestureActive();
      const hitsChip = pointHitsIndicatorChip(x, y);
      if (!hitsChip && isTouch && stopGestureActive && isTapOnInteractiveUi(ev)) return;
      if (!hitsChip && !(isTouch && stopGestureActive)) return;
      ev.preventDefault();
      ev.stopPropagation();
      if (isTouch) suppressSyntheticClick();
      void handleStopAction();
    };
    document.addEventListener('click', (ev) => {
      handleIndicatorTap(ev, ev.clientX, ev.clientY, false);
    }, true);
    document.addEventListener('touchend', (ev) => {
      const touch = ev.changedTouches && ev.changedTouches.length > 0 ? ev.changedTouches[0] : null;
      if (!touch) return;
      handleIndicatorTap(ev, touch.clientX, touch.clientY, true);
    }, { passive: false, capture: true });
  }

  // Left-click/tap on canvas -> toggle voice recording
  const clickTarget = canvasViewport || document.getElementById('workspace');
  const syncIndicatorOnViewportChange = () => {
    updateAssistantActivityIndicator();
  };
  if (canvasViewport instanceof HTMLElement) {
    syncInkLayerSize();
    canvasViewport.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true, capture: true });
    let canvasSwipeStart = null;
    let canvasSwipeHandled = false;
    let horizontalWheelAccum = 0;
    let horizontalWheelLastAt = 0;
    const resetCanvasSwipe = () => {
      canvasSwipeStart = null;
      canvasSwipeHandled = false;
    };
    canvasViewport.addEventListener('touchstart', (ev) => {
      if (!isMobileViewport() && !isLikelyIOS()) return;
      if (state.prReviewDrawerOpen || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      canvasSwipeStart = { x: touch.clientX, y: touch.clientY };
      canvasSwipeHandled = false;
    }, { passive: true });
    canvasViewport.addEventListener('touchmove', (ev) => {
      if (!canvasSwipeStart || canvasSwipeHandled || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      const dx = touch.clientX - canvasSwipeStart.x;
      const dy = touch.clientY - canvasSwipeStart.y;
      if (!state.hasArtifact) return;
      if (Math.abs(dx) < 48) return;
      if (Math.abs(dx) <= Math.abs(dy) * 1.25) return;
      const stepped = stepCanvasFile(dx < 0 ? 1 : -1);
      if (!stepped) return;
      canvasSwipeHandled = true;
      ev.preventDefault();
    }, { passive: false });
    canvasViewport.addEventListener('touchend', resetCanvasSwipe, { passive: true });
    canvasViewport.addEventListener('touchcancel', resetCanvasSwipe, { passive: true });
    canvasViewport.addEventListener('wheel', (ev) => {
      if (!state.hasArtifact) return;
      const absX = Math.abs(ev.deltaX);
      const absY = Math.abs(ev.deltaY);
      if (absX < 0.8) return;
      if (absX <= absY * 1.15) return;
      ev.preventDefault();
      const now = Date.now();
      if (now - horizontalWheelLastAt > 260) {
        horizontalWheelAccum = 0;
      }
      horizontalWheelAccum += ev.deltaX;
      if (Math.abs(horizontalWheelAccum) < 48) return;
      const stepped = stepCanvasFile(horizontalWheelAccum > 0 ? 1 : -1);
      if (!stepped) return;
      horizontalWheelAccum = 0;
      horizontalWheelLastAt = now;
    }, { passive: false });
    canvasViewport.addEventListener('pointerdown', (ev) => {
      if (!isPenInputMode()) return;
      if (ev.pointerType !== 'pen') return;
      if (isEditableTarget(ev.target)) return;
      if (ev.target instanceof Element && ev.target.closest('.edge-panel,#pr-file-pane,#pr-file-drawer-backdrop')) return;
      if (beginInkStroke(ev)) {
        try { window.getSelection()?.removeAllRanges(); } catch (_) {}
        setPenInkingState(true);
        ev.preventDefault();
        try { canvasViewport.setPointerCapture(ev.pointerId); } catch (_) {}
      }
    }, true);
    canvasViewport.addEventListener('pointermove', (ev) => {
      if (!isPenInputMode()) return;
      if (state.inkDraft.activePointerId !== ev.pointerId) return;
      if (extendInkStroke(ev)) {
        ev.preventDefault();
      }
    }, true);
    const finishInkPointer = (ev) => {
      if (state.inkDraft.activePointerId !== ev.pointerId) return;
      extendInkStroke(ev);
      resetInkDraftState();
      setPenInkingState(false);
      renderInkControls();
      ev.preventDefault();
    };
    canvasViewport.addEventListener('pointerup', finishInkPointer, true);
    canvasViewport.addEventListener('pointercancel', finishInkPointer, true);
    canvasViewport.addEventListener('selectstart', (ev) => {
      if (!isPenInputMode()) return;
      ev.preventDefault();
    }, true);
  }
  window.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true });
  window.addEventListener('resize', syncIndicatorOnViewportChange);

  if (clickTarget) {
    let touchTapStartX = 0;
    let touchTapStartY = 0;
    let touchTapTracking = false;
    let touchTapMoved = false;
    let touchLongTapTriggered = false;
    let touchEditTimer = null;
    const TOUCH_TAP_MOVE_THRESHOLD = 10;
    const clearTouchEditTimer = () => {
      if (touchEditTimer !== null) {
        clearTimeout(touchEditTimer);
        touchEditTimer = null;
      }
    };

    const handleWorkspaceTap = (target, x, y) => {
      if (isLiveSessionListenActive()) {
        if (isVoiceInteractionTarget(target, x, y)) return;
        cancelLiveSessionListen();
        if (isKeyboardInputMode()) {
          const anchor = state.hasArtifact && canvasText ? getAnchorFromPoint(x, y) : null;
          openComposerAt(x, y, anchor);
        } else {
          void beginVoiceCaptureFromPoint(x, y);
        }
        return;
      }
      if (isUiStopGestureActive()) {
        void handleStopAction();
        return;
      }
      if (isVoiceInteractionTarget(target, x, y)) return;
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;
      rememberMousePosition(x, y);
      if (isRecording()) {
        void stopVoiceCaptureAndSend();
        return;
      }
      if (isKeyboardInputMode()) {
        const anchor = state.hasArtifact && canvasText ? getAnchorFromPoint(x, y) : null;
        openComposerAt(x, y, anchor);
        return;
      }
      void beginVoiceCaptureFromPoint(x, y);
    };

    clickTarget.addEventListener('touchstart', (ev) => {
      if (ev.touches.length !== 1) {
        touchTapTracking = false;
        touchTapMoved = false;
        touchLongTapTriggered = false;
        clearTouchEditTimer();
        return;
      }
      const touch = ev.touches[0];
      if (isEditableTarget(ev.target)) {
        touchTapTracking = false;
        touchTapMoved = false;
        touchLongTapTriggered = false;
        clearTouchEditTimer();
        return;
      }
      touchTapStartX = touch.clientX;
      touchTapStartY = touch.clientY;
      touchTapTracking = !isVoiceInteractionTarget(ev.target, touch.clientX, touch.clientY);
      touchTapMoved = false;
      touchLongTapTriggered = false;
      clearTouchEditTimer();
      if (touchTapTracking && canEnterArtifactEditModeFromTarget(ev.target)) {
        touchEditTimer = window.setTimeout(() => {
          touchEditTimer = null;
          touchTapTracking = false;
          touchTapMoved = false;
          touchLongTapTriggered = enterArtifactEditMode(touchTapStartX, touchTapStartY);
          if (touchLongTapTriggered) suppressSyntheticClick();
        }, ARTIFACT_EDIT_LONG_TAP_MS);
      }
    }, { passive: true });

    clickTarget.addEventListener('touchmove', (ev) => {
      if ((!touchTapTracking && touchEditTimer === null) || touchTapMoved || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      if (Math.hypot(touch.clientX - touchTapStartX, touch.clientY - touchTapStartY) > TOUCH_TAP_MOVE_THRESHOLD) {
        touchTapMoved = true;
        clearTouchEditTimer();
      }
    }, { passive: true });

    clickTarget.addEventListener('touchend', (ev) => {
      if (touchLongTapTriggered) {
        touchLongTapTriggered = false;
        touchTapTracking = false;
        touchTapMoved = false;
        clearTouchEditTimer();
        ev.preventDefault();
        suppressSyntheticClick();
        return;
      }
      if (!touchTapTracking) return;
      touchTapTracking = false;
      if (touchTapMoved) {
        touchTapMoved = false;
        clearTouchEditTimer();
        return;
      }
      const touch = ev.changedTouches && ev.changedTouches.length > 0 ? ev.changedTouches[0] : null;
      if (!touch) return;
      clearTouchEditTimer();
      ev.preventDefault();
      suppressSyntheticClick();
      handleWorkspaceTap(ev.target, touch.clientX, touch.clientY);
    }, { passive: false });

    clickTarget.addEventListener('touchcancel', () => {
      touchTapTracking = false;
      touchTapMoved = false;
      touchLongTapTriggered = false;
      clearTouchEditTimer();
    }, { passive: true });

    clickTarget.addEventListener('click', (ev) => {
      if (isSuppressedClick()) return;
      if (ev.button !== 0) return;
      handleWorkspaceTap(ev.target, ev.clientX, ev.clientY);
    });
  }

  // Right-click -> artifact editor (text artifacts) or floating text input
  if (clickTarget) {
    clickTarget.addEventListener('contextmenu', (ev) => {
      if (state.artifactEditMode) {
        ev.preventDefault();
        return;
      }
      if (ev.target instanceof Element && ev.target.closest('.edge-panel')) return;
      if (canEnterArtifactEditModeFromTarget(ev.target)) {
        ev.preventDefault();
        enterArtifactEditMode(ev.clientX, ev.clientY);
        return;
      }
      ev.preventDefault();
      cancelLiveSessionListen();
      let anchor = null;
      if (state.hasArtifact && canvasText) {
        anchor = getAnchorFromPoint(ev.clientX, ev.clientY);
      }
      openComposerAt(ev.clientX, ev.clientY, anchor);
    });
  }

  // Text input Enter -> send
  const floatingInput = document.getElementById('floating-input');
  if (floatingInput instanceof HTMLTextAreaElement) {
    floatingInput.addEventListener('focus', () => {
      cancelLiveSessionListen();
    });
    floatingInput.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        const text = floatingInput.value.trim();
        if (text) {
          state.lastInputOrigin = 'text';
          floatingInput.value = '';
          floatingInput.blur();
          hideTextInput();
          settleKeyboardAfterSubmit();
          void submitMessage(text);
        }
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        hideTextInput();
      }
    });
    floatingInput.addEventListener('input', () => {
      floatingInput.style.height = 'auto';
      floatingInput.style.height = `${Math.min(floatingInput.scrollHeight, 240)}px`;
    });
  }

  // Chat pane input: Enter sends, Escape blurs, auto-resize
  const chatPaneInput = document.getElementById('chat-pane-input');
  if (chatPaneInput instanceof HTMLTextAreaElement) {
    chatPaneInput.addEventListener('focus', () => {
      cancelLiveSessionListen();
    });
    chatPaneInput.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        const text = chatPaneInput.value.trim();
        if (text) {
          state.lastInputOrigin = 'text';
          chatPaneInput.value = '';
          chatPaneInput.style.height = '';
          chatPaneInput.blur();
          settleKeyboardAfterSubmit();
          void submitMessage(text);
        }
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        chatPaneInput.value = '';
        chatPaneInput.style.height = '';
        chatPaneInput.blur();
        settleKeyboardAfterSubmit();
      }
    });
    chatPaneInput.addEventListener('input', () => {
      chatPaneInput.style.height = 'auto';
      chatPaneInput.style.height = `${Math.min(chatPaneInput.scrollHeight, 240)}px`;
    });

  }

  const inkClear = document.getElementById('ink-clear');
  if (inkClear instanceof HTMLButtonElement) {
    inkClear.addEventListener('click', () => {
      clearInkDraft();
      showStatus('ink cleared');
    });
  }
  const inkSubmit = document.getElementById('ink-submit');
  if (inkSubmit instanceof HTMLButtonElement) {
    inkSubmit.addEventListener('click', () => {
      void submitInkDraft();
    });
  }

  // Voice tap on chat history (only when panel is pinned, not just hover-active)
  const chatHistory = document.getElementById('chat-history');
  if (chatHistory) {
    chatHistory.addEventListener('click', (ev) => {
      if (isKeyboardInputMode()) return;
      if (ev.button !== 0) return;
      if (ev.target instanceof Element && ev.target.closest('a,button,input,textarea,select,[contenteditable="true"]')) return;
      if (isInEdgeZone(ev.clientX, ev.clientY)) return;
      const edgeR = chatHistory.closest('.edge-panel');
      if (edgeR && !edgeR.classList.contains('edge-pinned')) return;
      if (isLiveSessionListenActive()) {
        cancelLiveSessionListen();
        void beginVoiceCaptureFromPoint(ev.clientX, ev.clientY);
        return;
      }
      if (shouldStopInUiClick()) { void handleStopAction(); return; }
      if (isRecording()) { void stopVoiceCaptureAndSend(); return; }
      void beginVoiceCaptureFromPoint(ev.clientX, ev.clientY);
    });
  }

  // Click outside overlay/input -> dismiss
  document.addEventListener('mousedown', (ev) => {
    if (!(ev.target instanceof Element)) return;
    // Dismiss overlay on click outside
    if (isOverlayVisible()) {
      const overlay = document.getElementById('overlay');
      if (overlay && !overlay.contains(ev.target)) {
        hideOverlay();
      }
    }
    // Dismiss text input on click outside
    if (isTextInputVisible()) {
      const input = document.getElementById('floating-input');
      if (input && !input.contains(ev.target) && ev.button === 0) {
        hideTextInput();
      }
    }
  });

  // Keyboard typing auto-activates text input (rasa mode)
  document.addEventListener('keydown', (ev) => {
    // Escape handling
    if (ev.key === 'Escape' && !ev.metaKey && !ev.ctrlKey && !ev.altKey) {
      if (state.artifactEditMode) {
        ev.preventDefault();
        exitArtifactEditMode({ applyChanges: true });
        return;
      }
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
      if (state.inkDraft.dirty) {
        clearInkDraft();
        showStatus('ink cleared');
        return;
      }
      if (state.prReviewDrawerOpen) {
        setPrReviewDrawerOpen(false);
        return;
      }
      closeEdgePanels();
      if (state.hasArtifact) {
        clearCanvas();
        hideCanvasColumn();
        return;
      }
      void handleStopAction();
      return;
    }

    // Enter stops recording
    if (ev.key === 'Enter' && isRecording()) {
      ev.preventDefault();
      void stopVoiceCaptureAndSend();
      return;
    }
    if (ev.key === 'Enter' && isPenInputMode() && state.inkDraft.dirty) {
      ev.preventDefault();
      void submitInkDraft();
      return;
    }

    // Control long-press for PTT
    if (ev.key === 'Control' && !ev.repeat) {
      if (state.chatCtrlHoldTimer || state.chatVoiceCapture) return;
      if (isLiveSessionListenActive()) {
        cancelLiveSessionListen();
      }
      state.chatCtrlHoldTimer = window.setTimeout(() => {
        state.chatCtrlHoldTimer = null;
        const point = getCtrlVoiceCapturePoint();
        void beginVoiceCaptureFromPoint(point.x, point.y);
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
    if (state.artifactEditMode) return;

    if (ev.key === 'ArrowRight') {
      if (stepCanvasFile(1)) {
        ev.preventDefault();
      }
      return;
    }
    if (ev.key === 'ArrowLeft') {
      if (stepCanvasFile(-1)) {
        ev.preventDefault();
      }
      return;
    }

    if (state.prReviewMode) {
      if (ev.key === 'j' || ev.key === 'J') {
        ev.preventDefault();
        stepPrReviewFile(1);
        return;
      }
      if (ev.key === 'k' || ev.key === 'K') {
        ev.preventDefault();
        stepPrReviewFile(-1);
        return;
      }
    }

    // Auto-activate text input on printable key
    if (ev.key.length === 1 && !isTextInputVisible()) {
      // Route to chat pane input when chat pane is open (desktop only)
      const edgeR = document.getElementById('edge-right');
      const cpInput = document.getElementById('chat-pane-input');
      const chatPaneOpen = edgeR && (edgeR.classList.contains('edge-active') || edgeR.classList.contains('edge-pinned'));
      if (chatPaneOpen && cpInput instanceof HTMLTextAreaElement && !window.matchMedia('(max-width: 767px)').matches) {
        cancelLiveSessionListen();
        cpInput.focus();
        cpInput.value = ev.key;
        const caret = ev.key.length;
        cpInput.setSelectionRange(caret, caret);
        cpInput.dispatchEvent(new Event('input', { bubbles: true }));
        ev.preventDefault();
        return;
      }
      if (!isKeyboardInputMode()) {
        return;
      }
      const cx = window.innerWidth / 2 - 130;
      const cy = window.innerHeight / 2;
      cancelLiveSessionListen();
      openComposerAt(cx, cy, null, ev.key);
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
      void stopVoiceCaptureAndSend();
    }
  }, true);

  window.addEventListener('blur', () => {
    if (state.chatCtrlHoldTimer) {
      clearTimeout(state.chatCtrlHoldTimer);
      state.chatCtrlHoldTimer = null;
    }
    // Keep active capture alive on transient browser blur; hard stop is
    // handled by visibilitychange when the page is actually hidden.
    if (state.chatVoiceCapture && document.hidden) {
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

  initEdgePanels();
}

function showSplash() {
  const project = activeProject();
  const name = project?.name || '';
  if (!name) return;
  const splash = document.createElement('div');
  splash.className = 'splash';
  splash.textContent = name;
  document.getElementById('view-main')?.appendChild(splash);
  window.setTimeout(() => splash.classList.add('fade-out'), 100);
  window.setTimeout(() => splash.remove(), 1700);
}

async function init() {
  pendingRuntimeReloadContext = consumeRuntimeReloadContext();
  applyIPhoneFrameCorners();
  window.addEventListener('resize', () => {
    if (document.body.classList.contains('keyboard-open')) return;
    applyIPhoneFrameCorners();
    syncInkLayerSize();
    renderInkControls();
  });
  bindUi();
  syncInkLayerSize();
  renderInkControls();
  syncInputModeBodyState();
  updateAssistantActivityIndicator();
  startRuntimeReloadWatcher();
  startAssistantActivityWatcher();
  clearCanvas();
  hideCanvasColumn();
  showStatus('starting...');

  // Check TTS availability from runtime
  try {
    const runtime = await fetchRuntimeMeta();
    applyRuntimePreferences(runtime);
    renderInkControls();
    ttsEnabled = Boolean(runtime?.tts_enabled);
    applyRuntimeReasoningEffortOptions(runtime?.available_reasoning_efforts);
  } catch (_) {
    ttsEnabled = false;
    setYoloModeLocal(readYoloModePreference(), { persist: false, render: false });
  }
  await showDisclaimerModal().catch(() => {});
  setTTSSilentMode(state.ttsSilent, { persist: false, pinPanel: false });
  await initHotwordLifecycle();

  await fetchProjects();
  const initialProjectID = resolveInitialProjectID();
  if (!initialProjectID) throw new Error('no projects available');
  await switchProject(initialProjectID);
  // Pin chat panel now that all startup state is settled.
  if (isMobileSilent()) {
    const edgeRight = document.getElementById('edge-right');
    if (edgeRight) edgeRight.classList.add('edge-pinned');
  }
  restoreRuntimeReloadContext();
  showSplash();
  // Enable panel slide transitions only after startup is fully painted.
  requestAnimationFrame(() => requestAnimationFrame(initPanelMotionMode));
}

async function authGate() {
  const loginView = document.getElementById('view-login');
  const mainView = document.getElementById('view-main');
  const resp = await fetch(apiURL('setup'));
  const data = await resp.json();
  if (data.authenticated) {
    if (loginView) loginView.style.display = 'none';
    return;
  }
  const loginForm = document.getElementById('login-form');
  const loginPassword = document.getElementById('login-password');
  const loginError = document.getElementById('login-error');
  const loginPrompt = document.getElementById('login-prompt');
  const loginBtn = document.getElementById('btn-login');

  if (!data.has_password) {
    loginPassword.style.display = 'none';
    loginView.style.display = '';
    mainView.style.display = 'none';
    return new Promise(() => {});
  }

  loginView.style.display = '';
  mainView.style.display = 'none';

  await new Promise((resolve) => {
    loginForm.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      loginError.textContent = '';
      const pw = loginPassword.value;
      if (!pw) return;
      try {
        const r = await fetch(apiURL('login'), {
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
    showBootstrapError(String(err?.message || err));
  });
