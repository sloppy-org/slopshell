import { initVAD, float32ToWav } from './vad.js';

export const LIVE_SESSION_MODE_DIALOGUE = 'dialogue';
export const LIVE_SESSION_MODE_MEETING = 'meeting';
export const LIVE_SESSION_HOTWORD_DEFAULT = 'Alexa';

const DIALOGUE_LISTEN_DEFAULT_MS = 6000;
const DIALOGUE_LISTEN_MIN_MS = 500;

const hooks = {
  canStartDialogueListen: null,
  onStateChange: null,
  onDialogueListenTimeout: null,
  onDialogueListenError: null,
  onDialogueSpeechDetected: null,
  onDialogueListenCancelled: null,
  getAudioContext: null,
  acquireMicStream: null,
  requestMicRefresh: null,
  onMeetingSegment: null,
  onMeetingStarted: null,
  onMeetingStopped: null,
  onMeetingError: null,
};

const state = {
  active: false,
  mode: '',
  hotword: LIVE_SESSION_HOTWORD_DEFAULT,
  dialogueListenActive: false,
  dialogueListenTimer: null,
  dialogueListenSileroVAD: null,
  dialogueSessionToken: 0,
  dialogueRetryCount: 0,
  meetingCapture: null,
  meetingSessionID: '',
};

function normalizeMode(mode) {
  const normalized = String(mode || '').trim().toLowerCase();
  if (normalized === LIVE_SESSION_MODE_DIALOGUE) return LIVE_SESSION_MODE_DIALOGUE;
  if (normalized === LIVE_SESSION_MODE_MEETING) return LIVE_SESSION_MODE_MEETING;
  return '';
}

function liveSessionSnapshot() {
  return {
    liveSessionActive: state.active,
    liveSessionMode: state.mode,
    liveSessionHotword: state.hotword,
    liveSessionDialogueListenActive: state.dialogueListenActive,
    liveSessionDialogueListenTimer: state.dialogueListenTimer,
    liveSessionMeetingSessionID: state.meetingSessionID,
  };
}

function notifyStateChange() {
  if (typeof hooks.onStateChange === 'function') {
    hooks.onStateChange(liveSessionSnapshot());
  }
}

function resolveDialogueListenWindowMs() {
  try {
    const override = Number(window.__taburaConversationListenMs);
    if (Number.isFinite(override) && override >= DIALOGUE_LISTEN_MIN_MS) {
      return Math.floor(override);
    }
  } catch (_) {}
  return DIALOGUE_LISTEN_DEFAULT_MS;
}

function clearDialogueSileroVAD() {
  if (state.dialogueListenSileroVAD) {
    try { state.dialogueListenSileroVAD.destroy(); } catch (_) {}
    state.dialogueListenSileroVAD = null;
  }
}

function clearDialogueListenTimer() {
  if (state.dialogueListenTimer !== null) {
    window.clearTimeout(state.dialogueListenTimer);
    state.dialogueListenTimer = null;
  }
}

function closeDialogueListenWindow() {
  clearDialogueListenTimer();
  clearDialogueSileroVAD();
  if (state.dialogueListenActive) {
    state.dialogueListenActive = false;
  }
  notifyStateChange();
}

function canStartDialogueListen() {
  if (!state.active || state.mode !== LIVE_SESSION_MODE_DIALOGUE) return false;
  if (typeof hooks.canStartDialogueListen === 'function' && !hooks.canStartDialogueListen()) {
    return false;
  }
  return true;
}

function nextDialogueToken() {
  state.dialogueSessionToken += 1;
  return state.dialogueSessionToken;
}

function fireDialogueListenError(message) {
  if (state.dialogueRetryCount < 1 && state.active && state.mode === LIVE_SESSION_MODE_DIALOGUE) {
    state.dialogueRetryCount += 1;
    closeDialogueListenWindow();
    window.setTimeout(() => {
      if (!state.active || state.mode !== LIVE_SESSION_MODE_DIALOGUE) return;
      void openDialogueListenWindow();
    }, 1500);
    return;
  }
  closeDialogueListenWindow();
  if (typeof hooks.onDialogueListenError === 'function') {
    hooks.onDialogueListenError(message);
  }
}

