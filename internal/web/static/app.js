import { marked } from './vendor/marked.esm.js';
import { renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml } from './canvas.js';
import {
  getZenState, setZenMode,
  showIndicatorMode, hideIndicator,
  showTextInput, hideTextInput,
  showOverlay, hideOverlay, updateOverlay,
  isOverlayVisible, isTextInputVisible, isRecording, setRecording,
  getInputAnchor, setInputAnchor, getAnchorFromPoint,
  buildContextPrefix, getLastInputPosition, setLastInputPosition,
} from './zen.js';
import {
  configureConversation,
  isConversationMode,
  setConversationMode,
  onTTSPlaybackComplete,
  cancelConversationListen,
  isConversationListenActive,
} from './conversation.js';

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
  voiceAwaitingTurn: false,
  voiceTurns: new Set(),
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
  // Zen-specific: track if a canvas action happened during this turn
  zenCanvasActionThisTurn: false,
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
let devReloadBootID = '';
let devReloadTimer = null;
let devReloadInFlight = false;
let devReloadRequested = false;
let assistantActivityTimer = null;
let assistantActivityInFlight = false;

const ACTIVE_PROJECT_STORAGE_KEY = 'tabura.activeProjectId';
const LAST_VIEW_STORAGE_KEY = 'tabura.lastView';
const PROJECT_CHAT_MODEL_ALIASES = ['codex', 'gpt', 'spark'];
const PROJECT_CHAT_MODEL_REASONING_EFFORTS = {
  codex: ['low', 'medium', 'high', 'extra_high'],
  gpt: ['low', 'medium', 'high', 'extra_high'],
  spark: ['low', 'medium', 'high', 'extra_high'],
};
const TTS_SILENT_STORAGE_KEY = 'tabura.ttsSilent';

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

// Extract speakable text for TTS (everything except blocks).
function extractTTSText(markdown) {
  let text = stripBlocks(markdown);
  let lang = '';
  text = text.replace(_langTagRe, (_, l) => { if (!lang) lang = l.toLowerCase(); return ''; });
  text = stripMarkdownForSpeech(text);
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
}

function isMobileSilent() {
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
    root.style.removeProperty('--zen-cue-corner-radius');
    return;
  }
  const short = Math.min(Math.round(screen.width), Math.round(screen.height));
  const long = Math.max(Math.round(screen.width), Math.round(screen.height));
  const dpr = Math.max(1, Math.round(devicePixelRatio || 1));
  const match = IPHONE_CORNER_RADIUS_PROFILES.find(
    (p) => p.shortSide === short && p.longSide === long && p.dpr === dpr,
  );
  const r = match ? match.radius : (dpr >= 3 ? 55 : 44);
  root.style.setProperty('--zen-cue-corner-radius', `0 0 ${r}px ${r}px`);
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

function setTTSSilentMode(silent, { persist = true } = {}) {
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
    if (window.matchMedia('(max-width: 767px)').matches) {
      const edgeRight = document.getElementById('edge-right');
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    }
  } else {
    document.body.classList.remove('silent-mode');
  }
  renderEdgeTopModelButtons();
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
}

configureConversation({
  canStartConversationListen,
  onConversationListenStateChange: (snapshot) => {
    applyConversationStateSnapshot(snapshot);
    renderEdgeTopModelButtons();
    updateAssistantActivityIndicator();
  },
  onConversationListenTimeout: () => {
    if (!state.chatVoiceCapture && !state.voiceAwaitingTurn && !isAssistantWorking() && !isTTSSpeaking()) {
      showStatus('ready');
    }
  },
  onConversationSpeechDetected: () => {
    beginConversationVoiceCapture();
  },
  onConversationListenCancelled: () => {
    if (!state.chatVoiceCapture && !state.voiceAwaitingTurn && !isAssistantWorking() && !isTTSSpeaking()) {
      showStatus('ready');
    }
  },
  getAudioContext: () => ttsAudioCtx,
  acquireMicStream,
  computeDecibelFromTimeDomain,
});
applyConversationStateSnapshot();

