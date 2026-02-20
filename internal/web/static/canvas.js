import { marked } from './vendor/marked.esm.js';

const FORTRAN_KEYWORDS = [
  'program', 'module', 'contains', 'implicit', 'none',
  'integer', 'real', 'double', 'precision', 'complex', 'logical', 'character', 'type',
  'subroutine', 'function', 'call',
  'if', 'then', 'else', 'elseif', 'select', 'case', 'where',
  'do', 'enddo', 'end', 'stop', 'return', 'cycle', 'exit',
  'allocate', 'deallocate', 'parameter', 'intent', 'in', 'out', 'inout',
  'use', 'only', 'private', 'public', 'interface', 'elemental', 'pure', 'recursive',
];

function escapeHtml(text) {
  return String(text)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function withStashedParts(input, stasher) {
  const stash = [];
  const out = stasher(input, (html) => {
    const key = `@@HL${stash.length}@@`;
    stash.push({ key, html });
    return key;
  });
  let restored = out;
  for (const part of stash) {
    restored = restored.replaceAll(part.key, part.html);
  }
  return restored;
}

function highlightFortranInline(lineEscaped) {
  const kwPattern = new RegExp(`\\b(?:${FORTRAN_KEYWORDS.join('|')})\\b`, 'gi');
  return withStashedParts(lineEscaped, (source, put) => {
    let out = source;
    out = out.replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g, (m) => put(`<span class="hl-str">${m}</span>`));
    out = out.replace(/!.*/g, (m) => put(`<span class="hl-cmt">${m}</span>`));
    out = out.replace(/\b\d+(?:\.\d+)?(?:[edED][+\-]?\d+)?\b/g, '<span class="hl-num">$&</span>');
    out = out.replace(/\.(?:eq|ne|lt|le|gt|ge|and|or|not|true|false)\./gi, '<span class="hl-kw">$&</span>');
    out = out.replace(kwPattern, '<span class="hl-kw">$&</span>');
    return out;
  });
}

function highlightFortran(code) {
  return code.split('\n').map((line) => highlightFortranInline(escapeHtml(line))).join('\n');
}

function classifyDiffLine(line) {
  if (line.startsWith('diff --git') || line.startsWith('index ') || line.startsWith('+++ ') || line.startsWith('--- ')) {
    return 'meta';
  }
  if (line.startsWith('@@')) {
    return 'hunk';
  }
  if (line.startsWith('+') && !line.startsWith('+++')) {
    return 'add';
  }
  if (line.startsWith('-') && !line.startsWith('---')) {
    return 'del';
  }
  return 'ctx';
}

function highlightDiff(code) {
  const lines = code.split('\n');
  return lines.map((line) => {
    const kind = classifyDiffLine(line);
    if (kind === 'meta' || kind === 'hunk') {
      return `<span class="hl-diff-line hl-diff-${kind}">${escapeHtml(line)}</span>`;
    }
    if (!line) {
      return '<span class="hl-diff-line hl-diff-ctx"></span>';
    }
    const prefix = line.charAt(0);
    const rest = line.slice(1);
    const highlightedRest = highlightFortranInline(escapeHtml(rest));
    return `<span class="hl-diff-line hl-diff-${kind}">${escapeHtml(prefix)}${highlightedRest}</span>`;
  }).join('');
}

function renderCodeBlock(code, langRaw) {
  const lang = (langRaw || '').toLowerCase();
  if (lang === 'fortran' || lang === 'f90' || lang === 'f95' || lang === 'f03' || lang === 'f08') {
    return `<pre><code class="language-${escapeHtml(lang || 'fortran')}">${highlightFortran(code)}</code></pre>\n`;
  }
  if (lang === 'diff' || lang === 'patch' || lang === 'git') {
    return `<pre><code class="language-${escapeHtml(lang)}">${highlightDiff(code)}</code></pre>\n`;
  }
  return `<pre><code class="language-${escapeHtml(lang || 'plaintext')}">${escapeHtml(code)}</code></pre>\n`;
}

const renderer = new marked.Renderer();
renderer.code = ({ text, lang }) => renderCodeBlock(text || '', lang || '');

marked.setOptions({
  breaks: true,
  renderer,
});

