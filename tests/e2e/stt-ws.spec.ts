import { expect, test } from './live';
import {
  WS_URL,
  authenticate,
  getChatSessionId,
  openRawWS,
  buildWavSilence,
  buildWavSineWave,
  synthesizePiperWav,
} from './helpers';

test.describe('STT over WebSocket', () => {
  let sessionToken: string;
  let chatSessionId: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
    chatSessionId = await getChatSessionId(sessionToken);
  });

  test('real speech produces non-empty transcript', async () => {
    const speechWav = await synthesizePiperWav('The quick brown fox jumps over the lazy dog.');
    expect(speechWav.length).toBeGreaterThan(44);

    const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
    try {
      conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
      await conn.waitForText((m) => m.type === 'stt_started', 5_000);

      conn.ws.send(speechWav);
      conn.ws.send(JSON.stringify({ type: 'stt_stop' }));

      const result = await conn.waitForText(
        (m) => m.type === 'stt_result' || m.type === 'stt_empty' || m.type === 'stt_error',
        30_000,
      );
      expect(result.type).toBe('stt_result');
      const transcript = String(result.text || '').toLowerCase();
      expect(transcript.length).toBeGreaterThan(5);
    } finally {
      conn.close();
    }
  });

  test('silence returns stt_empty', async () => {
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

  test('too-short audio returns stt_empty with recording_too_short', async () => {
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

  test('cancel mid-stream returns stt_cancelled', async () => {
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
