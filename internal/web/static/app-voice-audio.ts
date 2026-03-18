import * as env from './app-env.js';
import * as context from './app-context.js';

const { float32ToWav, normalizeSpeechSamples } = env;
const { refs, state } = context;

const showStatus = (...args) => refs.showStatus(...args);

const VOICE_VAD_AUTO_SEND_DEFAULT = true;
const VOICE_VAD_AUTO_SEND_STORAGE_KEY = 'tabura.voiceVadAutoSend';
const VOICE_VAD_AUTO_SEND_QUERY_PARAM = 'voice_vad_auto_send';

function isFirefoxLinux() {
  const ua = String(navigator.userAgent || '').toLowerCase();
  return ua.includes('firefox') && ua.includes('linux') && !ua.includes('android');
}

export function firstNonEmptyChunkMimeType(chunks) {
  if (!Array.isArray(chunks)) return '';
  for (const chunk of chunks) {
    const mt = String(chunk?.type || '').trim();
    if (mt) return mt;
  }
  return '';
}

export function describeAudioTrack(stream) {
  const track = typeof stream?.getAudioTracks === 'function' ? stream.getAudioTracks()[0] : null;
  if (!track) return {};
  const settings = typeof track.getSettings === 'function' ? (track.getSettings() || {}) : {};
  const constraints = typeof track.getConstraints === 'function' ? (track.getConstraints() || {}) : {};
  return {
    label: String(track.label || '').trim(),
    ready_state: String(track.readyState || '').trim(),
    muted: Boolean(track.muted),
    settings,
    constraints,
  };
}

export function buildNormalizedSpeechWav(samples, sampleRate) {
  const normalized = normalizeSpeechSamples(samples);
  const wavBlob = float32ToWav(normalized.samples, sampleRate);
  if (normalized.applied && normalized.samples instanceof Float32Array && normalized.samples !== samples) {
    normalized.samples.fill(0);
  }
  return {
    blob: wavBlob,
    normalization_gain: Number(normalized.gain || 1),
    normalization_peak: Number(normalized.peak || 0),
    normalization_applied: normalized.applied === true,
  };
}

function clearPCMBackupChunks(capture) {
  const pcm = capture?._pcmBackup;
  if (!pcm || !Array.isArray(pcm.chunks)) return;
  for (const chunk of pcm.chunks) {
    if (chunk instanceof Float32Array) {
      chunk.fill(0);
    }
  }
  pcm.chunks = [];
  pcm.totalSamples = 0;
}

export function stopPCMBackupCapture(capture, { preserveSamples = true } = {}) {
  const pcm = capture?._pcmBackup;
  if (!pcm) return;
  if (pcm.processorNode) {
    try { pcm.processorNode.onaudioprocess = null; } catch (_) {}
    try { pcm.processorNode.disconnect(); } catch (_) {}
  }
  if (pcm.sourceNode) {
    try { pcm.sourceNode.disconnect(); } catch (_) {}
  }
  if (pcm.sinkNode) {
    try { pcm.sinkNode.disconnect(); } catch (_) {}
  }
  if (pcm.audioContext) {
    try { pcm.audioContext.close(); } catch (_) {}
  }
  pcm.processorNode = null;
  pcm.sourceNode = null;
  pcm.sinkNode = null;
  pcm.audioContext = null;
  if (!preserveSamples) {
    clearPCMBackupChunks(capture);
    capture._pcmBackup = null;
  }
}

export function startPCMBackupCapture(capture, stream) {
  if (!isFirefoxLinux()) return false;
  const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
  if (!AudioContextCtor) return false;
  let audioContext = null;
  let sourceNode = null;
  let processorNode = null;
  let sinkNode = null;
  try {
    audioContext = new AudioContextCtor();
    if (audioContext.state === 'suspended' && typeof audioContext.resume === 'function') {
      void audioContext.resume().catch(() => {});
    }
    if (typeof audioContext.createMediaStreamSource !== 'function' || typeof audioContext.createScriptProcessor !== 'function') {
      if (audioContext) {
        try { audioContext.close(); } catch (_) {}
      }
      return false;
    }
    sourceNode = audioContext.createMediaStreamSource(stream);
    processorNode = audioContext.createScriptProcessor(4096, 1, 1);
    sinkNode = typeof audioContext.createGain === 'function' ? audioContext.createGain() : null;
    if (sinkNode && sinkNode.gain) sinkNode.gain.value = 0;
    const backup = {
      audioContext,
      sourceNode,
      processorNode,
      sinkNode,
      sampleRate: Number(audioContext.sampleRate) || 16000,
      chunks: [],
      totalSamples: 0,
    };
    processorNode.onaudioprocess = (event) => {
      const input = event?.inputBuffer?.getChannelData?.(0);
      if (!(input instanceof Float32Array) || input.length === 0) return;
      const copy = new Float32Array(input.length);
      copy.set(input);
      backup.chunks.push(copy);
      backup.totalSamples += copy.length;
    };
    sourceNode.connect(processorNode);
    if (sinkNode) {
      processorNode.connect(sinkNode);
      sinkNode.connect(audioContext.destination);
    } else {
      processorNode.connect(audioContext.destination);
    }
    capture._pcmBackup = backup;
    return true;
  } catch (_) {
    try { processorNode?.disconnect(); } catch (_) {}
    try { sourceNode?.disconnect(); } catch (_) {}
    try { sinkNode?.disconnect(); } catch (_) {}
    try { audioContext?.close(); } catch (_) {}
    return false;
  }
}

