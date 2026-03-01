import { initVAD } from './vad.js';

const CONVERSATION_MODE_STORAGE_KEY = 'tabura.conversationMode';
const CONVERSATION_LISTEN_DEFAULT_MS = 6000;
const CONVERSATION_LISTEN_MIN_MS = 500;

function parseOptionalBoolean(value) {
  const normalized = String(value || '').trim().toLowerCase();
  if (!normalized) return null;
  if (normalized === '1' || normalized === 'true' || normalized === 'on' || normalized === 'yes') return true;
  if (normalized === '0' || normalized === 'false' || normalized === 'off' || normalized === 'no') return false;
  return null;
}

function readConversationModePreference() {
  try {
    const value = window.localStorage.getItem(CONVERSATION_MODE_STORAGE_KEY);
    const parsed = parseOptionalBoolean(value);
    return parsed === true;
  } catch (_) {
    return false;
  }
}

function persistConversationModePreference(enabled) {
  try {
    window.localStorage.setItem(CONVERSATION_MODE_STORAGE_KEY, enabled ? 'true' : 'false');
  } catch (_) {}
}

function resolveListenWindowMs() {
  try {
    const override = Number(window.__taburaConversationListenMs);
    if (Number.isFinite(override) && override >= CONVERSATION_LISTEN_MIN_MS) {
      return Math.floor(override);
    }
  } catch (_) {}
  return CONVERSATION_LISTEN_DEFAULT_MS;
}

const hooks = {
  canStartConversationListen: null,
  onConversationListenStateChange: null,
  onConversationListenTimeout: null,
  onConversationSpeechDetected: null,
  onConversationListenCancelled: null,
  getAudioContext: null,
  acquireMicStream: null,
};

const state = {
  conversationMode: readConversationModePreference(),
  conversationListenActive: false,
  conversationListenTimer: null,
  conversationListenSileroVAD: null,
  conversationSessionToken: 0,
};

function notifyConversationStateChange() {
  if (typeof hooks.onConversationListenStateChange === 'function') {
    hooks.onConversationListenStateChange({
      conversationMode: state.conversationMode,
      conversationListenActive: state.conversationListenActive,
      conversationListenTimer: state.conversationListenTimer,
    });
  }
}

function clearConversationSileroVAD() {
  if (state.conversationListenSileroVAD) {
    try { state.conversationListenSileroVAD.destroy(); } catch (_) {}
    state.conversationListenSileroVAD = null;
  }
}

function clearConversationAudioMonitor() {
  clearConversationSileroVAD();
}

function clearConversationListenTimer() {
  if (state.conversationListenTimer !== null) {
    window.clearTimeout(state.conversationListenTimer);
    state.conversationListenTimer = null;
  }
}

function closeConversationListenWindow() {
  clearConversationListenTimer();
  clearConversationAudioMonitor();
  if (state.conversationListenActive) {
    state.conversationListenActive = false;
  }
  notifyConversationStateChange();
}

function canStartConversationListen() {
  if (!state.conversationMode) return false;
  if (typeof hooks.canStartConversationListen === 'function' && !hooks.canStartConversationListen()) {
    return false;
  }
  return true;
}

function nextConversationToken() {
  state.conversationSessionToken += 1;
  return state.conversationSessionToken;
}

async function startSileroConversationMonitor(stream, token) {
  try {
    const instance = await initVAD({
      stream,
      positiveSpeechThreshold: 0.5,
      negativeSpeechThreshold: 0.3,
      redemptionMs: 300,
      minSpeechMs: 100,
      preSpeechPadMs: 0,
      onSpeechStart() {
        if (token !== state.conversationSessionToken) return;
        if (!state.conversationListenActive) return;
        onConversationSpeechDetected();
      },
    });

    if (token !== state.conversationSessionToken || !state.conversationListenActive) {
      if (instance) instance.destroy();
      return;
    }

    state.conversationListenSileroVAD = instance;
    if (instance) instance.start();
    notifyConversationStateChange();
  } catch (_) {}
}

function startConversationAudioMonitor(stream, token) {
  void startSileroConversationMonitor(stream, token);
}

async function openConversationListenWindow() {
  if (!canStartConversationListen()) return;
  closeConversationListenWindow();
  const token = nextConversationToken();
  state.conversationListenActive = true;
  state.conversationListenTimer = window.setTimeout(() => {
    if (token !== state.conversationSessionToken) return;
    onConversationListenTimeout();
  }, resolveListenWindowMs());
  notifyConversationStateChange();

  try {
    const audioCtx = typeof hooks.getAudioContext === 'function' ? hooks.getAudioContext() : null;
    if (audioCtx && audioCtx.state === 'suspended' && typeof audioCtx.resume === 'function') {
      await audioCtx.resume().catch(() => {});
    }
    const stream = typeof hooks.acquireMicStream === 'function' ? await hooks.acquireMicStream() : null;
    if (token !== state.conversationSessionToken) return;
    if (!stream || !canStartConversationListen()) {
      onConversationListenTimeout();
      return;
    }
    startConversationAudioMonitor(stream, token);
  } catch (_) {
    if (token !== state.conversationSessionToken) return;
    onConversationListenTimeout();
  }
}

export function configureConversation(config = {}) {
  hooks.canStartConversationListen = config.canStartConversationListen || null;
  hooks.onConversationListenStateChange = config.onConversationListenStateChange || null;
  hooks.onConversationListenTimeout = config.onConversationListenTimeout || null;
  hooks.onConversationSpeechDetected = config.onConversationSpeechDetected || null;
  hooks.onConversationListenCancelled = config.onConversationListenCancelled || null;
  hooks.getAudioContext = config.getAudioContext || null;
  hooks.acquireMicStream = config.acquireMicStream || null;
  notifyConversationStateChange();
}

export function isConversationMode() {
  return state.conversationMode;
}

export function setConversationMode(enabled) {
  const next = Boolean(enabled);
  if (state.conversationMode === next) return state.conversationMode;
  state.conversationMode = next;
  persistConversationModePreference(next);
  if (!next) {
    cancelConversationListen();
  } else {
    notifyConversationStateChange();
  }
  return state.conversationMode;
}

export function isConversationListenActive() {
  return state.conversationListenActive;
}

export function onTTSPlaybackComplete() {
  if (!canStartConversationListen()) return;
  void openConversationListenWindow();
}

export function onConversationListenTimeout() {
  if (!state.conversationListenActive) return;
  nextConversationToken();
  closeConversationListenWindow();
  if (typeof hooks.onConversationListenTimeout === 'function') {
    hooks.onConversationListenTimeout();
  }
}

export function onConversationSpeechDetected() {
  if (!state.conversationListenActive) return;
  nextConversationToken();
  closeConversationListenWindow();
  if (typeof hooks.onConversationSpeechDetected === 'function') {
    hooks.onConversationSpeechDetected();
  }
}

export function cancelConversationListen() {
  if (!state.conversationListenActive && state.conversationListenTimer === null && state.conversationListenSileroVAD === null) {
    return;
  }
  nextConversationToken();
  closeConversationListenWindow();
  if (typeof hooks.onConversationListenCancelled === 'function') {
    hooks.onConversationListenCancelled();
  }
}
