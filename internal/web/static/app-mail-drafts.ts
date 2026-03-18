import * as env from './app-env.js';
import * as context from './app-context.js';
import { mailAuthoringUsesVoice, startVoiceMailMode, onMailDraftBodyTap } from './app-mail-voice.js';

const { apiURL } = env;
const { refs, state } = context;

const applyCanvasArtifactEvent = (...args) => refs.applyCanvasArtifactEvent(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const showStatus = (...args) => refs.showStatus(...args);
const startDictationMode = (...args) => refs.startDictationMode(...args);

const MAIL_DRAFT_EDITOR_ID = 'mail-draft-editor';
const MAIL_DRAFT_STATUS_ID = 'mail-draft-status';
const MAIL_DRAFT_SUGGESTIONS_ID = 'mail-draft-recipient-suggestions';

function clearMailDraftTimer() {
  if (state.mailDraft?.saveTimer) {
    window.clearTimeout(state.mailDraft.saveTimer);
    state.mailDraft.saveTimer = null;
  }
}

export function resetMailDraftState() {
  clearMailDraftTimer();
  Object.assign(state.mailDraft, {
    artifactId: 0,
    itemId: 0,
    saveTimer: null,
    saving: false,
    sending: false,
    status: '',
  });
}

function mailDraftEventFromPayload(payload) {
  const draft = payload?.draft || payload || {};
  return {
    kind: 'email_draft',
    title: String(draft?.title || 'Draft email').trim() || 'Draft email',
    draft,
  };
}

function setMailDraftStatus(text = '') {
  state.mailDraft.status = String(text || '').trim();
  const node = document.getElementById(MAIL_DRAFT_STATUS_ID);
  if (node instanceof HTMLElement) {
    node.textContent = state.mailDraft.status;
  }
}

function updateRenderedMailDraftMeta(draft) {
  const root = activeMailDraftEditor();
  if (!(root instanceof HTMLElement)) return;
  const heading = root.querySelector('.mail-draft-title');
  if (heading instanceof HTMLElement) {
    heading.textContent = String(draft?.title || 'Draft email').trim() || 'Draft email';
  }
}

function activeMailDraftEditor() {
  return document.getElementById(MAIL_DRAFT_EDITOR_ID);
}

function readMailDraftForm() {
  const root = activeMailDraftEditor();
  if (!(root instanceof HTMLElement)) return null;
  const fieldValue = (selector) => {
    const node = root.querySelector(selector);
    return node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement ? String(node.value || '') : '';
  };
  return {
    to: splitAddresses(fieldValue('[name="to"]')),
    cc: splitAddresses(fieldValue('[name="cc"]')),
    bcc: splitAddresses(fieldValue('[name="bcc"]')),
    subject: fieldValue('[name="subject"]'),
    body: fieldValue('[name="body"]'),
  };
}

function splitAddresses(raw) {
  return String(raw || '')
    .split(',')
    .map((value) => String(value || '').trim())
    .filter(Boolean);
}

async function fetchMailDraft(path, options = {}) {
  const resp = await fetch(apiURL(path), options);
  if (!resp.ok) {
    throw new Error((await resp.text()).trim() || `HTTP ${resp.status}`);
  }
  const payload = await resp.json();
  return payload?.draft || payload;
}

async function presentMailDraft(path, options = {}, successText = '') {
  const draft = await fetchMailDraft(path, options);
  applyCanvasArtifactEvent(mailDraftEventFromPayload(draft));
  await loadItemSidebarView(state.itemSidebarView);
  if (successText) {
    showStatus(successText);
  }
  return draft;
}

export async function createNewMailDraft() {
  try {
    await presentMailDraft('mail/drafts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    }, 'draft created');
    return true;
  } catch (err) {
    showStatus(`mail draft failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function replyToSidebarItem(item) {
  const itemID = Number(item?.id || 0);
  if (itemID <= 0) return false;
  try {
    await presentMailDraft('mail/drafts/reply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ item_id: itemID }),
    }, 'reply draft ready');
    return true;
  } catch (err) {
    showStatus(`reply draft failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

function mailReplyArtifactTitle(item) {
  return String(item?.artifact_title || item?.title || '').trim();
}

export async function launchNewMailAuthoring() {
  if (!mailAuthoringUsesVoice()) {
    return createNewMailDraft();
  }
  const ok = await createNewMailDraft();
  if (ok && state.mailDraft?.artifactId > 0) {
    startVoiceMailMode(state.mailDraft.artifactId);
  }
  return ok;
}

export async function launchReplyAuthoring(item) {
  if (!mailAuthoringUsesVoice()) {
    return replyToSidebarItem(item);
  }
  const ok = await replyToSidebarItem(item);
  if (ok && state.mailDraft?.artifactId > 0) {
    startVoiceMailMode(state.mailDraft.artifactId);
  }
  return ok;
}

export async function replyAllToSidebarItem(item) {
  const itemID = Number(item?.id || 0);
  if (itemID <= 0) return false;
  try {
    await presentMailDraft('mail/drafts/reply-all', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ item_id: itemID }),
    }, 'reply all draft ready');
    return true;
  } catch (err) {
    showStatus(`reply all draft failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function launchReplyAllAuthoring(item) {
  if (!mailAuthoringUsesVoice()) {
    return replyAllToSidebarItem(item);
  }
  const ok = await replyAllToSidebarItem(item);
  if (ok && state.mailDraft?.artifactId > 0) {
    startVoiceMailMode(state.mailDraft.artifactId);
  }
  return ok;
}

export async function forwardSidebarItem(item) {
  const itemID = Number(item?.id || 0);
  if (itemID <= 0) return false;
  try {
    await presentMailDraft('mail/drafts/forward', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ item_id: itemID }),
    }, 'forward draft ready');
    return true;
  } catch (err) {
    showStatus(`forward draft failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export async function launchForwardAuthoring(item) {
  if (!mailAuthoringUsesVoice()) {
    return forwardSidebarItem(item);
  }
  const ok = await forwardSidebarItem(item);
  if (ok && state.mailDraft?.artifactId > 0) {
    startVoiceMailMode(state.mailDraft.artifactId);
  }
  return ok;
}

export async function openMailDraftArtifact(artifactID) {
  const id = Number(artifactID || 0);
  if (id <= 0) return false;
  try {
    const draft = await fetchMailDraft(`mail/drafts/${encodeURIComponent(String(id))}`, { cache: 'no-store' });
    applyCanvasArtifactEvent(mailDraftEventFromPayload(draft));
    return true;
  } catch (err) {
    showStatus(`mail draft open failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

async function saveActiveMailDraft(options: Record<string, any> = {}) {
  const artifactID = Number(state.mailDraft?.artifactId || 0);
  const form = readMailDraftForm();
  const allowWhileSending = Boolean(options?.allowWhileSending);
  if (artifactID <= 0 || !form || (state.mailDraft.sending && !allowWhileSending)) return false;
  state.mailDraft.saving = true;
  setMailDraftStatus('Saving draft…');
  try {
    const draft = await fetchMailDraft(`mail/drafts/${encodeURIComponent(String(artifactID))}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    });
    state.mailDraft.saving = false;
    state.mailDraft.artifactId = Number(draft?.artifact_id || artifactID);
    state.mailDraft.itemId = Number(draft?.item_id || state.mailDraft.itemId || 0);
    updateRenderedMailDraftMeta(draft);
    setMailDraftStatus('Draft saved');
    return true;
  } catch (err) {
    state.mailDraft.saving = false;
    setMailDraftStatus('Save failed');
    showStatus(`mail draft save failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export function scheduleMailDraftSave() {
  clearMailDraftTimer();
  state.mailDraft.saveTimer = window.setTimeout(() => {
    state.mailDraft.saveTimer = null;
    void saveActiveMailDraft();
  }, 450);
  setMailDraftStatus('Unsaved changes');
}

export async function sendActiveMailDraft() {
  const artifactID = Number(state.mailDraft?.artifactId || 0);
  if (artifactID <= 0) return false;
  clearMailDraftTimer();
  try {
    state.mailDraft.sending = true;
    const saved = await saveActiveMailDraft({ allowWhileSending: true });
    if (!saved) {
      state.mailDraft.sending = false;
      setMailDraftStatus('Send failed');
      return false;
    }
    setMailDraftStatus('Sending…');
    const draft = await fetchMailDraft(`mail/drafts/${encodeURIComponent(String(artifactID))}/send`, {
      method: 'POST',
    });
    state.mailDraft.sending = false;
    applyCanvasArtifactEvent(mailDraftEventFromPayload(draft));
    await loadItemSidebarView(state.itemSidebarView);
    setMailDraftStatus('Sent');
    showStatus('mail sent');
    return true;
  } catch (err) {
    state.mailDraft.sending = false;
    setMailDraftStatus('Send failed');
    showStatus(`mail send failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

async function populateRecipientSuggestions(root) {
  if (!(root instanceof HTMLElement)) return;
  try {
    const resp = await fetch(apiURL('actors'), { cache: 'no-store' });
    if (!resp.ok) return;
    const payload = await resp.json();
    const actors = Array.isArray(payload?.actors) ? payload.actors : [];
    const datalist = root.querySelector(`#${MAIL_DRAFT_SUGGESTIONS_ID}`);
    if (!(datalist instanceof HTMLDataListElement)) return;
    datalist.innerHTML = '';
    actors.forEach((actor) => {
      const email = String(actor?.email || '').trim();
      if (!email) return;
      const option = document.createElement('option');
      const name = String(actor?.name || '').trim();
      option.value = email;
      option.label = name ? `${name} <${email}>` : email;
      datalist.appendChild(option);
    });
  } catch (_) {}
}

function buildMailDraftField(field) {
  const row = document.createElement('label');
  row.className = 'mail-draft-field-line';

  const key = document.createElement('span');
  key.className = 'mail-draft-key';
  key.textContent = field.label;

  const value = document.createElement('div');
  value.className = 'mail-draft-field-value';

  const input = document.createElement('input');
  input.type = 'text';
  input.name = field.name;
  input.value = field.value;
  input.autocomplete = 'off';
  if (field.list) input.setAttribute('list', field.list);
  input.addEventListener('input', scheduleMailDraftSave);

  value.appendChild(input);
  row.appendChild(key);
  row.appendChild(value);
  return row;
}

export function renderMailDraftArtifact(root, event) {
  const draft = event?.draft && typeof event.draft === 'object' ? event.draft : {};
  if (!(root instanceof HTMLElement)) return;
  resetMailDraftState();
  Object.assign(state.mailDraft, {
    artifactId: Number(draft?.artifact_id || 0),
    itemId: Number(draft?.item_id || 0),
    saving: false,
    sending: false,
    status: String(draft?.status || 'draft').trim() || 'draft',
  });
  root.innerHTML = '';
  root.classList.add('mail-draft-canvas');

  const shell = document.createElement('section');
  shell.id = MAIL_DRAFT_EDITOR_ID;
  shell.className = 'mail-draft-editor canvas-embedded-ui';

  const header = document.createElement('header');
  header.className = 'mail-draft-header';
  const headerCopy = document.createElement('div');
  headerCopy.className = 'mail-draft-header-copy';
  const kicker = document.createElement('div');
  kicker.className = 'mail-draft-kicker';
  kicker.textContent = 'Mail Draft';
  const heading = document.createElement('h1');
  heading.className = 'mail-draft-title';
  heading.textContent = String(draft?.title || 'Draft email').trim() || 'Draft email';
  const account = document.createElement('p');
  account.className = 'mail-draft-account';
  account.textContent = `${String(draft?.account_label || '').trim()}${draft?.provider ? ` · ${String(draft.provider).trim()}` : ''}`.trim();
  headerCopy.appendChild(kicker);
  headerCopy.appendChild(heading);
  if (account.textContent) {
    headerCopy.appendChild(account);
  }
  const headerActions = document.createElement('div');
  headerActions.className = 'mail-draft-header-actions';
  const status = document.createElement('p');
  status.id = MAIL_DRAFT_STATUS_ID;
  status.className = 'mail-draft-status';
  status.textContent = String(draft?.status || 'draft').trim() || 'draft';
  const send = document.createElement('button');
  send.type = 'button';
  send.className = 'edge-btn';
  send.id = 'mail-draft-send';
  send.textContent = 'Send';
  headerActions.appendChild(status);
  headerActions.appendChild(send);
  header.appendChild(headerCopy);
  header.appendChild(headerActions);
  shell.appendChild(header);

  const datalist = document.createElement('datalist');
  datalist.id = MAIL_DRAFT_SUGGESTIONS_ID;
  shell.appendChild(datalist);

  const paper = document.createElement('div');
  paper.className = 'mail-draft-paper';

  const envelope = document.createElement('section');
  envelope.className = 'mail-draft-envelope';
  const envelopeLabel = document.createElement('div');
  envelopeLabel.className = 'mail-draft-section-label';
  envelopeLabel.textContent = 'Envelope';
  const envelopeFields = document.createElement('div');
  envelopeFields.className = 'mail-draft-envelope-fields';
  const fields = [
    { label: 'To', name: 'to', value: Array.isArray(draft?.to) ? draft.to.join(', ') : '', list: MAIL_DRAFT_SUGGESTIONS_ID },
    { label: 'Cc', name: 'cc', value: Array.isArray(draft?.cc) ? draft.cc.join(', ') : '', list: MAIL_DRAFT_SUGGESTIONS_ID },
    { label: 'Bcc', name: 'bcc', value: Array.isArray(draft?.bcc) ? draft.bcc.join(', ') : '', list: MAIL_DRAFT_SUGGESTIONS_ID },
    { label: 'Subject', name: 'subject', value: String(draft?.subject || '') },
  ];
  fields.forEach((field) => {
    envelopeFields.appendChild(buildMailDraftField(field));
  });
  envelope.appendChild(envelopeLabel);
  envelope.appendChild(envelopeFields);
  paper.appendChild(envelope);

  const letter = document.createElement('section');
  letter.className = 'mail-draft-letter';
  const letterLabel = document.createElement('div');
  letterLabel.className = 'mail-draft-section-label';
  letterLabel.textContent = 'Message';
  const bodyRow = document.createElement('label');
  bodyRow.className = 'mail-draft-body-row';
  const body = document.createElement('textarea');
  body.name = 'body';
  body.className = 'mail-draft-body';
  body.value = String(draft?.body || '');
  body.placeholder = 'Write the message here.';
  body.addEventListener('input', scheduleMailDraftSave);
  body.addEventListener('pointerdown', (ev) => onMailDraftBodyTap(ev));
  bodyRow.appendChild(body);
  letter.appendChild(letterLabel);
  letter.appendChild(bodyRow);
  paper.appendChild(letter);

  shell.appendChild(paper);

  root.appendChild(shell);
  const sendButton = root.querySelector('#mail-draft-send');
  if (sendButton instanceof HTMLButtonElement) {
    sendButton.addEventListener('click', () => { void sendActiveMailDraft(); });
  }
  setMailDraftStatus(String(draft?.status || 'draft').trim() || 'draft');
  void populateRecipientSuggestions(shell);
}
