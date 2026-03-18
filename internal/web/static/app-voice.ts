import * as env from './app-env.js';
import * as context from './app-context.js';
import {
  clearDialogueDiagnostics,
  emitDialogueServerDiagnostic,
  pushDialogueDiagnosticEvent,
  recordDialogueSTTEmpty,
  recordDialogueSTTResult,
  recordDialogueSTTStart,
  recordDialogueTranscriptSegment,
  recordDialogueVoiceError,
} from './app-dialogue-diagnostics.js';
import { DialogueTurnController } from './dialogue-turn-policy.js';
import {
  configureTurnIntelligence,
  isTurnIntelligenceConnected,
  resetTurnIntelligence,
  sendTurnConfig,
  sendTurnTranscriptSegment,
} from './turn-client.js';
import {
  acquireMicStream,
  buildNormalizedSpeechWav,
  canUseMicrophoneCapture,
  describeAudioTrack,
  firstNonEmptyChunkMimeType,
  isVoiceVADAutoSendEnabled,
  openPrintView,
  releaseMicStream,
  requestMicRefresh,
  startPCMBackupCapture,
  stopPCMBackupCapture,
  takePCMBackupWavBlob,
} from './app-voice-audio.js';
export {
  acquireMicStream,
  openPrintView,
  releaseMicStream,
  requestMicRefresh,
} from './app-voice-audio.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, resumeDialogueListen, setDialogueTTSBargeInMode, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const stopTTSPlayback = (...args) => refs.stopTTSPlayback(...args);
const exitPrReviewMode = (...args) => refs.exitPrReviewMode(...args);
const abortPendingSubmit = (...args) => refs.abortPendingSubmit(...args);
const submitMessage = (...args) => refs.submitMessage(...args);
const canSpeakTTS = (...args) => refs.canSpeakTTS(...args);
const requestHotwordSync = (...args) => refs.requestHotwordSync(...args);
const isDialogueLiveSession = (...args) => refs.isDialogueLiveSession(...args);
const shouldShowCompanionIdleSurface = (...args) => refs.shouldShowCompanionIdleSurface(...args);
const persistLastView = (...args) => refs.persistLastView(...args);
const exitArtifactEditMode = (...args) => refs.exitArtifactEditMode(...args);
const showVoiceCaptureNotice = (...args) => refs.showVoiceCaptureNotice(...args);
const microphoneUnavailableMessage = (...args) => refs.microphoneUnavailableMessage(...args);
const startVoiceLifecycleOp = (...args) => refs.startVoiceLifecycleOp(...args);
const setVoiceLifecycle = (...args) => refs.setVoiceLifecycle(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const isUiReadyForStatus = (...args) => refs.isUiReadyForStatus(...args);
const syncVoiceLifecycle = (...args) => refs.syncVoiceLifecycle(...args);
const maybeHandleDictationTranscript = (...args) => refs.maybeHandleDictationTranscript(...args);
const beginConversationVoiceCapture = (...args) => refs.beginConversationVoiceCapture(...args);
const isVoiceMailActive = () => Boolean(refs.isVoiceMailActive?.());
const handleVoiceMailTranscript = (...args) => refs.handleVoiceMailTranscript(...args);

const VOICE_VAD_NO_SPEECH_MS = 4000;
const VOICE_VAD_MAX_RECORDING_HARD_MS = 240000;
const HOTWORD_VAD_NO_SPEECH_MS = 7000;
const VOICE_VAD_RECORDER_CHUNK_MS = 250;
const VOICE_CAPTURE_STOP_FLUSH_TIMEOUT_MS = 1500;

const VOICE_TRIGGER_SOURCE_MANUAL = 'manual';
const VOICE_TRIGGER_SOURCE_HOTWORD = 'hotword';
const VOICE_TRIGGER_SOURCE_DIALOGUE = 'dialogue_listen';
const VOICE_TRIGGER_SOURCE_BARGE_IN = 'barge_in';

function submitDialogueTurn(text) {
  if (!isDialogueLiveSession()) {
    dialogueTurnController.reset();
    resetTurnIntelligence();
    return;
  }
  pushDialogueDiagnosticEvent('submit_dialogue_turn', { text: String(text || '').trim() });
  showStatus('sending...');
  state.voiceTranscriptSubmitInFlight = true;
  state.voiceAwaitingTurn = true;
  updateAssistantActivityIndicator();
  void submitMessage(text, { kind: 'voice_transcript' });
}

function reopenDialogueListen(reason) {
  if (!isDialogueLiveSession()) {
    dialogueTurnController.reset();
    resetTurnIntelligence();
    return;
  }
  state.voiceAwaitingTurn = false;
  setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, reason);
  updateAssistantActivityIndicator();
  showStatus('listening...');
  pushDialogueDiagnosticEvent('reopen_dialogue_listen', { reason: String(reason || '').trim() });
  window.setTimeout(() => {
    if (!isDialogueLiveSession()) {
      dialogueTurnController.reset();
      resetTurnIntelligence();
      return;
    }
    resumeDialogueListen();
  }, 0);
}