const els = {};
let activeTextEventId = null;
let activePdfEvent = null;
let draftMark = null;
let activeMailContext = null;
let mailCapabilitiesRequestSeq = 0;

const DEFAULT_PRODUCER_MCP_URL = 'http://127.0.0.1:8090/mcp';

function getEls() {
  if (!els.empty) {
    els.empty = document.getElementById('canvas-empty');
    els.text = document.getElementById('canvas-text');
    els.image = document.getElementById('canvas-image');
    els.img = document.getElementById('canvas-img');
    els.pdf = document.getElementById('canvas-pdf');
    els.title = document.getElementById('canvas-title');
    els.mode = document.getElementById('canvas-mode');
    els.markType = document.getElementById('canvas-mark-type');
    els.markComment = document.getElementById('canvas-mark-comment');
  }
  return els;
}

function sanitizeHtml(html) {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const dangerous = doc.querySelectorAll('script,iframe,object,embed,link[rel="import"],form,svg,base,style');
  dangerous.forEach(el => el.remove());
  doc.querySelectorAll('*').forEach(el => {
    for (const attr of [...el.attributes]) {
      const val = attr.value.trim().toLowerCase();
      const isDangerous = attr.name.startsWith('on')
        || val.startsWith('javascript:')
        || val.startsWith('vbscript:')
        || (val.startsWith('data:') && !val.startsWith('data:image/'));
      if (isDangerous) {
        el.removeAttribute(attr.name);
      }
    }
  });
  return doc.body.innerHTML;
}

function hideAll() {
  const e = getEls();
  e.empty.style.display = 'none';
  e.text.style.display = 'none';
  e.image.style.display = 'none';
  e.pdf.style.display = 'none';
}

function ensureTextOverlay() {
  const e = getEls();
  let overlay = e.text.querySelector('.canvas-mark-overlay');
  if (!overlay) {
    overlay = document.createElement('div');
    overlay.className = 'canvas-mark-overlay';
    e.text.appendChild(overlay);
  }
  return overlay;
}

function clearOverlay() {
  const e = getEls();
  const overlay = e.text.querySelector('.canvas-mark-overlay');
  if (overlay) {
    overlay.innerHTML = '';
  }
}

function getSelectedMarkType() {
  const e = getEls();
  if (!e.markType) return 'highlight';
  return e.markType.value || 'highlight';
}

function getMarkComment() {
  const e = getEls();
  if (!e.markComment) return null;
  const text = (e.markComment.value || '').trim();
  return text || null;
}

function renderDraftOverlay() {
  clearOverlay();
  if (!draftMark || !activeTextEventId || draftMark.event_id !== activeTextEventId) {
    return;
  }
  if (!Array.isArray(draftMark.rects) || !draftMark.rects.length) {
    return;
  }

  const overlay = ensureTextOverlay();
  const markType = draftMark.type || 'highlight';
  for (const rect of draftMark.rects) {
    if (!Array.isArray(rect) || rect.length !== 4) continue;
    const el = document.createElement('div');
    el.className = `canvas-mark-rect canvas-mark-${markType}`;
    el.style.left = `${rect[0]}px`;
    el.style.top = `${rect[1]}px`;
    el.style.width = `${Math.max(1, rect[2])}px`;
    el.style.height = `${Math.max(2, rect[3])}px`;
    overlay.appendChild(el);
  }
}

function computeRectsFromRange(root, range) {
  const rootRect = root.getBoundingClientRect();
  const rects = [];
  for (const r of range.getClientRects()) {
    if (!r.width || !r.height) continue;
    rects.push([
      r.left - rootRect.left + root.scrollLeft,
      r.top - rootRect.top + root.scrollTop,
      r.width,
      r.height,
    ]);
  }
  return rects;
}

function isSelectionInside(root, selection) {
  if (!selection || selection.rangeCount === 0) return false;
  const range = selection.getRangeAt(0);
  return root.contains(range.commonAncestorContainer);
}

