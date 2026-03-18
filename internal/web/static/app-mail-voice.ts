import * as env from './app-env.js';
import * as context from './app-context.js';

const { apiURL } = env;
const { refs, state, VOICE_LIFECYCLE } = context;

const showStatus = (...args) => refs.showStatus(...args);
const beginConversationVoiceCapture = (...args) => refs.beginConversationVoiceCapture(...args);
const setVoiceLifecycle = (...args) => refs.setVoiceLifecycle(...args);
const scheduleMailDraftSave = (...args) => refs.scheduleMailDraftSave(...args);

let voiceMailArtifactId = 0;
let voiceMailActive = false;
let voiceMailPolishing = false;
let voiceMailPolishAbort: AbortController | null = null;

export function isVoiceMailActive(): boolean {
  return voiceMailActive;
}

export function startVoiceMailMode(artifactId: number) {
  voiceMailArtifactId = artifactId;
  voiceMailActive = true;
  voiceMailPolishing = false;
  if (voiceMailPolishAbort) {
    voiceMailPolishAbort.abort();
    voiceMailPolishAbort = null;
  }
  beginConversationVoiceCapture('manual');
}

export function stopVoiceMailMode() {
  voiceMailActive = false;
  voiceMailPolishing = false;
  voiceMailArtifactId = 0;
  if (voiceMailPolishAbort) {
    voiceMailPolishAbort.abort();
    voiceMailPolishAbort = null;
  }
}

function getMailDraftBodyTextarea(): HTMLTextAreaElement | null {
  const editor = document.getElementById('mail-draft-editor');
  if (!(editor instanceof HTMLElement)) return null;
  const textarea = editor.querySelector('textarea[name="body"]');
  return textarea instanceof HTMLTextAreaElement ? textarea : null;
}

export async function handleVoiceMailTranscript(text: string) {
  const textarea = getMailDraftBodyTextarea();
  if (!textarea) {
    showStatus('mail draft body not found');
    return;
  }

  const trimmed = String(text || '').trim();
  if (!trimmed) return;

  // Insert raw text at cursor or append
  const current = textarea.value;
  const start = textarea.selectionStart ?? current.length;
  const end = textarea.selectionEnd ?? current.length;
  const before = current.slice(0, start);
  const after = current.slice(end);
  const separator = before && !before.endsWith('\n') && !before.endsWith(' ') ? ' ' : '';
  textarea.value = before + separator + trimmed + after;
  textarea.setSelectionRange(
    start + separator.length + trimmed.length,
    start + separator.length + trimmed.length,
  );

  // Polish
  textarea.classList.add('polishing');
  textarea.classList.remove('polished');
  voiceMailPolishing = true;

  if (voiceMailPolishAbort) {
    voiceMailPolishAbort.abort();
  }
  voiceMailPolishAbort = new AbortController();

  try {
    const polishURL = voiceMailArtifactId > 0
      ? apiURL(`mail/drafts/${encodeURIComponent(String(voiceMailArtifactId))}/polish`)
      : apiURL('text/polish');
    const resp = await fetch(polishURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        body: textarea.value,
        style: 'email',
      }),
      signal: voiceMailPolishAbort.signal,
    });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    const payload = await resp.json();
    const polished = String(payload?.polished_body || '').trim();
    if (polished) {
      textarea.value = polished;
    }
    textarea.classList.remove('polishing');
    textarea.classList.add('polished');
    setTimeout(() => textarea.classList.remove('polished'), 1500);
    scheduleMailDraftSave();
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return;
    textarea.classList.remove('polishing');
    showStatus('polish: ' + String(err?.message || err || 'failed'));
    // Keep raw text on failure
    scheduleMailDraftSave();
  } finally {
    voiceMailPolishing = false;
    voiceMailPolishAbort = null;
  }
}

export function onMailDraftBodyTap(_event: Event) {
  if (!voiceMailActive) return;
  beginConversationVoiceCapture('manual');
}

export function mailAuthoringUsesVoice(): boolean {
  return String(state.interaction?.tool || '').trim().toLowerCase() === 'prompt';
}
