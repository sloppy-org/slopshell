import { expect, test } from './live';
import { WS_URL, authenticate, getChatSessionId, openRawWS } from './helpers';

test.describe('STT+TTS round-trip @local-only', () => {
  let sessionToken: string;
  let chatSessionId: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
    chatSessionId = await getChatSessionId(sessionToken);
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

  test('Piper HTTP TTS -> STT HTTP round-trip', async () => {
    const ttsResp = await fetch('http://127.0.0.1:8424/v1/audio/speech', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ input: 'Hello, how are you doing today?', voice: 'en', response_format: 'wav' }),
    });
    expect(ttsResp.ok).toBe(true);
    const ttsWav = Buffer.from(await ttsResp.arrayBuffer());
    expect(ttsWav.length).toBeGreaterThan(44);

    const form = new FormData();
    form.append('mime_type', 'audio/wav');
    form.append('file', new Blob([ttsWav], { type: 'audio/wav' }), 'audio-input');

    const headers: Record<string, string> = {};
    if (sessionToken) {
      headers['Cookie'] = `tabura_session=${sessionToken}`;
    }

    const sttResp = await fetch('http://127.0.0.1:8420/api/stt/transcribe', {
      method: 'POST',
      headers,
      body: form,
    });
    expect(sttResp.ok).toBe(true);
    const body = (await sttResp.json()) as Record<string, unknown>;
    const transcript = String(body.text || '').trim().toLowerCase();
    expect(transcript.length).toBeGreaterThan(3);
  });
});