export function takePCMBackupWavBlob(capture) {
  const pcm = capture?._pcmBackup;
  if (!pcm || !Array.isArray(pcm.chunks) || pcm.totalSamples <= 0) return null;
  const merged = new Float32Array(pcm.totalSamples);
  let offset = 0;
  for (const chunk of pcm.chunks) {
    if (!(chunk instanceof Float32Array) || chunk.length === 0) continue;
    merged.set(chunk, offset);
    offset += chunk.length;
  }
  clearPCMBackupChunks(capture);
  if (offset <= 0) {
    merged.fill(0);
    return null;
  }
  const samples = offset === merged.length ? merged : new Float32Array(merged.subarray(0, offset));
  const normalized = buildNormalizedSpeechWav(samples, pcm.sampleRate || 16000);
  merged.fill(0);
  if (samples !== merged) {
    samples.fill(0);
  }
  if (!(normalized.blob instanceof Blob) || normalized.blob.size <= 44) return null;
  return normalized;
}

export function canUseMicrophoneCapture() {
  return Boolean(window.MediaRecorder)
    && Boolean(navigator.mediaDevices)
    && typeof navigator.mediaDevices.getUserMedia === 'function';
}

const MIC_CAPTURE_CONSTRAINTS = {
  echoCancellation: true,
  autoGainControl: true,
  noiseSuppression: true,
  channelCount: 1,
  volume: 1.0,
};

let cachedMicStream = null;
let micStreamPromise = null;
let cachedMicStreamCleanup = null;
let micRefreshRequested = false;

function detachCachedMicStreamObservers() {
  if (typeof cachedMicStreamCleanup === 'function') {
    try {
      cachedMicStreamCleanup();
    } catch (_) {}
  }
  cachedMicStreamCleanup = null;
}

export function requestMicRefresh() {
  micRefreshRequested = true;
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
  const stream = cachedMicStream;
  detachCachedMicStreamObservers();
  cachedMicStream = null;
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
    if (cachedMicStream === stream) {
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

  cachedMicStreamCleanup = () => {
    for (const dispose of disposers) {
      try { dispose(); } catch (_) {}
    }
  };
}

export function acquireMicStream() {
  if (cachedMicStream && !micRefreshRequested && streamHasLiveAudioTrack(cachedMicStream)) {
    return Promise.resolve(cachedMicStream);
  }
  if (cachedMicStream) invalidateCachedMicStream({ stopTracks: false });
  if (micStreamPromise) return micStreamPromise;
  micStreamPromise = navigator.mediaDevices.getUserMedia({
    audio: { ...MIC_CAPTURE_CONSTRAINTS },
  }).then((stream) => {
    const track = typeof stream?.getAudioTracks === 'function' ? stream.getAudioTracks()[0] : null;
    if (track && typeof track.applyConstraints === 'function') {
      void track.applyConstraints({ ...MIC_CAPTURE_CONSTRAINTS }).catch(() => {});
    }
    micRefreshRequested = false;
    cachedMicStream = stream;
    observeCachedMicStream(stream);
    micStreamPromise = null;
    return stream;
  }).catch((err) => {
    micStreamPromise = null;
    throw err;
  });
  return micStreamPromise;
}

export function releaseMicStream({ force = false } = {}) {
  if (!cachedMicStream) return;
  const activeCapture = state.chatVoiceCapture;
  if (!force && activeCapture && activeCapture.mediaStream === cachedMicStream && !activeCapture.stopping) {
    return;
  }
  invalidateCachedMicStream({ stopTracks: true });
}

export function parseOptionalBoolean(value) {
  if (typeof value === 'boolean') return value;
  const normalized = String(value || '').trim().toLowerCase();
  if (!normalized) return null;
  if (normalized === '1' || normalized === 'true' || normalized === 'on' || normalized === 'yes') return true;
  if (normalized === '0' || normalized === 'false' || normalized === 'off' || normalized === 'no') return false;
  return null;
}

export function isVoiceVADAutoSendEnabled() {
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

export function openPrintView(url) {
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
  if (Array.isArray(window.__harnessLog)) {
    window.__harnessLog.push({ type: 'print', action: 'open', url: nextURL });
  }
  showStatus('print view opened');
}