async function startSileroDialogueMonitor(stream, token) {
  try {
    const instance = await initVAD({
      stream,
      positiveSpeechThreshold: 0.5,
      negativeSpeechThreshold: 0.3,
      redemptionMs: 300,
      minSpeechMs: 100,
      preSpeechPadMs: 0,
      onSpeechStart() {
        if (token !== state.dialogueSessionToken) return;
        if (!state.dialogueListenActive) return;
        onDialogueSpeechDetected();
      },
    });

    if (token !== state.dialogueSessionToken || !state.dialogueListenActive) {
      if (instance) instance.destroy();
      return;
    }

    if (!instance) {
      fireDialogueListenError('speech detection unavailable (VAD failed to load — run scripts/fetch-vad-assets.sh)');
      return;
    }

    state.dialogueListenSileroVAD = instance;
    instance.start();
    state.dialogueRetryCount = 0;
    if (token === state.dialogueSessionToken && state.dialogueListenActive) {
      state.dialogueListenTimer = window.setTimeout(() => {
        if (token !== state.dialogueSessionToken) return;
        onDialogueListenTimeout();
      }, resolveDialogueListenWindowMs());
    }
    notifyStateChange();
  } catch (err) {
    if (token === state.dialogueSessionToken && state.dialogueListenActive) {
      const detail = String(err?.message || err || 'unknown error');
      fireDialogueListenError(`speech detection failed: ${detail}`);
    }
  }
}

async function openDialogueListenWindow() {
  if (!canStartDialogueListen()) return;
  closeDialogueListenWindow();
  const token = nextDialogueToken();
  state.dialogueListenActive = true;
  notifyStateChange();

  if (typeof hooks.requestMicRefresh === 'function') {
    hooks.requestMicRefresh();
  }

  try {
    const audioCtx = typeof hooks.getAudioContext === 'function' ? hooks.getAudioContext() : null;
    if (audioCtx && audioCtx.state === 'suspended' && typeof audioCtx.resume === 'function') {
      await audioCtx.resume().catch(() => {});
    }
    const stream = typeof hooks.acquireMicStream === 'function' ? await hooks.acquireMicStream() : null;
    if (token !== state.dialogueSessionToken) return;
    if (!stream) {
      fireDialogueListenError('microphone unavailable — check browser permissions');
      return;
    }
    if (!canStartDialogueListen()) {
      onDialogueListenTimeout();
      return;
    }
    void startSileroDialogueMonitor(stream, token);
  } catch (err) {
    if (token !== state.dialogueSessionToken) return;
    const detail = String(err?.message || err || 'unknown error');
    fireDialogueListenError(`dialogue listen failed: ${detail}`);
  }
}

function resetMeetingState(capture = null) {
  if (capture && state.meetingCapture && state.meetingCapture !== capture) return;
  state.meetingCapture = null;
  state.meetingSessionID = '';
}

export function configureLiveSession(config: Record<string, any> = {}) {
  hooks.canStartDialogueListen = config.canStartDialogueListen || null;
  hooks.onStateChange = config.onStateChange || null;
  hooks.onDialogueListenTimeout = config.onDialogueListenTimeout || null;
  hooks.onDialogueListenError = config.onDialogueListenError || null;
  hooks.onDialogueSpeechDetected = config.onDialogueSpeechDetected || null;
  hooks.onDialogueListenCancelled = config.onDialogueListenCancelled || null;
  hooks.getAudioContext = config.getAudioContext || null;
  hooks.acquireMicStream = config.acquireMicStream || null;
  hooks.requestMicRefresh = config.requestMicRefresh || null;
  hooks.onMeetingSegment = config.onMeetingSegment || null;
  hooks.onMeetingStarted = config.onMeetingStarted || null;
  hooks.onMeetingStopped = config.onMeetingStopped || null;
  hooks.onMeetingError = config.onMeetingError || null;
  notifyStateChange();
}

