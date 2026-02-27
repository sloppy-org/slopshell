const CONVERSATION_MODE_STORAGE_KEY = 'tabura.conversationMode';
const CONVERSATION_LISTEN_DEFAULT_MS = 6000;
const CONVERSATION_LISTEN_MIN_MS = 500;
const VAD_FRAME_MS = 40;
const VAD_NOISE_FLOOR_SAMPLES = 8;
const VAD_NOISE_FLOOR_PERCENTILE = 0.35;
const VAD_NOISE_FLOOR_ADAPT_ALPHA = 0.12;
const VAD_SPEECH_START_OFFSET_DB = 3;
const VAD_SPEECH_START_THRESHOLD_MIN_DB = -42;
const VAD_SPEECH_START_FRAMES = 4;
const VAD_NOISE_FLOOR_MIN_DB = -60;
const VAD_NOISE_FLOOR_MAX_DB = -18;

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

function computeDecibelFromTimeDomain(data) {
  let sumSquares = 0;
  for (let i = 0; i < data.length; i += 1) {
    const sample = (data[i] - 128) / 128;
    sumSquares += sample * sample;
  }
  const rms = Math.sqrt(sumSquares / Math.max(1, data.length));
  if (rms <= 0 || Number.isNaN(rms)) return -100;
  return 20 * Math.log10(rms);
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
  computeDecibelFromTimeDomain: null,
};

const state = {
  conversationMode: readConversationModePreference(),
  conversationListenActive: false,
  conversationListenTimer: null,
  conversationListenMonitor: null,
  conversationListenSource: null,
  conversationListenAnalyser: null,
  conversationListenSilentGain: null,
  conversationListenBins: null,
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

function clearConversationAudioMonitor() {
  if (state.conversationListenMonitor !== null) {
    window.clearInterval(state.conversationListenMonitor);
    state.conversationListenMonitor = null;
  }
  if (state.conversationListenSource) {
    try { state.conversationListenSource.disconnect(); } catch (_) {}
    state.conversationListenSource = null;
  }
  if (state.conversationListenSilentGain) {
    try { state.conversationListenSilentGain.disconnect(); } catch (_) {}
    state.conversationListenSilentGain = null;
  }
  if (state.conversationListenAnalyser) {
    try { state.conversationListenAnalyser.disconnect(); } catch (_) {}
    state.conversationListenAnalyser = null;
  }
  state.conversationListenBins = null;
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

function seedConversationNoiseFloor(options, db) {
  if (options.noiseFloorDb != null) return false;
  if (options.noiseSamples.length >= VAD_NOISE_FLOOR_SAMPLES) return false;
  options.noiseSamples.push(db);
  if (options.noiseSamples.length < VAD_NOISE_FLOOR_SAMPLES) return true;
  const seededFloor = percentileValue(options.noiseSamples, VAD_NOISE_FLOOR_PERCENTILE);
  if (seededFloor != null) {
    options.noiseFloorDb = clampNumber(
      seededFloor,
      VAD_NOISE_FLOOR_MIN_DB,
      VAD_NOISE_FLOOR_MAX_DB,
    );
  }
  return true;
}

function updateConversationStartThreshold(options, db) {
  const startThresholdBefore = Math.max(
    VAD_SPEECH_START_THRESHOLD_MIN_DB,
    options.noiseFloorDb + VAD_SPEECH_START_OFFSET_DB,
  );
  if (db <= startThresholdBefore) {
    options.noiseFloorDb = clampNumber(
      ((1 - VAD_NOISE_FLOOR_ADAPT_ALPHA) * options.noiseFloorDb) + (VAD_NOISE_FLOOR_ADAPT_ALPHA * db),
      VAD_NOISE_FLOOR_MIN_DB,
      VAD_NOISE_FLOOR_MAX_DB,
    );
  }
  return Math.max(
    VAD_SPEECH_START_THRESHOLD_MIN_DB,
    options.noiseFloorDb + VAD_SPEECH_START_OFFSET_DB,
  );
}

function runConversationMonitorFrame(token, options, computeDb) {
  if (token !== state.conversationSessionToken) {
    closeConversationListenWindow();
    return;
  }
  if (!state.conversationListenActive || !canStartConversationListen()) {
    cancelConversationListen();
    return;
  }
  const analyser = state.conversationListenAnalyser;
  const bins = state.conversationListenBins;
  if (!analyser || !(bins instanceof Uint8Array)) {
    cancelConversationListen();
    return;
  }

  analyser.getByteTimeDomainData(bins);
  const db = computeDb(bins);

  if (seedConversationNoiseFloor(options, db)) return;
  if (options.noiseFloorDb == null) return;

  const startThresholdDb = updateConversationStartThreshold(options, db);
  if (db >= startThresholdDb) {
    options.speechFrames += 1;
  } else {
    options.speechFrames = 0;
  }
  if (options.speechFrames >= VAD_SPEECH_START_FRAMES) {
    onConversationSpeechDetected();
  }
}

function startConversationAudioMonitor(stream, token) {
  const audioCtx = typeof hooks.getAudioContext === 'function' ? hooks.getAudioContext() : null;
  if (!audioCtx || typeof audioCtx.createMediaStreamSource !== 'function' || typeof audioCtx.createAnalyser !== 'function') {
    return;
  }

  let source = null;
  try {
    source = audioCtx.createMediaStreamSource(stream);
    const analyser = audioCtx.createAnalyser();
    analyser.fftSize = 1024;
    analyser.smoothingTimeConstant = 0.25;
    const bins = new Uint8Array(analyser.frequencyBinCount);
    source.connect(analyser);
    // iOS Safari requires the graph to terminate at destination for
    // AnalyserNode to receive live data from a MediaStreamSource.
    let silentGain = null;
    if (typeof audioCtx.createGain === 'function') {
      silentGain = audioCtx.createGain();
      silentGain.gain.value = 0;
      analyser.connect(silentGain);
      silentGain.connect(audioCtx.destination);
    }

    state.conversationListenSource = source;
    state.conversationListenAnalyser = analyser;
    state.conversationListenSilentGain = silentGain;
    state.conversationListenBins = bins;
  } catch (_) {
    if (source) {
      try { source.disconnect(); } catch (_) {}
    }
    clearConversationAudioMonitor();
    return;
  }

  const options = {
    noiseSamples: [],
    noiseFloorDb: null,
    speechFrames: 0,
  };

  const computeDb = typeof hooks.computeDecibelFromTimeDomain === 'function'
    ? hooks.computeDecibelFromTimeDomain
    : computeDecibelFromTimeDomain;

  state.conversationListenMonitor = window.setInterval(() => {
    runConversationMonitorFrame(token, options, computeDb);
  }, VAD_FRAME_MS);

  notifyConversationStateChange();
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
  hooks.computeDecibelFromTimeDomain = config.computeDecibelFromTimeDomain || null;
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
  if (!state.conversationListenActive && state.conversationListenTimer === null && state.conversationListenMonitor === null) {
    return;
  }
  nextConversationToken();
  closeConversationListenWindow();
  if (typeof hooks.onConversationListenCancelled === 'function') {
    hooks.onConversationListenCancelled();
  }
}