function getSelectionOffsets(root, range) {
  const startProbe = range.cloneRange();
  startProbe.selectNodeContents(root);
  startProbe.setEnd(range.startContainer, range.startOffset);
  const startOffset = startProbe.toString().length;

  const endProbe = range.cloneRange();
  endProbe.selectNodeContents(root);
  endProbe.setEnd(range.endContainer, range.endOffset);
  const endOffset = endProbe.toString().length;

  return { startOffset, endOffset };
}

function lineFromOffset(lines, charOffset) {
  let charCount = 0;
  for (let i = 0; i < lines.length; i++) {
    if (charCount + lines[i].length >= charOffset) {
      return i + 1;
    }
    charCount += lines[i].length + 1;
  }
  return Math.max(1, lines.length);
}

function sendSelectionFeedback(payload) {
  const { getState } = window._tabulaApp || {};
  if (!getState) return;
  const state = getState();
  if (!state.canvasWs || state.canvasWs.readyState !== WebSocket.OPEN) return;
  state.canvasWs.send(JSON.stringify(payload));
}

function clearTextInteractionHandlers() {
  const e = getEls();
  if (e.text._selectionHandler) {
    document.removeEventListener('selectionchange', e.text._selectionHandler);
    e.text._selectionHandler = null;
  }
  if (e.text._mouseUpSelectionHandler) {
    e.text.removeEventListener('mouseup', e.text._mouseUpSelectionHandler);
    e.text._mouseUpSelectionHandler = null;
  }
  if (e.text._keyUpSelectionHandler) {
    e.text.removeEventListener('keyup', e.text._keyUpSelectionHandler);
    e.text._keyUpSelectionHandler = null;
  }
  if (e.text._selectionRaf) {
    cancelAnimationFrame(e.text._selectionRaf);
    e.text._selectionRaf = null;
  }
  if (e.text._scrollHandler) {
    e.text.removeEventListener('scroll', e.text._scrollHandler);
    e.text._scrollHandler = null;
  }
  if (e.text._mailClickHandler) {
    e.text.removeEventListener('click', e.text._mailClickHandler);
    e.text._mailClickHandler = null;
  }
  e.text.classList.remove('mail-artifact');
  activeMailContext = null;
}

function normalizeMailHeadersContext(event) {
  const triage = event?.meta?.message_triage_v1;
  if (!triage || typeof triage !== 'object') return null;
  const rawHeaders = Array.isArray(triage.headers) ? triage.headers : [];
  const headers = rawHeaders
    .map((h) => ({
      id: String(h?.id || '').trim(),
      date: String(h?.date || '').trim(),
      sender: String(h?.sender || '').trim(),
      subject: String(h?.subject || '').trim(),
    }))
    .filter((h) => h.id !== '');
  if (!headers.length) return null;
  return {
    eventId: event.event_id,
    provider: String(triage.provider || '').trim(),
    folder: String(triage.folder || '').trim(),
    count: Number.isFinite(Number(triage.count)) ? Number(triage.count) : headers.length,
    producerMcpUrl: String(event?.meta?.producer_mcp_url || DEFAULT_PRODUCER_MCP_URL).trim() || DEFAULT_PRODUCER_MCP_URL,
    headers,
    capabilities: null,
  };
}

function formatMailDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatLocalDateTimeInput(date) {
  const pad2 = (n) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}T${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}

function setMailRowStatus(row, text, tone = 'info') {
  const status = row.querySelector('[data-mail-row-status]');
  if (!status) return;
  status.textContent = text || '';
  status.className = `mail-row-status ${tone ? `mail-row-status-${tone}` : ''}`;
}

function setMailRowBusy(row, busy) {
  row.classList.toggle('mail-row-busy', Boolean(busy));
  row.querySelectorAll('button').forEach((button) => {
    if (button.dataset.mailAction === 'defer-cancel') return;
    if (!busy && button.dataset.mailLocked === '1') {
      button.disabled = true;
      return;
    }
    button.disabled = Boolean(busy);
  });
}

function closeMailDeferControls(row) {
  const controls = row.querySelector('[data-mail-defer-controls]');
  if (!controls) return;
  controls.hidden = true;
}