function handleTurnAction(payload: Record<string, any> = {}) {
  if (!isDialogueLiveSession()) {
    dialogueTurnController.reset();
    resetTurnIntelligence();
    return;
  }
  const action = String(payload?.action || '').trim().toLowerCase();
  state.dialogueDiagnostics.lastAction = {
    action,
    reason: String(payload?.reason || '').trim(),
    text: String(payload?.text || '').trim(),
    wait_ms: Number(payload?.wait_ms || 0),
    rollback_audio_ms: Number(payload?.rollback_audio_ms || 0),
    interrupt_assistant: payload?.interrupt_assistant === true,
  };
  pushDialogueDiagnosticEvent('turn_action', state.dialogueDiagnostics.lastAction);
  if (action === 'yield') {
    const interruptedAssistant = payload?.interrupt_assistant === true;
    if (interruptedAssistant) {
      beginConversationVoiceCapture('barge_in');
    }
    return;
  }
  if (action === 'continue_listening') {
    reopenDialogueListen('dialogue-turn-continue');
    return;
  }
  if (action === 'backchannel') {
    reopenDialogueListen('dialogue-turn-backchannel');
    return;
  }
  if (action === 'finalize_user_turn') {
    const text = String(payload?.text || '').trim();
    if (text) {
      submitDialogueTurn(text);
    } else {
      reopenDialogueListen('dialogue-turn-empty-finalize');
    }
  }
}

const dialogueTurnController = new DialogueTurnController({
  onFinalize(text) {
    submitDialogueTurn(text);
  },
  onContinue() {
    reopenDialogueListen('dialogue-turn-continue');
  },
  onBackchannel() {
    reopenDialogueListen('dialogue-turn-backchannel');
  },
});

