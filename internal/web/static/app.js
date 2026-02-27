import { marked } from './vendor/marked.esm.js';
import { renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml } from './canvas.js';
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
  configureConversation,
  isConversationMode,
  setConversationMode,
  onTTSPlaybackComplete,
  cancelConversationListen,
  isConversationListenActive,
} from './conversation.js';
import {
  initHotword,
  startHotwordMonitor,
  stopHotwordMonitor,
  isHotwordActive,
  onHotwordDetected,
  setHotwordThreshold,
  getPreRollAudio,
  getHotwordMicStream,
} from './hotword.js';

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
  ttsSilent: false,
  pendingByTurn: new Map(),
  pendingQueue: [],
  assistantActiveTurns: new Set(),
  assistantUnknownTurns: 0,
  assistantRemoteActiveCount: 0,
  assistantRemoteQueuedCount: 0,
  assistantRemoteDelegateActiveCount: 0,
  assistantCancelInFlight: false,
  assistantLastError: '',
  ttsPlaying: false,
  conversationMode: false,
  conversationListenActive: false,
  conversationListenTimer: null,
  hotwordEnabled: false,
  hotwordActive: false,
  voiceAwaitingTurn: false,
  voiceTurns: new Set(),
  voiceLifecycle: 'idle',
  voiceLifecycleSeq: 0,
  voiceLifecycleReason: '',
  indicatorSuppressedByCanvasUpdate: false,
  chatCtrlHoldTimer: null,
  chatVoiceCapture: null,
  reasoningEffortsByAlias: {
    codex: ['low', 'medium', 'high', 'extra_high'],
    gpt: ['low', 'medium', 'high', 'extra_high'],
    spark: ['low', 'medium', 'high', 'extra_high'],
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
  fileSidebarMode: 'workspace',
  workspaceBrowserPath: '',
  workspaceBrowserEntries: [],
  workspaceBrowserLoading: false,
  workspaceBrowserError: '',
  workspaceOpenFilePath: '',
  workspaceStepInFlight: false,
  prReviewAwaitingArtifact: false,
};

export function getState() {
  return state;
}

function isVoiceTurn() {
  return state.lastInputOrigin === 'voice';
}

window._taburaApp = { getState, acquireMicStream, sttStart, sttSendBlob, sttStop, sttCancel };

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_CHAT_MATH_SEGMENT_';
const DEV_UI_RELOAD_POLL_MS = 1500;
const ASSISTANT_ACTIVITY_POLL_MS = 1200;
const CHAT_WS_STALE_THRESHOLD_MS = 20000;
let localMessageSeq = 0;
const CHAT_CTRL_LONG_PRESS_MS = 180;
const CHAT_SEND_HOLD_MS = 300;
// Frontend end-of-utterance policy:
// - start/end speech from local mic energy
// - pure VAD commit (no semantic EOU sidecar)
// - no-speech timeout + relaxed max duration to avoid hanging capture
const VOICE_VAD_AUTO_SEND_DEFAULT = true;
const VOICE_VAD_AUTO_SEND_STORAGE_KEY = 'tabura.voiceVadAutoSend';
const VOICE_VAD_AUTO_SEND_QUERY_PARAM = 'voice_vad_auto_send';
const VOICE_VAD_MIN_UTTERANCE_MS = 300;
const VOICE_VAD_CANDIDATE_SILENCE_MS = 900;
const VOICE_VAD_CANDIDATE_RECHECK_MS = 450;
const VOICE_VAD_HARD_SILENCE_MS = 2500;
const VOICE_VAD_NO_SPEECH_MS = 4000;
const VOICE_VAD_MAX_RECORDING_SOFT_MS = 120000;
const VOICE_VAD_MAX_RECORDING_HARD_MS = 240000;
const VOICE_VAD_FRAME_MS = 40;
const VOICE_VAD_RECORDER_CHUNK_MS = 250;
const VOICE_VAD_NOISE_FLOOR_SAMPLES = 8;
const VOICE_VAD_NOISE_FLOOR_PERCENTILE = 0.35;
const VOICE_VAD_NOISE_FLOOR_ADAPT_ALPHA = 0.12;
const VOICE_VAD_SPEECH_START_OFFSET_DB = 3;
const VOICE_VAD_SPEECH_END_OFFSET_DB = 1.5;
const VOICE_VAD_SPEECH_START_THRESHOLD_MIN_DB = -42;
const VOICE_VAD_SPEECH_END_THRESHOLD_MIN_DB = -45;
const VOICE_VAD_SPEECH_START_FRAMES = 4;
const VOICE_VAD_NOISE_FLOOR_MIN_DB = -60;
const VOICE_VAD_NOISE_FLOOR_MAX_DB = -18;
const VOICE_CAPTURE_STOP_FLUSH_TIMEOUT_MS = 1500;
const STT_STOP_TIMEOUT_MS = 8000;
const STOP_REQUEST_TIMEOUT_MS = 3500;
const VOICE_LIFECYCLE = Object.freeze({
  IDLE: 'idle',
  LISTENING: 'listening',
  RECORDING: 'recording',
  STOPPING_RECORDING: 'stopping_recording',
  AWAITING_TURN: 'awaiting_turn',
  ASSISTANT_WORKING: 'assistant_working',
  TTS_PLAYING: 'tts_playing',
});
let devReloadBootID = '';
let devReloadTimer = null;
let devReloadInFlight = false;
let devReloadRequested = false;
let assistantActivityTimer = null;
let assistantActivityInFlight = false;
let assistantSilentCancelInFlight = false;
let chatWsLastMessageAt = 0;