function openMailDeferControls(row) {
  const controls = row.querySelector('[data-mail-defer-controls]');
  const input = row.querySelector('[data-mail-defer-input]');
  if (!controls || !input) return;
  controls.hidden = false;
  input.value = formatLocalDateTimeInput(new Date(Date.now() + 60 * 60 * 1000));
  if (typeof input.showPicker === 'function') {
    input.showPicker();
  } else {
    input.focus();
  }
}

function setCapabilityHint(context) {
  const e = getEls();
  const hint = e.text.querySelector('[data-mail-capability-hint]');
  if (!hint) return;
  const provider = context.provider || 'default';
  if (!context.capabilities) {
    hint.textContent = `Provider ${provider}: checking defer capability...`;
    return;
  }
  const native = Boolean(context.capabilities.supports_native_defer);
  hint.textContent = native
    ? `Provider ${provider}: native defer available`
    : `Provider ${provider}: defer is stub/not supported`;
  e.text.querySelectorAll('[data-mail-action="defer"]').forEach((btn) => {
    btn.textContent = native ? 'Defer' : 'Defer (stub)';
  });
}

async function fetchMailCapabilities(eventId, context) {
  const requestSeq = ++mailCapabilitiesRequestSeq;
  try {
    const resp = await fetch('/api/mail/action-capabilities', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        provider: context.provider,
        producer_mcp_url: context.producerMcpUrl,
      }),
    });
    const payload = await resp.json();
    if (!resp.ok) {
      throw new Error(payload?.error || 'capability request failed');
    }
    if (requestSeq !== mailCapabilitiesRequestSeq || activeTextEventId !== eventId) return;
    context.capabilities = payload.capabilities || null;
  } catch (_) {
    if (requestSeq !== mailCapabilitiesRequestSeq || activeTextEventId !== eventId) return;
    context.capabilities = {
      supports_native_defer: false,
    };
  }
  setCapabilityHint(context);
}

