import { wsURL } from './paths.js';

type TurnActionPayload = {
  type?: string;
  action?: string;
  text?: string;
  reason?: string;
  wait_ms?: number;
  interrupt_assistant?: boolean;
  rollback_audio_ms?: number;
};

type TurnClientConfig = {
  onAction?: (payload: TurnActionPayload) => void;
};

const state = {
  ws: null as WebSocket | null,
  token: 0,
  sessionId: '',
  connected: false,
  onAction: null as ((payload: TurnActionPayload) => void) | null,
};

export function configureTurnIntelligence(config: TurnClientConfig = {}) {
  state.onAction = typeof config?.onAction === 'function' ? config.onAction : null;
}

export function openTurnWs(sessionId: string) {
  const targetSessionId = String(sessionId || '').trim();
  closeTurnWs();
  if (!targetSessionId) return;
  const token = state.token + 1;
  state.token = token;
  state.sessionId = targetSessionId;
  const ws = new WebSocket(wsURL(`turn/${encodeURIComponent(targetSessionId)}`));
  state.ws = ws;
  ws.onopen = () => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    state.connected = true;
  };
  ws.onmessage = (event) => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    if (typeof event.data !== 'string') return;
    let payload: TurnActionPayload | null = null;
    try { payload = JSON.parse(event.data); } catch (_) { return; }
    if (!payload || payload.type !== 'turn_action') return;
    if (typeof state.onAction === 'function') {
      state.onAction(payload);
    }
  };
  ws.onclose = () => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    state.connected = false;
    state.ws = null;
  };
  ws.onerror = () => {
    if (token !== state.token || targetSessionId !== state.sessionId) return;
    state.connected = false;
  };
}

export function closeTurnWs() {
  state.token += 1;
  state.connected = false;
  if (state.ws) {
    try { state.ws.close(); } catch (_) {}
  }
  state.ws = null;
  state.sessionId = '';
}

export function isTurnIntelligenceConnected() {
  return Boolean(state.connected && state.ws && state.ws.readyState === WebSocket.OPEN);
}

export function sendTurnEvent(payload: Record<string, any>) {
  const ws = state.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN) return false;
  ws.send(JSON.stringify(payload || {}));
  return true;
}

export function sendTurnListenState(active: boolean) {
  return sendTurnEvent({ type: 'turn_listen_state', active: Boolean(active) });
}

export function sendTurnSpeechStart(interruptedAssistant = false) {
  return sendTurnEvent({
    type: 'turn_speech_start',
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function sendTurnSpeechProbability(speechProb: number, interruptedAssistant = false) {
  const prob = Number.isFinite(Number(speechProb)) ? Number(speechProb) : 0;
  return sendTurnEvent({
    type: 'turn_speech_prob',
    speech_prob: prob,
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function sendTurnTranscriptSegment(text: string, durationMs: number, interruptedAssistant = false) {
  return sendTurnEvent({
    type: 'turn_transcript_segment',
    text: String(text || ''),
    duration_ms: Math.max(0, Number(durationMs) || 0),
    interrupted_assistant: Boolean(interruptedAssistant),
  });
}

export function sendTurnPlaybackProgress(playing: boolean, playedMs: number) {
  return sendTurnEvent({
    type: 'turn_playback',
    playing: Boolean(playing),
    played_ms: Math.max(0, Number(playedMs) || 0),
  });
}

export function resetTurnIntelligence() {
  return sendTurnEvent({ type: 'turn_reset' });
}