const ACTIVE_PROJECT_STORAGE_KEY = 'tabura.activeProjectId';
const LAST_VIEW_STORAGE_KEY = 'tabura.lastView';
const PROJECT_CHAT_MODEL_ALIASES = ['codex', 'gpt', 'spark'];
const PROJECT_CHAT_MODEL_REASONING_EFFORTS = {
  codex: ['low', 'medium', 'high', 'extra_high'],
  gpt: ['low', 'medium', 'high', 'extra_high'],
  spark: ['low', 'medium', 'high', 'extra_high'],
};
const TTS_SILENT_STORAGE_KEY = 'tabura.ttsSilent';
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
        onTTSPlaybackComplete();
      }
      return;
    }
    this._playing = true;
    if (!state.ttsPlaying) {
      cancelConversationListen();
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

function canSpeakTTS() {
  return Boolean(ttsEnabled) && !Boolean(state.ttsSilent);
}

function canStartHotwordMonitor() {
  const mode = syncVoiceLifecycle('can-start-hotword');
  if (!state.hotwordEnabled) return false;
  if (!state.conversationMode) return false;
  if (!canSpeakTTS()) return false;
  if (mode === VOICE_LIFECYCLE.RECORDING || mode === VOICE_LIFECYCLE.STOPPING_RECORDING) return false;
  if (mode === VOICE_LIFECYCLE.TTS_PLAYING) return false;
  if (state.chatVoiceCapture) return false;
  if (isStopCapableLifecycle(mode)) return false;
  return true;
}

async function syncHotwordMonitor() {
  if (!state.hotwordEnabled || !canStartHotwordMonitor()) {
    if (isHotwordActive()) {
      stopHotwordMonitor();
    }
    state.hotwordActive = false;
    return;
  }
  if (isHotwordActive()) {
    state.hotwordActive = true;
    return;
  }
  try {
    const stream = await acquireMicStream();
    await startHotwordMonitor(stream);
  } catch (_) {}
  state.hotwordActive = isHotwordActive();
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

function applyConversationStateSnapshot(snapshot = null) {
  const nextMode = snapshot && typeof snapshot === 'object'
    ? Boolean(snapshot.conversationMode)
    : isConversationMode();
  const nextListenActive = snapshot && typeof snapshot === 'object'
    ? Boolean(snapshot.conversationListenActive)
    : isConversationListenActive();
  const nextListenTimer = snapshot && typeof snapshot === 'object'
    ? (snapshot.conversationListenTimer ?? null)
    : null;
  state.conversationMode = nextMode;
  state.conversationListenActive = nextListenActive;
  state.conversationListenTimer = nextListenTimer;
  requestHotwordSync();
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
  if (state.ttsSilent === next) return;
  state.ttsSilent = next;
  if (persist) {
    persistTTSSilentPreference(next);
  }
  if (next) {
    cancelConversationListen();
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
  setTTSSilentMode(next);
  showStatus(next ? 'silent mode on' : 'voice mode on');
}

// Single shared AudioContext — created once, unlocked via resume() on user
// gesture per Web Audio API best practice (MDN). Safari iOS requires resume()
// to be called from a user-initiated event; once resumed the context stays
// running until the page is closed.
const ttsAudioCtx = new (window.AudioContext || window.webkitAudioContext)();
function unlockAudioContext() {
  if (ttsAudioCtx.state === 'suspended') {
    ttsAudioCtx.resume();
  }
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

configureConversation({
  canStartConversationListen,
  onConversationListenStateChange: (snapshot) => {
    applyConversationStateSnapshot(snapshot);
    renderEdgeTopModelButtons();
    updateAssistantActivityIndicator();
  },
  onConversationListenTimeout: () => {
    requestHotwordSync();
    updateAssistantActivityIndicator();
  },
  onConversationSpeechDetected: () => {
    beginConversationVoiceCapture();
  },
  onConversationListenCancelled: () => {
    requestHotwordSync();
    updateAssistantActivityIndicator();
  },
  getAudioContext: () => ttsAudioCtx,
  acquireMicStream,
  computeDecibelFromTimeDomain,
});
applyConversationStateSnapshot();

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
  const effort = String(value || '').trim().toLowerCase();
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

let _sttResolve = null;
let _sttReject = null;
let _sttActive = false;
let _sttStopTimer = null;

function clearSTTStopWait() {
  if (_sttStopTimer !== null) {
    window.clearTimeout(_sttStopTimer);
    _sttStopTimer = null;
  }
}

function sttStart(mimeType) {
  const ws = state.chatWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    return Promise.reject(new Error('chat WebSocket not connected'));
  }
  clearSTTStopWait();
  _sttResolve = null;
  _sttReject = null;
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
    clearSTTStopWait();
    return Promise.reject(new Error('chat WebSocket not connected'));
  }
  _sttActive = false;
  clearSTTStopWait();
  return new Promise((resolve, reject) => {
    _sttResolve = resolve;
    _sttReject = reject;
    _sttStopTimer = window.setTimeout(() => {
      if (_sttReject) {
        _sttReject(new Error('STT stop timed out'));
      }
      _sttResolve = null;
      _sttReject = null;
      _sttStopTimer = null;
    }, STT_STOP_TIMEOUT_MS);
    ws.send(JSON.stringify({ type: 'stt_stop' }));
  });
}

function sttCancel() {
  _sttActive = false;
  clearSTTStopWait();
  if (_sttReject) {
    _sttReject(new Error('STT cancelled'));
    _sttResolve = null;
    _sttReject = null;
  }
  if (_sttResolve) {
    _sttResolve = null;
  }
  const ws = state.chatWs;
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'stt_cancel' }));
  }
}