export function getLiveSessionSnapshot() {
  return liveSessionSnapshot();
}

export function isLiveSessionActive() {
  return state.active;
}

export function getLiveSessionMode() {
  return state.mode;
}

export function isLiveSessionListenActive() {
  return state.dialogueListenActive;
}

export async function startLiveSession(mode, ws) {
  const nextMode = normalizeMode(mode);
  if (!nextMode) return false;
  if (state.active && state.mode === nextMode) return true;
  stopLiveSession();
  state.active = true;
  state.mode = nextMode;
  notifyStateChange();
  if (nextMode === LIVE_SESSION_MODE_DIALOGUE) {
    void openDialogueListenWindow();
    return true;
  }

  const capture = new MeetingLiveCapture();
  capture.onSegment = hooks.onMeetingSegment;
  capture.onStarted = (message) => {
    if (state.meetingCapture !== capture) return;
    state.meetingSessionID = String(message?.session_id || '').trim();
    notifyStateChange();
    if (typeof hooks.onMeetingStarted === 'function') {
      hooks.onMeetingStarted(message);
    }
  };
  capture.onStopped = (message) => {
    if (state.meetingCapture !== capture) return;
    resetMeetingState(capture);
    state.active = false;
    state.mode = '';
    notifyStateChange();
    if (typeof hooks.onMeetingStopped === 'function') {
      hooks.onMeetingStopped(message);
    }
  };
  capture.onError = (message) => {
    if (state.meetingCapture !== capture) return;
    resetMeetingState(capture);
    state.active = false;
    state.mode = '';
    notifyStateChange();
    if (typeof hooks.onMeetingError === 'function') {
      hooks.onMeetingError(message);
    }
  };
  state.meetingCapture = capture;
  const started = await capture.start(ws);
  if (!started) {
    if (state.meetingCapture === capture) {
      resetMeetingState(capture);
      state.active = false;
      state.mode = '';
      notifyStateChange();
    }
    return false;
  }
  return true;
}

export function stopLiveSession() {
  closeDialogueListenWindow();
  const capture = state.meetingCapture;
  resetMeetingState(capture);
  state.active = false;
  state.mode = '';
  if (capture) {
    capture.stop();
  }
  notifyStateChange();
}

export function cancelLiveSessionListen() {
  if (!state.dialogueListenActive && state.dialogueListenTimer === null && state.dialogueListenSileroVAD === null) {
    return;
  }
  nextDialogueToken();
  closeDialogueListenWindow();
  if (typeof hooks.onDialogueListenCancelled === 'function') {
    hooks.onDialogueListenCancelled();
  }
}

export function onLiveSessionTTSPlaybackComplete() {
  if (!canStartDialogueListen()) return;
  void openDialogueListenWindow();
}

export function onDialogueListenTimeout() {
  if (!state.dialogueListenActive) return;
  nextDialogueToken();
  closeDialogueListenWindow();
  if (typeof hooks.onDialogueListenTimeout === 'function') {
    hooks.onDialogueListenTimeout();
  }
}

export function onDialogueSpeechDetected() {
  if (!state.dialogueListenActive) return;
  nextDialogueToken();
  closeDialogueListenWindow();
  if (typeof hooks.onDialogueSpeechDetected === 'function') {
    hooks.onDialogueSpeechDetected();
  }
}

export function handleLiveSessionMessage(message) {
  if (!state.meetingCapture) return false;
  return state.meetingCapture.handleMessage(message);
}