function ensureTTSChunker() {
  if (ttsSentenceChunker) return;
  ttsPlayer = new TTSPlayer();
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
    audio: { ...MIC_CAPTURE_CONSTRAINTS },
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

function releaseMicStream({ force = false } = {}) {
  if (!_cachedMicStream) return;
  const activeCapture = state.chatVoiceCapture;
  if (!force && activeCapture && activeCapture.mediaStream === _cachedMicStream && !activeCapture.stopping) {
    return;
  }
  _cachedMicStream.getTracks().forEach((t) => t.stop());
  _cachedMicStream = null;
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
    sttCancel();
    stopChatVoiceMedia(capture);
    if (state.chatVoiceCapture === capture) {
      state.chatVoiceCapture = null;
    }
    updateAssistantActivityIndicator();
    window.setTimeout(() => {
      if (!state.chatVoiceCapture && !isAssistantWorking() && !isTTSSpeaking()) {
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
        void stopZenVoiceCaptureAndSend();
        return;
      }

      if (hitCandidate) {
        if (!options.pendingCommitAtMs) {
          options.pendingCommitAtMs = now + VOICE_VAD_CANDIDATE_RECHECK_MS;
          return;
        }
        if (now >= options.pendingCommitAtMs) {
          stopVADMonitor(capture);
          void stopZenVoiceCaptureAndSend();
        }
        return;
      }

      if (options.pendingCommitAtMs) {
        options.pendingCommitAtMs = 0;
      }
    };

    const timer = window.setInterval(update, VOICE_VAD_FRAME_MS);
    capture.vadState = { source, analyser, timer, options, bins, isRunning: true };
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
      recorder.stop();
    } catch (_) {
      finish();
      return;
    }
    timeoutId = window.setTimeout(finish, VOICE_CAPTURE_STOP_FLUSH_TIMEOUT_MS);
  });
}

async function beginZenVoiceCapture(x, y, anchor, options = null) {
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
      void stopZenVoiceCaptureAndSend();
    }
  } catch (err) {
    setRecording(false);
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

async function stopZenVoiceCaptureAndSend() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  capture.stopRequested = true;
  if (!capture.active) return;
  capture.stopping = true;
  setRecording(false);
  state.voiceAwaitingTurn = true;
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
      throw new Error('speech recognizer returned empty text');
    }
    showStatus('sending...');
    void zenSubmitMessage(transcript, { kind: 'voice_transcript' });
  } catch (err) {
    state.voiceAwaitingTurn = false;
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
  setZenMode('artifact');
  persistLastView({ mode: 'artifact' });
  if (!isVoiceTurn() && isDirectAssistantWorking()) {
    hideOverlay();
  }
  updateAssistantActivityIndicator();
}