function handleSTTWSMessage(payload) {
  const type = String(payload?.type || '');
  if (type === 'stt_result') {
    clearSTTStopWait();
    if (_sttResolve) {
      _sttResolve({ text: payload.text || '' });
      _sttResolve = null;
      _sttReject = null;
    }
    return true;
  }
  if (type === 'stt_error') {
    clearSTTStopWait();
    if (_sttReject) {
      _sttReject(new Error(payload.error || 'STT failed'));
      _sttResolve = null;
      _sttReject = null;
    }
    return true;
  }
  if (type === 'stt_empty') {
    clearSTTStopWait();
    if (_sttResolve) {
      _sttResolve({ text: '', reason: payload.reason || 'empty' });
      _sttResolve = null;
      _sttReject = null;
    }
    return true;
  }
  if (type === 'stt_started' || type === 'stt_cancelled') {
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
  releaseMicStream();
}

function computeDecibelFromTimeDomain(data) {
  let sumSquares = 0;
  for (let i = 0; i < data.length; i++) {
    const sample = (data[i] - 128) / 128;
    sumSquares += sample * sample;
  }
  const rms = Math.sqrt(sumSquares / Math.max(1, data.length));
  if (rms <= 0 || Number.isNaN(rms)) return -100;
  return 20 * Math.log10(rms);
}

function clampNumber(value, min, max) {
  return Math.max(min, Math.min(max, value));
}

function percentileValue(values, percentile) {
  if (!Array.isArray(values) || values.length === 0) return null;
  const sorted = values
    .map((value) => Number(value))
    .filter((value) => Number.isFinite(value))
    .sort((a, b) => a - b);
  if (sorted.length === 0) return null;
  const rank = clampNumber(percentile, 0, 1) * (sorted.length - 1);
  const lower = Math.floor(rank);
  const upper = Math.ceil(rank);
  if (lower === upper) return sorted[lower];
  const weight = rank - lower;
  return (sorted[lower] * (1 - weight)) + (sorted[upper] * weight);
}

function startVADMonitor(capture) {
  if (!isVoiceVADAutoSendEnabled()) return;
  if (!capture || capture.vadState) return;
  if (!capture.mediaStream) return;
  if (!ttsAudioCtx || typeof ttsAudioCtx.createAnalyser !== 'function' || typeof ttsAudioCtx.createMediaStreamSource !== 'function') return;

  if (ttsAudioCtx.state === 'suspended') {
    ttsAudioCtx.resume().catch(() => {});
  }

  const handleNoSpeechTimeout = () => {
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
  };

  const options = {
    startAtMs: performance.now(),
    speechMs: 0,
    silenceMs: 0,
    hasSpeech: false,
    pendingCommitAtMs: 0,
    speechFrames: 0,
    noiseSamples: [],
    noiseFloorDb: null,
    isRunning: true,
  };

  let source;
  let analyser;
  try {
    source = ttsAudioCtx.createMediaStreamSource(capture.mediaStream);
    analyser = ttsAudioCtx.createAnalyser();
    analyser.fftSize = 1024;
    analyser.smoothingTimeConstant = 0.25;
    const bins = new Uint8Array(analyser.frequencyBinCount);
    source.connect(analyser);
    // iOS Safari requires the graph to terminate at destination for
    // AnalyserNode to receive live data from a MediaStreamSource.
    let silentGain = null;
    if (typeof ttsAudioCtx.createGain === 'function') {
      silentGain = ttsAudioCtx.createGain();
      silentGain.gain.value = 0;
      analyser.connect(silentGain);
      silentGain.connect(ttsAudioCtx.destination);
    }

    const update = () => {
      if (!options.isRunning || !capture || capture.stopping || state.chatVoiceCapture !== capture) {
        stopVADMonitor(capture);
        return;
      }

      analyser.getByteTimeDomainData(bins);
      const db = computeDecibelFromTimeDomain(bins);
      const now = performance.now();
      const elapsed = now - options.startAtMs;

      if (options.noiseFloorDb == null && options.noiseSamples.length < VOICE_VAD_NOISE_FLOOR_SAMPLES) {
        options.noiseSamples.push(db);
        if (options.noiseSamples.length >= VOICE_VAD_NOISE_FLOOR_SAMPLES) {
          const seededFloor = percentileValue(options.noiseSamples, VOICE_VAD_NOISE_FLOOR_PERCENTILE);
          if (seededFloor != null) {
            options.noiseFloorDb = clampNumber(
              seededFloor,
              VOICE_VAD_NOISE_FLOOR_MIN_DB,
              VOICE_VAD_NOISE_FLOOR_MAX_DB,
            );
          }
        }
      }

      if (options.noiseFloorDb == null) {
        if (elapsed >= VOICE_VAD_NO_SPEECH_MS) {
          handleNoSpeechTimeout();
          return;
        }
        return;
      }

      const startThresholdBefore = Math.max(
        VOICE_VAD_SPEECH_START_THRESHOLD_MIN_DB,
        options.noiseFloorDb + VOICE_VAD_SPEECH_START_OFFSET_DB,
      );
      const endThresholdBefore = Math.max(
        VOICE_VAD_SPEECH_END_THRESHOLD_MIN_DB,
        options.noiseFloorDb + VOICE_VAD_SPEECH_END_OFFSET_DB,
      );
      const floorUpdateCeilDb = options.hasSpeech ? endThresholdBefore + 2 : startThresholdBefore;
      // Keep tracking ambient floor but avoid pulling it up while speech is active.
      if (db <= floorUpdateCeilDb) {
        options.noiseFloorDb = clampNumber(
          ((1 - VOICE_VAD_NOISE_FLOOR_ADAPT_ALPHA) * options.noiseFloorDb) + (VOICE_VAD_NOISE_FLOOR_ADAPT_ALPHA * db),
          VOICE_VAD_NOISE_FLOOR_MIN_DB,
          VOICE_VAD_NOISE_FLOOR_MAX_DB,
        );
      }

      const startThresholdDb = Math.max(
        VOICE_VAD_SPEECH_START_THRESHOLD_MIN_DB,
        options.noiseFloorDb + VOICE_VAD_SPEECH_START_OFFSET_DB,
      );
      const endThresholdDb = Math.max(
        VOICE_VAD_SPEECH_END_THRESHOLD_MIN_DB,
        options.noiseFloorDb + VOICE_VAD_SPEECH_END_OFFSET_DB,
      );

      if (!options.hasSpeech) {
        if (db >= startThresholdDb) {
          options.speechFrames += 1;
        } else {
          options.speechFrames = 0;
        }
        if (options.speechFrames >= VOICE_VAD_SPEECH_START_FRAMES) {
          options.hasSpeech = true;
          options.speechStartAt = now;
          options.silenceMs = 0;
          options.speechFrames = 0;
        }
      }

      if (!options.hasSpeech) {
        if (elapsed >= VOICE_VAD_NO_SPEECH_MS) {
          handleNoSpeechTimeout();
          return;
        }
        return;
      }

      if (db >= endThresholdDb) {
        options.silenceMs = 0;
      } else {
        options.silenceMs += VOICE_VAD_FRAME_MS;
      }

      options.speechMs = Math.max(0, now - options.speechStartAt);
      if (options.speechMs < VOICE_VAD_MIN_UTTERANCE_MS) return;
      const hitCandidate = options.silenceMs >= VOICE_VAD_CANDIDATE_SILENCE_MS;
      const hitHardSilence = options.silenceMs >= VOICE_VAD_HARD_SILENCE_MS;
      const hitSoftMaxDuration = elapsed >= VOICE_VAD_MAX_RECORDING_SOFT_MS;
      const hitHardMaxDuration = elapsed >= VOICE_VAD_MAX_RECORDING_HARD_MS;

      if (hitHardSilence || hitHardMaxDuration || hitSoftMaxDuration) {
        stopVADMonitor(capture);
        void stopVoiceCaptureAndSend();
        return;
      }

      if (hitCandidate) {
        if (!options.pendingCommitAtMs) {
          options.pendingCommitAtMs = now + VOICE_VAD_CANDIDATE_RECHECK_MS;
          return;
        }
        if (now >= options.pendingCommitAtMs) {
          stopVADMonitor(capture);
          void stopVoiceCaptureAndSend();
        }
        return;
      }

      if (options.pendingCommitAtMs) {
        options.pendingCommitAtMs = 0;
      }
    };

    const timer = window.setInterval(update, VOICE_VAD_FRAME_MS);
    capture.vadState = { source, analyser, silentGain, timer, options, bins, isRunning: true };
  } catch (_) {
    if (source) {
      try { source.disconnect(); } catch (_) {}
    }
    capture.vadState = null;
  }
}

function stopVADMonitor(capture) {
  if (!capture || !capture.vadState) return;
  const state = capture.vadState;
  capture.vadState = null;
  if (state.options) state.options.isRunning = false;
  state.isRunning = false;
  if (state.timer) window.clearInterval(state.timer);
  if (state.source) {
    try { state.source.disconnect(); } catch (_) {}
  }
  if (state.silentGain) {
    try { state.silentGain.disconnect(); } catch (_) {}
  }
  if (state.analyser) {
    try { state.analyser.disconnect(); } catch (_) {}
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
      if (typeof recorder.requestData === 'function') {
        try { recorder.requestData(); } catch (_) {}
      }
      recorder.stop();
    } catch (_) {
      finish();
      return;
    }
    timeoutId = window.setTimeout(finish, VOICE_CAPTURE_STOP_FLUSH_TIMEOUT_MS);
  });
}