configureTurnIntelligence({
  profile: state.turnPolicyProfile,
  evalLoggingEnabled: state.turnEvalLoggingEnabled !== false,
  onAction(payload) {
    handleTurnAction(payload || {});
  },
  onReady(payload) {
    const metrics = payload?.metrics || null;
    if (!state.dialogueDiagnostics) clearDialogueDiagnostics();
    state.dialogueDiagnostics.connected = true;
    state.dialogueDiagnostics.sessionId = String(payload?.session_id || state.chatSessionId || '').trim();
    state.dialogueDiagnostics.profile = String(payload?.profile || state.turnPolicyProfile || 'balanced').trim().toLowerCase() || 'balanced';
    state.dialogueDiagnostics.evalLoggingEnabled = payload?.eval_logging_enabled !== false;
    state.dialogueDiagnostics.readyAt = Date.now();
    if (metrics) {
      state.dialogueDiagnostics.lastMetrics = metrics;
    }
    pushDialogueDiagnosticEvent('turn_ready', {
      session_id: state.dialogueDiagnostics.sessionId,
      profile: state.dialogueDiagnostics.profile,
      eval_logging_enabled: state.dialogueDiagnostics.evalLoggingEnabled,
    });
  },
  onMetrics(payload) {
    const metrics = payload?.metrics || null;
    if (!metrics) return;
    if (!state.dialogueDiagnostics) clearDialogueDiagnostics();
    state.dialogueDiagnostics.connected = isTurnIntelligenceConnected();
    state.dialogueDiagnostics.profile = String(metrics?.profile || state.turnPolicyProfile || 'balanced').trim().toLowerCase() || 'balanced';
    state.dialogueDiagnostics.evalLoggingEnabled = metrics?.eval_logging_enabled !== false;
    state.dialogueDiagnostics.lastMetrics = metrics;
    const lastUpdate = String(metrics?.metadata?.last_update || '').trim();
    if (lastUpdate && ['action', 'profile', 'eval_logging', 'reset', 'playback'].includes(lastUpdate)) {
      pushDialogueDiagnosticEvent('turn_metrics', {
        last_update: lastUpdate,
        last_action: String(metrics?.last_action || '').trim(),
        last_reason: String(metrics?.last_reason || '').trim(),
        playback_active: Boolean(metrics?.playback_active),
        played_audio_ms: Number(metrics?.played_audio_ms || 0),
        speech_starts: Number(metrics?.speech_starts || 0),
        overlap_yields: Number(metrics?.speech_overlap_yields || 0),
        continuation_timeouts: Number(metrics?.continuation_timeouts || 0),
      });
    }
  },
});

function normalizeVoiceTriggerSource(value: unknown): string {
  const normalized = String(value || '').trim().toLowerCase();
  if (normalized === VOICE_TRIGGER_SOURCE_HOTWORD) return VOICE_TRIGGER_SOURCE_HOTWORD;
  if (normalized === VOICE_TRIGGER_SOURCE_DIALOGUE) return VOICE_TRIGGER_SOURCE_DIALOGUE;
  if (normalized === VOICE_TRIGGER_SOURCE_BARGE_IN) return VOICE_TRIGGER_SOURCE_BARGE_IN;
  return VOICE_TRIGGER_SOURCE_MANUAL;
}