export class MeetingLiveCapture {
  _ws: any;
  _stream: any;
  _vadInstance: any;
  _active: boolean;
  _sessionId: any;
  _onSegment: any;
  _onStarted: any;
  _onStopped: any;
  _onError: any;
  _sampleRate: number;
  _maxSegmentDurationMS: number;
  _sessionRamCapBytes: number;
  _rollingSamples: Float32Array | null;
  _sessionChunks: Uint8Array[];
  _sessionBufferedBytes: number;
  constructor(options: Record<string, any> = {}) {
    this._ws = null;
    this._stream = null;
    this._vadInstance = null;
    this._active = false;
    this._sessionId = null;
    this._onSegment = null;
    this._onStarted = null;
    this._onStopped = null;
    this._onError = null;
    this._sampleRate = 16000;
    this._maxSegmentDurationMS = normalizePositiveNumber(options.maxSegmentDurationMS, 30_000);
    this._sessionRamCapBytes = normalizeBytesCap(options.sessionRamCapMB, 64 * 1024 * 1024);
    this._rollingSamples = null;
    this._sessionChunks = [];
    this._sessionBufferedBytes = 0;
  }

  get active() {
    return this._active;
  }

  get sessionId() {
    return this._sessionId;
  }

  get pendingSegmentSamples() {
    return this._rollingSamples ? this._rollingSamples.length : 0;
  }

  get sessionBufferedChunks() {
    return this._sessionChunks.length;
  }

  get sessionBufferedBytes() {
    return this._sessionBufferedBytes;
  }

  set onSegment(fn) {
    this._onSegment = typeof fn === 'function' ? fn : null;
  }

  set onStarted(fn) {
    this._onStarted = typeof fn === 'function' ? fn : null;
  }

  set onStopped(fn) {
    this._onStopped = typeof fn === 'function' ? fn : null;
  }

  set onError(fn) {
    this._onError = typeof fn === 'function' ? fn : null;
  }

  async start(ws) {
    if (this._active) return true;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      this._emitError('Live meeting connection is unavailable');
      return false;
    }

    this._ws = ws;
    this._clearAudioBuffers();

    try {
      this._stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (err) {
      this._emitError('Microphone access denied: ' + err.message);
      return false;
    }

    this._active = true;
    ws.send(JSON.stringify({ type: 'participant_start' }));
    await this._startSileroCapture();
    return this._active;
  }

  async _startSileroCapture() {
    try {
      const instance = await initVAD({
        stream: this._stream,
        positiveSpeechThreshold: 0.5,
        negativeSpeechThreshold: 0.3,
        redemptionMs: 800,
        minSpeechMs: 300,
        preSpeechPadMs: 300,
        onSpeechEnd: (audio) => {
          void this._handleSpeechEnd(audio);
        },
        onError: (err) => this._handleCaptureError(err),
      });

      if (!this._active) {
        if (instance) instance.destroy();
        return;
      }
      if (!instance) {
        this._handleCaptureError(new Error('Silero VAD unavailable'));
        return;
      }

      this._vadInstance = instance;
      instance.start();
    } catch (err) {
      this._handleCaptureError(err);
    }
  }

  stop() {
    if (!this._active) return;
    this._active = false;
    this._clearAudioBuffers();

    if (this._vadInstance) {
      try { this._vadInstance.destroy(); } catch (_) {}
      this._vadInstance = null;
    }

    if (this._stream) {
      for (const track of this._stream.getTracks()) {
        track.stop();
      }
      this._stream = null;
    }

    if (this._ws && this._ws.readyState === WebSocket.OPEN) {
      this._ws.send(JSON.stringify({ type: 'participant_stop' }));
    }
    this._ws = null;
  }

  handleMessage(msg) {
    if (!msg || typeof msg.type !== 'string') return false;
    switch (msg.type) {
      case 'participant_started':
        this._sessionId = msg.session_id || null;
        if (this._onStarted) this._onStarted(msg);
        return true;
      case 'participant_segment_text':
        if (this._onSegment) this._onSegment(msg);
        return true;
      case 'participant_stopped':
        this._sessionId = null;
        this._cleanup();
        if (this._onStopped) this._onStopped(msg);
        return true;
      case 'participant_error':
        this._sessionId = null;
        this._cleanup();
        this._emitError(msg.error || 'unknown live meeting error');
        return true;
      default:
        return false;
    }
  }

