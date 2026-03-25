import * as context from './app-context.js';
import { LIVE_SESSION_MODE_MEETING } from './live-session.js';

const { refs, state } = context;

let defaultConversationActivationTimer: number | null = null;
let defaultConversationActivationInFlight = false;

const updateRuntimePreferences = (...args) => refs.updateRuntimePreferences(...args);
const setTTSSilentMode = (...args) => refs.setTTSSilentMode(...args);
const setFastMode = (...args) => refs.setFastMode(...args);
const updateLivePolicy = (...args) => refs.updateLivePolicy(...args);
const activateLiveSession = (...args) => refs.activateLiveSession(...args);
const applyLiveSessionStateSnapshot = (...args) => refs.applyLiveSessionStateSnapshot(...args);
const renderEdgeTopModelButtons = (...args) => refs.renderEdgeTopModelButtons(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);

async function waitForChatSocketOpen(timeoutMs = 12_000) {
  const deadline = Date.now() + Math.max(500, timeoutMs);
  while (Date.now() < deadline) {
    const ws = state.chatWs;
    if (ws && ws.readyState === WebSocket.OPEN) {
      return true;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 120));
  }
  return false;
}

function syncMeetingRuntimeUi() {
  applyLiveSessionStateSnapshot();
  renderEdgeTopModelButtons();
  updateAssistantActivityIndicator();
}

function clearDefaultConversationActivationTimer() {
  if (defaultConversationActivationTimer !== null) {
    window.clearTimeout(defaultConversationActivationTimer);
    defaultConversationActivationTimer = null;
  }
}

async function activateDefaultConversationRuntimeOnce() {
  if (!state.activeWorkspaceId || state.projectSwitchInFlight) return false;
  if (!(await waitForChatSocketOpen(2_000))) return false;
  if (state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_MEETING) {
    syncMeetingRuntimeUi();
    return true;
  }
  try {
    const started = await activateLiveSession(LIVE_SESSION_MODE_MEETING);
    if (!started) return false;
  } catch (_) {
    return false;
  }
  syncMeetingRuntimeUi();
  return true;
}

function scheduleDefaultConversationRuntimeActivation(delayMs = 0) {
  clearDefaultConversationActivationTimer();
  defaultConversationActivationTimer = window.setTimeout(() => {
    defaultConversationActivationTimer = null;
    void ensureDefaultConversationRuntimeActivation();
  }, Math.max(0, delayMs));
}

export async function ensureDefaultConversationRuntimeActivation() {
  if (defaultConversationActivationInFlight) {
    return state.liveSessionActive && state.liveSessionMode === LIVE_SESSION_MODE_MEETING;
  }
  defaultConversationActivationInFlight = true;
  try {
    for (let attempt = 0; attempt < 6; attempt += 1) {
      if (await activateDefaultConversationRuntimeOnce()) {
        clearDefaultConversationActivationTimer();
        return true;
      }
      await new Promise((resolve) => window.setTimeout(resolve, 200 * (attempt + 1)));
    }
    scheduleDefaultConversationRuntimeActivation(1_000);
    return false;
  } finally {
    defaultConversationActivationInFlight = false;
  }
}

export async function ensureDefaultConversationRuntime() {
  const patch: Record<string, any> = {};
  if (state.ttsSilent) {
    patch.silent_mode = false;
  }
  if (state.fastMode) {
    patch.fast_mode = false;
  }
  if (Object.keys(patch).length > 0) {
    await updateRuntimePreferences(patch);
  } else {
    setTTSSilentMode(false, { persist: false, pinPanel: false });
    setFastMode(false, { persist: false });
  }
  if (state.livePolicy !== LIVE_SESSION_MODE_MEETING) {
    await updateLivePolicy(LIVE_SESSION_MODE_MEETING);
  }
  await ensureDefaultConversationRuntimeActivation();
}
