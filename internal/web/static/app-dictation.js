import * as env from './app-env.js';
import * as context from './app-context.js';

const { apiURL, getActiveArtifactTitle, getPreviousArtifactText } = env;
const { refs, state } = context;

const showStatus = (...args) => refs.showStatus(...args);
const applyCanvasArtifactEvent = (...args) => refs.applyCanvasArtifactEvent(...args);
const submitMessage = (...args) => refs.submitMessage(...args);

const DICTATION_ACTIONS_ID = 'dictation-actions';
const DICTATION_INDICATOR_ID = 'dictation-indicator';

function dictationState() {
  return state.dictation;
}

function normalizeDictationResponse(payload) {
  const data = payload?.dictation && typeof payload.dictation === 'object' ? payload.dictation : {};
  const targetKind = String(data?.target_kind || 'document_section').trim().toLowerCase() || 'document_section';
  return {
    active: Boolean(data?.active),
    targetKind,
    targetLabel: String(data?.target_label || 'Document Section').trim() || 'Document Section',
    prompt: String(data?.prompt || '').trim(),
    artifactTitle: String(data?.artifact_title || '').trim(),
    transcript: String(data?.transcript || '').trim(),
    draftText: String(data?.draft_text || '').trim(),
    scratchPath: String(data?.scratch_path || '').trim(),
  };
}

function applyDictationState(next) {
  Object.assign(dictationState(), {
    active: Boolean(next?.active),
    targetKind: String(next?.targetKind || 'document_section').trim() || 'document_section',
    targetLabel: String(next?.targetLabel || 'Document Section').trim() || 'Document Section',
    prompt: String(next?.prompt || '').trim(),
    artifactTitle: String(next?.artifactTitle || '').trim(),
    transcript: String(next?.transcript || '').trim(),
    draftText: String(next?.draftText || '').trim(),
    scratchPath: String(next?.scratchPath || '').trim(),
  });
  renderDictationUi();
}

function activeDraftText() {
  const editor = document.getElementById('artifact-editor');
  if (state.artifactEditMode && editor instanceof HTMLTextAreaElement) {
    return String(editor.value || '').trim();
  }
  return String(getPreviousArtifactText() || dictationState().draftText || '').trim();
}

function dictationDispatchPrompt(targetLabel, draftText) {
  const label = String(targetLabel || 'draft').trim();
  return `Use this dictated ${label.toLowerCase()} draft as the content to dispatch.\n\n${draftText}`;
}

function dictationCanvasPayload() {
  const current = dictationState();
  if (!current.draftText) return null;
  return {
    kind: 'text_artifact',
    title: current.scratchPath || '.tabura/artifacts/tmp/dictation.md',
    text: current.draftText,
    surface_default: 'editor',
    meta: { surface_default: 'editor' },
  };
}

function renderDraftOnCanvas() {
  const payload = dictationCanvasPayload();
  if (!payload) return;
  applyCanvasArtifactEvent(payload);
}

async function requestDictation(path, options = {}) {
  const sessionID = String(state.chatSessionId || '').trim();
  if (!sessionID) throw new Error('chat session missing');
  const resp = await fetch(apiURL(`chat/sessions/${encodeURIComponent(sessionID)}/dictation${path}`), options);
  if (!resp.ok) {
    throw new Error((await resp.text()).trim() || `HTTP ${resp.status}`);
  }
  const payload = await resp.json();
  return normalizeDictationResponse(payload);
}

export function ensureDictationUi() {
  let indicator = document.getElementById(DICTATION_INDICATOR_ID);
  if (!(indicator instanceof HTMLElement)) {
    indicator = document.createElement('section');
    indicator.id = DICTATION_INDICATOR_ID;
    indicator.className = 'dictation-indicator';
    indicator.innerHTML = `
      <div class="dictation-indicator-copy">
        <div class="dictation-indicator-kicker">Dictation</div>
        <div class="dictation-indicator-target"></div>
      </div>
      <div class="dictation-indicator-actions" id="${DICTATION_ACTIONS_ID}">
        <button id="dictation-send" type="button" class="edge-btn">Send</button>
        <button id="dictation-stop" type="button" class="edge-btn">Stop</button>
      </div>
    `;
    document.body.appendChild(indicator);
    indicator.querySelector('#dictation-send')?.addEventListener('click', () => { void sendDictationDraft(); });
    indicator.querySelector('#dictation-stop')?.addEventListener('click', () => { void stopDictationMode(); });
  }
  return indicator;
}

