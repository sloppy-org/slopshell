import * as env from './app-env.js';
import * as context from './app-context.js';

const { apiURL } = env;
const { refs, state } = context;
const applyCanvasArtifactEvent = (...args) => refs.applyCanvasArtifactEvent(...args);
const showStatus = (...args) => refs.showStatus(...args);

const MAIL_TRIAGE_DEFAULT_LIMIT = 100;
const mailTriageMessageCache = new Map();

function resetMailTriageState() {
  Object.assign(state.mailTriage, {
    active: false,
    accountId: 0,
    accountLabel: '',
    provider: '',
    folder: '',
    filterText: '',
    queue: [],
    index: 0,
    loading: false,
    submitting: false,
    completed: 0,
    decisions: { inbox: 0, cc: 0, archive: 0, trash: 0 },
    currentMessage: null,
    prefetchedMessage: null,
    prefetchedMessageID: '',
    lastReviewId: 0,
  });
  mailTriageMessageCache.clear();
}

function mailTriageFetchJSON(path, options = {}) {
  return fetch(apiURL(path), options).then(async (resp) => {
    const payload = await resp.json().catch(() => null);
    if (!resp.ok) {
      const message = String(payload?.error || payload?.message || `HTTP ${resp.status}`).trim();
      throw new Error(message || `HTTP ${resp.status}`);
    }
    return payload?.data || payload;
  });
}

function preferredMailTriageAccount(accounts) {
  const list = Array.isArray(accounts) ? accounts : [];
  return list.find((account) => String(account?.provider || '').trim().toLowerCase() === 'exchange_ews' && String(account?.sphere || '').trim().toLowerCase() === 'work')
    || list.find((account) => String(account?.sphere || '').trim().toLowerCase() === 'work')
    || list[0]
    || null;
}

function folderForMailTriageMode(account, mode) {
  const provider = String(account?.provider || '').trim().toLowerCase();
  if (String(mode || '').trim().toLowerCase() === 'junk') {
    return provider === 'exchange_ews' ? 'Junk-E-Mail' : 'junk';
  }
  return provider === 'exchange_ews' ? 'Posteingang' : 'inbox';
}

function filterForMailTriageMode(mode) {
  return String(mode || '').trim().toLowerCase() === 'junk' ? '[SUSPICIOUS MESSAGE]' : '';
}

function triageTitleForFolder(folder) {
  const normalized = String(folder || '').trim().toLowerCase();
  if (normalized === 'junk-e-mail' || normalized === 'junk') {
    return 'Junk Audit';
  }
  return 'Inbox Triage';
}

function triageEventFromState() {
  return {
    kind: 'email_triage',
    title: triageTitleForFolder(state.mailTriage.folder),
    triage: {
      ...state.mailTriage,
      currentMessage: state.mailTriage.currentMessage,
    },
  };
}

function rerenderMailTriage() {
  applyCanvasArtifactEvent(triageEventFromState());
}

function currentMailTriageQueueEntry(index = state.mailTriage.index) {
  const queue = Array.isArray(state.mailTriage.queue) ? state.mailTriage.queue : [];
  return index >= 0 && index < queue.length ? queue[index] : null;
}

async function fetchMailTriageMessage(messageID) {
  const id = String(messageID || '').trim();
  if (!id) {
    throw new Error('missing message id');
  }
  const cached = mailTriageMessageCache.get(id);
  if (cached) {
    return cached;
  }
  const promise = mailTriageFetchJSON(`external-accounts/${encodeURIComponent(String(state.mailTriage.accountId || 0))}/mail/messages/${encodeURIComponent(id)}`)
    .then((payload) => payload?.message || null)
    .catch((err) => {
      mailTriageMessageCache.delete(id);
      throw err;
    });
  mailTriageMessageCache.set(id, promise);
  return promise;
}

function queueNextPrefetch() {
  const current = currentMailTriageQueueEntry(state.mailTriage.index + 1);
  if (!current?.id) {
    state.mailTriage.prefetchedMessage = null;
    state.mailTriage.prefetchedMessageID = '';
    return;
  }
  const id = String(current.id || '').trim();
  fetchMailTriageMessage(id)
    .then((message) => {
      if (String(currentMailTriageQueueEntry(state.mailTriage.index + 1)?.id || '').trim() !== id) {
        return;
      }
      state.mailTriage.prefetchedMessage = message;
      state.mailTriage.prefetchedMessageID = id;
      rerenderMailTriage();
    })
    .catch(() => {});
}