async function callMailAction(context, action, messageID, untilAt) {
  const req = {
    provider: context.provider,
    action,
    message_id: messageID,
    producer_mcp_url: context.producerMcpUrl,
  };
  if (untilAt) {
    req.until_at = untilAt;
  }
  const resp = await fetch('/api/mail/action', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  let payload = {};
  const raw = await resp.text();
  if (raw) {
    try {
      payload = JSON.parse(raw);
    } catch (_) {
      if (!resp.ok) {
        throw new Error(raw);
      }
    }
  }
  if (!resp.ok) {
    throw new Error(typeof payload === 'object' && payload !== null && payload.error ? payload.error : raw || 'mail action failed');
  }
  if (typeof payload !== 'object' || payload === null) {
    throw new Error('mail action returned invalid response');
  }
  return payload.result || payload;
}

function renderMailHeadersHtml(context) {
  const provider = context.provider || 'default';
  const folder = context.folder || '-';
  const rows = context.headers.map((header) => `
    <tr data-message-id="${escapeHtml(header.id)}">
      <td>${escapeHtml(formatMailDate(header.date))}</td>
      <td>${escapeHtml(header.sender || '(no sender)')}</td>
      <td>${escapeHtml(header.subject || '(no subject)')}</td>
      <td class="mail-row-actions">
        <div class="mail-action-buttons">
          <button type="button" data-mail-action="open">Open</button>
          <button type="button" data-mail-action="archive">Archive</button>
          <button type="button" data-mail-action="delete">Delete</button>
          <button type="button" data-mail-action="defer">Defer</button>
        </div>
        <div class="mail-defer-controls" data-mail-defer-controls hidden>
          <input type="datetime-local" data-mail-defer-input>
          <button type="button" data-mail-action="defer-apply">Apply</button>
          <button type="button" data-mail-action="defer-cancel">Cancel</button>
        </div>
        <div class="mail-row-status" data-mail-row-status></div>
      </td>
    </tr>
  `).join('');
  return `
    <div class="mail-triage-head">
      <div><strong>Provider:</strong> ${escapeHtml(provider)}</div>
      <div><strong>Folder:</strong> ${escapeHtml(folder)}</div>
      <div><strong>Count:</strong> ${escapeHtml(String(context.count))}</div>
      <div class="mail-capability-hint" data-mail-capability-hint>Provider ${escapeHtml(provider)}: checking defer capability...</div>
    </div>
    <table class="mail-triage-table">
      <thead>
        <tr>
          <th>Date</th>
          <th>Sender</th>
          <th>Subject</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>
  `;
}

function lockMailRowActions(row) {
  row.querySelectorAll('button').forEach((button) => {
    button.dataset.mailLocked = '1';
    button.disabled = true;
  });
}

function applyMailActionState(row, action, result, untilAt) {
  if (result && result.status === 'stub_not_supported') {
    setMailRowStatus(row, 'Defer is not supported for this provider yet.', 'warning');
    return;
  }
  switch (action) {
    case 'open':
      row.classList.add('mail-row-opened');
      setMailRowStatus(row, 'Opened.', 'success');
      break;
    case 'archive':
      row.classList.add('mail-row-archived');
      setMailRowStatus(row, 'Archived.', 'success');
      lockMailRowActions(row);
      break;
    case 'delete':
      row.classList.add('mail-row-deleted');
      setMailRowStatus(row, 'Moved to trash.', 'success');
      lockMailRowActions(row);
      break;
    case 'defer': {
      row.classList.add('mail-row-deferred');
      const when = result?.deferred_until_at || untilAt;
      const whenDisplay = formatMailDate(when);
      setMailRowStatus(row, `Deferred until ${whenDisplay}.`, 'success');
      closeMailDeferControls(row);
      break;
    }
    default:
      break;
  }
}

function setupMailActionHandlers(eventId, context) {
  const e = getEls();
  if (e.text._mailClickHandler) {
    e.text.removeEventListener('click', e.text._mailClickHandler);
  }

  const onClick = (ev) => {
    const button = ev.target.closest('button[data-mail-action]');
    if (!button) return;
    const row = button.closest('tr[data-message-id]');
    if (!row) return;
    const action = button.dataset.mailAction;
    const messageID = row.dataset.messageId || '';
    if (!messageID) return;

    if (action === 'defer-cancel') {
      closeMailDeferControls(row);
      return;
    }
    if (action === 'defer') {
      const supportsNative = Boolean(context.capabilities?.supports_native_defer);
      if (!supportsNative) {
        setMailRowStatus(row, 'Defer is currently a stub for this provider.', 'warning');
        return;
      }
      openMailDeferControls(row);
      return;
    }
    if (action === 'defer-apply') {
      const input = row.querySelector('[data-mail-defer-input]');
      if (!input || !input.value) {
        setMailRowStatus(row, 'Choose a defer date/time first.', 'error');
        return;
      }
      const parsed = new Date(input.value);
      if (Number.isNaN(parsed.getTime())) {
        setMailRowStatus(row, 'Invalid defer date/time.', 'error');
        return;
      }
      const untilAt = parsed.toISOString();
      setMailRowBusy(row, true);
      setMailRowStatus(row, 'Applying defer...', 'info');
      void callMailAction(context, 'defer', messageID, untilAt)
        .then((result) => {
          if (activeTextEventId !== eventId) return;
          applyMailActionState(row, 'defer', result, untilAt);
        })
        .catch((err) => {
          if (activeTextEventId !== eventId) return;
          setMailRowStatus(row, String(err?.message || err || 'defer failed'), 'error');
        })
        .finally(() => {
          if (activeTextEventId !== eventId) return;
          setMailRowBusy(row, false);
        });
      return;
    }
    if (action !== 'open' && action !== 'archive' && action !== 'delete') {
      return;
    }
    setMailRowBusy(row, true);
    setMailRowStatus(row, `Running ${action}...`, 'info');
    void callMailAction(context, action, messageID, '')
      .then((result) => {
        if (activeTextEventId !== eventId) return;
        applyMailActionState(row, action, result, '');
      })
      .catch((err) => {
        if (activeTextEventId !== eventId) return;
        setMailRowStatus(row, String(err?.message || err || 'action failed'), 'error');
      })
      .finally(() => {
        if (activeTextEventId !== eventId) return;
        setMailRowBusy(row, false);
      });
  };

  e.text._mailClickHandler = onClick;
  e.text.addEventListener('click', onClick);
}

function renderMailArtifact(event, context) {
  const e = getEls();
  e.text.classList.add('mail-artifact');
  e.text.innerHTML = renderMailHeadersHtml(context);
  setupMailActionHandlers(event.event_id, context);
  setCapabilityHint(context);
  void fetchMailCapabilities(event.event_id, context);
}

function setupTextSelection(eventId) {
  const e = getEls();
  clearTextInteractionHandlers();

  const clearDraftSelection = () => {
    if (draftMark && draftMark.event_id === eventId) {
      draftMark = null;
      clearOverlay();
      const state = window._tabulaApp?.getState?.();
      sendSelectionFeedback({
        kind: 'mark_clear_draft',
        session_id: state?.sessionId || '',
        artifact_id: eventId,
      });
    }
  };

  const handleSelection = () => {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !isSelectionInside(e.text, selection)) {
      clearDraftSelection();
      return;
    }
    const text = selection.toString();
    if (!text) {
      clearDraftSelection();
      return;
    }

    const range = selection.getRangeAt(0);
    const fullText = e.text.textContent || '';
    const lines = fullText.split('\n');

    const { startOffset, endOffset } = getSelectionOffsets(e.text, range);
    const lineStart = lineFromOffset(lines, startOffset);
    const lineEnd = lineFromOffset(lines, endOffset);

    const markType = getSelectedMarkType();
    const rects = computeRectsFromRange(e.text, range);
    const state = window._tabulaApp?.getState?.();
    draftMark = {
      event_id: eventId,
      type: markType,
      line_start: lineStart,
      line_end: lineEnd,
      start_offset: startOffset,
      end_offset: endOffset,
      text,
      comment: getMarkComment(),
      rects,
    };
    renderDraftOverlay();

    sendSelectionFeedback({
      kind: 'text_selection',
      session_id: state?.sessionId || '',
      artifact_id: eventId,
      event_id: eventId,
      line_start: lineStart,
      line_end: lineEnd,
      start_offset: startOffset,
      end_offset: endOffset,
      text,
      rects,
      mark_type: markType,
      comment: getMarkComment(),
    });
  };

  const handler = () => {
    if (e.text._selectionRaf) {
      cancelAnimationFrame(e.text._selectionRaf);
    }
    e.text._selectionRaf = requestAnimationFrame(() => {
      e.text._selectionRaf = null;
      handleSelection();
    });
  };

  document.addEventListener('selectionchange', handler);
  e.text._selectionHandler = handler;
  e.text._mouseUpSelectionHandler = handler;
  e.text._keyUpSelectionHandler = handler;
  e.text.addEventListener('mouseup', handler);
  e.text.addEventListener('keyup', handler);

  if (e.text._scrollHandler) {
    e.text.removeEventListener('scroll', e.text._scrollHandler);
  }
  e.text._scrollHandler = () => renderDraftOverlay();
  e.text.addEventListener('scroll', e.text._scrollHandler);
}

