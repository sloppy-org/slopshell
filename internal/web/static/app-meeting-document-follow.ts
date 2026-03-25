import * as env from './app-env.js';
import * as context from './app-context.js';

const {
  apiURL,
  captureVisualReasoningContext,
  describeCanvasNavigationContext,
  getCanvasDocumentPositionAnchor,
} = env;

const { refs, state } = context;

const sendCanvasPositionEvent = (...args) => refs.sendCanvasPositionEvent(...args);
const stepCanvasFile = (...args) => refs.stepCanvasFile(...args);
const showStatus = (...args) => refs.showStatus(...args);

let trackingActive = false;
let viewportTimer = 0;
let followTimer = 0;
let lastPositionKey = '';

function hasTrackableMeetingDocument() {
  if (!trackingActive || !state.liveSessionActive || state.liveSessionMode !== 'meeting') return false;
  if (!state.hasArtifact) return false;
  const artifact = (state.currentCanvasArtifact || {}) as Record<string, any>;
  const kind = String(artifact.kind || artifact.artifactKind || '').trim().toLowerCase();
  if (kind !== 'text_artifact' && kind !== 'text') {
    return false;
  }
  const ref = String(artifact.path || artifact.title || '').trim();
  return ref !== '';
}

function normalizeGesture(raw, fallback = 'viewport_change') {
  const value = String(raw || '').trim().toLowerCase();
  return value || fallback;
}

function positionKeyForAnchor(anchor) {
  if (!anchor || typeof anchor !== 'object') return '';
  return JSON.stringify([
    String(anchor.path || '').trim(),
    String(anchor.title || '').trim(),
    String(anchor.view || '').trim(),
    String(anchor.element || '').trim(),
    Number(anchor.page || 0),
    Number(anchor.line || 0),
    Math.round(Number(anchor.relativeX || 0) * 1000),
    Math.round(Number(anchor.relativeY || 0) * 1000),
  ]);
}

function broadcastCurrentDocumentPosition(gesture = 'viewport_change', { force = false } = {}) {
  if (!hasTrackableMeetingDocument()) return false;
  const anchor = getCanvasDocumentPositionAnchor();
  if (!anchor) return false;
  const key = positionKeyForAnchor(anchor);
  if (!force && key && key === lastPositionKey) {
    return false;
  }
  lastPositionKey = key;
  return sendCanvasPositionEvent(anchor, {
    gesture: normalizeGesture(gesture),
    clientX: Number(anchor.clientX || 0),
    clientY: Number(anchor.clientY || 0),
  });
}

function scheduleViewportPositionBroadcast(gesture = 'viewport_change') {
  if (!hasTrackableMeetingDocument()) return;
  if (viewportTimer) {
    window.clearTimeout(viewportTimer);
  }
  viewportTimer = window.setTimeout(() => {
    viewportTimer = 0;
    broadcastCurrentDocumentPosition(gesture);
  }, 120);
}

export function cancelPendingMeetingDocumentPositionBroadcast() {
  if (!viewportTimer) return;
  window.clearTimeout(viewportTimer);
  viewportTimer = 0;
}

async function decideMeetingDocumentFollow(text) {
  const transcript = String(text || '').trim();
  if (!transcript) return null;
  const context = describeCanvasNavigationContext();
  if (!context?.artifactKind || !context?.current) return null;
  const anchor = getCanvasDocumentPositionAnchor();
  const visual = anchor
    ? captureVisualReasoningContext(Number(anchor.clientX || 0), Number(anchor.clientY || 0))
    : null;
  const resp = await fetch(apiURL('participant/document-follow/decide'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      transcript,
      artifact_kind: context.artifactKind,
      artifact_title: context.artifactTitle,
      artifact_path: context.artifactPath,
      current: context.current,
      previous: context.previous,
      next: context.next,
      snapshot_data_url: String(visual?.snapshotDataURL || '').trim(),
    }),
  });
  if (!resp.ok) {
    throw new Error(`document follow HTTP ${resp.status}`);
  }
  return resp.json();
}

export function handleMeetingSegmentDocumentFollow(message) {
  if (!trackingActive || !state.liveSessionActive || state.liveSessionMode !== 'meeting') return;
  const transcript = String(message?.text || '').trim();
  if (!transcript) return;
  if (followTimer) {
    window.clearTimeout(followTimer);
  }
  followTimer = window.setTimeout(async () => {
    followTimer = 0;
    try {
      const result = await decideMeetingDocumentFollow(transcript);
      const action = String(result?.action || '').trim().toLowerCase();
      if (action !== 'next' && action !== 'previous') return;
      const stepped = stepCanvasFile(action === 'next' ? 1 : -1);
      if (!stepped) return;
      showStatus(action === 'next' ? 'next page' : 'previous page');
      window.setTimeout(() => {
        broadcastCurrentDocumentPosition('document_flip', { force: true });
      }, 80);
    } catch (err) {
      console.warn('meeting document follow failed:', err);
    }
  }, 180);
}

function handleCanvasRendered() {
  cancelPendingMeetingDocumentPositionBroadcast();
  broadcastCurrentDocumentPosition('canvas_render', { force: true });
}

export function startMeetingDocumentTracking() {
  if (trackingActive) return;
  trackingActive = true;
  lastPositionKey = '';
  const viewport = document.getElementById('canvas-viewport');
  if (viewport instanceof HTMLElement) {
    viewport.addEventListener('scroll', handleCanvasRendered, { passive: true });
  }
  document.addEventListener('tabura:canvas-rendered', handleCanvasRendered);
  if (hasTrackableMeetingDocument()) {
    broadcastCurrentDocumentPosition('meeting_started', { force: true });
  }
}

export function stopMeetingDocumentTracking() {
  trackingActive = false;
  lastPositionKey = '';
  if (viewportTimer) {
    window.clearTimeout(viewportTimer);
    viewportTimer = 0;
  }
  if (followTimer) {
    window.clearTimeout(followTimer);
    followTimer = 0;
  }
  const viewport = document.getElementById('canvas-viewport');
  if (viewport instanceof HTMLElement) {
    viewport.removeEventListener('scroll', handleCanvasRendered);
  }
  document.removeEventListener('tabura:canvas-rendered', handleCanvasRendered);
}
