import { expect, test } from '@playwright/test';
import { execSync } from 'child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'fs';
import { join, resolve } from 'path';
import { tmpdir } from 'os';
import WebSocket from 'ws';

const SERVER_URL = 'http://127.0.0.1:8420';
const WS_URL = 'ws://127.0.0.1:8420';
const SESSION_COOKIE_NAME = 'tabura_session';

// ---------------------------------------------------------------------------
// Environment / .env loading
// ---------------------------------------------------------------------------

function loadEnvPassword(): string {
  if (process.env.TABURA_TEST_PASSWORD) return process.env.TABURA_TEST_PASSWORD;
  try {
    const envPath = resolve(__dirname, '../../.env');
    const lines = readFileSync(envPath, 'utf-8').split('\n');
    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed.startsWith('#') || !trimmed.includes('=')) continue;
      const [key, ...rest] = trimmed.split('=');
      if (key.trim() === 'TABURA_TEST_PASSWORD') {
        const val = rest.join('=').trim();
        if (val) return val;
      }
    }
  } catch {}
  return '';
}

const testPassword = loadEnvPassword();

// ---------------------------------------------------------------------------
// Service health probes
// ---------------------------------------------------------------------------

function serviceRunning(url: string): boolean {
  try {
    execSync(`curl -fsS --max-time 2 ${url}`, { stdio: 'pipe' });
    return true;
  } catch {
    return false;
  }
}

function ttsServiceRunning(): boolean {
  try {
    const result = execSync(
      `curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8424/v1/audio/speech -H 'Content-Type: application/json' -d '{"input":"test","voice":"en","response_format":"wav"}'`,
      { stdio: 'pipe' },
    );
    return result.toString().trim() === '200';
  } catch {
    return false;
  }
}

function ffmpegAvailable(): boolean {
  try {
    execSync('ffmpeg -version', { stdio: 'pipe' });
    return true;
  } catch {
    return false;
  }
}

function sttServiceRunning(): boolean {
  try {
    execSync('curl -fsS --max-time 2 http://127.0.0.1:8427/healthz', { stdio: 'pipe' });
    return true;
  } catch {
    return false;
  }
}

const webServerUp = serviceRunning(`${SERVER_URL}/api/setup`);

function serverNeedsAuth(): boolean {
  if (!webServerUp) return false;
  try {
    const out = execSync(`curl -sS --max-time 2 ${SERVER_URL}/api/setup`, { stdio: 'pipe' });
    const setup = JSON.parse(out.toString());
    return Boolean(setup.has_password);
  } catch {
    return false;
  }
}

const needsAuth = serverNeedsAuth();
const canAuth = !needsAuth || Boolean(testPassword);
const sttUp = webServerUp && sttServiceRunning();
const ttsUp = webServerUp && ttsServiceRunning();
const ffmpegUp = ffmpegAvailable();

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

