import { expect, test } from './live';
import { WS_URL, authenticate, getChatSessionId, openRawWS } from './helpers';

test.describe('TTS over WebSocket', () => {
  let sessionToken: string;
  let chatSessionId: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
    chatSessionId = await getChatSessionId(sessionToken);
  });

  test('tts_speak returns WAV audio with valid RIFF header', async () => {
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

  test('WAV has expected sample rate and mono channel', async () => {
    const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
    try {
      conn.ws.send(JSON.stringify({ type: 'tts_speak', text: 'Sample rate check.', lang: 'en' }));
      const wav = await conn.waitForBinary(15_000);
      expect(wav.length).toBeGreaterThan(44);

      const channels = wav.readUInt16LE(22);
      const sampleRate = wav.readUInt32LE(24);

      expect(channels).toBe(1);
      expect(sampleRate).toBeGreaterThanOrEqual(16000);
      expect(sampleRate).toBeLessThanOrEqual(48000);
    } finally {
      conn.close();
    }
  });

  test('multiple tts_speak requests return multiple WAVs', async () => {
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