  _cleanup() {
    this._active = false;
    this._clearAudioBuffers();
    if (this._vadInstance) {
      try { this._vadInstance.destroy(); } catch (_) {}
      this._vadInstance = null;
    }
    if (this._stream) {
      for (const track of this._stream.getTracks()) {
        track.stop();
      }
      this._stream = null;
    }
    this._ws = null;
  }

  _emitError(message) {
    if (this._onError) {
      this._onError(message);
    }
  }

  async _handleSpeechEnd(audio) {
    if (!this._active || !this._ws) return;
    const samples = normalizeSegmentSamples(audio, this._sampleRate, this._maxSegmentDurationMS);
    if (!samples) return;

    this._clearRollingSamples();
    this._rollingSamples = samples;
    const wavBlob = float32ToWav(samples, this._sampleRate);
    if (!(wavBlob instanceof Blob) || wavBlob.size <= 44) {
      this._clearRollingSamples();
      return;
    }

    let tempBytes = null;
    try {
      tempBytes = new Uint8Array(await wavBlob.arrayBuffer());
      this._retainSessionChunk(tempBytes);
      if (this._active && this._ws?.readyState === WebSocket.OPEN) {
        this._ws.send(wavBlob);
      }
    } catch (err) {
      this._handleCaptureError(err);
    } finally {
      zeroizeByteArray(tempBytes);
      this._clearRollingSamples();
    }
  }

  _retainSessionChunk(bytes) {
    if (!(bytes instanceof Uint8Array) || bytes.length === 0) return;
    if (bytes.length > this._sessionRamCapBytes) {
      this._clearSessionChunks();
      return;
    }
    while (this._sessionBufferedBytes + bytes.length > this._sessionRamCapBytes && this._sessionChunks.length > 0) {
      const dropped = this._sessionChunks.shift();
      zeroizeByteArray(dropped);
      this._sessionBufferedBytes -= dropped ? dropped.length : 0;
    }
    const copy = new Uint8Array(bytes.length);
    copy.set(bytes);
    this._sessionChunks.push(copy);
    this._sessionBufferedBytes += copy.length;
  }

  _handleCaptureError(err) {
    this._cleanup();
    const message = err && typeof err === 'object' && 'message' in err
      ? String(err.message || 'unknown live meeting error')
      : String(err || 'unknown live meeting error');
    this._emitError(message);
  }

  _clearAudioBuffers() {
    this._clearRollingSamples();
    this._clearSessionChunks();
  }

  _clearRollingSamples() {
    if (this._rollingSamples instanceof Float32Array) {
      this._rollingSamples.fill(0);
    }
    this._rollingSamples = null;
  }

  _clearSessionChunks() {
    for (const chunk of this._sessionChunks) {
      zeroizeByteArray(chunk);
    }
    this._sessionChunks = [];
    this._sessionBufferedBytes = 0;
  }
}

function normalizePositiveNumber(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function normalizeBytesCap(sessionRamCapMB, fallback) {
  const mb = Number(sessionRamCapMB);
  if (!Number.isFinite(mb) || mb <= 0) return fallback;
  return Math.max(1, Math.floor(mb * 1024 * 1024));
}

function normalizeSegmentSamples(audio, sampleRate, maxSegmentDurationMS) {
  if (!(audio instanceof Float32Array) || audio.length === 0) return null;
  const maxSamples = Math.max(1, Math.floor(sampleRate * (maxSegmentDurationMS / 1000)));
  const start = audio.length > maxSamples ? audio.length - maxSamples : 0;
  return new Float32Array(audio.subarray(start));
}

function zeroizeByteArray(bytes) {
  if (bytes instanceof Uint8Array) {
    bytes.fill(0);
  }
}