export function renderDictationUi() {
  const root = ensureDictationUi();
  if (!(root instanceof HTMLElement)) return;
  const current = dictationState();
  const visible = current.active;
  root.style.display = visible ? 'flex' : 'none';
  root.setAttribute('aria-hidden', visible ? 'false' : 'true');
  const target = root.querySelector('.dictation-indicator-target');
  if (target) {
    const transcriptState = current.transcript ? 'Listening for more voice input.' : 'Waiting for speech.';
    target.textContent = `${current.targetLabel} draft. ${transcriptState}`;
  }
  const sendButton = root.querySelector('#dictation-send');
  if (sendButton instanceof HTMLButtonElement) {
    sendButton.disabled = !current.draftText || current.saving;
  }
}

export async function maybeHandleDictationCommand(text) {
  const trimmed = String(text || '').trim();
  if (!trimmed) return false;
  const normalized = trimmed.toLowerCase();
  if (!['take a letter', 'draft a reply', 'write my review', 'dictate'].includes(normalized)) {
    return false;
  }
  try {
    const next = await requestDictation('/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt: trimmed,
        artifact_title: String(getActiveArtifactTitle() || '').trim(),
      }),
    });
    applyDictationState(next);
    showStatus(`${next.targetLabel.toLowerCase()} dictation ready`);
    return true;
  } catch (err) {
    showStatus(`dictation failed: ${String(err?.message || err)}`);
    return true;
  }
}

export function isDictationActive() {
  return Boolean(dictationState().active);
}

export async function appendDictationTranscript(text) {
  const transcript = String(text || '').trim();
  if (!transcript) return false;
  dictationState().saving = true;
  renderDictationUi();
  try {
    const next = await requestDictation('/append', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: transcript }),
    });
    applyDictationState(next);
    renderDraftOnCanvas();
    showStatus('draft updated');
    return true;
  } catch (err) {
    showStatus(`dictation failed: ${String(err?.message || err)}`);
    return false;
  } finally {
    dictationState().saving = false;
    renderDictationUi();
  }
}

export async function saveDictationDraft(text) {
  if (!isDictationActive()) return false;
  const draftText = String(text || '').trim();
  if (!draftText) return false;
  dictationState().saving = true;
  renderDictationUi();
  try {
    const next = await requestDictation('/draft', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ draft_text: draftText }),
    });
    applyDictationState(next);
    return true;
  } catch (err) {
    showStatus(`dictation save failed: ${String(err?.message || err)}`);
    return false;
  } finally {
    dictationState().saving = false;
    renderDictationUi();
  }
}

export function maybePersistDictationDraft(text) {
  const title = String(getActiveArtifactTitle() || '').trim();
  const current = dictationState();
  if (!current.active || !current.scratchPath || title !== current.scratchPath) return;
  void saveDictationDraft(text);
}

export async function stopDictationMode() {
  if (!isDictationActive()) return false;
  try {
    const next = await requestDictation('', { method: 'DELETE' });
    applyDictationState(next);
    showStatus('dictation stopped');
    return true;
  } catch (err) {
    showStatus(`dictation failed: ${String(err?.message || err)}`);
    return false;
  }
}

export async function sendDictationDraft() {
  const current = dictationState();
  if (!current.active) return false;
  const draftText = activeDraftText();
  if (!draftText) {
    showStatus('dictation draft empty');
    return false;
  }
  const saved = await saveDictationDraft(draftText);
  if (!saved) return false;
  const sent = await submitMessage(dictationDispatchPrompt(current.targetLabel, draftText), { kind: 'dictation_dispatch' });
  if (sent) {
    void stopDictationMode();
    showStatus('draft dispatched');
  }
  return sent;
}

export async function maybeHandleDictationTranscript(text) {
  if (!isDictationActive()) return false;
  return appendDictationTranscript(text);
}

export function initDictationUi() {
  ensureDictationUi();
  renderDictationUi();
}
