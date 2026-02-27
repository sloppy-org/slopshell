const HOTWORD_VENDOR_BASE = '/static/vendor/openwakeword';
const HOTWORD_MODEL_FILES = {
  mel: `${HOTWORD_VENDOR_BASE}/melspectrogram.onnx`,
  embedding: `${HOTWORD_VENDOR_BASE}/embedding_model.onnx`,
  keyword: `${HOTWORD_VENDOR_BASE}/alexa.onnx`,
};
const HOTWORD_DEFAULT_THRESHOLD = 0.5;
const HOTWORD_DETECTION_COOLDOWN_MS = 1500;
const HOTWORD_TARGET_SAMPLE_RATE = 16000;
const HOTWORD_FRAME_MS = 80;
const HOTWORD_TARGET_FRAME_SAMPLES = Math.floor((HOTWORD_TARGET_SAMPLE_RATE * HOTWORD_FRAME_MS) / 1000);
const HOTWORD_MEL_CONTEXT_SAMPLES = 160 * 3;
const HOTWORD_MEL_BANDS = 32;
const HOTWORD_MEL_WINDOW = 76;
const HOTWORD_EMBEDDING_DIM = 96;
const HOTWORD_KEYWORD_FRAMES = 16;
const HOTWORD_RAW_BUFFER_MAX = HOTWORD_TARGET_SAMPLE_RATE * 10;
const HOTWORD_MEL_BUFFER_MAX = 10 * 97;
const HOTWORD_FEATURE_BUFFER_MAX = 120;
const HOTWORD_RING_BUFFER_SIZE = HOTWORD_TARGET_SAMPLE_RATE * 2;

const listeners = new Set();

const state = {
  initialized: false,
  available: false,
  active: false,
  threshold: HOTWORD_DEFAULT_THRESHOLD,
  mode: 'none',
  mock: null,
  model: null,
  audioCtx: null,
  sourceNode: null,
  processorNode: null,
  sinkNode: null,
  micStream: null,
  targetSampleBuffer: new Float32Array(0),
  pendingFrames: [],
  processingFrames: false,
  lastDetectionAt: 0,
};

const ringBuffer = {
  buffer: new Float32Array(HOTWORD_RING_BUFFER_SIZE),
  writePos: 0,
  filled: 0,
};

function resetRingBuffer() {
  ringBuffer.buffer = new Float32Array(HOTWORD_RING_BUFFER_SIZE);
  ringBuffer.writePos = 0;
  ringBuffer.filled = 0;
}

function writeRingBuffer(samples) {
  const buf = ringBuffer.buffer;
  const size = buf.length;
  for (let i = 0; i < samples.length; i += 1) {
    buf[ringBuffer.writePos] = samples[i];
    ringBuffer.writePos = (ringBuffer.writePos + 1) % size;
  }
  ringBuffer.filled = Math.min(ringBuffer.filled + samples.length, size);
}

const pipeline = {
  rawBuffer: null,
  rawLen: 0,
  melBuffer: null,
  melLen: 0,
  featureBuffer: null,
  featureLen: 0,
};

function resetPipeline() {
  pipeline.rawBuffer = new Float32Array(HOTWORD_RAW_BUFFER_MAX);
  pipeline.rawLen = 0;
  pipeline.melBuffer = new Float32Array(HOTWORD_MEL_BUFFER_MAX * HOTWORD_MEL_BANDS);
  pipeline.melLen = 0;
  pipeline.featureBuffer = new Float32Array(HOTWORD_FEATURE_BUFFER_MAX * HOTWORD_EMBEDDING_DIM);
  pipeline.featureLen = 0;
}

function appendRaw(samples) {
  const need = pipeline.rawLen + samples.length;
  if (need > HOTWORD_RAW_BUFFER_MAX) {
    const drop = need - HOTWORD_RAW_BUFFER_MAX;
    pipeline.rawBuffer.copyWithin(0, drop, pipeline.rawLen);
    pipeline.rawLen -= drop;
  }
  pipeline.rawBuffer.set(samples, pipeline.rawLen);
  pipeline.rawLen += samples.length;
}