function setupPdfOverlay() {
  const e = getEls();
  if (e.pdf._pdfClickHandler) {
    e.pdf.removeEventListener('click', e.pdf._pdfClickHandler);
  }
  const clickHandler = (ev) => {
    if (!activePdfEvent) return;
    const markType = getSelectedMarkType();
    if (markType !== 'comment_point') return;

    const rect = e.pdf.getBoundingClientRect();
    const x = ev.clientX - rect.left;
    const y = ev.clientY - rect.top;
    const page = Number(activePdfEvent.page || 0);
    const comment = getMarkComment();

    sendSelectionFeedback({
      kind: 'mark_set',
      session_id: (window._tabulaApp?.getState?.().sessionId) || '',
      artifact_id: activePdfEvent.event_id,
      intent: 'draft',
      type: 'comment_point',
      target_kind: 'pdf_point',
      target: { page, x, y, rect: [x - 8, y - 8, x + 8, y + 8] },
      comment,
    });

    const marker = document.createElement('div');
    marker.className = 'canvas-mark-rect canvas-mark-comment_point';
    marker.style.left = `${x - 5}px`;
    marker.style.top = `${y - 5}px`;
    marker.style.width = '10px';
    marker.style.height = '10px';
    marker.style.position = 'absolute';
    marker.style.pointerEvents = 'none';
    if (window.getComputedStyle(e.pdf).position === 'static') {
      e.pdf.style.position = 'relative';
    }
    e.pdf.appendChild(marker);
  };
  e.pdf._pdfClickHandler = clickHandler;
  e.pdf.addEventListener('click', clickHandler);
}

