import { expect, test } from './live';
import { authenticate, synthesizePiperWav, transcodeWavToM4A, postSTTTranscribeAPI, buildWavSilence } from './helpers';

test.describe('STT over HTTP', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('POST WAV speech to /api/stt/transcribe returns transcript', async () => {
    const wav = await synthesizePiperWav('Tabura end to end speech to text verification.');
    expect(wav.length).toBeGreaterThan(44);
    expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');

    const { status, payload, raw } = await postSTTTranscribeAPI(sessionToken, 'audio/wav', wav);
    expect(status, raw).toBe(200);
    const text = String(payload.text || '').trim();
    expect(text.length).toBeGreaterThan(0);
  });

  test('POST M4A speech to /api/stt/transcribe returns transcript', async () => {
    const wav = await synthesizePiperWav('Tabura m4a normalization to stt verification.');
    const m4a = transcodeWavToM4A(wav);
    expect(m4a.length).toBeGreaterThan(512);

    const { status, payload, raw } = await postSTTTranscribeAPI(sessionToken, 'audio/mp4', m4a);
    expect(status, raw).toBe(200);
    const text = String(payload.text || '').trim();
    expect(text.length).toBeGreaterThan(0);
  });

  test('POST silence WAV to /api/stt/transcribe returns empty or error', async () => {
    const silence = buildWavSilence(2000);
    const { status, payload } = await postSTTTranscribeAPI(sessionToken, 'audio/wav', silence);
    if (status === 200) {
      const text = String(payload.text || '').trim();
      expect(text.length).toBe(0);
    } else {
      expect([400, 422]).toContain(status);
    }
  });
});