async function loadCurrentMailTriageMessage() {
  const queue = Array.isArray(state.mailTriage.queue) ? state.mailTriage.queue : [];
  while (state.mailTriage.index < queue.length) {
    const current = currentMailTriageQueueEntry();
    if (!current?.id) {
      state.mailTriage.index += 1;
      continue;
    }
    try {
      const currentID = String(current.id || '').trim();
      if (state.mailTriage.prefetchedMessageID === currentID && state.mailTriage.prefetchedMessage) {
        state.mailTriage.currentMessage = state.mailTriage.prefetchedMessage;
      } else {
        state.mailTriage.currentMessage = await fetchMailTriageMessage(currentID);
      }
      state.mailTriage.active = true;
      state.mailTriage.prefetchedMessage = null;
      state.mailTriage.prefetchedMessageID = '';
      rerenderMailTriage();
      queueNextPrefetch();
      return true;
    } catch (err) {
      showStatus(`triage skip: ${String(err?.message || err || 'unknown error')}`);
      state.mailTriage.index += 1;
    }
  }
  state.mailTriage.currentMessage = null;
  state.mailTriage.prefetchedMessage = null;
  state.mailTriage.prefetchedMessageID = '';
  state.mailTriage.active = true;
  rerenderMailTriage();
  return false;
}