export function newMediaRecorder(stream) {
  const candidates = isFirefoxLinux()
    ? ['audio/webm;codecs=opus', 'audio/ogg;codecs=opus']
    : ['audio/ogg;codecs=opus', 'audio/webm;codecs=opus'];
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

export function isLikelyIOS() {
  const ua = String(navigator.userAgent || '').toLowerCase();
  return /iphone|ipad|ipod/.test(ua)
    || (ua.includes('macintosh') && navigator.maxTouchPoints > 1);
}

function isFirefoxLinux() {
  const ua = String(navigator.userAgent || '').toLowerCase();
  return ua.includes('firefox') && ua.includes('linux') && !ua.includes('android');
}

let _sttMimeType = '';
let _sttParts = [];
let _sttActive = false;
let _sttAbortController = null;

function recordHarnessSTTAction(action, payload = {}) {
  if (!Array.isArray(window.__harnessLog)) return;
  window.__harnessLog.push({ type: 'stt', action, ...payload });
}

export function sttStart(mimeType) {
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

export function sttSendBlob(blob) {
  if (!_sttActive) return Promise.resolve();
  if (!blob || blob.size <= 0) return Promise.resolve();
  _sttParts.push(blob);
  recordHarnessSTTAction('append', { bytes: Number(blob.size) || 0 });
  return Promise.resolve();
}

export function sttStop() {
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

export function sttCancel() {
  _sttActive = false;
  _sttMimeType = '';
  _sttParts = [];
  if (_sttAbortController) {
    try { _sttAbortController.abort(); } catch (_) {}
    _sttAbortController = null;
  }
  recordHarnessSTTAction('cancel');
}

export function handleSTTWSMessage(payload) {
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

export function stopChatVoiceMedia(capture) {
  if (!capture) return;
  if (capture.vadState?.isRunning) {
    stopVADMonitor(capture);
  }
  stopPCMBackupCapture(capture, { preserveSamples: true });
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

export function startVADMonitor(capture) {
  if (!isVoiceVADAutoSendEnabled()) return;
  if (!capture || capture.vadState) return;
  if (!capture.mediaStream) return;
  void startSileroVADMonitor(capture);
}

export async function startSileroVADMonitor(capture) {
  const triggerSource = normalizeVoiceTriggerSource(capture?.triggerSource);
  const isHotwordCapture = triggerSource === VOICE_TRIGGER_SOURCE_HOTWORD;
  const vadNoSpeechMs = isHotwordCapture ? HOTWORD_VAD_NO_SPEECH_MS : VOICE_VAD_NO_SPEECH_MS;

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
      redemptionMs: isHotwordCapture ? 1400 : undefined,
      minSpeechMs: isHotwordCapture ? 400 : undefined,
      onSpeechStart() {
        if (!vadState.isRunning || vadState.committed) return;
        emitDialogueServerDiagnostic('voice_capture_vad_speech_start', {
          trigger_source: triggerSource,
        });
        if (state.chatVoiceCapture === capture) {
          capture.speechDetected = true;
          setVoiceLifecycle(VOICE_LIFECYCLE.LISTENING, 'voice-capture-vad-speech-start');
          updateAssistantActivityIndicator();
        }
        if (vadState.noSpeechTimer) {
          window.clearTimeout(vadState.noSpeechTimer);
          vadState.noSpeechTimer = null;
        }
      },
      onSpeechEnd(audio) {
        if (!vadState.isRunning || vadState.committed) return;
        vadState.committed = true;
        emitDialogueServerDiagnostic('voice_capture_vad_speech_end', {
          trigger_source: triggerSource,
          samples: audio instanceof Float32Array ? audio.length : 0,
        });
        if (audio instanceof Float32Array && audio.length > 0) {
          const normalized = buildNormalizedSpeechWav(audio, 16000);
          capture._vadAudioBlob = normalized.blob;
          capture._vadAudioNormalization = normalized;
          capture._vadAudioDurationMs = Math.round((audio.length / 16000) * 1000);
          capture._vadAutoStopped = true;
        }
        stopVADMonitor(capture);
        void stopVoiceCaptureAndSend();
      },
      onError(err) {
        emitDialogueServerDiagnostic('voice_capture_vad_error', {
          trigger_source: triggerSource,
          message: String(err?.message || err || 'unknown error'),
        });
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

export function stopChatVoiceMediaAndFlush(capture) {
  if (!capture?.mediaRecorder) {
    stopChatVoiceMedia(capture);
    return Promise.resolve();
  }
  const recorder = capture.mediaRecorder;
  if (recorder.state === 'inactive') {
    stopChatVoiceMedia(capture);
    return Promise.resolve();
  }
  return new Promise<void>((resolve) => {
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
      // Some desktop browsers enqueue the final dataavailable chunk just after
      // stop. Give the recorder a brief settle window so we do not submit an
      // empty blob and misclassify real speech as "recording too short".
      timeoutId = window.setTimeout(finish, 120);
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

export async function beginVoiceCapture(x, y, anchor, options: Record<string, any> = {}) {
  if (state.chatVoiceCapture) return;
  if (!canUseMicrophoneCapture()) {
    showVoiceCaptureNotice(microphoneUnavailableMessage(), x, y);
    return;
  }
  const triggerSource = normalizeVoiceTriggerSource(options?.triggerSource);
  if (triggerSource === VOICE_TRIGGER_SOURCE_MANUAL || !isDialogueLiveSession()) {
    dialogueTurnController.reset();
    resetTurnIntelligence();
  }
  cancelLiveSessionListen();
  const interruptedAssistant = Boolean(state.ttsPlaying);
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

  const capture: Record<string, any> = {
    active: false,
    stopping: false,
    stopRequested: false,
    autoSend: true,
    triggerSource,
    interruptedAssistant,
    mediaStream: null,
    mediaRecorder: null,
    chunks: [],
    recorderChunkCount: 0,
    recorderChunkBytes: 0,
    startedAtMs: Date.now(),
    speechDetected: false,
  };
  emitDialogueServerDiagnostic('voice_capture_begin', {
    trigger_source: triggerSource,
    firefox_linux: isFirefoxLinux(),
    live_dialogue: isDialogueLiveSession(),
  });
  state.chatVoiceCapture = capture;
  state.lastInputOrigin = 'voice';
  state.voiceAwaitingTurn = false;
  state.dialogueSpeechRecognizedAt = 0;
  state.indicatorSuppressedByCanvasUpdate = false;
  startVoiceLifecycleOp('voice-capture-begin');
  setVoiceLifecycle(VOICE_LIFECYCLE.RECORDING, 'voice-capture-begin');
  setLastInputPosition(x, y);
  setRecording(true);
  setInputAnchor(anchor || null);
  updateAssistantActivityIndicator();
  if (!shouldShowCompanionIdleSurface()) {
    showStatus('recording...');
  }
  try {
    const stream = await acquireMicStream();
    if (state.chatVoiceCapture !== capture) {
      if (vadAudioContext) { try { vadAudioContext.close(); } catch (_) {} }
      return;
    }
    const recorder = newMediaRecorder(stream);
    capture.mimeType = String(recorder?.mimeType || '').trim();
    emitDialogueServerDiagnostic('voice_capture_stream_ready', {
      trigger_source: triggerSource,
      audio_tracks: typeof stream?.getAudioTracks === 'function' ? stream.getAudioTracks().length : 0,
      track: describeAudioTrack(stream),
    });
    if (state.chatVoiceCapture !== capture) {
      if (vadAudioContext) { try { vadAudioContext.close(); } catch (_) {} }
      return;
    }
    capture.mediaStream = stream;
    capture.mediaRecorder = recorder;
    capture._vadAudioContext = vadAudioContext;
    vadAudioContext = null;
    const pcmBackupStarted = startPCMBackupCapture(capture, stream);
    emitDialogueServerDiagnostic('voice_recorder_ready', {
      trigger_source: triggerSource,
      mime_type: capture.mimeType || '',
      pcm_backup_started: pcmBackupStarted,
      track: describeAudioTrack(stream),
    });
    capture.active = true;
    recorder.addEventListener('dataavailable', (ev) => {
      if (!ev?.data || ev.data.size <= 0) return;
      capture.chunks.push(ev.data);
      capture.recorderChunkCount += 1;
      capture.recorderChunkBytes += Number(ev.data.size) || 0;
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

export async function stopVoiceCaptureAndSend() {
  const capture = state.chatVoiceCapture;
  if (!capture || capture.stopping) return;
  const opSeq = startVoiceLifecycleOp('voice-capture-stop-send');
  const triggerSource = normalizeVoiceTriggerSource(capture?.triggerSource);
  const isDialogueAutoCapture = isDialogueLiveSession()
    && capture?._vadAutoStopped === true
    && triggerSource !== VOICE_TRIGGER_SOURCE_MANUAL;
  capture.stopRequested = true;
  if (!capture.active) return;
  capture.stopping = true;
  setRecording(false);
  setVoiceLifecycle(VOICE_LIFECYCLE.STOPPING_RECORDING, 'voice-capture-stop-send');
  state.voiceAwaitingTurn = false;
  setVoiceLifecycle(VOICE_LIFECYCLE.LISTENING, 'voice-stt-start');
  state.indicatorSuppressedByCanvasUpdate = false;
  updateAssistantActivityIndicator();
  showStatus('transcribing...');
  let remoteStopped = false;
  let reopenDialogueListen = false;
  try {
    let sttBlob = null;
    let mimeType = '';
    let sttSource = '';
    let normalizationGain = 1;
    let normalizationPeak = 0;
    let normalizationApplied = false;
    if (capture._vadAudioBlob) {
      // VAD auto-stop: use speech audio directly, skip MediaRecorder flush
      // so Safari cannot interfere via its broken stop/dataavailable ordering.
      sttBlob = capture._vadAudioBlob;
      mimeType = 'audio/wav';
      sttSource = 'vad_blob';
      capture._vadAudioBlob = null;
      normalizationGain = Number(capture?._vadAudioNormalization?.normalization_gain || 1);
      normalizationPeak = Number(capture?._vadAudioNormalization?.normalization_peak || 0);
      normalizationApplied = capture?._vadAudioNormalization?.normalization_applied === true;
      capture._vadAudioNormalization = null;
    } else {
      // Manual stop / timeout: flush MediaRecorder and use its chunks.
      await stopChatVoiceMediaAndFlush(capture);
      const pcmFallbackBlob = takePCMBackupWavBlob(capture);
      if (isFirefoxLinux() && pcmFallbackBlob) {
        sttBlob = pcmFallbackBlob.blob;
        mimeType = 'audio/wav';
        sttSource = 'pcm_backup';
        normalizationGain = Number(pcmFallbackBlob.normalization_gain || 1);
        normalizationPeak = Number(pcmFallbackBlob.normalization_peak || 0);
        normalizationApplied = pcmFallbackBlob.normalization_applied === true;
      } else {
        mimeType = String(capture.mimeType || '').trim();
        if (!mimeType) {
          mimeType = firstNonEmptyChunkMimeType(capture.chunks);
        }
        if (capture.chunks.length > 0) {
          sttBlob = mimeType
            ? new Blob(capture.chunks, { type: mimeType })
            : new Blob(capture.chunks);
          sttSource = 'recorder';
          if (!mimeType) {
            mimeType = String(sttBlob?.type || '').trim();
          }
          capture.chunks = [];
        }
        if (!mimeType) {
          mimeType = isLikelyIOS() ? 'audio/mp4' : 'audio/webm';
        }
        if (!sttBlob && pcmFallbackBlob) {
          sttBlob = pcmFallbackBlob.blob;
          mimeType = 'audio/wav';
          sttSource = 'pcm_backup';
          normalizationGain = Number(pcmFallbackBlob.normalization_gain || 1);
          normalizationPeak = Number(pcmFallbackBlob.normalization_peak || 0);
          normalizationApplied = pcmFallbackBlob.normalization_applied === true;
        }
      }
    }
    emitDialogueServerDiagnostic('voice_capture_finalize', {
      trigger_source: triggerSource,
      source: sttSource || 'unknown',
      mime_type: mimeType || '',
      recorder_chunk_count: Number(capture.recorderChunkCount || 0),
      recorder_chunk_bytes: Number(capture.recorderChunkBytes || 0),
      pcm_backup_samples: Number(capture?._pcmBackup?.totalSamples || 0),
      normalization_gain: normalizationGain,
      normalization_peak: normalizationPeak,
      normalization_applied: normalizationApplied,
      upload_bytes: Number(sttBlob?.size || 0),
    });
    recordDialogueSTTStart(triggerSource, mimeType, Boolean(sttBlob && capture._vadAudioDurationMs));
    sttStart(mimeType);
    if (sttBlob) {
      await sttSendBlob(sttBlob);
    }
    const result = await sttStop();
    remoteStopped = true;
    const transcript = String(result?.text || '').trim();
    emitDialogueServerDiagnostic('voice_stt_result', {
      trigger_source: triggerSource,
      reason: String(result?.reason || '').trim(),
      chars: transcript.length,
    });
    if (!transcript) {
      recordDialogueSTTEmpty(triggerSource, result?.reason);
      if (isDialogueLiveSession() && triggerSource !== VOICE_TRIGGER_SOURCE_MANUAL) {
        state.voiceAwaitingTurn = false;
        reopenDialogueListen = true;
        return;
      }
      throw new Error(voiceCaptureEmptyReasonMessage(result?.reason));
    }
    const segmentDurationMs = Math.max(
      0,
      Number(capture._vadAudioDurationMs || 0) || (Date.now() - Number(capture.startedAtMs || Date.now())),
    );
    state.dialogueSpeechRecognizedAt = Date.now();
    recordDialogueSTTResult(triggerSource, transcript.length, segmentDurationMs, Boolean(capture.interruptedAssistant));
    if (isDialogueAutoCapture) {
      state.voiceAwaitingTurn = true;
      setVoiceLifecycle(VOICE_LIFECYCLE.AWAITING_TURN, 'dialogue-turn-segment');
      updateAssistantActivityIndicator();
      if (isTurnIntelligenceConnected()) {
        if (sendTurnTranscriptSegment(transcript, segmentDurationMs, Boolean(capture.interruptedAssistant))) {
          recordDialogueTranscriptSegment(transcript.length, segmentDurationMs, Boolean(capture.interruptedAssistant), 'turn_intelligence');
          return;
        }
      }
      recordDialogueTranscriptSegment(transcript.length, segmentDurationMs, Boolean(capture.interruptedAssistant), 'local_policy');
      dialogueTurnController.consume({
        text: transcript,
        durationMs: segmentDurationMs,
        interruptedAssistant: Boolean(capture.interruptedAssistant),
      });
      return;
    }
    if (isVoiceMailActive()) {
      await handleVoiceMailTranscript(transcript);
      dialogueTurnController.reset();
      state.voiceAwaitingTurn = false;
      setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-mail-transcript-finished');
    } else if (await maybeHandleDictationTranscript(transcript)) {
      dialogueTurnController.reset();
      state.voiceAwaitingTurn = false;
      setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'dictation-transcript-finished');
    } else {
      dialogueTurnController.reset();
      state.voiceAwaitingTurn = true;
      setVoiceLifecycle(VOICE_LIFECYCLE.AWAITING_TURN, 'voice-transcript-submit');
      showStatus('sending...');
      state.voiceTranscriptSubmitInFlight = true;
      void submitMessage(transcript, { kind: 'voice_transcript' });
    }
  } catch (err) {
    dialogueTurnController.reset();
    if (opSeq !== state.voiceLifecycleSeq) return;
    state.voiceAwaitingTurn = false;
    setVoiceLifecycle(VOICE_LIFECYCLE.IDLE, 'voice-capture-stop-failed');
    updateAssistantActivityIndicator();
    const message = String(err?.message || err || 'voice capture failed');
    recordDialogueVoiceError(triggerSource, message);
    emitDialogueServerDiagnostic('voice_capture_error', {
      trigger_source: triggerSource,
      message,
    });
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
    stopPCMBackupCapture(capture, { preserveSamples: false });
    if (opSeq === state.voiceLifecycleSeq) {
      syncVoiceLifecycle('voice-capture-stop-finished');
    }
    updateAssistantActivityIndicator();
    if (reopenDialogueListen && isDialogueLiveSession()) {
      window.setTimeout(() => {
        if (!isDialogueLiveSession()) return;
        resumeDialogueListen();
      }, 0);
    }
  }
}

export function resetDialogueTurnController() {
  dialogueTurnController.reset();
  resetTurnIntelligence();
}

export function cancelChatVoiceCapture() {
  const capture = state.chatVoiceCapture;
  if (!capture) return;
  dialogueTurnController.reset();
  resetTurnIntelligence();
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