function appendMelFrames(data, nFrames) {
  const elems = nFrames * HOTWORD_MEL_BANDS;
  const totalRows = pipeline.melLen + nFrames;
  if (totalRows > HOTWORD_MEL_BUFFER_MAX) {
    const dropRows = totalRows - HOTWORD_MEL_BUFFER_MAX;
    const dropElems = dropRows * HOTWORD_MEL_BANDS;
    pipeline.melBuffer.copyWithin(0, dropElems, pipeline.melLen * HOTWORD_MEL_BANDS);
    pipeline.melLen -= dropRows;
  }
  const offset = pipeline.melLen * HOTWORD_MEL_BANDS;
  for (let i = 0; i < elems; i += 1) {
    pipeline.melBuffer[offset + i] = data[i] / 10 + 2;
  }
  pipeline.melLen += nFrames;
}

function appendEmbedding(data) {
  if (pipeline.featureLen >= HOTWORD_FEATURE_BUFFER_MAX) {
    pipeline.featureBuffer.copyWithin(0, HOTWORD_EMBEDDING_DIM, pipeline.featureLen * HOTWORD_EMBEDDING_DIM);
    pipeline.featureLen -= 1;
  }
  pipeline.featureBuffer.set(data, pipeline.featureLen * HOTWORD_EMBEDDING_DIM);
  pipeline.featureLen += 1;
}

function clampNumber(value, min, max) {
  const n = Number(value);
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, n));
}

function resolveMock() {
  const candidate = window.__taburaHotwordMock;
  if (!candidate || typeof candidate !== 'object') return null;
  if (typeof candidate.init !== 'function') return null;
  if (typeof candidate.start !== 'function') return null;
  if (typeof candidate.stop !== 'function') return null;
  return candidate;
}

function loadScript(src) {
  return new Promise((resolve, reject) => {
    const existing = document.querySelector(`script[data-hotword-src="${src}"]`);
    if (existing) {
      if (existing.dataset.loaded === 'true') {
        resolve();
        return;
      }
      existing.addEventListener('load', () => resolve(), { once: true });
      existing.addEventListener('error', () => reject(new Error(`failed to load ${src}`)), { once: true });
      return;
    }

    const script = document.createElement('script');
    script.src = src;
    script.async = true;
    script.dataset.hotwordSrc = src;
    script.addEventListener('load', () => {
      script.dataset.loaded = 'true';
      resolve();
    }, { once: true });
    script.addEventListener('error', () => reject(new Error(`failed to load ${src}`)), { once: true });
    document.head.appendChild(script);
  });
}

async function ensureOrtRuntime() {
  if (window.ort) return window.ort;
  await loadScript(`${HOTWORD_VENDOR_BASE}/ort.min.js`);
  if (!window.ort) {
    throw new Error('onnx runtime not available after script load');
  }
  return window.ort;
}

function extractTensor(output) {
  if (!output) return null;
  if (output.data && output.dims) return output;
  if (output instanceof Float32Array) return { data: output, dims: [1, output.length] };
  if (Array.isArray(output)) {
    const arr = Float32Array.from(output.map((value) => Number(value) || 0));
    return { data: arr, dims: [1, arr.length] };
  }
  return null;
}

function tensorScore(output) {
  const tensor = extractTensor(output);
  if (!tensor || !tensor.data || tensor.data.length === 0) return 0;
  let maxValue = Number.NEGATIVE_INFINITY;
  for (let i = 0; i < tensor.data.length; i += 1) {
    const value = Number(tensor.data[i]);
    if (Number.isFinite(value) && value > maxValue) {
      maxValue = value;
    }
  }
  if (!Number.isFinite(maxValue)) return 0;
  if (maxValue >= 0 && maxValue <= 1) return maxValue;
  const logistic = 1 / (1 + Math.exp(-maxValue));
  if (!Number.isFinite(logistic)) return 0;
  return clampNumber(logistic, 0, 1);
}