export async function openMailTriageMode(options = {}) {
  const mode = String(options?.mode || 'inbox').trim().toLowerCase();
  const limitValue = Number(options?.limit || MAIL_TRIAGE_DEFAULT_LIMIT);
  const limit = Number.isFinite(limitValue) && limitValue > 0 ? Math.floor(limitValue) : MAIL_TRIAGE_DEFAULT_LIMIT;
  resetMailTriageState();
  state.mailTriage.loading = true;
  rerenderMailTriage();
  try {
    const accountsPayload = await mailTriageFetchJSON('mail/accounts');
    const account = preferredMailTriageAccount(accountsPayload?.accounts);
    if (!account?.id) {
      throw new Error('no mail account available');
    }
    const folder = folderForMailTriageMode(account, mode);
    const filterText = filterForMailTriageMode(mode);
    const query = new URLSearchParams({ folder, limit: String(limit) });
    if (filterText) {
      query.set('text', filterText);
    }
    const [listPayload, reviewsPayload] = await Promise.all([
      mailTriageFetchJSON(`external-accounts/${encodeURIComponent(String(account.id))}/mail/messages?${query.toString()}`),
      mailTriageFetchJSON(`external-accounts/${encodeURIComponent(String(account.id))}/mail-triage/manual/reviews?limit=1&folder=${encodeURIComponent(folder)}`),
    ]);
    const reviewedMessageIDs = new Set(
      Array.isArray(reviewsPayload?.reviewed_message_ids)
        ? reviewsPayload.reviewed_message_ids.map((value) => String(value || '').trim()).filter(Boolean)
        : [],
    );
    const queue = Array.isArray(listPayload?.messages) ? listPayload.messages.map((message) => ({
      id: String(message?.ID || '').trim(),
      subject: String(message?.Subject || '').trim(),
      sender: String(message?.Sender || '').trim(),
      labels: Array.isArray(message?.Labels) ? message.Labels.slice() : [],
      date: String(message?.Date || '').trim(),
    })).filter((message) => message.id && !reviewedMessageIDs.has(message.id)) : [];
    Object.assign(state.mailTriage, {
      active: true,
      accountId: Number(account.id || 0),
      accountLabel: String(account.label || account.account_name || account.provider || 'Mail').trim(),
      provider: String(account.provider || '').trim(),
      folder,
      filterText,
      queue,
      index: 0,
      loading: false,
      submitting: false,
      completed: 0,
      decisions: { inbox: 0, cc: 0, archive: 0, trash: 0 },
      currentMessage: null,
      prefetchedMessage: null,
      prefetchedMessageID: '',
      lastReviewId: 0,
    });
    if (queue.length === 0) {
      rerenderMailTriage();
      showStatus('no mail triage candidates');
      return false;
    }
    showStatus(`${triageTitleForFolder(folder).toLowerCase()} ready`);
    return loadCurrentMailTriageMessage();
  } catch (err) {
    state.mailTriage.loading = false;
    rerenderMailTriage();
    showStatus(`mail triage failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

export function openInboxMailTriage() {
  return openMailTriageMode({ mode: 'inbox', limit: MAIL_TRIAGE_DEFAULT_LIMIT });
}

export function openJunkMailTriage() {
  return openMailTriageMode({ mode: 'junk', limit: MAIL_TRIAGE_DEFAULT_LIMIT });
}

function recordDecisionLocally(action) {
  const key = String(action || '').trim().toLowerCase();
  const next = { ...(state.mailTriage.decisions || {}) };
  if (Object.prototype.hasOwnProperty.call(next, key)) {
    next[key] = Number(next[key] || 0) + 1;
  }
  state.mailTriage.decisions = next;
  state.mailTriage.completed = Number(state.mailTriage.completed || 0) + 1;
}

export async function submitMailTriageDecision(action) {
  const normalized = String(action || '').trim().toLowerCase();
  const message = state.mailTriage.currentMessage;
  if (state.mailTriage.submitting || !message?.ID || !state.mailTriage.accountId) {
    return false;
  }
  state.mailTriage.submitting = true;
  rerenderMailTriage();
  try {
    const payload = await mailTriageFetchJSON(`external-accounts/${encodeURIComponent(String(state.mailTriage.accountId))}/mail-triage/manual/reviews`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        message_id: String(message.ID || ''),
        folder: String(state.mailTriage.folder || ''),
        action: normalized,
      }),
    });
    recordDecisionLocally(normalized);
    state.mailTriage.lastReviewId = Number(payload?.review?.id || 0);
    state.mailTriage.index += 1;
    state.mailTriage.currentMessage = null;
    state.mailTriage.submitting = false;
    showStatus(`${normalized} saved`);
    return loadCurrentMailTriageMessage();
  } catch (err) {
    state.mailTriage.submitting = false;
    rerenderMailTriage();
    showStatus(`triage action failed: ${String(err?.message || err || 'unknown error')}`);
    return false;
  }
}

function mailTriageShortcutActionForKey(key) {
  switch (String(key || '').trim()) {
    case 'ArrowLeft':
      return 'inbox';
    case 'ArrowUp':
      return 'cc';
    case 'ArrowDown':
      return 'archive';
    case 'ArrowRight':
      return 'trash';
    default:
      return '';
  }
}

export function handleMailTriageShortcut(ev) {
  if (!state.mailTriage.active || !state.mailTriage.currentMessage || state.mailTriage.loading || state.mailTriage.submitting) {
    return false;
  }
  const action = mailTriageShortcutActionForKey(ev?.key);
  if (!action) {
    return false;
  }
  ev.preventDefault();
  void submitMailTriageDecision(action);
  return true;
}

function triageBodyText(message) {
  const bodyText = String(message?.BodyText || '').trim();
  if (bodyText) return bodyText;
  return String(message?.Snippet || '').trim();
}

function formatMailTriageDate(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function triageDecisionBadge(text, value) {
  const badge = document.createElement('span');
  badge.className = 'mail-triage-badge';
  badge.textContent = `${text}: ${value}`;
  return badge;
}

export function renderMailTriageArtifact(root, event) {
  if (!(root instanceof HTMLElement)) return;
  const triage = event?.triage && typeof event.triage === 'object' ? event.triage : state.mailTriage;
  root.classList.add('mail-triage-canvas', 'canvas-embedded-ui');
  root.innerHTML = '';

  const shell = document.createElement('div');
  shell.className = 'mail-triage-shell';

  const header = document.createElement('div');
  header.className = 'mail-triage-header';

  const headerCopy = document.createElement('div');
  headerCopy.className = 'mail-triage-header-copy';
  const kicker = document.createElement('div');
  kicker.className = 'mail-triage-kicker';
  kicker.textContent = `${triageTitleForFolder(triage.folder)} • ${String(triage.accountLabel || 'Mail').trim() || 'Mail'}`;
  headerCopy.appendChild(kicker);
  const title = document.createElement('h1');
  title.textContent = String(event?.title || triageTitleForFolder(triage.folder));
  headerCopy.appendChild(title);
  const detail = document.createElement('div');
  detail.className = 'mail-triage-detail';
  const queueLength = Array.isArray(triage.queue) ? triage.queue.length : 0;
  const index = Number(triage.index || 0);
  const progressText = queueLength > 0 && index < queueLength ? `${index + 1} / ${queueLength}` : `${Math.min(index, queueLength)} / ${queueLength}`;
  detail.textContent = [progressText, String(triage.filterText || '').trim() ? `filter ${triage.filterText}` : '', `stored reviews ${Number(triage.lastReviewId || 0) > 0 ? 'on' : 'pending'}`, 'left inbox • up cc • down archive • right trash']
    .filter(Boolean)
    .join(' • ');
  headerCopy.appendChild(detail);
  header.appendChild(headerCopy);

  const closeButton = document.createElement('button');
  closeButton.type = 'button';
  closeButton.className = 'edge-btn mail-triage-close';
  closeButton.textContent = 'Close';
  closeButton.addEventListener('click', () => {
    resetMailTriageState();
    applyCanvasArtifactEvent({ kind: 'clear_canvas' });
  });
  header.appendChild(closeButton);
  shell.appendChild(header);

  const body = document.createElement('div');
  body.className = 'mail-triage-body';

  if (triage.loading) {
    const empty = document.createElement('div');
    empty.className = 'mail-triage-empty';
    empty.textContent = 'Loading mail triage queue...';
    body.appendChild(empty);
  } else if (!triage.currentMessage) {
    const empty = document.createElement('div');
    empty.className = 'mail-triage-empty';
    empty.textContent = Array.isArray(triage.queue) && triage.queue.length > 0
      ? 'Manual triage complete for this batch.'
      : 'No messages matched this triage mode.';
    body.appendChild(empty);
  } else {
    const message = triage.currentMessage;
    const meta = document.createElement('div');
    meta.className = 'mail-triage-meta';
    meta.appendChild(triageDecisionBadge('From', String(message.Sender || '').trim() || 'Unknown sender'));
    meta.appendChild(triageDecisionBadge('Date', formatMailTriageDate(message.Date)));
    if (Array.isArray(message.Labels) && message.Labels.length > 0) {
      meta.appendChild(triageDecisionBadge('Labels', message.Labels.join(', ')));
    }
    if (message.IsFlagged) {
      meta.appendChild(triageDecisionBadge('Flagged', 'yes'));
    }
    body.appendChild(meta);

    const subject = document.createElement('h2');
    subject.className = 'mail-triage-subject';
    subject.textContent = String(message.Subject || '').trim() || '(no subject)';
    body.appendChild(subject);

    const snippet = document.createElement('div');
    snippet.className = 'mail-triage-snippet';
    snippet.textContent = String(message.Snippet || '').trim();
    if (snippet.textContent) {
      body.appendChild(snippet);
    }

    const text = document.createElement('pre');
    text.className = 'mail-triage-message';
    text.textContent = triageBodyText(message);
    body.appendChild(text);

    const actions = document.createElement('div');
    actions.className = 'mail-triage-actions';
    [
      ['Inbox', 'inbox'],
      ['CC', 'cc'],
      ['Archive', 'archive'],
      ['Trash', 'trash'],
    ].forEach(([label, action]) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = `edge-btn mail-triage-action mail-triage-action-${String(action)}`;
      button.textContent = String(label);
      button.disabled = Boolean(triage.submitting);
      button.addEventListener('click', () => {
        void submitMailTriageDecision(action);
      });
      actions.appendChild(button);
    });
    body.appendChild(actions);
  }

  const footer = document.createElement('div');
  footer.className = 'mail-triage-footer';
  const counts = triage.decisions || {};
  footer.appendChild(triageDecisionBadge('Inbox', Number(counts.inbox || 0)));
  footer.appendChild(triageDecisionBadge('CC', Number(counts.cc || 0)));
  footer.appendChild(triageDecisionBadge('Archive', Number(counts.archive || 0)));
  footer.appendChild(triageDecisionBadge('Trash', Number(counts.trash || 0)));
  body.appendChild(footer);

  shell.appendChild(body);
  root.appendChild(shell);
}