async function authenticate(): Promise<string> {
  const setupResp = await fetch(`${SERVER_URL}/api/setup`);
  const setup = (await setupResp.json()) as Record<string, unknown>;

  if (!setup.has_password) return '';
  if (!testPassword) return '';

  const loginResp = await fetch(`${SERVER_URL}/api/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: testPassword }),
  });
  if (!loginResp.ok) {
    throw new Error(`Login failed: HTTP ${loginResp.status}`);
  }

  const setCookie = loginResp.headers.get('set-cookie') || '';
  const match = setCookie.match(new RegExp(`${SESSION_COOKIE_NAME}=([^;]+)`));
  if (!match) {
    throw new Error('Login succeeded but no session cookie returned');
  }
  return match[1];
}

// ---------------------------------------------------------------------------
// Authenticated fetch helper
// ---------------------------------------------------------------------------

async function authFetch(url: string, sessionToken: string): Promise<Response> {
  const headers: Record<string, string> = {};
  if (sessionToken) {
    headers['Cookie'] = `${SESSION_COOKIE_NAME}=${sessionToken}`;
  }
  return fetch(url, { headers });
}

async function getChatSessionId(sessionToken: string): Promise<string> {
  const resp = await authFetch(`${SERVER_URL}/api/projects`, sessionToken);
  if (!resp.ok) throw new Error(`/api/projects failed: HTTP ${resp.status}`);
  const body = (await resp.json()) as Record<string, unknown>;
  const list = (body.projects as Array<Record<string, unknown>>) || [];
  const project = list[0];
  if (!project?.chat_session_id) {
    throw new Error('No project with chat_session_id found');
  }
  return String(project.chat_session_id);
}

// ---------------------------------------------------------------------------
// WAV generators
// ---------------------------------------------------------------------------

function buildWavSilence(durationMs: number, sampleRate = 16000, bitsPerSample = 16): Buffer {
  const bytesPerSample = bitsPerSample / 8;
  const numSamples = Math.floor(sampleRate * (durationMs / 1000));
  const dataSize = numSamples * bytesPerSample;
  const buf = Buffer.alloc(44 + dataSize);
  buf.write('RIFF', 0);
  buf.writeUInt32LE(36 + dataSize, 4);
  buf.write('WAVE', 8);
  buf.write('fmt ', 12);
  buf.writeUInt32LE(16, 16);
  buf.writeUInt16LE(1, 20);
  buf.writeUInt16LE(1, 22);
  buf.writeUInt32LE(sampleRate, 24);
  buf.writeUInt32LE(sampleRate * bytesPerSample, 28);
  buf.writeUInt16LE(bytesPerSample, 32);
  buf.writeUInt16LE(bitsPerSample, 34);
  buf.write('data', 36);
  buf.writeUInt32LE(dataSize, 40);
  return buf;
}

function buildWavSineWave(durationMs: number, freq = 440, sampleRate = 16000, bitsPerSample = 16): Buffer {
  const bytesPerSample = bitsPerSample / 8;
  const numSamples = Math.floor(sampleRate * (durationMs / 1000));
  const dataSize = numSamples * bytesPerSample;
  const buf = Buffer.alloc(44 + dataSize);
  buf.write('RIFF', 0);
  buf.writeUInt32LE(36 + dataSize, 4);
  buf.write('WAVE', 8);
  buf.write('fmt ', 12);
  buf.writeUInt32LE(16, 16);
  buf.writeUInt16LE(1, 20);
  buf.writeUInt16LE(1, 22);
  buf.writeUInt32LE(sampleRate, 24);
  buf.writeUInt32LE(sampleRate * bytesPerSample, 28);
  buf.writeUInt16LE(bytesPerSample, 32);
  buf.writeUInt16LE(bitsPerSample, 34);
  buf.write('data', 36);
  buf.writeUInt32LE(dataSize, 40);
  const amplitude = 0.8 * (Math.pow(2, bitsPerSample - 1) - 1);
  for (let i = 0; i < numSamples; i++) {
    const sample = Math.round(amplitude * Math.sin(2 * Math.PI * freq * i / sampleRate));
    buf.writeInt16LE(sample, 44 + i * bytesPerSample);
  }
  return buf;
}

async function synthesizePiperWav(text: string): Promise<Buffer> {
  const resp = await fetch('http://127.0.0.1:8424/v1/audio/speech', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ input: text, voice: 'en', response_format: 'wav' }),
  });
  if (!resp.ok) {
    throw new Error(`Piper TTS failed: HTTP ${resp.status}`);
  }
  return Buffer.from(await resp.arrayBuffer());
}

function transcodeWavToM4A(wav: Buffer): Buffer {
  const dir = mkdtempSync(join(tmpdir(), 'tabura-stt-m4a-'));
  const inPath = join(dir, 'input.wav');
  const outPath = join(dir, 'output.m4a');
  writeFileSync(inPath, wav);
  try {
    execSync(
      `ffmpeg -hide_banner -loglevel error -nostdin -y -i "${inPath}" -ac 1 -ar 16000 -c:a aac -b:a 64k "${outPath}"`,
      { stdio: 'pipe' },
    );
    return readFileSync(outPath);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

async function postSTTTranscribeAPI(sessionToken: string, mimeType: string, audio: Buffer) {
  const headers: Record<string, string> = {};
  if (sessionToken) {
    headers['Cookie'] = `${SESSION_COOKIE_NAME}=${sessionToken}`;
  }
  const form = new FormData();
  form.append('mime_type', mimeType);
  form.append('file', new Blob([audio], { type: mimeType }), 'audio-input');
  const resp = await fetch(`${SERVER_URL}/api/stt/transcribe`, {
    method: 'POST',
    headers,
    body: form,
  });
  const raw = await resp.text();
  let payload: Record<string, unknown> = {};
  if (raw) {
    try {
      payload = JSON.parse(raw) as Record<string, unknown>;
    } catch {
      payload = {};
    }
  }
  return { status: resp.status, payload, raw };
}

// ---------------------------------------------------------------------------
// Raw WebSocket wrapper with auth cookie
// ---------------------------------------------------------------------------

type WSMessage = { kind: 'text'; data: string } | { kind: 'binary'; data: Buffer };

interface RawWSConn {
  ws: WebSocket;
  messages: WSMessage[];
  waitForText: (predicate: (msg: Record<string, unknown>) => boolean, timeoutMs?: number) => Promise<Record<string, unknown>>;
  waitForBinary: (timeoutMs?: number) => Promise<Buffer>;
  close: () => void;
}

function openRawWS(url: string, sessionToken: string): Promise<RawWSConn> {
  return new Promise((resolve, reject) => {
    const headers: Record<string, string> = {};
    if (sessionToken) {
      headers['Cookie'] = `${SESSION_COOKIE_NAME}=${sessionToken}`;
    }
    const ws = new WebSocket(url, { headers });
    const messages: WSMessage[] = [];
    const listeners: Array<(msg: WSMessage) => void> = [];

    ws.on('open', () => {
      resolve({
        ws,
        messages,
        waitForText(predicate, timeoutMs = 10_000) {
          return new Promise((res, rej) => {
            const timer = setTimeout(() => rej(new Error(`waitForText timed out after ${timeoutMs}ms`)), timeoutMs);
            for (const m of messages) {
              if (m.kind === 'text') {
                try {
                  const parsed = JSON.parse(m.data);
                  if (predicate(parsed)) { clearTimeout(timer); res(parsed); return; }
                } catch {}
              }
            }
            const listener = (msg: WSMessage) => {
              if (msg.kind !== 'text') return;
              try {
                const parsed = JSON.parse(msg.data);
                if (predicate(parsed)) {
                  clearTimeout(timer);
                  const idx = listeners.indexOf(listener);
                  if (idx >= 0) listeners.splice(idx, 1);
                  res(parsed);
                }
              } catch {}
            };
            listeners.push(listener);
          });
        },
        waitForBinary(timeoutMs = 15_000) {
          return new Promise((res, rej) => {
            const timer = setTimeout(() => rej(new Error(`waitForBinary timed out after ${timeoutMs}ms`)), timeoutMs);
            for (const m of messages) {
              if (m.kind === 'binary') { clearTimeout(timer); res(m.data); return; }
            }
            const listener = (msg: WSMessage) => {
              if (msg.kind !== 'binary') return;
              clearTimeout(timer);
              const idx = listeners.indexOf(listener);
              if (idx >= 0) listeners.splice(idx, 1);
              res(msg.data);
            };
            listeners.push(listener);
          });
        },
        close() { ws.close(); },
      });
    });

    ws.on('message', (data: Buffer | string, isBinary: boolean) => {
      const msg: WSMessage = isBinary
        ? { kind: 'binary', data: Buffer.isBuffer(data) ? data : Buffer.from(String(data)) }
        : { kind: 'text', data: Buffer.isBuffer(data) ? data.toString('utf-8') : String(data) };
      messages.push(msg);
      for (const listener of listeners.slice()) listener(msg);
    });

    ws.on('error', reject);
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('STT/TTS system tests', () => {
  let sessionToken: string;
  let chatSessionId: string;

  test.beforeAll(async () => {
    if (!webServerUp) throw new Error('Tabura web server not running on :8420');
    if (!canAuth) throw new Error('Server requires auth but TABURA_TEST_PASSWORD not set (env or .env)');
    sessionToken = await authenticate();
    chatSessionId = await getChatSessionId(sessionToken);
  });

  test.describe('TTS', () => {
    test.beforeAll(() => {
      if (!ttsUp) throw new Error('Piper TTS not running on :8424');
    });

    test('TTS returns WAV audio with valid RIFF header', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'tts_speak', text: 'Hello world.', lang: 'en' }));
        const wav = await conn.waitForBinary(15_000);
        expect(wav.length).toBeGreaterThan(44);
        expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');
        expect(wav.slice(8, 12).toString('ascii')).toBe('WAVE');
      } finally {
        conn.close();
      }
    });

    test('TTS handles multiple sentences in order', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        const sentences = ['First sentence.', 'Second sentence.', 'Third sentence.'];
        for (const text of sentences) {
          conn.ws.send(JSON.stringify({ type: 'tts_speak', text, lang: 'en' }));
        }

        const wavBuffers: Buffer[] = [];
        for (let i = 0; i < 3; i++) {
          const wav = await new Promise<Buffer>((resolve, reject) => {
            const timer = setTimeout(() => reject(new Error(`TTS wav ${i} timed out`)), 20_000);
            const check = () => {
              const binaries = conn.messages.filter((m): m is { kind: 'binary'; data: Buffer } => m.kind === 'binary');
              if (binaries.length > wavBuffers.length) {
                clearTimeout(timer);
                resolve(binaries[wavBuffers.length].data);
                return;
              }
              setTimeout(check, 50);
            };
            check();
          });
          wavBuffers.push(wav);
        }

        expect(wavBuffers).toHaveLength(3);
        for (const wav of wavBuffers) {
          expect(wav.length).toBeGreaterThan(44);
          expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');
        }
      } finally {
        conn.close();
      }
    });
  });

  test.describe('STT', () => {
    test.beforeAll(() => {
      if (!sttUp) throw new Error('voxtype STT service not running on :8427');
    });

    test('STT rejects short audio as too short', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await conn.waitForText((m) => m.type === 'stt_started', 5_000);

        conn.ws.send(buildWavSilence(10));
        conn.ws.send(JSON.stringify({ type: 'stt_stop' }));

        const result = await conn.waitForText(
          (m) => m.type === 'stt_empty' || m.type === 'stt_result' || m.type === 'stt_error',
          10_000,
        );
        expect(result.type).toBe('stt_empty');
        expect(result.reason).toBe('recording_too_short');
      } finally {
        conn.close();
      }
    });

    test('STT returns empty for silence', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await conn.waitForText((m) => m.type === 'stt_started', 5_000);

        conn.ws.send(buildWavSilence(2000));
        conn.ws.send(JSON.stringify({ type: 'stt_stop' }));

        const result = await conn.waitForText(
          (m) => m.type === 'stt_empty' || m.type === 'stt_result' || m.type === 'stt_error',
          15_000,
        );
        expect(['stt_empty', 'stt_error']).toContain(result.type);
      } finally {
        conn.close();
      }
    });

    test('STT cancel discards audio', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await conn.waitForText((m) => m.type === 'stt_started', 5_000);

        conn.ws.send(buildWavSineWave(1000));
        conn.ws.send(JSON.stringify({ type: 'stt_cancel' }));

        const result = await conn.waitForText((m) => m.type === 'stt_cancelled', 5_000);
        expect(result.type).toBe('stt_cancelled');
      } finally {
        conn.close();
      }
    });
  });

  test.describe('STT+TTS round-trip', () => {
    test.beforeAll(() => {
      if (!sttUp || !ttsUp) throw new Error('Both STT and TTS services required');
    });

    test('TTS-generated audio round-trips through STT', async () => {
      const ttsConn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      let ttsWav: Buffer;
      try {
        ttsConn.ws.send(JSON.stringify({ type: 'tts_speak', text: 'The quick brown fox jumps over the lazy dog.', lang: 'en' }));
        ttsWav = await ttsConn.waitForBinary(15_000);
        expect(ttsWav.length).toBeGreaterThan(44);
      } finally {
        ttsConn.close();
      }

      const sttConn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        sttConn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await sttConn.waitForText((m) => m.type === 'stt_started', 5_000);

        sttConn.ws.send(ttsWav);
        sttConn.ws.send(JSON.stringify({ type: 'stt_stop' }));

        const result = await sttConn.waitForText(
          (m) => m.type === 'stt_result' || m.type === 'stt_empty' || m.type === 'stt_error',
          60_000,
        );
        expect(result.type).toBe('stt_result');
        const transcript = String(result.text || '').toLowerCase();
        expect(transcript).toBeTruthy();
        expect(transcript.length).toBeGreaterThan(5);
      } finally {
        sttConn.close();
      }
    });

    test('Piper-generated WAV round-trips through authenticated /api/stt/transcribe', async () => {
      const wav = await synthesizePiperWav('Tabura end to end speech to text verification.');
      expect(wav.length).toBeGreaterThan(44);
      expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');

      const { status, payload, raw } = await postSTTTranscribeAPI(sessionToken, 'audio/wav', wav);
      expect(status, raw).toBe(200);
      const text = String(payload.text || '').trim();
      expect(text.length).toBeGreaterThan(0);
    });

    test('Piper-generated M4A round-trips through authenticated /api/stt/transcribe', async () => {
      test.skip(!ffmpegUp, 'ffmpeg not available');
      const wav = await synthesizePiperWav('Tabura m4a normalization to whisper verification.');
      const m4a = transcodeWavToM4A(wav);
      expect(m4a.length).toBeGreaterThan(512);

      const { status, payload, raw } = await postSTTTranscribeAPI(sessionToken, 'audio/mp4', m4a);
      expect(status, raw).toBe(200);
      const text = String(payload.text || '').trim();
      expect(text.length).toBeGreaterThan(0);
    });
  });
});
