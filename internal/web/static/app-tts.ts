import * as env from './app-env.js';
import * as context from './app-context.js';
import { sendTurnPlaybackProgress } from './turn-client.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, resumeDialogueListen, setDialogueTTSBargeInMode, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const renderEdgeTopModelButtons = (...args) => refs.renderEdgeTopModelButtons(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const beginConversationVoiceCapture = (...args) => refs.beginConversationVoiceCapture(...args);
const acquireMicStream = (...args) => refs.acquireMicStream(...args);
const handleStopAction = (...args) => refs.handleStopAction(...args);
const isStopCapableLifecycle = (...args) => refs.isStopCapableLifecycle(...args);
const syncVoiceLifecycle = (...args) => refs.syncVoiceLifecycle(...args);
const parseOptionalBoolean = (...args) => refs.parseOptionalBoolean(...args);
const normalizeCompanionRuntimeState = (...args) => refs.normalizeCompanionRuntimeState(...args);
const getTTSAudioContext = (...args) => refs.getTTSAudioContext(...args);
const interactionConversationMode = (...args) => refs.interactionConversationMode(...args);

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
export function stripBlocks(text) {
  text = text.replace(_canvasFileBlockRe, ' ');
  text = text.replace(_partialBlockRe, ' ');
  text = text.replace(_canvasFileMarkerRefRe, ' ');
  text = text.replace(_canvasDirectiveOpenRe, ' ');
  text = text.replace(_canvasDirectiveCloseRe, ' ');
  return text;
}

export function stripMarkdownForSpeech(text) {
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
export function cleanForOverlay(markdown) {
  return stripBlocks(markdown).replace(_langTagRe, '').trim();
}

export function inferTTSLanguage(text) {
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
    'was', 'wie', 'wer', 'wo', 'wann', 'warum', 'wieso', 'weshalb',
    'welche', 'welcher', 'welches', 'mir', 'mein', 'meine', 'einen', 'einem',
    'einer', 'spaet', 'spät', 'uhr', 'zeichne', 'erklaere', 'erkläre',
  ]);
  let hits = 0;
  for (const token of tokens) {
    if (germanHints.has(token)) hits += 1;
  }
  if (hits >= 2 && hits / tokens.length >= 0.08) return 'de';
  if (tokens.length <= 6 && hits >= 1 && ['was', 'wie', 'wer', 'wo', 'wann', 'warum', 'wieso', 'weshalb'].includes(tokens[0])) {
    return 'de';
  }
  return '';
}