async function beginVoiceCapture(x, y, anchor, options = null) {
  if (state.chatVoiceCapture) return;
  if (!canUseMicrophoneCapture()) return;
  const manualStopOnly = Boolean(options && options.manualStopOnly);
  cancelConversationListen();
  // Interrupt TTS playback when starting recording
  stopTTSPlayback();
  const capture = {
    active: false,
    stopping: false,
    stopRequested: false,
    manualStopOnly,
    autoSend: true,
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
    recorder.start(VOICE_VAD_RECORDER_CHUNK_MS);
    if (!capture.stopRequested && !capture.manualStopOnly) {
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
    showStatus(`voice capture failed: ${message}`);
    sttCancel();
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
  }
}

async function stopVoiceCaptureAndSend() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  const opSeq = startVoiceLifecycleOp('voice-capture-stop-send');
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
      if (state.conversationMode) {
        onTTSPlaybackComplete();
        return;
      }
      throw new Error('speech recognizer returned empty text');
    }
    showStatus('sending...');
    void submitMessage(transcript, { kind: 'voice_transcript' });
  } catch (err) {
    if (opSeq !== state.voiceLifecycleSeq) return;
    state.voiceAwaitingTurn = false;
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-capture-stop-failed');
    updateAssistantActivityIndicator();
    const message = String(err?.message || err || 'voice capture failed');
    showStatus(`voice capture failed: ${message}`);
  } finally {
    if (!remoteStopped) {
      sttCancel();
    }
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
    if (opSeq === state.voiceLifecycleSeq) {
      syncVoiceLifecycle('voice-capture-stop-finished');
    }
    updateAssistantActivityIndicator();
  }
}

function cancelChatVoiceCapture() {
  const capture = state.chatVoiceCapture;
  if (!capture) return;
  setRecording(false);
  state.voiceAwaitingTurn = false;
  abortPendingSubmit('voice_transcript');
  sttCancel();
  stopChatVoiceMedia(capture);
  state.chatVoiceCapture = null;
  setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-capture-cancelled');
  updateAssistantActivityIndicator();
}

