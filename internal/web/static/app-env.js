export { marked } from './vendor/marked.esm.js';
export { apiURL, wsURL } from './paths.js';
export {
  renderCanvas,
  clearCanvas,
  getLocationFromSelection,
  clearLineHighlight,
  escapeHtml,
  sanitizeHtml,
  getActiveArtifactTitle,
  getActiveTextEventId,
  getPreviousArtifactText,
} from './canvas.js';
export {
  getUiState, setUiMode,
  showIndicatorMode, hideIndicator,
  showTextInput, hideTextInput,
  showOverlay, hideOverlay, updateOverlay,
  isOverlayVisible, isTextInputVisible, isRecording, setRecording,
  getInputAnchor, setInputAnchor, pinCursorAnchor, clearCursorAnchor, getCursorAnchor, getAnchorFromPoint,
  buildContextPrefix, getLastInputPosition, setLastInputPosition,
} from './ui.js';
export {
  configureLiveSession,
  getLiveSessionSnapshot,
  handleLiveSessionMessage,
  isLiveSessionListenActive,
  LIVE_SESSION_HOTWORD_DEFAULT,
  LIVE_SESSION_MODE_DIALOGUE,
  LIVE_SESSION_MODE_MEETING,
  onLiveSessionTTSPlaybackComplete,
  cancelLiveSessionListen,
  startLiveSession,
  stopLiveSession,
} from './live-session.js';
export {
  initHotword,
  startHotwordMonitor,
  stopHotwordMonitor,
  isHotwordActive,
  onHotwordDetected,
  setHotwordThreshold,
  setHotwordAudioContext,
  getPreRollAudio,
  getHotwordMicStream,
} from './hotword.js';
export { initVAD, ensureVADLoaded, float32ToWav } from './vad.js';