function hideCanvasColumn() {
  exitPrReviewMode();
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

function isDirectAssistantWorking() {
  return hasLocalAssistantWork()
    || state.assistantRemoteActiveCount > 0
    || state.assistantRemoteQueuedCount > 0
    || state.assistantCancelInFlight;
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

function canStartConversationListen() {
  if (!canSpeakTTS()) return false;
  if (isRecording()) return false;
  if (state.chatVoiceCapture) return false;
  if (state.voiceAwaitingTurn) return false;
  if (isAssistantWorking()) return false;
  if (isTTSSpeaking()) return false;
  return true;
}

function beginConversationVoiceCapture() {
  const x = Math.floor(window.innerWidth / 2);
  const y = Math.floor(window.innerHeight / 2);
  void beginZenVoiceCapture(x, y, null);
}

function currentIndicatorMode() {
  if (isRecording()) return 'recording';
  if (state.voiceAwaitingTurn) return 'play';
  if (isConversationListenActive()) return 'listening';
  if (state.indicatorSuppressedByCanvasUpdate) return '';
  if (isAssistantWorking() || isTTSSpeaking()) return 'play';
  return '';
}

function shouldStopInUiClick() {
  return state.voiceAwaitingTurn || isAssistantWorking() || isTTSSpeaking() || isConversationListenActive();
}

function updateAssistantActivityIndicator() {
  if (!hasLocalAssistantWork() && state.assistantRemoteActiveCount <= 0 && state.assistantRemoteQueuedCount <= 0) {
    state.assistantUnknownTurns = 0;
    state.assistantActiveTurns.clear();
  }
  const pos = getLastInputPosition();
  const px = Number.isFinite(pos?.x) && pos.x > 0 ? pos.x : Math.floor(window.innerWidth / 2);
  const py = Number.isFinite(pos?.y) && pos.y > 0 ? pos.y : Math.floor(window.innerHeight / 2);
  const mode = currentIndicatorMode();
  if (mode) {
    showIndicatorMode(mode, px, py);
  } else {
    hideIndicator();
  }
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

function setPrReviewDrawerOpen(open) {
  const shouldOpen = Boolean(open);
  state.prReviewDrawerOpen = shouldOpen;
  const pane = document.getElementById('pr-file-pane');
  const backdrop = document.getElementById('pr-file-drawer-backdrop');
  if (pane) pane.classList.toggle('is-open', shouldOpen);
  if (backdrop) backdrop.classList.toggle('is-open', shouldOpen);
}

function resetPrReviewUi() {
  document.body.classList.remove('pr-review-mode');
  setPrReviewDrawerOpen(false);
  const title = document.getElementById('pr-file-pane-title');
  if (title) title.textContent = 'Files';
  const list = document.getElementById('pr-file-list');
  if (list) list.innerHTML = '';
}

function renderPrReviewFileList() {
  const list = document.getElementById('pr-file-list');
  if (!(list instanceof HTMLElement)) return;
  const title = document.getElementById('pr-file-pane-title');
  const files = Array.isArray(state.prReviewFiles) ? state.prReviewFiles : [];
  if (title) {
    if (files.length > 0) {
      const prefix = state.prReviewPRNumber ? `PR #${state.prReviewPRNumber}` : 'PR Review';
      title.textContent = `${prefix} (${state.prReviewActiveIndex + 1}/${files.length})`;
    } else {
      title.textContent = 'Files';
    }
  }
  list.innerHTML = '';
  files.forEach((file, index) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'pr-file-item';
    if (index === state.prReviewActiveIndex) {
      button.classList.add('is-active');
    }
    button.setAttribute('aria-label', String(file?.path || `File ${index + 1}`));
    button.dataset.index = String(index);

    const status = document.createElement('span');
    const statusName = String(file?.status || 'modified').toLowerCase();
    status.className = `pr-file-status status-${statusName}`;
    status.textContent = statusBadgeForDiffFile(statusName);

    const label = document.createElement('span');
    label.className = 'pr-file-name';
    label.textContent = String(file?.path || `(file ${index + 1})`);

    button.appendChild(status);
    button.appendChild(label);
    button.addEventListener('click', () => {
      setPrReviewActiveFile(index);
      if (isMobileViewport()) {
        setPrReviewDrawerOpen(false);
      }
    });
    list.appendChild(button);
  });
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
    if (isHubProject(project)) {
      continue;
    }
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

function renderEdgeTopModelButtons() {
  const host = document.getElementById('edge-top-models');
  if (!(host instanceof HTMLElement)) return;
  host.innerHTML = '';
  const project = activeProject();
  const hub = hubProject();
  const hubActive = isHubActive();
  const selectedAlias = activeProjectChatModelAlias();
  const selectedEffort = activeProjectChatModelReasoningEffort();
  const effortOptions = reasoningEffortOptionsForAlias(selectedAlias);
  if (hub && hub.id) {
    const hubButton = document.createElement('button');
    hubButton.type = 'button';
    hubButton.className = 'edge-project-btn edge-model-btn edge-hub-btn';
    hubButton.textContent = 'Hub';
    if (hubActive) {
      hubButton.classList.add('is-active');
    }
    hubButton.disabled = state.projectSwitchInFlight || state.projectModelSwitchInFlight;
    hubButton.addEventListener('click', () => {
      void switchToHub();
    });
    host.appendChild(hubButton);
  }
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
    option.textContent = effort.replace(/_/g, ' ');
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
    if (!resp.ok) return;
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
    if (document.hidden) {
      cancelConversationListen();
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
    if (event.data instanceof ArrayBuffer) {
      if (!canSpeakTTS()) return;
      if (ttsPlayer) ttsPlayer.enqueue(event.data);
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
      const enabled = setConversationMode(!isConversationMode());
      applyConversationStateSnapshot();
      renderEdgeTopModelButtons();
      updateAssistantActivityIndicator();
      showStatus(enabled ? 'conversation mode on' : 'conversation mode off');
    }
    return;
  }

  if (type === 'turn_started') {
    const turnID = String(payload.turn_id || '').trim();
    const turnIsVoice = state.voiceAwaitingTurn || isVoiceTurn();
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
    state.zenCanvasActionThisTurn = false;
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
      getZenState().overlayTurnId = payload.turn_id || null;
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
      return;
    }

    // First non-empty response: show on canvas (silent) / speak (voice)
    const trimmedMd = String(md || '').trim();
    if (trimmedMd && !state.turnFirstResponseShown) {
      state.turnFirstResponseShown = true;
      if (isMobileSilent()) {
        renderCanvas({ kind: 'text_artifact', title: '', text: md });
      }
      if (isVoiceTurn() && canSpeakTTS()) {
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
    const shouldSpeakTurn = turnID ? state.voiceTurns.has(turnID) : false;
    trackAssistantTurnFinished(turnID);
    state.assistantLastError = '';
    showStatus('ready');
    updateAssistantActivityIndicator();
    void refreshAssistantActivity();

    if (shouldSpeakTurn && !autoCanvas && canSpeakTTS() && md.trim()) {
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
      if (state.zenCanvasActionThisTurn) {
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
      state.zenCanvasActionThisTurn = false;
      return;
    }
    if (!isVoiceTurn()) {
      if (autoCanvas || state.hasArtifact) {
        hideOverlay();
        state.zenCanvasActionThisTurn = false;
        return;
      }
      const cleaned = cleanForOverlay(md);
      if (state.zenCanvasActionThisTurn && !cleaned) {
        hideOverlay();
      } else if (cleaned) {
        updateOverlay(cleaned);
      } else {
        hideOverlay();
      }
    }
    state.zenCanvasActionThisTurn = false;
    return;
  }

  if (type === 'item_completed') {
    const turnID = String(payload.turn_id || '').trim();
    const line = formatItemCompletedLabel(payload);
    appendAssistantProgressForTurn(turnID, line);
    return;
  }

  if (type === 'turn_cancelled') {
    state.voiceAwaitingTurn = false;
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

async function zenSubmitMessage(text, options = {}) {
  const trimmed = String(text || '').trim();
  if (!trimmed || !state.chatSessionId) return;
  cancelConversationListen();
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

async function cancelActiveAssistantTurn(options = null) {
  const force = Boolean(options && options.force);
  if (!state.chatSessionId || state.assistantCancelInFlight) return false;
  if (!force) {
    await refreshAssistantActivity();
    if (!isAssistantWorking()) {
      showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
      updateAssistantActivityIndicator();
      return false;
    }
  }
  state.assistantCancelInFlight = true;
  updateAssistantActivityIndicator();
  showStatus('stopping...');
  let canceled = 0;
  try {
    const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(state.chatSessionId)}/cancel`, { method: 'POST' });
    if (!resp.ok) {
      const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
      showStatus(`stop failed: ${detail}`);
      return false;
    }
    const payload = await resp.json();
    canceled = Number(payload?.canceled || 0);
    if (canceled <= 0) {
      await refreshAssistantActivity();
      if (!isAssistantWorking()) {
        showStatus(state.assistantLastError ? state.assistantLastError : 'idle');
      }
    }
  } catch (err) {
    showStatus(`stop failed: ${String(err?.message || err)}`);
    return false;
  } finally {
    state.assistantCancelInFlight = false;
    updateAssistantActivityIndicator();
    window.setTimeout(() => { void refreshAssistantActivity(); }, 120);
  }
  return canceled > 0;
}

async function cancelActiveAssistantTurnWithRetry(maxAttempts = 3) {
  const attempts = Number.isFinite(maxAttempts) ? Math.max(1, Math.floor(maxAttempts)) : 1;
  for (let i = 0; i < attempts; i += 1) {
    const canceled = await cancelActiveAssistantTurn({ force: true });
    if (canceled) return true;
    await refreshAssistantActivity();
    if (!isAssistantWorking()) return false;
    if (i + 1 < attempts) {
      await new Promise((resolve) => window.setTimeout(resolve, 140));
    }
  }
  return false;
}

async function handleZenStopAction() {
  if (isConversationListenActive()) {
    cancelConversationListen();
    if (!state.chatVoiceCapture && !state.voiceAwaitingTurn && !isAssistantWorking() && !isTTSSpeaking()) {
      showStatus('ready');
    }
    return;
  }

  const capture = state.chatVoiceCapture;
  if (capture && capture.stopping) {
    cancelChatVoiceCapture();
    return;
  }
  const isCaptureActive = Boolean(capture && !capture.stopping);
  if (isCaptureActive) {
    await stopZenVoiceCaptureAndSend();
    return;
  }

  if (isTTSSpeaking()) {
    stopTTSPlayback();
  }

  const hadVoiceAwaitingTurn = state.voiceAwaitingTurn;
  if (hadVoiceAwaitingTurn) {
    abortPendingSubmit('voice_transcript');
    state.voiceAwaitingTurn = false;
    sttCancel();
    updateAssistantActivityIndicator();
  }

  const canceled = await cancelActiveAssistantTurnWithRetry(3);
  if (canceled) return;

  if (hadVoiceAwaitingTurn) return;

  if (capture) {
    sttCancel();
    updateAssistantActivityIndicator();
  }
}

function applyCanvasArtifactEvent(payload) {
  const kind = String(payload?.kind || '').trim().toLowerCase();
  if (kind === 'clear_canvas') {
    state.prReviewAwaitingArtifact = false;
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
  state.zenCanvasActionThisTurn = state.zenCanvasActionThisTurn || realCanvasArtifact;
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
const EDGE_TAP_SIZE_PX = 20;
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
  return topOpen || rightOpen;
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
    edgeLeftTap.addEventListener('click', (ev) => {
      ev.preventDefault();
      handleRasaEdgeTap();
    });
  }

  const edgeRightTap = document.getElementById('edge-right-tap');
  if (edgeRightTap) {
    edgeRightTap.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    });
    // Direct touch handler: iOS system gesture recognizer can intercept
    // document-level touch events near screen edges. Handle on the button
    // itself with touch-action:manipulation to bypass system gestures.
    edgeRightTap.addEventListener('touchend', (ev) => {
      ev.preventDefault();
      if (edgeRight) edgeRight.classList.add('edge-pinned');
    }, { passive: false });
  }

  const prDrawerToggle = document.getElementById('pr-file-drawer-toggle');
  if (prDrawerToggle) {
    prDrawerToggle.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (!state.prReviewMode) return;
      setPrReviewDrawerOpen(!state.prReviewDrawerOpen);
    });
  }
  const prDrawerBackdrop = document.getElementById('pr-file-drawer-backdrop');
  if (prDrawerBackdrop) {
    prDrawerBackdrop.addEventListener('click', () => {
      setPrReviewDrawerOpen(false);
    });
  }
  const prPaneClose = document.getElementById('pr-file-pane-close');
  if (prPaneClose) {
    prPaneClose.addEventListener('click', () => {
      setPrReviewDrawerOpen(false);
    });
  }

  // Mobile: touch tap and swipe from edges.
  // Buttons don't reliably fire click on iOS, so handle everything here.
  let edgeTouchHandled = false;
  document.addEventListener('touchstart', (ev) => {
    if (ev.touches.length !== 1) return;
    const t = ev.touches[0];
    const edgeTapSize = getEdgeTapSizePx();
    edgeTouchHandled = false;
    if (t.clientX > window.innerWidth - edgeTapSize) {
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
    if (edgeTouchStart.edge === 'right' && dx < -30 && edgeRight) {
      edgeRight.classList.add('edge-active');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'top' && dy > 30 && edgeTop) {
      edgeTop.classList.add('edge-active');
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
        switch (edgeTouchStart.edge) {
          case 'bottom': handleRasaEdgeTap(); break;
          case 'right': if (edgeRight) edgeRight.classList.add('edge-pinned'); break;
          case 'top': if (edgeTop) edgeTop.classList.add('edge-pinned'); break;
        }
        // Prevent iOS from synthesizing a click after edge tap — the
        // panel pin above can cause the click to land inside the
        // newly-visible panel (e.g. chatHistory) and start recording.
        ev.preventDefault();
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
          root.style.setProperty('--zen-cue-corner-radius', '0 0 0 0');
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
  const zenIndicator = document.getElementById('zen-indicator');
  if (zenIndicator && zenIndicator.parentElement !== document.body) {
    document.body.appendChild(zenIndicator);
  }
  let lastMouseX = Math.floor(window.innerWidth / 2);
  let lastMouseY = Math.floor(window.innerHeight / 2);
  let hasLastMousePosition = false;
  const isInEdgeZone = (x, y) => {
    const s = getEdgeTapSizePx();
    return x > window.innerWidth - s || y < s || y > window.innerHeight - s;
  };
  const isVoiceInteractionTarget = (target, x, y) => (
    isInEdgeZone(x, y)
    || (target instanceof Element
      && target.closest('button,a,input,textarea,select,[contenteditable="true"],.zen-overlay,.zen-input,.edge-panel,#canvas-pdf .canvas-pdf-page,#canvas-pdf .textLayer,#canvas-pdf .annotationLayer'))
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
    return beginZenVoiceCapture(x, y, anchor, options);
  };

  document.addEventListener('mousemove', (ev) => {
    rememberMousePosition(ev.clientX, ev.clientY);
  }, { passive: true });
  document.addEventListener('pointerdown', (ev) => {
    if (ev.pointerType !== 'mouse') return;
    rememberMousePosition(ev.clientX, ev.clientY);
  }, true);

  if (zenIndicator) {
    let lastIndicatorTouchAt = 0;
    const isIndicatorArmed = () => (
      zenIndicator.classList.contains('is-working')
      || zenIndicator.classList.contains('is-recording')
      || zenIndicator.classList.contains('is-listening')
    );
    const pointHitsIndicatorChip = (x, y) => {
      const chips = zenIndicator.querySelectorAll('.zen-record-dot, .zen-play-icon');
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
    const handleZenIndicatorTap = (ev, x, y, isTouch = false) => {
      if (!isIndicatorArmed()) return;
      if (!pointHitsIndicatorChip(x, y)) return;
      if (!isTouch && Date.now() - lastIndicatorTouchAt < 600) return;
      if (isTouch) lastIndicatorTouchAt = Date.now();
      ev.preventDefault();
      ev.stopPropagation();
      void handleZenStopAction();
    };
    document.addEventListener('click', (ev) => {
      handleZenIndicatorTap(ev, ev.clientX, ev.clientY, false);
    }, true);
    document.addEventListener('touchend', (ev) => {
      const touch = ev.changedTouches && ev.changedTouches.length > 0 ? ev.changedTouches[0] : null;
      if (!touch) return;
      handleZenIndicatorTap(ev, touch.clientX, touch.clientY, true);
    }, { passive: false, capture: true });
  }

  // Zen: Left-click/tap on canvas -> toggle voice recording
  const zenClickTarget = canvasViewport || document.getElementById('workspace');
  const syncIndicatorOnViewportChange = () => {
    updateAssistantActivityIndicator();
  };
  if (canvasViewport instanceof HTMLElement) {
    canvasViewport.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true, capture: true });
    let prSwipeStart = null;
    let prSwipeHandled = false;
    const resetPrSwipe = () => {
      prSwipeStart = null;
      prSwipeHandled = false;
    };
    canvasViewport.addEventListener('touchstart', (ev) => {
      if (!state.prReviewMode || !isMobileViewport()) return;
      if (state.prReviewDrawerOpen || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      prSwipeStart = { x: touch.clientX, y: touch.clientY };
      prSwipeHandled = false;
    }, { passive: true });
    canvasViewport.addEventListener('touchmove', (ev) => {
      if (!prSwipeStart || prSwipeHandled || !state.prReviewMode || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      const dx = touch.clientX - prSwipeStart.x;
      const dy = touch.clientY - prSwipeStart.y;
      if (Math.abs(dx) < 48) return;
      if (Math.abs(dx) <= Math.abs(dy) * 1.25) return;
      const moved = stepPrReviewFile(dx < 0 ? 1 : -1);
      if (!moved) return;
      prSwipeHandled = true;
      ev.preventDefault();
    }, { passive: false });
    canvasViewport.addEventListener('touchend', resetPrSwipe, { passive: true });
    canvasViewport.addEventListener('touchcancel', resetPrSwipe, { passive: true });
  }
  window.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true });
  window.addEventListener('resize', syncIndicatorOnViewportChange);

  if (zenClickTarget) {
    let mouseHoldTimer = null;
    let mouseHoldActive = false;
    let mouseHoldSuppressClick = false;
    let mouseHoldPointerId = null;
    let mouseHoldX = 0;
    let mouseHoldY = 0;
    const MOUSE_HOLD_MOVE_THRESHOLD = 5;
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
    zenClickTarget.addEventListener('pointerdown', (ev) => {
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
      if (typeof zenClickTarget.setPointerCapture === 'function') {
        try { zenClickTarget.setPointerCapture(ev.pointerId); } catch (_) {}
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

    zenClickTarget.addEventListener('pointermove', (ev) => {
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
        void stopZenVoiceCaptureAndSend();
      }
    };
    const handleMousePointerRelease = (ev) => {
      if (mouseHoldPointerId !== null && mouseHoldPointerId !== ev.pointerId) return;
      if (typeof zenClickTarget.releasePointerCapture === 'function') {
        try { zenClickTarget.releasePointerCapture(ev.pointerId); } catch (_) {}
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

    zenClickTarget.addEventListener('click', (ev) => {
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
        void handleZenStopAction();
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
        void stopZenVoiceCaptureAndSend();
        return;
      }

      void beginVoiceCaptureFromPoint(x, y);
    });
  }

  // Zen: Right-click -> text input
  if (zenClickTarget) {
    zenClickTarget.addEventListener('contextmenu', (ev) => {
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

  // Zen: Text input Enter -> send
  const zenInput = document.getElementById('zen-input');
  if (zenInput instanceof HTMLTextAreaElement) {
    zenInput.addEventListener('focus', () => {
      cancelConversationListen();
    });
    zenInput.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        const text = zenInput.value.trim();
        if (text) {
          state.lastInputOrigin = 'text';
          zenInput.value = '';
          zenInput.blur();
          hideTextInput();
          settleKeyboardAfterSubmit();
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
          void zenSubmitMessage(text);
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
        if (isRecording()) void stopZenVoiceCaptureAndSend();
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
      if (shouldStopInUiClick()) { void handleZenStopAction(); return; }
      if (isRecording()) { void stopZenVoiceCaptureAndSend(); return; }
      void beginVoiceCaptureFromPoint(ev.clientX, ev.clientY);
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
      if (state.prReviewMode && state.prReviewDrawerOpen) {
        setPrReviewDrawerOpen(false);
        return;
      }
      closeEdgePanels();
      if (state.hasArtifact) {
        clearCanvas();
        hideCanvasColumn();
        return;
      }
      void handleZenStopAction();
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

    if (state.prReviewMode) {
      if (ev.key === 'ArrowRight' || ev.key === 'j' || ev.key === 'J') {
        ev.preventDefault();
        stepPrReviewFile(1);
        return;
      }
      if (ev.key === 'ArrowLeft' || ev.key === 'k' || ev.key === 'K') {
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
  navigator.mediaDevices.getUserMedia({ audio: { ...MIC_CAPTURE_CONSTRAINTS } })
    .then((stream) => {
      stream.getTracks().forEach((track) => track.stop());
    })
    .catch(() => {});
}

async function init() {
  applyIPhoneFrameCorners();
  window.addEventListener('resize', () => {
    if (document.body.classList.contains('keyboard-open')) return;
    applyIPhoneFrameCorners();
  });
  bindUi();
  warmMicStream();
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
  setTTSSilentMode(readTTSSilentPreference(), { persist: false });

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