export function renderCanvas(event) {
  const e = getEls();

  if (event.kind === 'text_artifact') {
    hideAll();
    e.text.style.display = '';
    clearTextInteractionHandlers();
    e.title.textContent = event.title || 'Text';
    e.mode.textContent = 'review';
    e.mode.className = 'badge review';
    activeTextEventId = event.event_id;
    activePdfEvent = null;
    clearOverlay();
    const mailContext = normalizeMailHeadersContext(event);
    if (mailContext) {
      activeMailContext = mailContext;
      renderMailArtifact(event, mailContext);
      return;
    }
    activeMailContext = null;
    e.text.innerHTML = sanitizeHtml(marked.parse(event.text || ''));
    setupTextSelection(event.event_id);
  } else if (event.kind === 'image_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.image.style.display = '';
    const state = (window._tabulaApp || {}).getState ? window._tabulaApp.getState() : {};
    const sid = state.sessionId || '';
    e.img.src = `/api/files/${encodeURIComponent(sid)}/${encodeURIComponent(event.path)}`;
    e.img.alt = event.title || 'Image';
    e.title.textContent = event.title || 'Image';
    e.mode.textContent = 'review';
    e.mode.className = 'badge review';
    activeTextEventId = null;
    activePdfEvent = null;
    draftMark = null;
    clearOverlay();
  } else if (event.kind === 'pdf_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.pdf.style.display = '';
    const pdfState = (window._tabulaApp || {}).getState ? window._tabulaApp.getState() : {};
    const pdfSid = pdfState.sessionId || '';
    e.pdf.innerHTML = '';
    const iframe = document.createElement('iframe');
    iframe.src = `/api/files/${encodeURIComponent(pdfSid)}/${encodeURIComponent(event.path)}`;
    iframe.style.cssText = 'width:100%;height:100%;border:none;';
    e.pdf.appendChild(iframe);
    e.title.textContent = event.title || 'PDF';
    e.mode.textContent = 'review';
    e.mode.className = 'badge review';
    activeTextEventId = null;
    activePdfEvent = event;
    draftMark = null;
    clearOverlay();
    setupPdfOverlay();
  } else if (event.kind === 'clear_canvas') {
    clearTextInteractionHandlers();
    clearCanvas();
  }
}

export function clearCanvas() {
  const e = getEls();
  clearTextInteractionHandlers();
  hideAll();
  e.empty.style.display = '';
  e.title.textContent = 'Canvas';
  e.mode.textContent = 'prompt';
  e.mode.className = 'badge';
  activeTextEventId = null;
  activePdfEvent = null;
  draftMark = null;
  clearOverlay();
}

export function initCanvasControls() {
  const e = getEls();
  const commitBtn = document.getElementById('btn-canvas-commit');
  const clearBtn = document.getElementById('btn-canvas-clear-draft');

  if (commitBtn) {
    commitBtn.addEventListener('click', () => {
      const { getState } = window._tabulaApp || {};
      if (!getState) return;
      const state = getState();
      if (!state.canvasWs || state.canvasWs.readyState !== WebSocket.OPEN) return;
      state.canvasWs.send(JSON.stringify({
        kind: 'mark_commit',
        session_id: state.sessionId,
        include_draft: true,
      }));
    });
  }

  if (clearBtn) {
    clearBtn.addEventListener('click', () => {
      const { getState } = window._tabulaApp || {};
      if (!getState) return;
      const state = getState();
      if (!state.canvasWs || state.canvasWs.readyState !== WebSocket.OPEN) return;
      state.canvasWs.send(JSON.stringify({
        kind: 'mark_clear_draft',
        session_id: state.sessionId,
        artifact_id: activeTextEventId,
      }));
      draftMark = null;
      clearOverlay();
    });
  }

  if (e.markType) {
    e.markType.addEventListener('change', () => {
      if (draftMark) {
        draftMark.type = getSelectedMarkType();
        renderDraftOverlay();
      }
    });
  }
}
