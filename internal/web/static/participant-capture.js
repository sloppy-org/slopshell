/**
 * ParticipantCapture: browser mic capture for meeting transcription.
 * Streams speech segments to the server via WebSocket using Silero VAD.
 *
 * Privacy: no audio is persisted to localStorage or indexedDB.
 * All buffers are zeroized on stop/destroy. See docs/meeting-notes-privacy.md.
 */

import { initVAD, float32ToWav } from './vad.js';

export class ParticipantCapture {
  constructor() {
    this._ws = null;
    this._stream = null;
    this._vadInstance = null;
    this._active = false;
    this._sessionId = null;
    this._onSegment = null;
    this._onStarted = null;
    this._onStopped = null;
    this._onError = null;
  }

  get active() {
    return this._active;
  }

  get sessionId() {
    return this._sessionId;
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
    if (this._active) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      this._emitError('WebSocket not connected');
      return;
    }

    this._ws = ws;

    try {
      this._stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (err) {
      this._emitError('Microphone access denied: ' + err.message);
      return;
    }

    this._active = true;
    ws.send(JSON.stringify({ type: 'participant_start' }));
    await this._startSileroCapture();
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
          if (!this._active || !this._ws) return;
          if (!(audio instanceof Float32Array) || audio.length === 0) return;
          const wavBlob = float32ToWav(audio, 16000);
          if (wavBlob.size > 44) {
            this._ws.send(wavBlob);
          }
        },
      });

      if (!this._active) {
        if (instance) instance.destroy();
        return;
      }

      this._vadInstance = instance;
      if (instance) instance.start();
    } catch (_) {}
  }

  stop() {
    if (!this._active) return;
    this._active = false;

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
        this._emitError(msg.error || 'unknown participant error');
        return true;
      default:
        return false;
    }
  }

  destroy() {
    this.stop();
    this._sessionId = null;
    this._onSegment = null;
    this._onStarted = null;
    this._onStopped = null;
    this._onError = null;
  }

  _cleanup() {
    this._active = false;
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
}