function concatFloat32(a, b) {
  if (!(a instanceof Float32Array) || a.length === 0) return b;
  if (!(b instanceof Float32Array) || b.length === 0) return a;
  const out = new Float32Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

function resampleToTargetRate(samples, sourceRate) {
  if (!(samples instanceof Float32Array) || samples.length === 0) {
    return new Float32Array(0);
  }
  const srcRate = Number(sourceRate);
  if (!Number.isFinite(srcRate) || srcRate <= 0 || srcRate === HOTWORD_TARGET_SAMPLE_RATE) {
    return samples;
  }

  const ratio = srcRate / HOTWORD_TARGET_SAMPLE_RATE;
  const outLength = Math.max(1, Math.floor(samples.length / ratio));
  const out = new Float32Array(outLength);

  for (let i = 0; i < outLength; i += 1) {
    const srcPos = i * ratio;
    const left = Math.floor(srcPos);
    const right = Math.min(samples.length - 1, left + 1);
    const weight = srcPos - left;
    const leftVal = samples[left] || 0;
    const rightVal = samples[right] || 0;
    out[i] = (leftVal * (1 - weight)) + (rightVal * weight);
  }

  return out;
}

function emitHotwordDetected() {
  const now = Date.now();
  if ((now - state.lastDetectionAt) < HOTWORD_DETECTION_COOLDOWN_MS) {
    return;
  }
  state.lastDetectionAt = now;
  listeners.forEach((listener) => {
    try {
      listener();
    } catch (_) {}
  });
}

async function runSession(session, inputTensor) {
  const inputName = session.inputNames[0];
  const feed = { [inputName]: inputTensor };
  const outputs = await session.run(feed);
  return outputs[session.outputNames[0]] || null;
}

async function runPipelineOnnx(frame) {
  if (!state.model || !state.model.ort) return 0;
  const { ort, melSession, embeddingSession, keywordSession } = state.model;
  if (!keywordSession || !pipeline.rawBuffer) return 0;

  const int16Scaled = new Float32Array(frame.length);
  for (let i = 0; i < frame.length; i += 1) {
    int16Scaled[i] = Math.round(frame[i] * 32767);
  }
  appendRaw(int16Scaled);

  try {
    const melInputLen = Math.min(pipeline.rawLen, HOTWORD_TARGET_FRAME_SAMPLES + HOTWORD_MEL_CONTEXT_SAMPLES);
    if (melInputLen < 400) return 0;

    const melInputData = pipeline.rawBuffer.slice(pipeline.rawLen - melInputLen, pipeline.rawLen);
    const melTensor = new ort.Tensor('float32', melInputData, [1, melInputLen]);
    const melOut = await runSession(melSession, melTensor);
    if (!melOut) return 0;

    const dims = melOut.dims;
    const nFrames = dims[dims.length - 2];
    appendMelFrames(melOut.data, nFrames);

    if (pipeline.melLen >= HOTWORD_MEL_WINDOW) {
      const windowStart = (pipeline.melLen - HOTWORD_MEL_WINDOW) * HOTWORD_MEL_BANDS;
      const windowEnd = pipeline.melLen * HOTWORD_MEL_BANDS;
      const embInputData = new Float32Array(HOTWORD_MEL_WINDOW * HOTWORD_MEL_BANDS);
      embInputData.set(pipeline.melBuffer.subarray(windowStart, windowEnd));
      const embTensor = new ort.Tensor('float32', embInputData, [1, HOTWORD_MEL_WINDOW, HOTWORD_MEL_BANDS, 1]);
      const embOut = await runSession(embeddingSession, embTensor);
      if (!embOut) return 0;

      const embedding = new Float32Array(HOTWORD_EMBEDDING_DIM);
      for (let i = 0; i < HOTWORD_EMBEDDING_DIM; i += 1) {
        embedding[i] = embOut.data[i];
      }
      appendEmbedding(embedding);
    }

    if (pipeline.featureLen >= HOTWORD_KEYWORD_FRAMES) {
      const fStart = (pipeline.featureLen - HOTWORD_KEYWORD_FRAMES) * HOTWORD_EMBEDDING_DIM;
      const fEnd = pipeline.featureLen * HOTWORD_EMBEDDING_DIM;
      const kwInputData = new Float32Array(HOTWORD_KEYWORD_FRAMES * HOTWORD_EMBEDDING_DIM);
      kwInputData.set(pipeline.featureBuffer.subarray(fStart, fEnd));
      const kwTensor = new ort.Tensor('float32', kwInputData, [1, HOTWORD_KEYWORD_FRAMES, HOTWORD_EMBEDDING_DIM]);
      const kwOut = await runSession(keywordSession, kwTensor);
      return tensorScore(kwOut);
    }

    return 0;
  } catch (_) {
    return 0;
  }
}

async function processFrameQueue() {
  if (state.processingFrames) return;
  state.processingFrames = true;
  try {
    while (state.active && state.pendingFrames.length > 0) {
      const frame = state.pendingFrames.shift();
      if (!(frame instanceof Float32Array) || frame.length === 0) continue;
      const score = await runPipelineOnnx(frame);
      if (score >= state.threshold) {
        emitHotwordDetected();
      }
    }
  } finally {
    state.processingFrames = false;
  }
}

function onAudioProcess(event) {
  if (!state.active) return;
  const inputBuffer = event?.inputBuffer;
  if (!inputBuffer || typeof inputBuffer.getChannelData !== 'function') return;
  const channel = inputBuffer.getChannelData(0);
  if (!(channel instanceof Float32Array) || channel.length === 0) return;

  const resampled = resampleToTargetRate(channel, inputBuffer.sampleRate);
  if (resampled.length === 0) return;

  writeRingBuffer(resampled);
  state.targetSampleBuffer = concatFloat32(state.targetSampleBuffer, resampled);

  while (state.targetSampleBuffer.length >= HOTWORD_TARGET_FRAME_SAMPLES) {
    const frame = state.targetSampleBuffer.slice(0, HOTWORD_TARGET_FRAME_SAMPLES);
    state.targetSampleBuffer = state.targetSampleBuffer.slice(HOTWORD_TARGET_FRAME_SAMPLES);
    state.pendingFrames.push(frame);
  }

  void processFrameQueue();
}

function stopOnnxNodes() {
  if (state.processorNode) {
    state.processorNode.onaudioprocess = null;
    try { state.processorNode.disconnect(); } catch (_) {}
    state.processorNode = null;
  }
  if (state.sourceNode) {
    try { state.sourceNode.disconnect(); } catch (_) {}
    state.sourceNode = null;
  }
  if (state.sinkNode) {
    try { state.sinkNode.disconnect(); } catch (_) {}
    state.sinkNode = null;
  }
  if (state.audioCtx) {
    try { state.audioCtx.close(); } catch (_) {}
    state.audioCtx = null;
  }
  state.targetSampleBuffer = new Float32Array(0);
  state.pendingFrames = [];
  state.processingFrames = false;
  state.micStream = null;
  resetPipeline();
  resetRingBuffer();
}

async function startOnnxMonitor(stream) {
  const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
  if (!AudioContextCtor) return false;

  resetPipeline();
  resetRingBuffer();
  state.micStream = stream;

  const audioCtx = new AudioContextCtor();
  const sourceNode = audioCtx.createMediaStreamSource(stream);
  if (typeof audioCtx.createScriptProcessor !== 'function') {
    throw new Error('ScriptProcessor is not supported in this browser');
  }

  const processorNode = audioCtx.createScriptProcessor(4096, 1, 1);
  const sinkNode = typeof audioCtx.createGain === 'function' ? audioCtx.createGain() : null;
  if (sinkNode) {
    sinkNode.gain.value = 0;
  }

  processorNode.onaudioprocess = onAudioProcess;
  sourceNode.connect(processorNode);
  if (sinkNode) {
    processorNode.connect(sinkNode);
    sinkNode.connect(audioCtx.destination);
  } else {
    processorNode.connect(audioCtx.destination);
  }

  if (audioCtx.state === 'suspended' && typeof audioCtx.resume === 'function') {
    await audioCtx.resume().catch(() => {});
  }

  state.audioCtx = audioCtx;
  state.sourceNode = sourceNode;
  state.processorNode = processorNode;
  state.sinkNode = sinkNode;
  return true;
}

async function initOnnxModel() {
  const ort = await ensureOrtRuntime();
  if (ort?.env?.wasm) {
    ort.env.wasm.wasmPaths = `${HOTWORD_VENDOR_BASE}/`;
  }

  const sessionOptions = {
    executionProviders: ['wasm'],
    graphOptimizationLevel: 'all',
  };

  const melSession = await ort.InferenceSession.create(HOTWORD_MODEL_FILES.mel, sessionOptions);
  const embeddingSession = await ort.InferenceSession.create(HOTWORD_MODEL_FILES.embedding, sessionOptions);
  const keywordSession = await ort.InferenceSession.create(HOTWORD_MODEL_FILES.keyword, sessionOptions);

  state.model = {
    ort,
    melSession,
    embeddingSession,
    keywordSession,
  };
}

export async function initHotword(options = {}) {
  const force = Boolean(options && options.force);
  if (state.initialized && !force) return state.available;
  if (force) {
    stopHotwordMonitor();
    state.initialized = false;
    state.available = false;
    state.mode = 'none';
    state.mock = null;
    state.model = null;
    state.lastDetectionAt = 0;
  }

  state.initialized = true;
  state.threshold = HOTWORD_DEFAULT_THRESHOLD;

  const mock = resolveMock();
  if (mock) {
    state.mock = mock;
    state.mode = 'mock';
    try {
      const ok = await Promise.resolve(mock.init());
      state.available = Boolean(ok);
      if (!state.available) {
        state.mode = 'none';
        state.mock = null;
      }
      return state.available;
    } catch (_) {
      state.available = false;
      state.mode = 'none';
      state.mock = null;
      return false;
    }
  }

  try {
    await initOnnxModel();
    state.mode = 'onnx';
    state.available = true;
    return true;
  } catch (err) {
    state.available = false;
    state.mode = 'none';
    state.model = null;
    console.warn('Hotword initialization failed:', err);
    return false;
  }
}

export async function startHotwordMonitor(micStream) {
  if (!state.initialized) {
    await initHotword();
  }
  if (!state.available || !micStream) {
    return false;
  }
  if (state.active) {
    return true;
  }

  if (state.mode === 'mock' && state.mock) {
    try {
      state.mock.start(micStream, () => emitHotwordDetected());
      state.active = true;
      return true;
    } catch (_) {
      state.active = false;
      return false;
    }
  }

  if (state.mode !== 'onnx') {
    return false;
  }

  try {
    const started = await startOnnxMonitor(micStream);
    state.active = Boolean(started);
    return state.active;
  } catch (err) {
    stopOnnxNodes();
    state.active = false;
    console.warn('Hotword monitor start failed:', err);
    return false;
  }
}

export function stopHotwordMonitor() {
  if (!state.active) return;
  if (state.mode === 'mock' && state.mock) {
    try {
      state.mock.stop();
    } catch (_) {}
  }
  if (state.mode === 'onnx') {
    stopOnnxNodes();
  }
  state.active = false;
}

export function isHotwordActive() {
  return state.active;
}

export function onHotwordDetected(callback) {
  if (typeof callback !== 'function') {
    return () => {};
  }
  listeners.add(callback);
  return () => {
    listeners.delete(callback);
  };
}

export function setHotwordThreshold(value) {
  state.threshold = clampNumber(value, 0, 1);
  if (state.mode === 'mock' && state.mock && typeof state.mock.setThreshold === 'function') {
    try {
      state.mock.setThreshold(state.threshold);
    } catch (_) {}
  }
  return state.threshold;
}

export function getPreRollAudio() {
  if (ringBuffer.filled === 0) return new Float32Array(0);
  const size = ringBuffer.buffer.length;
  const len = Math.min(ringBuffer.filled, size);
  const out = new Float32Array(len);
  const startPos = (ringBuffer.writePos - len + size) % size;
  if (startPos + len <= size) {
    out.set(ringBuffer.buffer.subarray(startPos, startPos + len));
  } else {
    const firstPart = size - startPos;
    out.set(ringBuffer.buffer.subarray(startPos, size), 0);
    out.set(ringBuffer.buffer.subarray(0, len - firstPart), firstPart);
  }
  return out;
}

export function getHotwordMicStream() {
  return state.micStream || null;
}
