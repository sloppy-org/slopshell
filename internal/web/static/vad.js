const VAD_CDN_URL = 'https://cdn.jsdelivr.net/npm/@ricky0123/vad-web@0.0.30/dist/bundle.min.js';
const VAD_MODEL_URL = 'https://cdn.jsdelivr.net/npm/@ricky0123/vad-web@0.0.30/dist/silero_vad_v5.onnx';
const VAD_WORKLET_URL = 'https://cdn.jsdelivr.net/npm/@ricky0123/vad-web@0.0.30/dist/vad.worklet.bundle.min.js';
const ORT_CDN_URL = 'https://cdn.jsdelivr.net/npm/onnxruntime-web@1.21.0/dist/ort.min.mjs';
const ORT_WASM_PATH = 'https://cdn.jsdelivr.net/npm/onnxruntime-web@1.21.0/dist/';

const state = {
  loaded: false,
  loading: null,
  available: false,
  mock: null,
};

function resolveMock() {
  const candidate = window.__taburaVadMock;
  if (!candidate || typeof candidate !== 'object') return null;
  if (typeof candidate.init !== 'function') return null;
  return candidate;
}

async function loadRuntime() {
  if (state.loaded) return;
  if (state.loading) {
    await state.loading;
    return;
  }
  state.loading = (async () => {
    try {
      const mock = resolveMock();
      if (mock) {
        state.mock = mock;
        const ok = await Promise.resolve(mock.init());
        state.available = Boolean(ok);
        state.loaded = true;
        return;
      }
      await import(ORT_CDN_URL);
      if (typeof ort !== 'undefined' && ort.env?.wasm) {
        ort.env.wasm.wasmPaths = ORT_WASM_PATH;
        ort.env.wasm.numThreads = 1;
      }
      await new Promise((resolve, reject) => {
        const script = document.createElement('script');
        script.src = VAD_CDN_URL;
        script.onload = resolve;
        script.onerror = () => reject(new Error('vad-web bundle load failed'));
        document.head.appendChild(script);
      });
      state.available = typeof vad !== 'undefined' && typeof vad.MicVAD === 'function';
      state.loaded = true;
    } catch (err) {
      console.warn('Silero VAD load failed:', err);
      state.available = false;
      state.loaded = true;
    }
  })();
  await state.loading;
  state.loading = null;
}

export function isVADAvailable() {
  return state.available;
}

export async function ensureVADLoaded() {
  await loadRuntime();
  return state.available;
}

export async function initVAD(options = {}) {
  await loadRuntime();
  if (!state.available) return null;

  const onSpeechStart = typeof options.onSpeechStart === 'function' ? options.onSpeechStart : null;
  const onSpeechEnd = typeof options.onSpeechEnd === 'function' ? options.onSpeechEnd : null;
  const onFrameProcessed = typeof options.onFrameProcessed === 'function' ? options.onFrameProcessed : null;
  const onError = typeof options.onError === 'function' ? options.onError : null;

  const positiveSpeechThreshold = Number(options.positiveSpeechThreshold) || 0.6;
  const negativeSpeechThreshold = Number(options.negativeSpeechThreshold) || 0.35;
  const redemptionFrames = Math.round((Number(options.redemptionMs) || 600) / 32);
  const minSpeechFrames = Math.round((Number(options.minSpeechMs) || 250) / 32);
  const preSpeechPadFrames = Math.round((Number(options.preSpeechPadMs) || 300) / 32);

  const stream = options.stream instanceof MediaStream ? options.stream : null;

  if (state.mock) {
    return createMockVADInstance(state.mock, {
      onSpeechStart, onSpeechEnd, onFrameProcessed, onError,
    });
  }

  try {
    const micVADOptions = {
      positiveSpeechThreshold,
      negativeSpeechThreshold,
      redemptionFrames,
      minSpeechFrames,
      preSpeechPadFrames,
      modelURL: VAD_MODEL_URL,
      workletURL: VAD_WORKLET_URL,
      ortConfig(ortInstance) {
        if (ortInstance?.env?.wasm) {
          ortInstance.env.wasm.wasmPaths = ORT_WASM_PATH;
          ortInstance.env.wasm.numThreads = 1;
        }
      },
      onSpeechStart() {
        if (onSpeechStart) onSpeechStart();
      },
      onSpeechEnd(audio) {
        if (onSpeechEnd) onSpeechEnd(audio);
      },
      onFrameProcessed(probs) {
        if (onFrameProcessed) onFrameProcessed(probs);
      },
    };
    if (stream) micVADOptions.stream = stream;
    const micVAD = await vad.MicVAD.new(micVADOptions);

    let active = false;
    return {
      start() {
        if (active) return;
        active = true;
        micVAD.start();
      },
      pause() {
        if (!active) return;
        active = false;
        micVAD.pause();
      },
      destroy() {
        active = false;
        micVAD.destroy();
      },
      isActive() {
        return active;
      },
    };
  } catch (err) {
    if (onError) onError(err);
    return null;
  }
}

function createMockVADInstance(mock, callbacks) {
  let active = false;
  const handle = mock.create ? mock.create({
    onSpeechStart: callbacks.onSpeechStart,
    onSpeechEnd: callbacks.onSpeechEnd,
    onFrameProcessed: callbacks.onFrameProcessed,
  }) : null;
  return {
    start() {
      if (active) return;
      active = true;
      if (handle && typeof handle.start === 'function') handle.start();
    },
    pause() {
      if (!active) return;
      active = false;
      if (handle && typeof handle.pause === 'function') handle.pause();
    },
    destroy() {
      active = false;
      if (handle && typeof handle.destroy === 'function') handle.destroy();
    },
    isActive() {
      return active;
    },
  };
}

export function float32ToWav(samples, sampleRate = 16000) {
  if (!(samples instanceof Float32Array) || samples.length === 0) {
    return new Blob([], { type: 'audio/wav' });
  }
  const numSamples = samples.length;
  const bytesPerSample = 2;
  const dataSize = numSamples * bytesPerSample;
  const buffer = new ArrayBuffer(44 + dataSize);
  const view = new DataView(buffer);

  writeString(view, 0, 'RIFF');
  view.setUint32(4, 36 + dataSize, true);
  writeString(view, 8, 'WAVE');
  writeString(view, 12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * bytesPerSample, true);
  view.setUint16(32, bytesPerSample, true);
  view.setUint16(34, 16, true);
  writeString(view, 36, 'data');
  view.setUint32(40, dataSize, true);

  let offset = 44;
  for (let i = 0; i < numSamples; i += 1) {
    const clamped = Math.max(-1, Math.min(1, samples[i]));
    const int16 = clamped < 0 ? clamped * 32768 : clamped * 32767;
    view.setInt16(offset, int16, true);
    offset += 2;
  }

  return new Blob([buffer], { type: 'audio/wav' });
}

function writeString(view, offset, str) {
  for (let i = 0; i < str.length; i += 1) {
    view.setUint8(offset + i, str.charCodeAt(i));
  }
}