// Extract speakable text for TTS (everything except blocks).
export function extractTTSText(markdown) {
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


export class SentenceChunker {
  _buffer: string;
  _onSentence: any;
  _timer: any;
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
    const boundaries = /([.!?]+|[,;:])\s+/g;
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
    if (this._buffer.length > 72) {
      const softBreak = findChunkSoftBreak(this._buffer, 72);
      if (softBreak > 0) {
        const sentence = this._buffer.slice(0, softBreak).trim();
        this._buffer = this._buffer.slice(softBreak).trimStart();
        if (sentence) this._onSentence(sentence);
      }
    }
    if (this._buffer.length > 0) {
      this._timer = setTimeout(() => {
        this._timer = null;
        this.flush();
      }, 120);
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

function findChunkSoftBreak(text, softLimit = 72) {
  const clean = String(text || '');
  if (!clean) return -1;
  if (clean.length <= softLimit) return -1;
  const probe = clean.slice(0, softLimit + 1);
  const preferred = Math.max(
    probe.lastIndexOf(','),
    probe.lastIndexOf(';'),
    probe.lastIndexOf(':'),
    probe.lastIndexOf('\n'),
  );
  if (preferred >= 24) return preferred + 1;
  const whitespace = probe.lastIndexOf(' ');
  if (whitespace >= 32) return whitespace + 1;
  return -1;
}

export class TTSPlayer {
  _queue: any[];
  _playing: boolean;
  _stopped: boolean;
  _ctx: any;
  _currentSource: any;
  _nextStartTime: number;
  _playbackInterval: any;
  _playbackStartedAtMs: number;
  _playedAudioMs: number;
  constructor() {
    this._queue = [];
    this._playing = false;
    this._stopped = false;
    this._ctx = null;
    this._currentSource = null;
    this._nextStartTime = 0;
    this._playbackInterval = null;
    this._playbackStartedAtMs = 0;
    this._playedAudioMs = 0;
  }
  _ensureCtx() {
    if (!this._ctx) {
      this._ctx = getTTSAudioContext();
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
    this._finishPlaybackProgress();
    setDialogueTTSBargeInMode(false);
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
      this._finishPlaybackProgress();
      setDialogueTTSBargeInMode(false);
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
      setDialogueTTSBargeInMode(true);
      state.ttsPlaying = true;
      this._startPlaybackProgress();
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

  _startPlaybackProgress() {
    this._playedAudioMs = 0;
    this._playbackStartedAtMs = Date.now();
    sendTurnPlaybackProgress(true, 0);
    if (this._playbackInterval) {
      window.clearInterval(this._playbackInterval);
    }
    this._playbackInterval = window.setInterval(() => {
      if (!state.ttsPlaying) return;
      this._playedAudioMs = Math.max(0, Date.now() - this._playbackStartedAtMs);
      sendTurnPlaybackProgress(true, this._playedAudioMs);
    }, 100);
  }

  _finishPlaybackProgress() {
    if (this._playbackStartedAtMs > 0) {
      this._playedAudioMs = Math.max(this._playedAudioMs, Date.now() - this._playbackStartedAtMs);
      sendTurnPlaybackProgress(false, this._playedAudioMs);
    }
    if (this._playbackInterval) {
      window.clearInterval(this._playbackInterval);
      this._playbackInterval = null;
    }
    this._playbackStartedAtMs = 0;
    this._playedAudioMs = 0;
  }
}

let ttsPlayer = null;
let ttsSentenceChunker = null;
state.ttsEnabled = false;
let ttsLastSpeakText = '';
let ttsSpeakLang = 'en';
let ttsPendingTurnLang = '';
let ttsTurnLang = '';
let hotwordSyncInFlight = false;
let hotwordResyncQueued = false;
let hotwordInitAttempted = false;
let hotwordUnsubscribe = null;
let hotwordRetryTimer = null;
let hotwordStatusPollTimer = null;
let hotwordStatusPollInFlight = false;
let hotwordModelRevision = '';
const HOTWORD_RETRY_MS = 800;
const HOTWORD_STATUS_POLL_MS = 5000;
export function readTTSSilentPreference() {
  try {
    const value = window.localStorage.getItem(TTS_SILENT_STORAGE_KEY);
    const parsed = parseOptionalBoolean(value);
    return parsed === true;
  } catch (_) {
    return false;
  }
}

export function persistTTSSilentPreference(silent) {
  try {
    window.localStorage.setItem(TTS_SILENT_STORAGE_KEY, silent ? 'true' : 'false');
  } catch (_) {}
}

export function readYoloModePreference() {
  try {
    const value = window.localStorage.getItem(YOLO_MODE_STORAGE_KEY);
    const parsed = parseOptionalBoolean(value);
    return parsed === true;
  } catch (_) {
    return false;
  }
}

export function persistYoloModePreference(enabled) {
  try {
    window.localStorage.setItem(YOLO_MODE_STORAGE_KEY, enabled ? 'true' : 'false');
  } catch (_) {}
}

export function readSomedayReviewNudgePreference() {
  try {
    const value = window.localStorage.getItem(SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY);
    const parsed = parseOptionalBoolean(value);
    return parsed !== false;
  } catch (_) {
    return true;
  }
}

export function persistSomedayReviewNudgePreference(enabled) {
  try {
    window.localStorage.setItem(SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, enabled ? 'true' : 'false');
  } catch (_) {}
}

export function readSomedayReviewNudgeLastShownAt() {
  try {
    const raw = Number(window.localStorage.getItem(SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY) || '0');
    return Number.isFinite(raw) && raw > 0 ? raw : 0;
  } catch (_) {
    return 0;
  }
}

export function persistSomedayReviewNudgeLastShownAt(value = Date.now()) {
  try {
    window.localStorage.setItem(SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, String(Math.max(0, Number(value) || 0)));
  } catch (_) {}
}

export function setSomedayReviewNudgeEnabled(enabled, { persist = true } = {}) {
  const next = Boolean(enabled);
  state.somedayReviewNudgeEnabled = next;
  if (persist) persistSomedayReviewNudgePreference(next);
}

export function setYoloModeLocal(enabled, { persist = true, render = true } = {}) {
  const next = Boolean(enabled);
  if (state.yoloMode === next) return;
  state.yoloMode = next;
  if (persist) persistYoloModePreference(next);
  if (render) renderEdgeTopModelButtons();
}

export async function setYoloMode(enabled) {
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

export function toggleYoloMode() {
  if (state.projectSwitchInFlight || state.projectModelSwitchInFlight) return;
  const next = !Boolean(state.yoloMode);
  setYoloMode(next)
    .then(() => {
      showStatus(next ? 'autonomous policy on' : 'autonomous policy off');
    })
    .catch((err) => {
      showStatus(`autonomous policy update failed: ${String(err?.message || err || 'unknown error')}`);
      renderEdgeTopModelButtons();
    });
}

export function canSpeakTTS() {
  return Boolean(state.ttsEnabled) && !Boolean(state.ttsSilent);
}

export function clearHotwordRetry() {
  if (hotwordRetryTimer !== null) {
    window.clearTimeout(hotwordRetryTimer);
    hotwordRetryTimer = null;
  }
}

function hotwordStatusPollDelayMs() {
  const override = Number((window as any).__taburaHotwordStatusPollMs);
  if (Number.isFinite(override) && override >= 0) {
    return Math.floor(override);
  }
  return HOTWORD_STATUS_POLL_MS;
}

function scheduleHotwordStatusPoll(delayMs = hotwordStatusPollDelayMs()) {
  if (hotwordStatusPollTimer !== null) {
    window.clearTimeout(hotwordStatusPollTimer);
  }
  hotwordStatusPollTimer = window.setTimeout(() => {
    hotwordStatusPollTimer = null;
    void pollHotwordStatus();
  }, Math.max(0, delayMs));
}

async function pollHotwordStatus() {
  if (hotwordStatusPollInFlight) return;
  hotwordStatusPollInFlight = true;
  try {
    const resp = await fetch(apiURL('hotword/status'), { cache: 'no-store' });
    if (!resp.ok) return;
    const payload = await resp.json();
    const ready = Boolean(payload?.ready);
    const revision = String(payload?.model?.revision || '').trim();
    const revisionChanged = revision !== hotwordModelRevision;
    if (revisionChanged && ready && state.liveSessionActive) {
      hotwordModelRevision = revision;
      await initHotwordLifecycleWithOptions({ force: true });
      return;
    }
    if (revision) {
      hotwordModelRevision = revision;
    }
  } catch (_) {
  } finally {
    hotwordStatusPollInFlight = false;
    scheduleHotwordStatusPoll();
  }
}

export function scheduleHotwordRetry() {
  if (hotwordRetryTimer !== null) return;
  hotwordRetryTimer = window.setTimeout(() => {
    hotwordRetryTimer = null;
    requestHotwordSync();
  }, HOTWORD_RETRY_MS);
}

export function canStartHotwordMonitor() {
  const mode = syncVoiceLifecycle('can-start-hotword');
  if (!state.hotwordEnabled) return false;
  if (!state.liveSessionActive) return false;
  if (!state.ttsEnabled) return false;
  if (mode === VOICE_LIFECYCLE.RECORDING || mode === VOICE_LIFECYCLE.STOPPING_RECORDING) return false;
  if (state.chatVoiceCapture) return false;
  if (isMeetingLiveSession()) return true;
  if (isDialogueLiveSession() && mode === VOICE_LIFECYCLE.LISTENING) return true;
  if (isStopCapableLifecycle(mode)) return false;
  return true;
}

export async function syncHotwordMonitor() {
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

export function requestHotwordSync() {
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

export function configureHotwordLifecycle() {
  if (typeof hotwordUnsubscribe === 'function') return;
  hotwordUnsubscribe = onHotwordDetected(() => {
    if (!canStartHotwordMonitor()) return;
    stopHotwordMonitor();
    state.hotwordActive = false;
    if (isMeetingLiveSession()) {
      const mode = syncVoiceLifecycle('hotword-detected-meeting');
      if (mode !== VOICE_LIFECYCLE.IDLE && mode !== VOICE_LIFECYCLE.LISTENING) {
        void handleStopAction();
      } else {
        requestHotwordSync();
        updateAssistantActivityIndicator();
      }
      return;
    }
    beginConversationVoiceCapture('hotword');
    updateAssistantActivityIndicator();
  });
}

export async function initHotwordLifecycle() {
  return initHotwordLifecycleWithOptions();
}

export async function initHotwordLifecycleWithOptions(options: Record<string, any> = {}) {
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
      setHotwordThreshold(0.3);
      configureHotwordLifecycle();
    } else {
      console.warn('Hotword unavailable; continuing without wake-word activation.');
    }
  } catch (err) {
    state.hotwordEnabled = false;
    console.warn('Hotword initialization error:', err);
  }
  scheduleHotwordStatusPoll(force ? 0 : hotwordStatusPollDelayMs());
  requestHotwordSync();
  return state.hotwordEnabled;
}

export function applyLiveSessionStateSnapshot(snapshot = null) {
  const nextSnapshot = snapshot && typeof snapshot === 'object'
    ? snapshot
    : getLiveSessionSnapshot();
  state.liveSessionActive = Boolean(nextSnapshot.liveSessionActive);
  state.liveSessionMode = String(nextSnapshot.liveSessionMode || '').trim().toLowerCase();
  state.interaction.conversation = interactionConversationMode();
  state.liveSessionHotword = String(nextSnapshot.liveSessionHotword || LIVE_SESSION_HOTWORD_DEFAULT).trim() || LIVE_SESSION_HOTWORD_DEFAULT;
  state.liveSessionDialogueListenActive = Boolean(nextSnapshot.liveSessionDialogueListenActive);
  requestHotwordSync();
}

export function isDialogueLiveSession() {
  return state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_DIALOGUE;
}

export function isMeetingLiveSession() {
  return state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_MEETING;
}

export function liveSessionStatusSummary() {
  if (!state.liveSessionActive) return '';
  if (isDialogueLiveSession()) {
    const lifecycle = syncVoiceLifecycle('live-dialogue-summary');
    if (lifecycle === VOICE_LIFECYCLE.TTS_PLAYING) return 'Dialogue • Talking';
    if (lifecycle === VOICE_LIFECYCLE.ASSISTANT_WORKING || lifecycle === VOICE_LIFECYCLE.AWAITING_TURN) return 'Dialogue • Working';
    return 'Dialogue • Listening';
  }
  const runtimeState = normalizeCompanionRuntimeState(state.companionRuntimeState);
  if (runtimeState === COMPANION_RUNTIME_STATES.TALKING) return 'Meeting • Talking';
  if (runtimeState === COMPANION_RUNTIME_STATES.THINKING) return 'Meeting • Working';
  if (runtimeState === COMPANION_RUNTIME_STATES.ERROR) return 'Meeting • Error';
  if (runtimeState === COMPANION_RUNTIME_STATES.LISTENING) return 'Meeting • Listening';
  return 'Meeting • Quiet';
}

export function stopTTSPlayback() {
  if (ttsPlayer) { ttsPlayer.stop(); ttsPlayer = null; }
  if (ttsSentenceChunker) { ttsSentenceChunker.reset(); ttsSentenceChunker = null; }
  ttsLastSpeakText = '';
  ttsTurnLang = '';
  ttsSpeakLang = 'en';
  setDialogueTTSBargeInMode(false);
  if (state.ttsPlaying) {
    state.ttsPlaying = false;
    updateAssistantActivityIndicator();
  }
  requestHotwordSync();
}

export function ensureTTSChunker() {
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

export function queueTTSDiff(diffText) {
  if (!canSpeakTTS()) return;
  const fragment = String(diffText || '');
  if (fragment === '') return;
  ensureTTSChunker();
  ttsSentenceChunker.add(fragment);
}

export function computeTTSDiff(nextFullText, hintedDeltaText = '') {
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
    ttsLastSpeakText = next;
    return '';
  }
  ttsLastSpeakText = next;
  return next;
}

export function enqueueTTSAudio(audioData) {
  if (!canSpeakTTS()) return;
  if (!ttsPlayer) ttsPlayer = new TTSPlayer();
  ttsPlayer.enqueue(audioData);
}

export function setTTSSpeakLang(lang) {
  const next = String(lang || '').trim();
  if (!next) return;
  if (ttsTurnLang && ttsTurnLang !== next) return;
  ttsTurnLang = next;
  ttsSpeakLang = next;
}

export function getTTSLastSpeakText() {
  return ttsLastSpeakText;
}

export function flushTTSChunker() {
  if (ttsSentenceChunker) ttsSentenceChunker.flush();
}

export function hasTTSPlayer() {
  return Boolean(ttsPlayer);
}

export function primeTTSTurnLanguage(text) {
  const inferred = inferTTSLanguage(text);
  if (inferred) {
    ttsPendingTurnLang = inferred;
  }
}

export function beginTTSTurn() {
  ttsLastSpeakText = '';
  ttsTurnLang = ttsPendingTurnLang;
  ttsSpeakLang = ttsTurnLang || 'en';
  ttsPendingTurnLang = '';
}
