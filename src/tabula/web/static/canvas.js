import { marked } from './vendor/marked.esm.js';

marked.setOptions({ breaks: true });

const els = {};
let activeTextEventId = null;
let activePdfEvent = null;
let draftMark = null;

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

function getTextOffset(root, node, offset) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let total = 0;
  while (walker.nextNode()) {
    if (walker.currentNode === node) return total + offset;
    total += walker.currentNode.textContent.length;
  }
  return total + offset;
}

function sendSelectionFeedback(payload) {
  const { getState } = window._tabulaApp || {};
  if (!getState) return;
  const state = getState();
  if (!state.canvasWs || state.canvasWs.readyState !== WebSocket.OPEN) return;
  state.canvasWs.send(JSON.stringify(payload));
}

function setupTextSelection(eventId) {
  const e = getEls();
  if (e.text._selectionHandler) {
    document.removeEventListener('selectionchange', e.text._selectionHandler);
  }

  const handler = () => {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !isSelectionInside(e.text, selection)) {
      return;
    }
    const text = selection.toString();
    if (!text) {
      return;
    }

    const range = selection.getRangeAt(0);
    const fullText = e.text.textContent || '';
    const lines = fullText.split('\n');

    let charCount = 0;
    let lineStart = 1;
    let lineEnd = 1;
    const startOffset = getTextOffset(e.text, range.startContainer, range.startOffset);
    const endOffset = getTextOffset(e.text, range.endContainer, range.endOffset);

    for (let i = 0; i < lines.length; i++) {
      if (charCount + lines[i].length >= startOffset) {
        lineStart = i + 1;
        break;
      }
      charCount += lines[i].length + 1;
    }
    charCount = 0;
    for (let i = 0; i < lines.length; i++) {
      if (charCount + lines[i].length >= endOffset) {
        lineEnd = i + 1;
        break;
      }
      charCount += lines[i].length + 1;
    }

    const markType = getSelectedMarkType();
    const rects = computeRectsFromRange(e.text, range);
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

  document.addEventListener('selectionchange', handler);
  e.text._selectionHandler = handler;

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
    e.text.innerHTML = sanitizeHtml(marked.parse(event.text || ''));
    e.title.textContent = event.title || 'Text';
    e.mode.textContent = 'review';
    e.mode.className = 'badge review';
    activeTextEventId = event.event_id;
    activePdfEvent = null;
    clearOverlay();
    setupTextSelection(event.event_id);
  } else if (event.kind === 'image_artifact') {
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
    clearCanvas();
  }
}

export function clearCanvas() {
  const e = getEls();
  if (e.text._selectionHandler) {
    document.removeEventListener('selectionchange', e.text._selectionHandler);
    e.text._selectionHandler = null;
  }
  if (e.text._scrollHandler) {
    e.text.removeEventListener('scroll', e.text._scrollHandler);
    e.text._scrollHandler = null;
  }
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