function showCanvasColumn(paneId) {
  const col = document.getElementById('canvas-column');
  if (!col) return;
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
  exitPrReviewMode();
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

function hasRemoteAssistantWork() {
  return state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0
    || state.assistantRemoteDelegateActiveCount > 0;
}

function hasLocalStopCapableWork() {
  return state.assistantActiveTurns.size > 0
    || state.assistantUnknownTurns > 0
    || state.assistantCancelInFlight;
}

function isDirectAssistantWorking() {
  return hasLocalStopCapableWork()
    || state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0;
}

function isDelegateAssistantWorking() {
  return state.assistantRemoteDelegateActiveCount > 0;
}

function isAssistantWorking() {
  return isDirectAssistantWorking() || isDelegateAssistantWorking();
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
  if (isConversationListenActive()) return VOICE_LIFECYCLE.LISTENING;
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

function canStartConversationListen() {
  if (!canSpeakTTS()) return false;
  const mode = syncVoiceLifecycle('can-start-conversation');
  if (mode === VOICE_LIFECYCLE.RECORDING || mode === VOICE_LIFECYCLE.STOPPING_RECORDING) return false;
  if (mode === VOICE_LIFECYCLE.TTS_PLAYING) return false;
  if (state.chatVoiceCapture) return false;
  if (mode !== VOICE_LIFECYCLE.LISTENING && isStopCapableLifecycle(mode)) return false;
  return true;
}

function beginConversationVoiceCapture() {
  const x = Math.floor(window.innerWidth / 2);
  const y = Math.floor(window.innerHeight / 2);
  void beginVoiceCapture(x, y, null);
}

function currentIndicatorMode() {
  const mode = state.voiceLifecycle;
  if (mode === VOICE_LIFECYCLE.RECORDING) return 'recording';
  if (mode === VOICE_LIFECYCLE.LISTENING) return 'listening';
  if (isStopCapableLifecycle(mode)) return 'play';
  if (state.conversationMode && state.hotwordActive) return 'paused';
  if (state.indicatorSuppressedByCanvasUpdate) return '';
  return '';
}

function shouldStopInUiClick() {
  return isStopCapableLifecycle(syncVoiceLifecycle('ui-stop-check'));
}

function updateAssistantActivityIndicator() {
  if (!hasLocalAssistantWork() && state.assistantRemoteActiveCount <= 0 && state.assistantRemoteQueuedCount <= 0) {
    state.assistantUnknownTurns = 0;
    state.assistantActiveTurns.clear();
  }
  syncVoiceLifecycle('indicator-update');
  state.hotwordActive = isHotwordActive();
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

function sidebarFileKindForPath(path) {
  const lower = String(path || '').toLowerCase();
  if (lower.endsWith('.pdf')) return 'pdf_artifact';
  for (const ext of SIDEBAR_IMAGE_EXTENSIONS) {
    if (lower.endsWith(ext)) return 'image_artifact';
  }
  return 'text_artifact';
}

function renderSidebarRow({ icon, label, active = false, meta = '', onClick }) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'pr-file-item';
  if (active) {
    button.classList.add('is-active');
  }

  const iconEl = document.createElement('span');
  iconEl.className = `chooser-icon icon-${icon}`;

  const labelEl = document.createElement('span');
  labelEl.className = 'pr-file-name';
  labelEl.textContent = String(label || '');

  button.appendChild(iconEl);
  button.appendChild(labelEl);
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
  entries.forEach((entry) => {
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
  state.fileSidebarMode = 'workspace';
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
  const mode = state.fileSidebarMode === 'pr' && state.prReviewMode ? 'pr' : 'workspace';
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
  renderWorkspaceFileList(list);
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
  if (!state.prReviewMode) {
    state.fileSidebarMode = 'workspace';
  }
  renderPrReviewFileList();
  try {
    const urls = [
      `/api/projects/${encodeURIComponent(projectID)}/files?path=${encodeURIComponent(requestedPath)}`,
    ];
    if (projectID.toLowerCase() !== 'active') {
      urls.push(`/api/projects/active/files?path=${encodeURIComponent(requestedPath)}`);
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
    const resp = await fetch(`/api/files/${encodeURIComponent(sid)}/${encodeURIComponent(filePath)}`, { cache: 'no-store' });
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

async function refreshWorkspaceBrowser(resetPath = false) {
  const nextPath = resetPath ? '' : state.workspaceBrowserPath;
  return loadWorkspaceBrowserPath(nextPath);
}

function stepWorkspaceFile(delta) {
  if (state.prReviewMode || !state.hasArtifact) return false;
  if (state.workspaceStepInFlight) return false;
  const shift = Number(delta);
  if (!Number.isFinite(shift) || shift === 0) return false;
  const files = workspaceNavigableFilePaths();
  if (files.length <= 1) return false;
  const currentFile = normalizeWorkspaceBrowserPath(state.workspaceOpenFilePath);
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
  state.assistantRemoteDelegateActiveCount = 0;
  state.assistantCancelInFlight = false;
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

async function fetchProjects() {
  const resp = await fetch('/api/projects', { cache: 'no-store' });
  if (!resp.ok) throw new Error(`projects list failed: HTTP ${resp.status}`);
  const payload = await resp.json();
  const projects = Array.isArray(payload?.projects) ? payload.projects : [];
  state.projects = projects.map((project) => ({
    ...project,
    id: String(project?.id || ''),
    chat_model_reasoning_effort: String(project?.chat_model_reasoning_effort || '').trim().toLowerCase(),
  })).filter((project) => project.id);
  state.defaultProjectId = String(payload?.default_project_id || '').trim();
  state.serverActiveProjectId = String(payload?.active_project_id || '').trim();
  renderEdgeTopProjects();
  renderEdgeTopModelButtons();
}

function upsertProject(project) {
  if (!project || !project.id) return;
  if (project.chat_model_reasoning_effort !== undefined) {
    project.chat_model_reasoning_effort = String(project.chat_model_reasoning_effort || '').trim().toLowerCase();
  }
  const index = state.projects.findIndex((item) => item.id === project.id);
  if (index >= 0) {
    state.projects[index] = project;
  } else {
    state.projects.push(project);
  }
  renderEdgeTopModelButtons();
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
    if (isHubProject(project)) {
      button.classList.add('edge-hub-btn');
    }
    if (project.id === state.activeProjectId) {
      button.classList.add('is-active');
    }
    button.textContent = String(project.name || project.id || 'Project');
    button.title = String(project.root_path || '');
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
    option.textContent = effort === 'extra_high' ? 'xhigh' : effort.replace(/_/g, ' ');
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

  const convButton = document.createElement('button');
  convButton.type = 'button';
  convButton.className = 'edge-project-btn edge-model-btn edge-conv-btn';
  convButton.textContent = 'conv';
  convButton.setAttribute('aria-pressed', state.conversationMode ? 'true' : 'false');
  if (state.conversationMode) {
    convButton.classList.add('is-active');
  }
  convButton.disabled = !ttsEnabled || state.projectSwitchInFlight || state.projectModelSwitchInFlight;
  convButton.addEventListener('click', () => {
    const next = !isConversationMode();
    const enabled = setConversationMode(next);
    applyConversationStateSnapshot();
    renderEdgeTopModelButtons();
    updateAssistantActivityIndicator();
    showStatus(enabled ? 'conversation mode on' : 'conversation mode off');
  });
  host.appendChild(convButton);

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
    const resp = await fetch(`/api/projects/${encodeURIComponent(project.id)}/chat-model`, {
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
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(targetSessionID)}/activity`, { cache: 'no-store' });
    if (!resp.ok) {
      if (!hasLocalAssistantWork() && !state.assistantCancelInFlight) {
        state.assistantRemoteActiveCount = 0;
        state.assistantRemoteQueuedCount = 0;
        state.assistantRemoteDelegateActiveCount = 0;
        updateAssistantActivityIndicator();
      }
      return;
    }
    if (targetSessionID !== state.chatSessionId) return;
    const payload = await resp.json();
    const activeTurns = Number(payload?.active_turns || 0);
    const queuedTurns = Number(payload?.queued_turns || 0);
    const delegateActive = Number(payload?.delegate_active || 0);
    if (!Number.isFinite(activeTurns) || activeTurns < 0) return;
    if (!Number.isFinite(queuedTurns) || queuedTurns < 0) return;
    if (!Number.isFinite(delegateActive) || delegateActive < 0) return;
    state.assistantRemoteActiveCount = activeTurns;
    state.assistantRemoteQueuedCount = queuedTurns;
    state.assistantRemoteDelegateActiveCount = delegateActive;
    if (activeTurns <= 0 && queuedTurns <= 0 && delegateActive <= 0 && !state.assistantCancelInFlight) {
      state.assistantActiveTurns.clear();
      state.assistantUnknownTurns = 0;
    }
    updateAssistantActivityIndicator();
  } catch (_) {
    if (!hasLocalAssistantWork() && !state.assistantCancelInFlight) {
      state.assistantRemoteActiveCount = 0;
      state.assistantRemoteQueuedCount = 0;
      state.assistantRemoteDelegateActiveCount = 0;
      updateAssistantActivityIndicator();
    }
  } finally {
    assistantActivityInFlight = false;
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
      cancelConversationListen();
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
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/chat/${encodeURIComponent(targetSessionID)}`;
  const ws = new WebSocket(wsUrl);
  ws.binaryType = 'arraybuffer';
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
    handleChatEvent(payload);
  };

  ws.onclose = () => {
    if (turnToken !== state.chatWsToken || targetSessionID !== state.chatSessionId) return;
    cancelConversationListen();
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
    } else if (actionType === 'toggle_conversation') {
      const next = !isConversationMode();
      const enabled = setConversationMode(next);
      applyConversationStateSnapshot();
      renderEdgeTopModelButtons();
      updateAssistantActivityIndicator();
      showStatus(enabled ? 'conversation mode on' : 'conversation mode off');
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
  }
}

async function switchProject(projectID) {
  const nextProjectID = String(projectID || '').trim();
  if (!nextProjectID) return;
  if (state.projectSwitchInFlight) return;
  if (nextProjectID === state.activeProjectId && state.chatSessionId) return;

  state.projectSwitchInFlight = true;
  showStatus('switching project...');
  cancelConversationListen();
  cancelChatVoiceCapture();
  closeChatWs();
  closeCanvasWs();
  clearChatHistory();
  clearCanvas();
  state.workspaceOpenFilePath = '';
  state.workspaceStepInFlight = false;
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

async function submitMessage(text, options = {}) {
  const trimmed = String(text || '').trim();
  if (!trimmed || !state.chatSessionId) return;
  cancelConversationListen();
  startVoiceLifecycleOp('submit-message');
  const submitKind = String(options?.kind || '').trim();
  let submitController = null;
  if (submitKind) {
    submitController = new AbortController();
    setPendingSubmit(submitController, submitKind);
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
  };
  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/messages`, {
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
  }
}

function forceVoiceLifecycleIdle(statusText = 'stopped') {
  cancelConversationListen();
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
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/cancel`, {
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
  if (isConversationListenActive()) {
    cancelConversationListen();
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'stop-listening');
    showStatus('ready');
    updateAssistantActivityIndicator();
    return;
  }

  const capture = state.chatVoiceCapture;
  if (capture && capture.stopping) {
    cancelChatVoiceCapture();
    return;
  }
  const isCaptureActive = Boolean(capture && !capture.stopping);
  if (isCaptureActive) {
    await stopVoiceCaptureAndSend();
    return;
  }

  if (isTTSSpeaking()) {
    stopTTSPlayback();
  }

  const localStopCapable = shouldStopInUiClick() || hasLocalStopCapableWork() || state.voiceAwaitingTurn;
  forceVoiceLifecycleIdle('stopped');
  if (!localStopCapable && !hasRemoteAssistantWork()) return;
  void cancelActiveAssistantTurnWithRetry(3, { silent: true }).finally(() => {
    void refreshAssistantActivity();
  });
}

function applyCanvasArtifactEvent(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind === 'clear_canvas') {
    state.prReviewAwaitingArtifact = false;
    state.workspaceOpenFilePath = '';
    state.workspaceStepInFlight = false;
    exitPrReviewMode();
    renderCanvas(payload);
    hideCanvasColumn();
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
    const resp = await fetch(`/api/canvas/${encodeURIComponent(sessionID)}/snapshot`);
    if (!resp.ok) {
      if (!state.hasArtifact) {
        exitPrReviewMode();
        clearCanvas();
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
    }
  } catch (_) {
    if (!state.hasArtifact) {
      exitPrReviewMode();
      clearCanvas();
    }
  }
}

// Edge panel logic
let edgeTopTimer = null;
let edgeRightTimer = null;
let edgeTouchStart = null;
const EDGE_TAP_SIZE_PX = 30;
const EDGE_TAP_SIZE_SMALL_PX = 30;
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
    state.fileSidebarMode = 'workspace';
    if (!state.workspaceBrowserLoading && state.workspaceBrowserEntries.length === 0 && !state.workspaceBrowserError) {
      void refreshWorkspaceBrowser(false);
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
    // Top edge
    if (ev.clientY < edgeTapSize && edgeTop && !edgeTop.classList.contains('edge-pinned')) {
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
    } else if (t.clientY < edgeTapSize) {
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
  const indicatorNode = document.getElementById('indicator');
  if (indicatorNode && indicatorNode.parentElement !== document.body) {
    document.body.appendChild(indicatorNode);
  }
  let lastMouseX = Math.floor(window.innerWidth / 2);
  let lastMouseY = Math.floor(window.innerHeight / 2);
  let hasLastMousePosition = false;
  const isInEdgeZone = (x, y) => {
    const s = getEdgeTapSizePx();
    return x < s || x > window.innerWidth - s || y < s || y > window.innerHeight - s;
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
  const beginVoiceCaptureFromPoint = (x, y, options = null) => {
    let anchor = null;
    if (state.hasArtifact && canvasText) {
      anchor = getAnchorFromPoint(x, y);
    }
    return beginVoiceCapture(x, y, anchor, options);
  };

  document.addEventListener('mousemove', (ev) => {
    rememberMousePosition(ev.clientX, ev.clientY);
  }, { passive: true });
  document.addEventListener('pointerdown', (ev) => {
    if (ev.pointerType !== 'mouse') return;
    rememberMousePosition(ev.clientX, ev.clientY);
  }, true);

  if (indicatorNode) {
    let lastIndicatorTouchAt = 0;
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
      const hitsChip = pointHitsIndicatorChip(x, y);
      if (!hitsChip && isTouch && shouldStopInUiClick() && isTapOnInteractiveUi(ev)) return;
      if (!hitsChip && !(isTouch && shouldStopInUiClick())) return;
      if (!isTouch && Date.now() - lastIndicatorTouchAt < 600) return;
      if (isTouch) lastIndicatorTouchAt = Date.now();
      ev.preventDefault();
      ev.stopPropagation();
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
      if (!isMobileViewport()) return;
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
      if (Math.abs(dx) < 48) return;
      if (Math.abs(dx) <= Math.abs(dy) * 1.25) return;
      stepCanvasFile(dx < 0 ? 1 : -1);
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
      stepCanvasFile(horizontalWheelAccum > 0 ? 1 : -1);
      horizontalWheelAccum = 0;
      horizontalWheelLastAt = now;
    }, { passive: false });
  }
  window.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true });
  window.addEventListener('resize', syncIndicatorOnViewportChange);

  if (clickTarget) {
    let mouseHoldTimer = null;
    let mouseHoldActive = false;
    let mouseHoldSuppressClick = false;
    let mouseHoldPointerId = null;
    let mouseHoldX = 0;
    let mouseHoldY = 0;
    let touchTapStartX = 0;
    let touchTapStartY = 0;
    let touchTapTracking = false;
    let touchTapMoved = false;
    const MOUSE_HOLD_MOVE_THRESHOLD = 5;
    const TOUCH_TAP_MOVE_THRESHOLD = 10;
    const clearMouseHoldTimer = () => {
      if (!mouseHoldTimer) return;
      clearTimeout(mouseHoldTimer);
      mouseHoldTimer = null;
    };
    const clearMouseHoldState = () => {
      clearMouseHoldTimer();
      mouseHoldActive = false;
      mouseHoldPointerId = null;
    };

    // Mouse hold behaves as push-to-talk: press to start, release to stop.
    // A short click still uses tap-to-talk via the click handler below.
    clickTarget.addEventListener('pointerdown', (ev) => {
      if (ev.pointerType !== 'mouse' || !ev.isPrimary || ev.button !== 0) return;
      if (isVoiceInteractionTarget(ev.target, ev.clientX, ev.clientY)) return;
      if (isConversationListenActive()) {
        cancelConversationListen();
      }
      if (isRecording() || shouldStopInUiClick()) return;
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;
      clearMouseHoldTimer();
      mouseHoldActive = false;
      mouseHoldPointerId = ev.pointerId;
      mouseHoldX = ev.clientX;
      mouseHoldY = ev.clientY;
      if (typeof clickTarget.setPointerCapture === 'function') {
        try { clickTarget.setPointerCapture(ev.pointerId); } catch (_) {}
      }
      mouseHoldTimer = window.setTimeout(() => {
        mouseHoldTimer = null;
        if (mouseHoldPointerId !== ev.pointerId || state.chatVoiceCapture) return;
        mouseHoldActive = true;
        // Releasing a successful hold emits a click; ignore that click so we
        // do not immediately toggle/cancel after manual stop.
        mouseHoldSuppressClick = true;
        void beginVoiceCaptureFromPoint(mouseHoldX, mouseHoldY, { manualStopOnly: true });
      }, CHAT_SEND_HOLD_MS);
    }, true);

    clickTarget.addEventListener('pointermove', (ev) => {
      if (!mouseHoldTimer || mouseHoldPointerId !== ev.pointerId) return;
      const dx = ev.clientX - mouseHoldX;
      const dy = ev.clientY - mouseHoldY;
      if (Math.sqrt(dx * dx + dy * dy) > MOUSE_HOLD_MOVE_THRESHOLD) {
        clearMouseHoldTimer();
        mouseHoldPointerId = null;
      }
    }, true);

    const stopMousePushToTalk = () => {
      if (!mouseHoldActive) return;
      mouseHoldActive = false;
      if (isRecording()) {
        void stopVoiceCaptureAndSend();
      }
    };
    const handleMousePointerRelease = (ev) => {
      if (mouseHoldPointerId !== null && mouseHoldPointerId !== ev.pointerId) return;
      if (typeof clickTarget.releasePointerCapture === 'function') {
        try { clickTarget.releasePointerCapture(ev.pointerId); } catch (_) {}
      }
      if (mouseHoldTimer) {
        clearMouseHoldTimer();
        mouseHoldPointerId = null;
        return;
      }
      stopMousePushToTalk();
      mouseHoldPointerId = null;
    };
    window.addEventListener('pointerup', handleMousePointerRelease, true);
    window.addEventListener('pointercancel', handleMousePointerRelease, true);
    window.addEventListener('blur', clearMouseHoldState);

    // Some mobile browsers do not consistently synthesize click for canvas taps.
    // Handle tap-to-talk / tap-to-stop directly on touchend.
    clickTarget.addEventListener('touchstart', (ev) => {
      if (ev.touches.length !== 1) {
        touchTapTracking = false;
        touchTapMoved = false;
        return;
      }
      const touch = ev.touches[0];
      touchTapStartX = touch.clientX;
      touchTapStartY = touch.clientY;
      touchTapTracking = true;
      touchTapMoved = false;
    }, { passive: true });

    clickTarget.addEventListener('touchmove', (ev) => {
      if (!touchTapTracking || touchTapMoved || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      const dx = touch.clientX - touchTapStartX;
      const dy = touch.clientY - touchTapStartY;
      if (Math.hypot(dx, dy) > TOUCH_TAP_MOVE_THRESHOLD) {
        touchTapMoved = true;
      }
    }, { passive: true });

    clickTarget.addEventListener('touchend', (ev) => {
      if (!touchTapTracking) return;
      touchTapTracking = false;
      if (touchTapMoved) {
        touchTapMoved = false;
        return;
      }
      const touch = ev.changedTouches && ev.changedTouches.length > 0 ? ev.changedTouches[0] : null;
      if (!touch) return;
      const x = touch.clientX;
      const y = touch.clientY;
      rememberMousePosition(x, y);

      if (isConversationListenActive()) {
        if (isVoiceInteractionTarget(ev.target, x, y)) return;
        ev.preventDefault();
        cancelConversationListen();
        void beginVoiceCaptureFromPoint(x, y);
        return;
      }
      if (shouldStopInUiClick()) {
        ev.preventDefault();
        void handleStopAction();
        return;
      }

      if (isVoiceInteractionTarget(ev.target, x, y)) return;
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;

      ev.preventDefault();
      if (isRecording()) {
        void stopVoiceCaptureAndSend();
        return;
      }
      void beginVoiceCaptureFromPoint(x, y);
    }, { passive: false });

    clickTarget.addEventListener('touchcancel', () => {
      touchTapTracking = false;
      touchTapMoved = false;
    }, { passive: true });

    clickTarget.addEventListener('click', (ev) => {
      if (mouseHoldSuppressClick) {
        mouseHoldSuppressClick = false;
        ev.preventDefault();
        return;
      }
      if (isConversationListenActive()) {
        if (isVoiceInteractionTarget(ev.target, ev.clientX, ev.clientY)) return;
        if (ev.button !== 0) return;
        const x = ev.clientX;
        const y = ev.clientY;
        rememberMousePosition(x, y);
        cancelConversationListen();
        void beginVoiceCaptureFromPoint(x, y);
        return;
      }
      if (shouldStopInUiClick()) {
        ev.preventDefault();
        void handleStopAction();
        return;
      }

      // Ignore clicks on interactive elements
      if (isVoiceInteractionTarget(ev.target, ev.clientX, ev.clientY)) return;
      // Ignore if right-click
      if (ev.button !== 0) return;
      // Ignore text selection
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;

      const x = ev.clientX;
      const y = ev.clientY;
      rememberMousePosition(x, y);

      if (isRecording()) {
        void stopVoiceCaptureAndSend();
        return;
      }

      void beginVoiceCaptureFromPoint(x, y);
    });
  }

  // Right-click -> text input
  if (clickTarget) {
    clickTarget.addEventListener('contextmenu', (ev) => {
      if (ev.target instanceof Element && ev.target.closest('.edge-panel')) return;
      ev.preventDefault();
      cancelConversationListen();
      let anchor = null;
      if (state.hasArtifact && canvasText) {
        anchor = getAnchorFromPoint(ev.clientX, ev.clientY);
      }
      showTextInput(ev.clientX, ev.clientY, anchor);
    });
  }

  // Text input Enter -> send
  const floatingInput = document.getElementById('floating-input');
  if (floatingInput instanceof HTMLTextAreaElement) {
    floatingInput.addEventListener('focus', () => {
      cancelConversationListen();
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
      cancelConversationListen();
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

    // Touch-hold PTT on chat pane input
    let chatInputHoldTimer = null;
    let chatInputHoldActive = false;
    let chatInputHoldX = 0;
    let chatInputHoldY = 0;
    const CHAT_INPUT_HOLD_MOVE_THRESHOLD = 5;

    chatPaneInput.addEventListener('touchstart', (ev) => {
      if (ev.touches.length !== 1) return;
      const t = ev.touches[0];
      chatInputHoldActive = false;
      chatInputHoldX = t.clientX;
      chatInputHoldY = t.clientY;
      chatInputHoldTimer = window.setTimeout(() => {
        chatInputHoldTimer = null;
        chatInputHoldActive = true;
        chatPaneInput.blur();
        void beginVoiceCaptureFromPoint(chatInputHoldX, chatInputHoldY, { manualStopOnly: true });
      }, CHAT_SEND_HOLD_MS);
    }, { passive: true });

    chatPaneInput.addEventListener('touchmove', (ev) => {
      if (!chatInputHoldTimer) return;
      if (ev.touches.length !== 1) return;
      const t = ev.touches[0];
      const dx = t.clientX - chatInputHoldX;
      const dy = t.clientY - chatInputHoldY;
      if (Math.sqrt(dx * dx + dy * dy) > CHAT_INPUT_HOLD_MOVE_THRESHOLD) {
        if (chatInputHoldTimer) { clearTimeout(chatInputHoldTimer); chatInputHoldTimer = null; }
      }
    }, { passive: true });

    window.addEventListener('touchend', () => {
      if (chatInputHoldTimer) { clearTimeout(chatInputHoldTimer); chatInputHoldTimer = null; return; }
      if (chatInputHoldActive) {
        chatInputHoldActive = false;
        if (isRecording()) void stopVoiceCaptureAndSend();
      }
    }, { passive: true });

    window.addEventListener('touchcancel', () => {
      if (chatInputHoldTimer) { clearTimeout(chatInputHoldTimer); chatInputHoldTimer = null; }
      chatInputHoldActive = false;
    });
  }

  // Voice tap on chat history (only when panel is pinned, not just hover-active)
  const chatHistory = document.getElementById('chat-history');
  if (chatHistory) {
    chatHistory.addEventListener('click', (ev) => {
      if (ev.button !== 0) return;
      if (ev.target instanceof Element && ev.target.closest('a,button,input,textarea,select,[contenteditable="true"]')) return;
      if (isInEdgeZone(ev.clientX, ev.clientY)) return;
      const edgeR = chatHistory.closest('.edge-panel');
      if (edgeR && !edgeR.classList.contains('edge-pinned')) return;
      if (isConversationListenActive()) {
        cancelConversationListen();
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

    // Control long-press for PTT
    if (ev.key === 'Control' && !ev.repeat) {
      if (state.chatCtrlHoldTimer || state.chatVoiceCapture) return;
      if (isConversationListenActive()) {
        cancelConversationListen();
      }
      state.chatCtrlHoldTimer = window.setTimeout(() => {
        state.chatCtrlHoldTimer = null;
        const point = getCtrlVoiceCapturePoint();
        void beginVoiceCaptureFromPoint(point.x, point.y, { manualStopOnly: true });
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
        cancelConversationListen();
        cpInput.focus();
        cpInput.value = ev.key;
        const caret = ev.key.length;
        cpInput.setSelectionRange(caret, caret);
        cpInput.dispatchEvent(new Event('input', { bubbles: true }));
        ev.preventDefault();
        return;
      }
      const cx = window.innerWidth / 2 - 130;
      const cy = window.innerHeight / 2;
      cancelConversationListen();
      showTextInput(cx, cy, null);
      // Forward the keystroke
      const input = document.getElementById('floating-input');
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
        void beginVoiceCaptureFromPoint(artHoldX, artHoldY);
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
        void stopVoiceCaptureAndSend();
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
  splash.className = 'splash';
  splash.textContent = name;
  document.getElementById('view-main')?.appendChild(splash);
  window.setTimeout(() => splash.classList.add('fade-out'), 100);
  window.setTimeout(() => splash.remove(), 1700);
}

async function init() {
  applyIPhoneFrameCorners();
  window.addEventListener('resize', () => {
    if (document.body.classList.contains('keyboard-open')) return;
    applyIPhoneFrameCorners();
  });
  bindUi();
  updateAssistantActivityIndicator();
  startDevReloadWatcher();
  startAssistantActivityWatcher();
  clearCanvas();
  hideCanvasColumn();
  showStatus('starting...');

  // Check TTS availability from runtime
  try {
    const runtime = await fetchRuntimeMeta();
    ttsEnabled = Boolean(runtime?.tts_enabled);
    applyRuntimeReasoningEffortOptions(runtime?.available_reasoning_efforts);
  } catch (_) {
    ttsEnabled = false;
  }
  setTTSSilentMode(readTTSSilentPreference(), { persist: false, pinPanel: false });
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
  showSplash();
  // Enable panel slide transitions only after startup is fully painted.
  requestAnimationFrame(() => requestAnimationFrame(initPanelMotionMode));
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
