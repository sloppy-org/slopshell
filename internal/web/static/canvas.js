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
let submittedDraftMarks = [];
let submittedDraftMarkSeq = 0;
let activeMailContext = null;
let mailCapabilitiesRequestSeq = 0;

const DEFAULT_PRODUCER_MCP_URL = 'http://127.0.0.1:8090/mcp';
const SWIPE_LEFT_ARCHIVE_THRESHOLD_PX = -120;
const SWIPE_LEFT_DELETE_THRESHOLD_PX = -260;
const SWIPE_RIGHT_DEFER_THRESHOLD_PX = 120;
const SWIPE_MAX_TRANSLATE_PX = 320;
const DETAIL_SWIPE_NAV_THRESHOLD_PX = 90;
const UNDO_TIMEOUT_MS = Number(window.__TABULA_UNDO_TIMEOUT_MS || 5000);

let pendingUndoAction = null;
const MAIL_ASSIST_STATE = Object.freeze({
  IDLE: 'idle',
  CAPTURING: 'capturing',
  GENERATING: 'generating',
  READY: 'ready',
  ERROR: 'error',
});
const MAIL_RECORDING_MODE = Object.freeze({
  HOLD: 'hold',
  TOGGLE: 'toggle',
});
const MAIL_RECORDING_STATE = Object.freeze({
  IDLE: 'idle',
  RECORDING: 'recording',
});
const MAIL_RECORDING_ORIGIN = Object.freeze({
  HOLD_POINTER: 'hold_pointer',
  HOLD_KEYBOARD: 'hold_keyboard',
  TOGGLE_BUTTON: 'toggle_button',
});
const MAIL_DRAFT_INTENT = Object.freeze({
  PROMPT: 'prompt',
  DICTATION: 'dictation',
});
const MAIL_DRAFT_INTENT_FALLBACK_POLICY = MAIL_DRAFT_INTENT.PROMPT;
const REVIEW_LONG_PRESS_MS = 450;
const REVIEW_LONG_PRESS_MOVE_TOLERANCE_PX = 10;
const sttActionStart = 'start';
const sttActionAppend = 'append';
const sttActionStop = 'stop';
const sttActionCancel = 'cancel';
const mailAssistActionRegistry = new Map();
const DRAFT_PROMPT_CANCELLED_CODE = 'draft_prompt_cancelled';
let pendingDraftPromptCapture = null;
const POINT_COMMENT_MARK_SIZE_PX = 16;
const MATH_SEGMENT_TOKEN_PREFIX = '@@TABULA_MATH_SEGMENT_';

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

function extractMathSegments(markdownSource) {
  const source = String(markdownSource || '');
  const stash = [];
  let text = source;

  const normalizeMathSegment = (segment) => {
    const raw = String(segment || '');
    const trimmed = raw.trim();
    if (!trimmed.startsWith('$$') || !trimmed.endsWith('$$')) {
      return raw;
    }
    const inner = trimmed.slice(2, -2).trim();
    if (!inner) return raw;
    const hasTagOrLabel = /\\(?:tag|label)\{[^}]+\}/.test(inner);
    const hasDisplayEnv = /\\begin\{(?:equation|equation\*|align|align\*|aligned|gather|gather\*|multline|multline\*|split|eqnarray)\}/.test(inner);
    if (!hasTagOrLabel || hasDisplayEnv) {
      return raw;
    }
    return `\\begin{equation}\n${inner}\n\\end{equation}`;
  };

  const patterns = [
    /\$\$[\s\S]+?\$\$/g,
    /\\\[[\s\S]+?\\\]/g,
    /\\\([\s\S]+?\\\)/g,
  ];

  for (const pattern of patterns) {
    text = text.replace(pattern, (segment) => {
      const token = `${MATH_SEGMENT_TOKEN_PREFIX}${stash.length}@@`;
      stash.push(normalizeMathSegment(segment));
      return token;
    });
  }

  return { text, stash };
}

function restoreMathSegments(renderedHtml, mathSegments) {
  let output = String(renderedHtml || '');
  if (!Array.isArray(mathSegments) || mathSegments.length === 0) {
    return output;
  }
  for (let i = 0; i < mathSegments.length; i += 1) {
    const token = `${MATH_SEGMENT_TOKEN_PREFIX}${i}@@`;
    const safeSegment = escapeHtml(String(mathSegments[i] || ''));
    output = output.replaceAll(token, safeSegment);
  }
  return output;
}

function typesetMarkdownMath(root, attempt = 0) {
  if (!(root instanceof Element) || !root.isConnected) return;
  const mj = window.MathJax;
  if (!mj || typeof mj.typesetPromise !== 'function') {
    if (attempt >= 40) return;
    window.setTimeout(() => typesetMarkdownMath(root, attempt + 1), 75);
    return;
  }
  const startupReady = mj.startup && mj.startup.promise && typeof mj.startup.promise.then === 'function'
    ? mj.startup.promise
    : Promise.resolve();
  const originalMathText = root.textContent || '';
  const needsRefPass = /\\(?:eq)?ref\{[^}]+\}/.test(originalMathText) || /\\label\{[^}]+\}/.test(originalMathText);
  void startupReady
    .then(() => {
      if (!root.isConnected) return;
      if (typeof mj.texReset === 'function') {
        // Reset equation numbers/labels so each artifact is independent.
        mj.texReset();
      }
      if (typeof mj.typesetClear === 'function') {
        mj.typesetClear([root]);
      }
      return mj.typesetPromise([root]).then(() => {
        if (!needsRefPass || !root.isConnected) return;
        return mj.typesetPromise([root]);
      });
    })
    .catch((err) => {
      console.warn('MathJax typeset failed:', err);
    });
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

function cloneMarkForOverlay(mark) {
  if (!mark || typeof mark !== 'object') return null;
  const rects = Array.isArray(mark.rects)
    ? mark.rects
      .filter((rect) => Array.isArray(rect) && rect.length === 4)
      .map((rect) => rect.map((v) => Number(v)))
    : [];
  if (!rects.length) return null;
  return {
    local_id: String(mark.local_id || '').trim(),
    event_id: mark.event_id,
    type: mark.type || 'highlight',
    line_start: Number(mark.line_start || 1),
    line_end: Number(mark.line_end || 1),
    start_offset: Number(mark.start_offset || 0),
    end_offset: Number(mark.end_offset || 0),
    text: String(mark.text || ''),
    comment: String(mark.comment || ''),
    rects,
  };
}

function nextSubmittedDraftMarkID() {
  submittedDraftMarkSeq += 1;
  return `draft-mark-${submittedDraftMarkSeq}`;
}

function addSubmittedDraftMark(mark) {
  const normalized = cloneMarkForOverlay(mark);
  if (!normalized || !normalized.event_id) return;
  if (!normalized.local_id) {
    normalized.local_id = nextSubmittedDraftMarkID();
  }
  const idx = submittedDraftMarks.findIndex((existing) => (
    existing.event_id === normalized.event_id && existing.local_id === normalized.local_id
  ));
  if (idx >= 0) {
    submittedDraftMarks[idx] = normalized;
    return;
  }
  submittedDraftMarks.push(normalized);
}

function removeSubmittedDraftMark(eventId, localMarkID) {
  if (!eventId || !localMarkID) return null;
  const idx = submittedDraftMarks.findIndex((mark) => (
    mark.event_id === eventId && mark.local_id === localMarkID
  ));
  if (idx < 0) return null;
  const [removed] = submittedDraftMarks.splice(idx, 1);
  return removed || null;
}

function clearSubmittedDraftMarksForEvent(eventId) {
  if (!eventId) {
    submittedDraftMarks = [];
    return;
  }
  submittedDraftMarks = submittedDraftMarks.filter((mark) => mark.event_id !== eventId);
}

function markArea(mark) {
  if (!mark || !Array.isArray(mark.rects)) return Number.POSITIVE_INFINITY;
  let total = 0;
  for (const rect of mark.rects) {
    if (!Array.isArray(rect) || rect.length !== 4) continue;
    const width = Math.max(1, Number(rect[2]) || 0);
    const height = Math.max(1, Number(rect[3]) || 0);
    total += width * height;
  }
  return total || Number.POSITIVE_INFINITY;
}

function findSubmittedMarkAtPoint(root, eventId, clientX, clientY) {
  if (!root || !eventId) return null;
  const rootRect = root.getBoundingClientRect();
  const x = clientX - rootRect.left + root.scrollLeft;
  const y = clientY - rootRect.top + root.scrollTop;
  const candidates = [];
  for (const mark of submittedDraftMarks) {
    if (!mark || mark.event_id !== eventId || !Array.isArray(mark.rects)) continue;
    for (const rect of mark.rects) {
      if (!Array.isArray(rect) || rect.length !== 4) continue;
      const rx = Number(rect[0]) || 0;
      const ry = Number(rect[1]) || 0;
      const rw = Math.max(1, Number(rect[2]) || 0);
      const rh = Math.max(1, Number(rect[3]) || 0);
      if (x >= rx && x <= rx + rw && y >= ry && y <= ry + rh) {
        candidates.push(mark);
        break;
      }
    }
  }
  if (!candidates.length) return null;
  candidates.sort((a, b) => {
    const aPoint = a.type === 'comment_point';
    const bPoint = b.type === 'comment_point';
    if (aPoint && !bPoint) return -1;
    if (!aPoint && bPoint) return 1;
    return markArea(a) - markArea(b);
  });
  return candidates[0];
}

function targetFromExistingMark(mark) {
  if (!mark || !Array.isArray(mark.rects) || !mark.rects.length) return null;
  const anchor = mark.rects[mark.rects.length - 1];
  const pointX = Number(anchor?.[0] || 0) + Math.max(8, Number(anchor?.[2] || 0) / 2);
  const pointY = Number(anchor?.[1] || 0) + Math.max(12, Number(anchor?.[3] || 0)) + 8;
  return {
    lineStart: Number(mark.line_start || 1),
    lineEnd: Number(mark.line_end || 1),
    startOffset: Number(mark.start_offset || 0),
    endOffset: Number(mark.end_offset || 0),
    rects: mark.rects,
    pointX,
    pointY,
    markType: mark.type || 'highlight',
    localMarkID: mark.local_id || '',
  };
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
  if (!activeTextEventId) {
    return;
  }

  const marks = [];
  for (const mark of submittedDraftMarks) {
    if (mark && mark.event_id === activeTextEventId) {
      marks.push({ mark, submitted: true });
    }
  }
  if (draftMark && draftMark.event_id === activeTextEventId) {
    marks.push({ mark: draftMark, submitted: false });
  }
  if (!marks.length) {
    return;
  }

  const overlay = ensureTextOverlay();
  for (const entry of marks) {
    const { mark, submitted } = entry;
    const markType = mark.type || 'highlight';
    for (const rect of mark.rects || []) {
      if (!Array.isArray(rect) || rect.length !== 4) continue;
      const el = document.createElement('div');
      el.className = `canvas-mark-rect canvas-mark-${markType}`;
      el.style.left = `${rect[0]}px`;
      el.style.top = `${rect[1]}px`;
      el.style.width = `${Math.max(1, rect[2])}px`;
      el.style.height = `${Math.max(2, rect[3])}px`;
      if (submitted) {
        el.dataset.localMarkId = mark.local_id || '';
        if (mark.comment) {
          el.title = mark.comment;
        }
      }
      overlay.appendChild(el);
    }
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

function textRangeFromClientPoint(clientX, clientY) {
  if (typeof document.caretRangeFromPoint === 'function') {
    return document.caretRangeFromPoint(clientX, clientY);
  }
  if (typeof document.caretPositionFromPoint === 'function') {
    const caret = document.caretPositionFromPoint(clientX, clientY);
    if (!caret) return null;
    const range = document.createRange();
    range.setStart(caret.offsetNode, caret.offset);
    range.collapse(true);
    return range;
  }
  return null;
}

function pointTargetFromClientPoint(root, clientX, clientY) {
  const rootRect = root.getBoundingClientRect();
  const pointX = clientX - rootRect.left + root.scrollLeft;
  const pointY = clientY - rootRect.top + root.scrollTop;
  const half = Math.round(POINT_COMMENT_MARK_SIZE_PX / 2);
  const fallback = {
    lineStart: 1,
    lineEnd: 1,
    startOffset: 0,
    endOffset: 0,
    rects: [[pointX - half, pointY - half, POINT_COMMENT_MARK_SIZE_PX, POINT_COMMENT_MARK_SIZE_PX]],
    pointX,
    pointY,
  };

  const range = textRangeFromClientPoint(clientX, clientY);
  if (!range || !root.contains(range.startContainer)) {
    return fallback;
  }

  try {
    const startProbe = range.cloneRange();
    startProbe.selectNodeContents(root);
    startProbe.setEnd(range.startContainer, range.startOffset);
    const offset = startProbe.toString().length;
    const lines = (root.textContent || '').split('\n');
    const line = lineFromOffset(lines, offset);
    return {
      lineStart: line,
      lineEnd: line,
      startOffset: offset,
      endOffset: offset,
      rects: fallback.rects,
      pointX,
      pointY,
    };
  } catch (_) {
    return fallback;
  }
}

function submitPointDraftMark(eventId, target, comment, localMarkID = '') {
  const markID = String(localMarkID || '').trim() || nextSubmittedDraftMarkID();
  const normalizedComment = String(comment || '').trim();
  const state = window._tabulaApp?.getState?.();
  sendSelectionFeedback({
    kind: 'mark_set',
    session_id: state?.sessionId || '',
    mark_id: markID,
    artifact_id: eventId,
    intent: 'draft',
    type: 'comment_point',
    target_kind: 'text_range',
    target: {
      line_start: Number(target?.lineStart || 1),
      line_end: Number(target?.lineEnd || 1),
      start_offset: Number(target?.startOffset || 0),
      end_offset: Number(target?.endOffset || 0),
      rects: Array.isArray(target?.rects) ? target.rects : [],
    },
    comment: normalizedComment,
  });
  addSubmittedDraftMark({
    local_id: markID,
    event_id: eventId,
    type: 'comment_point',
    line_start: Number(target?.lineStart || 1),
    line_end: Number(target?.lineEnd || 1),
    start_offset: Number(target?.startOffset || 0),
    end_offset: Number(target?.endOffset || 0),
    comment: normalizedComment,
    rects: Array.isArray(target?.rects) ? target.rects : [],
  });
  renderDraftOverlay();
}

function showReviewVoiceHint(text, tone = 'info') {
  const e = getEls();
  if (!e.text) return;
  let hint = e.text.querySelector('[data-review-voice-hint]');
  if (!hint) {
    hint = document.createElement('div');
    hint.className = 'canvas-review-voice-hint';
    hint.dataset.reviewVoiceHint = '1';
    e.text.appendChild(hint);
  }
  hint.dataset.tone = tone;
  hint.textContent = String(text || '').trim();
}

function clearReviewVoiceHint() {
  const e = getEls();
  if (!e.text) return;
  const hint = e.text.querySelector('[data-review-voice-hint]');
  if (hint && hint.parentNode) {
    hint.parentNode.removeChild(hint);
  }
}

function newMediaRecorder(stream) {
  let recorder = null;
  try {
    const preferredType = 'audio/webm;codecs=opus';
    if (typeof window.MediaRecorder?.isTypeSupported === 'function'
      && window.MediaRecorder.isTypeSupported(preferredType)) {
      recorder = new window.MediaRecorder(stream, { mimeType: preferredType });
    } else {
      recorder = new window.MediaRecorder(stream);
    }
  } catch (_) {
    recorder = new window.MediaRecorder(stream);
  }
  return recorder;
}

function closeReviewCommentPopover() {
  const e = getEls();
  if (!e.text) return;
  const restoreFocusEl = e.text._reviewPopoverPreviousFocusEl;
  if (e.text._reviewPopoverOutsideHandler) {
    document.removeEventListener('pointerdown', e.text._reviewPopoverOutsideHandler, true);
    e.text._reviewPopoverOutsideHandler = null;
  }
  if (e.text._reviewPopoverKeyDownHandler) {
    document.removeEventListener('keydown', e.text._reviewPopoverKeyDownHandler, true);
    e.text._reviewPopoverKeyDownHandler = null;
  }
  if (e.text._reviewPopoverEl && e.text._reviewPopoverEl.parentNode) {
    e.text._reviewPopoverEl.parentNode.removeChild(e.text._reviewPopoverEl);
  }
  e.text._reviewPopoverEl = null;
  e.text._reviewPopoverSource = null;
  e.text._reviewPopoverPreviousFocusEl = null;
  if (!restoreFocusEl && !e.text.hasAttribute('tabindex')) {
    e.text.setAttribute('tabindex', '-1');
  }
  const focusTarget = restoreFocusEl || e.text;
  if (focusTarget && document.contains(focusTarget) && typeof focusTarget.focus === 'function') {
    try {
      focusTarget.focus({ preventScroll: true });
    } catch (_) {
      focusTarget.focus();
    }
  }
}

function shouldSuppressNextReviewOpen(root) {
  if (!root || !root._suppressNextReviewOpen) return false;
  root._suppressNextReviewOpen = false;
  return true;
}

function positionReviewCommentPopover(popover, root, x, y) {
  const pad = 10;
  const width = popover.offsetWidth || 260;
  const height = popover.offsetHeight || 120;
  const minX = root.scrollLeft + pad;
  const minY = root.scrollTop + pad;
  const maxX = root.scrollLeft + Math.max(pad, root.clientWidth - width - pad);
  const maxY = root.scrollTop + Math.max(pad, root.clientHeight - height - pad);
  const left = Math.min(Math.max(minX, x), maxX);
  const top = Math.min(Math.max(minY, y), maxY);
  popover.style.left = `${left}px`;
  popover.style.top = `${top}px`;
}

function selectionTargetFromDraftMark(eventId) {
  if (!draftMark || draftMark.event_id !== eventId) return null;
  const rects = Array.isArray(draftMark.rects) ? draftMark.rects : [];
  let pointX = 12;
  let pointY = 12;
  if (rects.length > 0 && Array.isArray(rects[rects.length - 1])) {
    const anchor = rects[rects.length - 1];
    pointX = Number(anchor[0]) + Math.max(8, Number(anchor[2] || 0) / 2);
    pointY = Number(anchor[1]) + Math.max(12, Number(anchor[3] || 0)) + 8;
  }
  return {
    lineStart: Number(draftMark.line_start || 1),
    lineEnd: Number(draftMark.line_end || 1),
    startOffset: Number(draftMark.start_offset || 0),
    endOffset: Number(draftMark.end_offset || 0),
    rects,
    pointX,
    pointY,
    markType: draftMark.type || 'highlight',
  };
}

function openReviewCommentPopover(eventId, options = {}) {
  const e = getEls();
  if (!e.text) return;
  const activeEl = document.activeElement;
  const previousFocusEl = activeEl instanceof HTMLElement && activeEl !== document.body ? activeEl : null;
  let target = null;
  if (options.source === 'selection') {
    target = selectionTargetFromDraftMark(eventId);
    if (!target) return;
  } else if (options.source === 'existing') {
    target = targetFromExistingMark(options.existingMark);
    if (!target) return;
  } else {
    const pointEvent = options.contextmenuEvent;
    if (!pointEvent) return;
    target = pointTargetFromClientPoint(e.text, pointEvent.clientX, pointEvent.clientY);
  }
  closeReviewCommentPopover();

  e.text._reviewPopoverPreviousFocusEl = previousFocusEl;
  const popover = document.createElement('form');
  popover.className = 'canvas-review-popover';
  popover.dataset.reviewPopover = 'true';
  popover.setAttribute('role', 'dialog');
  const isExistingMark = options.source === 'existing';
  popover.setAttribute('aria-label', isExistingMark ? 'View or edit comment' : 'Add comment');
  const inputId = `review-comment-input-${Math.random().toString(36).slice(2, 8)}`;
  const submitLabel = isExistingMark ? 'Update Comment' : 'Add Comment';
  const initialComment = isExistingMark
    ? String(options.existingMark?.comment || '')
    : String(options.initialComment || '');
  popover.innerHTML = `
    <label class="sr-only" for="${inputId}">Comment</label>
    <input id="${inputId}" type="text" maxlength="500" placeholder="Add comment (optional)">
    <div class="canvas-review-popover-actions">
      <button type="submit">${submitLabel}</button>
      <button type="button" data-review-cancel>Cancel</button>
    </div>
  `;
  e.text.appendChild(popover);
  const commentInput = popover.querySelector(`#${CSS.escape(inputId)}`);
  if (commentInput) {
    commentInput.value = initialComment;
  }
  positionReviewCommentPopover(popover, e.text, target.pointX, target.pointY);
  requestAnimationFrame(() => {
    positionReviewCommentPopover(popover, e.text, target.pointX, target.pointY);
    const input = popover.querySelector(`#${CSS.escape(inputId)}`);
    if (input && typeof input.focus === 'function') {
      try {
        input.focus({ preventScroll: true });
      } catch (_) {
        input.focus();
      }
    }
  });

  const cancelBtn = popover.querySelector('[data-review-cancel]');
  if (cancelBtn) {
    cancelBtn.addEventListener('click', (ev) => {
      ev.preventDefault();
      closeReviewCommentPopover();
      if (typeof options.onCancel === 'function') {
        options.onCancel();
      }
    });
  }

  popover.addEventListener('pointerdown', (ev) => {
    if (ev.target !== popover) return;
    ev.preventDefault();
    if (e.text) {
      e.text._suppressNextReviewOpen = true;
    }
    closeReviewCommentPopover();
    if (typeof options.onCancel === 'function') {
      options.onCancel();
    }
  });

  popover.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const input = popover.querySelector(`#${CSS.escape(inputId)}`);
    const comment = String(input?.value || '').trim();
    const state = window._tabulaApp?.getState?.();
    let outgoingMarkID = '';
    if (options.source === 'selection' && draftMark && draftMark.event_id === eventId) {
      if (!draftMark.local_id) {
        draftMark.local_id = nextSubmittedDraftMarkID();
      }
      outgoingMarkID = draftMark.local_id;
    } else if (options.source === 'existing') {
      outgoingMarkID = String(target.localMarkID || options.existingMark?.local_id || '').trim();
    } else {
      outgoingMarkID = nextSubmittedDraftMarkID();
    }
    const outgoingMarkType = options.source === 'selection'
      ? (target.markType || 'highlight')
      : options.source === 'existing'
        ? (target.markType || 'highlight')
        : 'comment_point';
    sendSelectionFeedback({
      kind: 'mark_set',
      session_id: state?.sessionId || '',
      mark_id: outgoingMarkID,
      artifact_id: eventId,
      intent: 'draft',
      type: outgoingMarkType,
      target_kind: 'text_range',
      target: {
        line_start: target.lineStart,
        line_end: target.lineEnd,
        start_offset: target.startOffset,
        end_offset: target.endOffset,
        rects: target.rects,
      },
      comment,
    });
    if (options.source === 'selection' && draftMark && draftMark.event_id === eventId) {
      draftMark.comment = comment;
      draftMark.local_id = outgoingMarkID;
      addSubmittedDraftMark(draftMark);
      draftMark = null;
      renderDraftOverlay();
    } else if (options.source === 'existing' && options.existingMark) {
      options.existingMark.comment = comment;
      addSubmittedDraftMark({
        ...options.existingMark,
        local_id: outgoingMarkID || target.localMarkID || options.existingMark.local_id || '',
        type: outgoingMarkType,
        line_start: target.lineStart,
        line_end: target.lineEnd,
        start_offset: target.startOffset,
        end_offset: target.endOffset,
        rects: target.rects,
        comment,
      });
      renderDraftOverlay();
    } else if (options.source !== 'selection') {
      addSubmittedDraftMark({
        local_id: outgoingMarkID,
        event_id: eventId,
        type: 'comment_point',
        line_start: target.lineStart,
        line_end: target.lineEnd,
        start_offset: target.startOffset,
        end_offset: target.endOffset,
        comment,
        rects: target.rects,
      });
      renderDraftOverlay();
    }
    closeReviewCommentPopover();
  });

  const outsideHandler = (ev) => {
    if (!popover.contains(ev.target)) {
      if (e.text) {
        const shouldSuppress = activeTextEventId === eventId
          && !!findSubmittedMarkAtPoint(e.text, eventId, ev.clientX, ev.clientY);
        e.text._suppressNextReviewOpen = shouldSuppress;
      }
      closeReviewCommentPopover();
      if (typeof options.onCancel === 'function') {
        options.onCancel();
      }
    }
  };
  document.addEventListener('pointerdown', outsideHandler, true);
  e.text._reviewPopoverOutsideHandler = outsideHandler;

  const keyDownHandler = (ev) => {
    if (ev.key === 'Escape') {
      ev.preventDefault();
      closeReviewCommentPopover();
      if (typeof options.onCancel === 'function') {
        options.onCancel();
      }
      return;
    }
    const isCommitShortcut = ev.key === 'Enter' && !ev.shiftKey && !ev.altKey;
    if (!isCommitShortcut) return;
    const target = ev.target instanceof Element ? ev.target : null;
    if (!target || !popover.contains(target)) return;
    const inCommentInput = target.matches('input, textarea') || target.closest('input, textarea');
    if (!inCommentInput) return;
    ev.preventDefault();
    if (typeof popover.requestSubmit === 'function') {
      popover.requestSubmit();
    } else {
      popover.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    }
    window.setTimeout(() => {
      const commitBtn = document.getElementById('btn-canvas-commit');
      if (commitBtn) {
        commitBtn.click();
      }
    }, 0);
  };
  document.addEventListener('keydown', keyDownHandler, true);
  e.text._reviewPopoverKeyDownHandler = keyDownHandler;
  e.text._reviewPopoverEl = popover;
  e.text._reviewPopoverSource = options.source || 'point';
}

function sendSelectionFeedback(payload) {
  const { getState } = window._tabulaApp || {};
  if (!getState) return;
  const state = getState();
  if (!state.canvasWs || state.canvasWs.readyState !== WebSocket.OPEN) return;
  state.canvasWs.send(JSON.stringify(payload));
}

function activeArtifactIDForCommit() {
  if (activeTextEventId) return activeTextEventId;
  return String(activePdfEvent?.event_id || '');
}

function waitForCanvasWsOpen(ws, timeoutMs = 450) {
  if (!ws || ws.readyState !== WebSocket.CONNECTING) {
    return Promise.resolve(false);
  }
  return new Promise((resolve) => {
    const started = Date.now();
    const timer = setInterval(() => {
      if (!ws || ws.readyState === WebSocket.OPEN) {
        clearInterval(timer);
        resolve(true);
        return;
      }
      if (ws.readyState !== WebSocket.CONNECTING || (Date.now() - started) >= timeoutMs) {
        clearInterval(timer);
        resolve(false);
      }
    }, 50);
  });
}

async function commitCanvasDraft() {
  const { getState } = window._tabulaApp || {};
  if (!getState) throw new Error('app state unavailable');
  const state = getState();
  const sessionID = String(state?.sessionId || '').trim();
  if (!sessionID) throw new Error('no active session');
  const artifactID = activeArtifactIDForCommit();
  const payload = {
    kind: 'mark_commit',
    session_id: sessionID,
    include_draft: true,
  };
  if (artifactID) {
    payload.artifact_id = artifactID;
  }

  const ws = state.canvasWs;
  const commitHTTP = async () => {
    const response = await fetch(`/api/canvas/${encodeURIComponent(sessionID)}/commit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        artifact_id: artifactID || '',
        include_draft: true,
      }),
    });
    if (!response.ok) {
      const detail = (await response.text()).trim();
      throw new Error(detail || `commit failed: HTTP ${response.status}`);
    }
    return response.json().catch(() => ({}));
  };

  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(payload));
    try {
      await commitHTTP();
      return { transport: 'ws+http' };
    } catch (err) {
      // Keep compatibility with harness/local WS-only flows where HTTP endpoint is unavailable.
      console.warn('commit HTTP follow-up failed after WS mark_commit:', err);
      return { transport: 'ws' };
    }
  }
  if (await waitForCanvasWsOpen(ws)) {
    ws.send(JSON.stringify(payload));
    try {
      await commitHTTP();
      return { transport: 'ws+http' };
    } catch (err) {
      console.warn('commit HTTP follow-up failed after delayed WS mark_commit:', err);
      return { transport: 'ws' };
    }
  }

  await commitHTTP();
  return { transport: 'http' };
}

function ensureUndoToast() {
  const host = document.getElementById('canvas-content');
  if (!host) return null;
  let toast = host.querySelector('.mail-undo-toast');
  if (!toast) {
    toast = document.createElement('div');
    toast.className = 'mail-undo-toast';
    toast.innerHTML = '<span data-mail-undo-message></span><button type="button" data-mail-undo-btn>Undo</button>';
    host.appendChild(toast);
  }
  return toast;
}

function hideUndoToast() {
  const toast = document.querySelector('.mail-undo-toast');
  if (!toast) return;
  toast.classList.remove('show');
  const btn = toast.querySelector('[data-mail-undo-btn]');
  if (btn) {
    btn.onclick = null;
  }
}

function showUndoToast(message, onUndo) {
  const toast = ensureUndoToast();
  if (!toast) return;
  const label = toast.querySelector('[data-mail-undo-message]');
  const btn = toast.querySelector('[data-mail-undo-btn]');
  if (label) {
    label.textContent = message;
  }
  if (btn) {
    btn.onclick = () => {
      onUndo();
    };
  }
  toast.classList.add('show');
}

function flushPendingUndoAction() {
  if (!pendingUndoAction) return;
  const pending = pendingUndoAction;
  pendingUndoAction = null;
  clearTimeout(pending.timerId);
  hideUndoToast();
  void pending.execute();
}

function resetMailAssistDomState() {
  const e = getEls();
  if (!e.text) return;
  delete e.text.dataset.mailAssistState;
  delete e.text.dataset.mailAssistActionId;
  delete e.text.dataset.mailAssistMessageId;
  delete e.text.dataset.mailAssistError;
  delete e.text.dataset.mailAssistHistory;
}

function resetMailRecordingDomState() {
  const e = getEls();
  if (!e.text) return;
  delete e.text.dataset.mailRecordingState;
  delete e.text.dataset.mailRecordingMode;
  delete e.text.dataset.mailRecordingActive;
  delete e.text.dataset.mailRecordingHistory;
  delete e.text.dataset.mailRecordingLastStop;
  e.text.classList.remove('mail-recording-active');
}

function clearSelectionInteractionHandlers() {
  const e = getEls();
  clearReviewVoiceHint();
  closeReviewCommentPopover();
  if (e.text) {
    e.text._suppressNextReviewOpen = false;
    e.text.style.cursor = '';
  }
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
  if (e.text._reviewContextMenuHandler) {
    e.text.removeEventListener('contextmenu', e.text._reviewContextMenuHandler, true);
    e.text._reviewContextMenuHandler = null;
  }
  if (e.text._reviewExistingMarkClickHandler) {
    e.text.removeEventListener('click', e.text._reviewExistingMarkClickHandler);
    e.text._reviewExistingMarkClickHandler = null;
  }
  if (e.text._reviewHoverMoveHandler) {
    e.text.removeEventListener('mousemove', e.text._reviewHoverMoveHandler);
    e.text._reviewHoverMoveHandler = null;
  }
  if (e.text._reviewHoverLeaveHandler) {
    e.text.removeEventListener('mouseleave', e.text._reviewHoverLeaveHandler);
    e.text._reviewHoverLeaveHandler = null;
  }
  if (e.text._reviewAnchorPointerHandler) {
    e.text.removeEventListener('mousemove', e.text._reviewAnchorPointerHandler);
    e.text.removeEventListener('pointerdown', e.text._reviewAnchorPointerHandler);
    e.text._reviewAnchorPointerHandler = null;
  }
  if (e.text._reviewLongPressPointerDownHandler) {
    e.text.removeEventListener('pointerdown', e.text._reviewLongPressPointerDownHandler);
    e.text._reviewLongPressPointerDownHandler = null;
  }
  if (e.text._reviewLongPressPointerMoveHandler) {
    window.removeEventListener('pointermove', e.text._reviewLongPressPointerMoveHandler);
    e.text._reviewLongPressPointerMoveHandler = null;
  }
  if (e.text._reviewLongPressPointerUpHandler) {
    window.removeEventListener('pointerup', e.text._reviewLongPressPointerUpHandler);
    window.removeEventListener('pointercancel', e.text._reviewLongPressPointerUpHandler);
    e.text._reviewLongPressPointerUpHandler = null;
  }
  if (e.text._reviewCtrlKeyDownHandler) {
    document.removeEventListener('keydown', e.text._reviewCtrlKeyDownHandler, true);
    e.text._reviewCtrlKeyDownHandler = null;
  }
  if (e.text._reviewCtrlKeyUpHandler) {
    document.removeEventListener('keyup', e.text._reviewCtrlKeyUpHandler, true);
    e.text._reviewCtrlKeyUpHandler = null;
  }
  if (e.text._reviewCtrlBlurHandler) {
    window.removeEventListener('blur', e.text._reviewCtrlBlurHandler);
    e.text._reviewCtrlBlurHandler = null;
  }
  e.text._reviewLastAnchor = null;
  const reviewVoiceCapture = e.text._reviewVoiceCapture;
  if (reviewVoiceCapture) {
    reviewVoiceCapture.cancelled = true;
    if (reviewVoiceCapture.longPressTimer) {
      clearTimeout(reviewVoiceCapture.longPressTimer);
      reviewVoiceCapture.longPressTimer = null;
    }
    if (reviewVoiceCapture.sttSessionID) {
      void callPushToPromptAction(null, sttActionCancel, {
        session_id: reviewVoiceCapture.sttSessionID,
      }).catch(() => {});
    }
    if (reviewVoiceCapture.mediaRecorder) {
      try {
        if (reviewVoiceCapture.mediaRecorder.state !== 'inactive') {
          reviewVoiceCapture.mediaRecorder.stop();
        }
      } catch (_) {
        // no-op
      }
    }
    if (reviewVoiceCapture.mediaStream && typeof reviewVoiceCapture.mediaStream.getTracks === 'function') {
      reviewVoiceCapture.mediaStream.getTracks().forEach((track) => {
        if (track && typeof track.stop === 'function') {
          track.stop();
        }
      });
    }
    e.text._reviewVoiceCapture = null;
  }
  const pdfHitLayer = e.pdf?._pdfHitLayer;
  if (pdfHitLayer && e.pdf._pdfClickHandler) {
    pdfHitLayer.removeEventListener('click', e.pdf._pdfClickHandler);
    e.pdf._pdfClickHandler = null;
  } else if (e.pdf._pdfClickHandler) {
    e.pdf.removeEventListener('click', e.pdf._pdfClickHandler);
    e.pdf._pdfClickHandler = null;
  }
  if (pdfHitLayer && e.pdf._pdfHoverMoveHandler) {
    pdfHitLayer.removeEventListener('mousemove', e.pdf._pdfHoverMoveHandler);
    e.pdf._pdfHoverMoveHandler = null;
  }
  if (pdfHitLayer && e.pdf._pdfHoverLeaveHandler) {
    pdfHitLayer.removeEventListener('mouseleave', e.pdf._pdfHoverLeaveHandler);
    e.pdf._pdfHoverLeaveHandler = null;
  }
  if (pdfHitLayer) {
    pdfHitLayer.style.cursor = '';
  }
  if (e.pdf) {
    e.pdf._pdfHitLayer = null;
  }
}

function clearMailInteractionHandlers() {
  const e = getEls();
  flushPendingUndoAction();
  if (activeMailContext) {
    const recording = getMailRecordingState(activeMailContext);
    stopMailRecordingMedia(recording);
  }
  if (e.text._mailClickHandler) {
    e.text.removeEventListener('click', e.text._mailClickHandler);
    e.text._mailClickHandler = null;
  }
  if (e.text._mailPointerDownHandler) {
    e.text.removeEventListener('pointerdown', e.text._mailPointerDownHandler);
    e.text._mailPointerDownHandler = null;
  }
  if (e.text._mailPointerMoveHandler) {
    window.removeEventListener('pointermove', e.text._mailPointerMoveHandler);
    e.text._mailPointerMoveHandler = null;
  }
  if (e.text._mailPointerUpHandler) {
    window.removeEventListener('pointerup', e.text._mailPointerUpHandler);
    window.removeEventListener('pointercancel', e.text._mailPointerUpHandler);
    e.text._mailPointerUpHandler = null;
  }
  if (e.text._mailDetailKeyDownHandler) {
    document.removeEventListener('keydown', e.text._mailDetailKeyDownHandler);
    e.text._mailDetailKeyDownHandler = null;
  }
  if (e.text._mailRecordingClickHandler) {
    e.text.removeEventListener('click', e.text._mailRecordingClickHandler);
    e.text._mailRecordingClickHandler = null;
  }
  if (e.text._mailRecordingPointerDownHandler) {
    e.text.removeEventListener('pointerdown', e.text._mailRecordingPointerDownHandler);
    e.text._mailRecordingPointerDownHandler = null;
  }
  if (e.text._mailRecordingPointerUpHandler) {
    window.removeEventListener('pointerup', e.text._mailRecordingPointerUpHandler);
    window.removeEventListener('pointercancel', e.text._mailRecordingPointerUpHandler);
    e.text._mailRecordingPointerUpHandler = null;
  }
  if (e.text._mailRecordingKeyDownHandler) {
    document.removeEventListener('keydown', e.text._mailRecordingKeyDownHandler);
    e.text._mailRecordingKeyDownHandler = null;
  }
  if (e.text._mailRecordingKeyUpHandler) {
    document.removeEventListener('keyup', e.text._mailRecordingKeyUpHandler);
    e.text._mailRecordingKeyUpHandler = null;
  }
  closeDraftPanel();
  resetMailAssistDomState();
  resetMailRecordingDomState();
  e.text.classList.remove('mail-artifact');
  activeMailContext = null;
}

function clearTextInteractionHandlers() {
  clearSelectionInteractionHandlers();
  clearMailInteractionHandlers();
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

async function callMailRead(context, messageID) {
  const resp = await fetch('/api/mail/read', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: context.provider,
      message_id: messageID,
      format: 'full',
      producer_mcp_url: context.producerMcpUrl,
    }),
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
    throw new Error(typeof payload === 'object' && payload !== null && payload.error ? payload.error : raw || 'mail read failed');
  }
  if (typeof payload !== 'object' || payload === null) {
    throw new Error('mail read returned invalid response');
  }
  return payload.message || payload.result?.message || null;
}

async function callMailMarkRead(context, messageID) {
  const resp = await fetch('/api/mail/mark-read', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: context.provider,
      message_id: messageID,
      producer_mcp_url: context.producerMcpUrl,
    }),
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
    throw new Error(typeof payload === 'object' && payload !== null && payload.error ? payload.error : raw || 'mark-read failed');
  }
  if (typeof payload !== 'object' || payload === null) {
    throw new Error('mark-read returned invalid response');
  }
  return payload.result || payload;
}

async function callDraftReply(context, message) {
  const resp = await fetch('/api/mail/draft-reply', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: context.provider,
      message_id: message.id,
      subject: message.subject,
      sender: message.sender,
      selection_text: message.selectionText || '',
      producer_mcp_url: context.producerMcpUrl,
    }),
  });
  let payload = {};
  const raw = await resp.text();
  if (raw) {
    try {
      payload = JSON.parse(raw);
    } catch (_) {
      if (!resp.ok) throw new Error(raw);
    }
  }
  if (!resp.ok) {
    throw new Error(typeof payload === 'object' && payload !== null && payload.error ? payload.error : raw || 'draft generation failed');
  }
  if (typeof payload !== 'object' || payload === null) {
    throw new Error('draft reply returned invalid response');
  }
  return payload;
}

function createPushToPromptSessionID() {
  const rand = Math.random().toString(36).slice(2, 10);
  return `ptp-${Date.now().toString(36)}-${rand}`;
}

function base64FromBytes(bytes) {
  if (!bytes || !bytes.length) return '';
  const chunkSize = 0x8000;
  let out = '';
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    out += String.fromCharCode(...chunk);
  }
  return btoa(out);
}

async function callPushToPromptAction(context, action, actionPayload = {}) {
  const req = {
    action,
    ...actionPayload,
  };
  const producerMCPURL = String(context?.producerMcpUrl || '').trim();
  if (producerMCPURL) {
    req.producer_mcp_url = producerMCPURL;
  }
  const customMCPURL = String(window.__TABULA_VOXTYPE_MCP_URL || '').trim();
  if (customMCPURL) {
    req.voxtype_mcp_url = customMCPURL;
  }
  const resp = await fetch('/api/stt/push-to-prompt', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  let responsePayload = {};
  const raw = await resp.text();
  if (raw) {
    try {
      responsePayload = JSON.parse(raw);
    } catch (_) {
      if (!resp.ok) {
        throw new Error(raw);
      }
    }
  }
  if (!resp.ok) {
    throw new Error(typeof responsePayload === 'object' && responsePayload !== null && responsePayload.error
      ? responsePayload.error
      : raw || 'push-to-prompt request failed');
  }
  if (typeof responsePayload !== 'object' || responsePayload === null) {
    throw new Error('push-to-prompt request returned invalid response');
  }
  return responsePayload;
}

async function callMailSTT(context, audioBlob) {
  if (!audioBlob || typeof audioBlob.arrayBuffer !== 'function') {
    throw new Error('missing recorded audio payload');
  }
  const audioBytes = new Uint8Array(await audioBlob.arrayBuffer());
  if (!audioBytes.length) {
    throw new Error('recorded audio payload is empty');
  }
  const req = {
    producer_mcp_url: context.producerMcpUrl,
    mime_type: audioBlob.type || 'application/octet-stream',
    audio_base64: base64FromBytes(audioBytes),
  };
  const resp = await fetch('/api/mail/stt', {
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
    throw new Error(typeof payload === 'object' && payload !== null && payload.error
      ? payload.error
      : raw || 'speech transcription failed');
  }
  if (typeof payload !== 'object' || payload === null) {
    throw new Error('speech transcription returned invalid response');
  }
  const transcript = String(payload.text || '').trim();
  if (!transcript) {
    throw new Error('speech transcription returned empty text');
  }
  return payload;
}

async function callMailDraftIntent(context, transcript) {
  const resp = await fetch('/api/mail/draft-intent', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: context.provider,
      transcript: String(transcript || ''),
      producer_mcp_url: context.producerMcpUrl,
    }),
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
    throw new Error(typeof payload === 'object' && payload !== null && payload.error
      ? payload.error
      : raw || 'draft intent inference failed');
  }
  if (typeof payload !== 'object' || payload === null) {
    throw new Error('draft intent inference returned invalid response');
  }
  return payload;
}

function findMailHeader(context, messageID) {
  for (const h of context.headers || []) {
    if (h.id === messageID) return h;
  }
  return null;
}

function findMailHeaderIndex(context, messageID) {
  for (let i = 0; i < (context.headers || []).length; i += 1) {
    if (context.headers[i].id === messageID) return i;
  }
  return -1;
}

function createDefaultMailRecordingState() {
  return {
    mode: MAIL_RECORDING_MODE.HOLD,
    state: MAIL_RECORDING_STATE.IDLE,
    origin: '',
    holdPointerId: null,
    lastStopReason: '',
    captureToken: 0,
    mediaRecorder: null,
    mediaStream: null,
    chunks: [],
    mimeType: 'audio/webm',
    captureBackend: '',
    sttSessionID: '',
    appendSeq: 0,
    appendChain: Promise.resolve(),
    startPromise: Promise.resolve(),
    appendError: '',
    transcribing: false,
    stopRequested: false,
    error: '',
    transitions: ['mode:hold', 'state:idle'],
  };
}

function getMailViewState(context) {
  if (!context.viewState) {
    context.viewState = {
      mode: 'list',
      currentIndex: 0,
      listScrollTop: 0,
      detailMessage: null,
      detailStatus: '',
      detailStatusTone: 'info',
      assist: {
        state: MAIL_ASSIST_STATE.IDLE,
        actionId: '',
        messageId: '',
        error: '',
        transitions: [MAIL_ASSIST_STATE.IDLE],
      },
      recording: createDefaultMailRecordingState(),
    };
  } else if (!context.viewState.recording) {
    context.viewState.recording = createDefaultMailRecordingState();
  }
  return context.viewState;
}

function setMailAssistDomState(context) {
  const e = getEls();
  const assist = getMailViewState(context).assist;
  if (!assist) {
    resetMailAssistDomState();
    return;
  }
  e.text.dataset.mailAssistState = assist.state || MAIL_ASSIST_STATE.IDLE;
  e.text.dataset.mailAssistActionId = assist.actionId || '';
  e.text.dataset.mailAssistMessageId = assist.messageId || '';
  if (assist.error) {
    e.text.dataset.mailAssistError = assist.error;
  } else {
    delete e.text.dataset.mailAssistError;
  }
  e.text.dataset.mailAssistHistory = (assist.transitions || []).join('>');
}

function setMailAssistState(context, nextState, details = {}) {
  const state = getMailViewState(context);
  if (!state.assist) {
    state.assist = {
      state: MAIL_ASSIST_STATE.IDLE,
      actionId: '',
      messageId: '',
      error: '',
      transitions: [MAIL_ASSIST_STATE.IDLE],
    };
  }
  const assist = state.assist;
  assist.state = nextState || MAIL_ASSIST_STATE.IDLE;
  assist.actionId = String(details.actionId ?? assist.actionId ?? '').trim();
  assist.messageId = String(details.messageId ?? assist.messageId ?? '').trim();
  assist.error = String(details.error ?? '').trim();
  if (!Array.isArray(assist.transitions)) {
    assist.transitions = [MAIL_ASSIST_STATE.IDLE];
  }
  const last = assist.transitions[assist.transitions.length - 1];
  if (last !== assist.state) {
    assist.transitions.push(assist.state);
    if (assist.transitions.length > 12) {
      assist.transitions = assist.transitions.slice(-12);
    }
  }
  setMailAssistDomState(context);
}

function getMailRecordingState(context) {
  const state = getMailViewState(context);
  if (!state.recording) {
    state.recording = createDefaultMailRecordingState();
  }
  return state.recording;
}

function pushMailRecordingTransition(recording, token) {
  if (!Array.isArray(recording.transitions)) {
    recording.transitions = ['mode:hold', 'state:idle'];
  }
  const value = String(token || '').trim();
  if (!value) return;
  const last = recording.transitions[recording.transitions.length - 1];
  if (last !== value) {
    recording.transitions.push(value);
  }
  if (recording.transitions.length > 20) {
    recording.transitions = recording.transitions.slice(-20);
  }
}

function recordingTriggerLabel(recording) {
  if (recording.transcribing) {
    return 'Transcribing...';
  }
  if (recording.mode === MAIL_RECORDING_MODE.TOGGLE) {
    return recording.state === MAIL_RECORDING_STATE.RECORDING ? 'Stop Push To Prompt' : 'Start Push To Prompt';
  }
  if (recording.state === MAIL_RECORDING_STATE.RECORDING) {
    return 'Push To Prompt active... release to stop';
  }
  return 'Hold to Push To Prompt';
}

function setMailRecordingDomState(context) {
  const e = getEls();
  if (!e.text) return;
  const recording = getMailRecordingState(context);
  const isActive = recording.state === MAIL_RECORDING_STATE.RECORDING;
  const indicator = recording.error
    ? recording.error
    : recording.transcribing
      ? 'Transcribing...'
      : isActive
    ? `Push To Prompt (${recording.mode} mode)`
    : `Ready for Push To Prompt (${recording.mode} mode)`;
  e.text.dataset.mailRecordingState = recording.state || MAIL_RECORDING_STATE.IDLE;
  e.text.dataset.mailRecordingMode = recording.mode || MAIL_RECORDING_MODE.HOLD;
  e.text.dataset.mailRecordingActive = isActive ? '1' : '0';
  e.text.dataset.mailRecordingHistory = (recording.transitions || []).join('>');
  if (recording.lastStopReason) {
    e.text.dataset.mailRecordingLastStop = recording.lastStopReason;
  } else {
    delete e.text.dataset.mailRecordingLastStop;
  }
  e.text.classList.toggle('mail-recording-active', isActive);

  e.text.querySelectorAll('[data-mail-record-indicator]').forEach((node) => {
    node.textContent = indicator;
    node.classList.toggle('mail-record-indicator-active', isActive);
  });
  e.text.querySelectorAll('button[data-mail-record-mode]').forEach((button) => {
    const mode = String(button.dataset.mailRecordMode || '').trim();
    const active = mode === recording.mode;
    button.classList.toggle('is-active', active);
    button.setAttribute('aria-pressed', active ? 'true' : 'false');
  });
  e.text.querySelectorAll('button[data-mail-record-action="trigger"]').forEach((button) => {
    button.textContent = recordingTriggerLabel(recording);
    button.setAttribute('aria-pressed', isActive ? 'true' : 'false');
    button.disabled = recording.transcribing;
  });
  e.text.querySelectorAll('button[data-mail-record-action="stop"]').forEach((button) => {
    button.disabled = !isActive || recording.transcribing;
    button.hidden = !isActive;
  });
}

function stopMailRecordingMedia(recording) {
  if (recording?.mediaRecorder) {
    try {
      if (recording.mediaRecorder.state !== 'inactive') {
        recording.mediaRecorder.stop();
      }
    } catch (_) {
      // no-op: recorder might already be stopping/stopped
    }
  }
  const stream = recording?.mediaStream;
  if (stream && typeof stream.getTracks === 'function') {
    stream.getTracks().forEach((track) => {
      if (track && typeof track.stop === 'function') {
        track.stop();
      }
    });
  }
  if (!recording) return;
  recording.mediaRecorder = null;
  recording.mediaStream = null;
  recording.chunks = [];
  recording.captureBackend = '';
  recording.sttSessionID = '';
  recording.appendSeq = 0;
  recording.appendChain = Promise.resolve();
  recording.startPromise = Promise.resolve();
  recording.appendError = '';
  recording.stopRequested = false;
}

async function pushToPromptAppendChunk(context, recording, chunkBlob) {
  if (!recording?.sttSessionID) return;
  if (!chunkBlob || typeof chunkBlob.arrayBuffer !== 'function' || chunkBlob.size <= 0) return;
  const bytes = new Uint8Array(await chunkBlob.arrayBuffer());
  if (!bytes.length) return;
  const seq = Number(recording.appendSeq || 0);
  recording.appendSeq = seq + 1;
  const payload = {
    session_id: recording.sttSessionID,
    seq,
    audio_base64: base64FromBytes(bytes),
  };
  const chain = recording.appendChain || Promise.resolve();
  recording.appendChain = chain.then(() => callPushToPromptAction(context, sttActionAppend, payload));
  try {
    await recording.appendChain;
  } catch (err) {
    recording.appendError = String(err?.message || err || 'chunk append failed');
    throw err;
  }
}

async function startMailRecordingMediaCapture(context, token) {
  const recording = getMailRecordingState(context);
  if (!pendingDraftPromptCapture) {
    return;
  }
  recording.mediaStream = null;
  recording.mediaRecorder = null;
  recording.chunks = [];
  recording.mimeType = 'audio/webm';
  recording.captureBackend = '';
  recording.sttSessionID = createPushToPromptSessionID();
  recording.appendSeq = 0;
  recording.appendChain = Promise.resolve();
  recording.appendError = '';
  const startResp = await callPushToPromptAction(context, sttActionStart, {
    session_id: recording.sttSessionID,
    mime_type: recording.mimeType,
  });
  const captureBackend = String(startResp?.capture_backend || '').trim().toLowerCase();
  recording.captureBackend = captureBackend || 'buffered';
  if (recording.captureBackend === 'daemon') {
    return;
  }
  if (!window.MediaRecorder || !navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
    throw new Error('Microphone capture is unavailable in this browser.');
  }
  const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  if (recording.captureToken !== token || recording.state !== MAIL_RECORDING_STATE.RECORDING) {
    if (stream && typeof stream.getTracks === 'function') {
      stream.getTracks().forEach((track) => {
        if (track && typeof track.stop === 'function') {
          track.stop();
        }
      });
    }
    return;
  }
  const recorder = newMediaRecorder(stream);
  recording.mediaStream = stream;
  recording.mediaRecorder = recorder;
  recording.mimeType = recorder.mimeType || recording.mimeType || 'audio/webm';
  recorder.addEventListener('dataavailable', (ev) => {
    if (ev?.data && ev.data.size > 0) {
      recording.chunks.push(ev.data);
      void pushToPromptAppendChunk(context, recording, ev.data).catch(() => {
        // appendError is set in helper; final transcription path will surface the error
      });
    }
  });
  recorder.start(300);
  if (recording.stopRequested) {
    try {
      recorder.stop();
    } catch (_) {
      stopMailRecordingMedia(recording);
    }
  }
}

async function stopMailRecordingMediaAndCollectBlob(context, token) {
  const recording = getMailRecordingState(context);
  if (recording.captureToken !== token) {
    return null;
  }
  const recorder = recording.mediaRecorder;
  if (!recorder) {
    return null;
  }
  const toBlob = () => {
    const parts = Array.isArray(recording.chunks) ? recording.chunks.slice() : [];
    stopMailRecordingMedia(recording);
    if (!parts.length) {
      return null;
    }
    return new Blob(parts, { type: recording.mimeType || recorder.mimeType || 'audio/webm' });
  };
  if (recorder.state === 'inactive') {
    return toBlob();
  }
  return new Promise((resolve, reject) => {
    const onStop = () => {
      recorder.removeEventListener('error', onError);
      resolve(toBlob());
    };
    const onError = () => {
      recorder.removeEventListener('stop', onStop);
      stopMailRecordingMedia(recording);
      reject(new Error('recording failed'));
    };
    recorder.addEventListener('stop', onStop, { once: true });
    recorder.addEventListener('error', onError, { once: true });
    try {
      recorder.stop();
    } catch (err) {
      recorder.removeEventListener('stop', onStop);
      recorder.removeEventListener('error', onError);
      stopMailRecordingMedia(recording);
      reject(err);
    }
  });
}

async function transcribePendingDraftPrompt(context, token) {
  const recording = getMailRecordingState(context);
  if (!pendingDraftPromptCapture) {
    stopMailRecordingMedia(recording);
    return;
  }
  const pending = pendingDraftPromptCapture;
  const isSamePending = () => pendingDraftPromptCapture === pending;
  recording.transcribing = true;
  recording.error = '';
  setMailRecordingDomState(context);
  if (isSamePending()) {
    setMailAssistStatus(pending.context, pending.row, pending.inDetail, 'Transcribing Push To Prompt input...', 'info');
  }
  try {
    await (recording.startPromise || Promise.resolve());
    let stt = null;
    if (recording.captureBackend === 'daemon') {
      if (!recording.sttSessionID) {
        throw new Error('No active Push To Prompt session.');
      }
      stt = await callPushToPromptAction(context, sttActionStop, {
        session_id: recording.sttSessionID,
      });
      stopMailRecordingMedia(recording);
    } else {
      const audioBlob = await stopMailRecordingMediaAndCollectBlob(context, token);
      if (!audioBlob || audioBlob.size <= 0) {
        throw new Error('No audio captured. Hold to record and try again.');
      }
      try {
        if (recording.sttSessionID) {
          await (recording.appendChain || Promise.resolve());
          if (recording.appendError) {
            throw new Error(recording.appendError);
          }
          stt = await callPushToPromptAction(context, sttActionStop, {
            session_id: recording.sttSessionID,
          });
        } else {
          stt = await callMailSTT(context, audioBlob);
        }
      } catch (stopErr) {
        stt = await callMailSTT(context, audioBlob);
      }
    }
    const transcript = String(stt?.text || '').trim();
    if (!transcript) {
      throw new Error('Speech recognizer returned empty text.');
    }
    let captureResult = {
      text: transcript,
      intent: MAIL_DRAFT_INTENT_FALLBACK_POLICY,
      fallbackApplied: false,
      fallbackPolicy: MAIL_DRAFT_INTENT_FALLBACK_POLICY,
      reason: 'intent_inference_unavailable',
    };
    let sourceLabel = 'Transcribed from voice input. Generating draft...';
    try {
      const inferred = await callMailDraftIntent(context, transcript);
      const inferredIntent = String(inferred?.intent || '').trim().toLowerCase();
      const intent = inferredIntent === MAIL_DRAFT_INTENT.DICTATION
        ? MAIL_DRAFT_INTENT.DICTATION
        : MAIL_DRAFT_INTENT.PROMPT;
      const fallbackApplied = Boolean(inferred?.fallback_applied);
      const fallbackPolicy = String(inferred?.fallback_policy || MAIL_DRAFT_INTENT_FALLBACK_POLICY).trim().toLowerCase() || MAIL_DRAFT_INTENT_FALLBACK_POLICY;
      const reason = String(inferred?.reason || '').trim() || 'intent_inferred';
      captureResult = {
        text: transcript,
        intent,
        fallbackApplied,
        fallbackPolicy,
        reason,
      };
      if (intent === MAIL_DRAFT_INTENT.DICTATION) {
        sourceLabel = 'Detected dictation. Using transcript as editable draft.';
      } else if (fallbackApplied) {
        sourceLabel = 'Intent ambiguous. Using prompt fallback policy to generate draft.';
      } else {
        sourceLabel = 'Detected prompt intent. Generating draft...';
      }
    } catch (_) {
      captureResult.fallbackApplied = true;
      captureResult.reason = 'intent_inference_failed';
      sourceLabel = 'Intent inference unavailable. Using prompt fallback policy to generate draft.';
    }
    resolvePendingDraftPromptCapture(captureResult, sourceLabel, pending);
  } catch (err) {
    if (!isSamePending()) {
      return;
    }
    const message = String(err?.message || err || 'speech transcription failed');
    recording.error = `Transcription failed: ${message}`;
    setMailAssistStatus(
      pending.context,
      pending.row,
      pending.inDetail,
      `Transcription failed: ${message}. Retry recording or type a prompt.`,
      'warning',
    );
  } finally {
    recording.transcribing = false;
    setMailRecordingDomState(context);
  }
}

function startMailRecording(context, origin) {
  const recording = getMailRecordingState(context);
  if (recording.state === MAIL_RECORDING_STATE.RECORDING) return false;
  const token = Number(recording.captureToken || 0) + 1;
  recording.captureToken = token;
  recording.stopRequested = false;
  recording.state = MAIL_RECORDING_STATE.RECORDING;
  recording.origin = String(origin || '').trim();
  recording.lastStopReason = '';
  recording.error = '';
  pushMailRecordingTransition(recording, 'state:recording');
  setMailRecordingDomState(context);
  if (pendingDraftPromptCapture) {
    recording.startPromise = startMailRecordingMediaCapture(context, token);
    void recording.startPromise.catch((err) => {
      if (recording.captureToken !== token) {
        return;
      }
      const pending = pendingDraftPromptCapture;
      recording.state = MAIL_RECORDING_STATE.IDLE;
      recording.origin = '';
      recording.stopRequested = false;
      recording.lastStopReason = 'capture_error';
      recording.error = `Recording failed: ${String(err?.message || err || 'capture failed')}`;
      pushMailRecordingTransition(recording, 'stop:capture_error');
      pushMailRecordingTransition(recording, 'state:idle');
      const sessionID = String(recording.sttSessionID || '').trim();
      if (sessionID) {
        void callPushToPromptAction(context, sttActionCancel, { session_id: sessionID }).catch(() => {});
      }
      stopMailRecordingMedia(recording);
      setMailRecordingDomState(context);
      if (pending) {
        setMailAssistStatus(
          pending.context,
          pending.row,
          pending.inDetail,
          `${recording.error}. Retry recording or type a prompt.`,
          'warning',
        );
      }
    });
  } else {
    recording.startPromise = Promise.resolve();
  }
  return true;
}

function stopMailRecording(context, reason) {
  const recording = getMailRecordingState(context);
  if (recording.state !== MAIL_RECORDING_STATE.RECORDING) return false;
  recording.state = MAIL_RECORDING_STATE.IDLE;
  recording.origin = '';
  recording.holdPointerId = null;
  recording.stopRequested = true;
  recording.lastStopReason = String(reason || 'stop').trim() || 'stop';
  pushMailRecordingTransition(recording, `stop:${recording.lastStopReason}`);
  pushMailRecordingTransition(recording, 'state:idle');
  setMailRecordingDomState(context);
  if (pendingDraftPromptCapture) {
    void transcribePendingDraftPrompt(context, recording.captureToken);
  } else {
    const sessionID = String(recording.sttSessionID || '').trim();
    if (sessionID) {
      void callPushToPromptAction(context, sttActionCancel, { session_id: sessionID }).catch(() => {});
    }
    stopMailRecordingMedia(recording);
  }
  return true;
}

function setMailRecordingMode(context, nextMode) {
  const recording = getMailRecordingState(context);
  const mode = nextMode === MAIL_RECORDING_MODE.TOGGLE ? MAIL_RECORDING_MODE.TOGGLE : MAIL_RECORDING_MODE.HOLD;
  if (recording.mode === mode) {
    setMailRecordingDomState(context);
    return false;
  }
  recording.mode = mode;
  pushMailRecordingTransition(recording, `mode:${mode}`);
  if (recording.state === MAIL_RECORDING_STATE.RECORDING) {
    stopMailRecording(context, 'mode_change');
    return true;
  }
  setMailRecordingDomState(context);
  return true;
}

function setMailAssistStatus(context, row, inDetail, text, tone) {
  if (row) {
    setMailRowStatus(row, text, tone);
    return;
  }
  if (inDetail) {
    setMailDetailStatus(context, text, tone);
  }
}

function setMailAssistBusy(row, inDetail, busy) {
  if (row) {
    setMailRowBusy(row, busy);
  }
  if (inDetail) {
    setMailDetailBusy(busy);
  }
}

function resolveMailAssistActionID(button, action) {
  const explicit = String(button?.dataset?.mailActionId || '').trim();
  if (explicit) return explicit;
  if (action === 'draft-reply') return 'mail.draft_reply';
  return '';
}

function registerMailAssistAction(actionId, handler) {
  const key = String(actionId || '').trim();
  if (!key || !handler || typeof handler.prepare !== 'function' || typeof handler.execute !== 'function') {
    return;
  }
  mailAssistActionRegistry.set(key, handler);
}

async function dispatchMailAssistAction(eventId, context, invocation) {
  const actionId = String(invocation?.actionId || '').trim();
  const row = invocation?.row || null;
  const inDetail = Boolean(invocation?.inDetail && !row);
  const messageID = String(invocation?.messageID || '').trim();
  const handler = mailAssistActionRegistry.get(actionId);
  if (!handler) {
    const msg = `Unsupported assist action_id: ${actionId || '(empty)'}`;
    setMailAssistState(context, MAIL_ASSIST_STATE.ERROR, { actionId, messageId: messageID, error: msg });
    setMailAssistStatus(context, row, inDetail, msg, 'error');
    return;
  }

  let assistBusy = false;
  try {
    setMailAssistState(context, MAIL_ASSIST_STATE.CAPTURING, { actionId, messageId: messageID, error: '' });
    if (typeof handler.onCapturing === 'function') {
      handler.onCapturing({ context, row, inDetail, messageID, actionId });
    } else {
      setMailAssistStatus(context, row, inDetail, 'Capturing assist context...', 'info');
    }

    const prepared = await handler.prepare({
      context,
      eventId,
      row,
      inDetail,
      messageID,
      actionId,
      selectionText: window.getSelection()?.toString?.() || '',
    });
    if (activeTextEventId !== eventId) return;

    setMailAssistState(context, MAIL_ASSIST_STATE.GENERATING, { actionId, messageId: messageID, error: '' });
    if (typeof handler.onGenerating === 'function') {
      handler.onGenerating({ context, row, inDetail, messageID, actionId, prepared });
    } else {
      setMailAssistStatus(context, row, inDetail, 'Generating assist output...', 'info');
    }
    setMailAssistBusy(row, inDetail, true);
    assistBusy = true;

    const payload = await handler.execute(prepared, { context, eventId, row, inDetail, messageID, actionId });
    if (activeTextEventId !== eventId) return;

    if (typeof handler.onReady === 'function') {
      handler.onReady(payload, prepared, { context, row, inDetail, messageID, actionId });
    } else {
      setMailAssistStatus(context, row, inDetail, 'Assist output ready.', 'success');
    }
    setMailAssistState(context, MAIL_ASSIST_STATE.READY, { actionId, messageId: messageID, error: '' });
  } catch (err) {
    if (activeTextEventId !== eventId) return;
    if (err && typeof err === 'object' && err.code === DRAFT_PROMPT_CANCELLED_CODE) {
      setMailAssistState(context, MAIL_ASSIST_STATE.IDLE, { actionId: '', messageId: '', error: '' });
      return;
    }
    const message = String(err?.message || err || 'assist action failed');
    if (typeof handler.onError === 'function') {
      handler.onError(message, { context, row, inDetail, messageID, actionId });
    } else {
      setMailAssistStatus(context, row, inDetail, message, 'error');
    }
    setMailAssistState(context, MAIL_ASSIST_STATE.ERROR, { actionId, messageId: messageID, error: message });
  } finally {
    if (activeTextEventId !== eventId) return;
    if (assistBusy) {
      if (row && row.isConnected) {
        setMailRowBusy(row, false);
      }
      if (inDetail) {
        setMailDetailBusy(false);
      }
    }
  }
}

function createDraftPromptCancelledError(message) {
  const err = new Error(message || 'Draft prompt capture cancelled.');
  err.code = DRAFT_PROMPT_CANCELLED_CODE;
  return err;
}

function cancelPendingDraftPromptCapture(message) {
  if (!pendingDraftPromptCapture) return false;
  const pending = pendingDraftPromptCapture;
  pendingDraftPromptCapture = null;
  pending.reject(createDraftPromptCancelledError(message));
  return true;
}

function getDraftPromptControls() {
  const panel = document.querySelector('[data-mail-draft-panel]');
  const promptInput = panel ? panel.querySelector('[data-mail-draft-prompt]') : null;
  const generateBtn = panel ? panel.querySelector('button[data-mail-action="draft-generate"]') : null;
  return { panel, promptInput, generateBtn };
}

function normalizeDraftPromptCaptureResult(capture) {
  const raw = typeof capture === 'string' ? { text: capture } : (capture || {});
  const text = String(raw.text || '').trim();
  const intent = String(raw.intent || '').trim().toLowerCase() === MAIL_DRAFT_INTENT.DICTATION
    ? MAIL_DRAFT_INTENT.DICTATION
    : MAIL_DRAFT_INTENT.PROMPT;
  const fallbackPolicy = String(raw.fallbackPolicy || MAIL_DRAFT_INTENT_FALLBACK_POLICY).trim().toLowerCase() || MAIL_DRAFT_INTENT_FALLBACK_POLICY;
  return {
    text,
    intent,
    fallbackApplied: Boolean(raw.fallbackApplied),
    fallbackPolicy,
    reason: String(raw.reason || '').trim() || 'manual_prompt',
  };
}

function resolvePendingDraftPromptCapture(capture, sourceLabel, expectedPending = null) {
  if (!pendingDraftPromptCapture) return false;
  if (expectedPending && pendingDraftPromptCapture !== expectedPending) return false;
  const pending = pendingDraftPromptCapture;
  const normalizedCapture = normalizeDraftPromptCaptureResult(capture);
  const normalized = normalizedCapture.text;
  if (!normalized) {
    setMailAssistStatus(pending.context, pending.row, pending.inDetail, 'Speech transcript was empty. Retry recording or type a prompt.', 'warning');
    return false;
  }
  const { promptInput, generateBtn } = getDraftPromptControls();
  if (promptInput) {
    promptInput.value = normalized;
    promptInput.disabled = true;
  }
  if (generateBtn) {
    generateBtn.disabled = true;
  }
  pendingDraftPromptCapture = null;
  pending.resolve(normalizedCapture);
  setMailAssistStatus(
    pending.context,
    pending.row,
    pending.inDetail,
    sourceLabel || 'Prompt captured. Generating draft...',
    'info',
  );
  return true;
}

function submitPendingDraftPromptCapture() {
  if (!pendingDraftPromptCapture) return false;
  const pending = pendingDraftPromptCapture;
  const { promptInput, generateBtn } = getDraftPromptControls();
  const promptText = String(promptInput?.value || '').trim();
  if (!promptText) {
    setMailAssistStatus(pending.context, pending.row, pending.inDetail, 'Enter a prompt before generating.', 'warning');
    if (promptInput && typeof promptInput.focus === 'function') {
      promptInput.focus();
    }
    return true;
  }
  if (promptInput) {
    promptInput.disabled = true;
  }
  if (generateBtn) {
    generateBtn.disabled = true;
  }
  return resolvePendingDraftPromptCapture({
    text: promptText,
    intent: MAIL_DRAFT_INTENT.PROMPT,
    fallbackApplied: false,
    fallbackPolicy: MAIL_DRAFT_INTENT_FALLBACK_POLICY,
    reason: 'manual_prompt',
  }, 'Prompt captured. Generating draft...');
}

function waitForDraftPromptCapture(context, row, inDetail, messageID, actionId) {
  cancelPendingDraftPromptCapture('superseded by new draft prompt');
  openDraftPanel('', 'Add prompt, then generate.', {
    showPrompt: true,
    focusPrompt: true,
    promptText: '',
    promptDisabled: false,
    generateDisabled: false,
  });
  setMailAssistStatus(context, row, inDetail, 'Add a prompt or use Push To Prompt voice input, then generate.', 'info');
  return new Promise((resolve, reject) => {
    pendingDraftPromptCapture = {
      resolve,
      reject,
      context,
      row,
      inDetail,
      messageID,
      actionId,
    };
  });
}

function openDraftPanel(content, sourceLabel, options = {}) {
  const panel = document.querySelector('[data-mail-draft-panel]');
  if (!panel) return;
  const textarea = panel.querySelector('[data-mail-draft-text]');
  const source = panel.querySelector('[data-mail-draft-source]');
  const promptWrap = panel.querySelector('[data-mail-draft-prompt-wrap]');
  const promptInput = panel.querySelector('[data-mail-draft-prompt]');
  const generateBtn = panel.querySelector('button[data-mail-action="draft-generate"]');
  const showPrompt = Boolean(options.showPrompt);
  if (textarea) {
    textarea.value = content || '';
  }
  if (source) {
    source.textContent = sourceLabel || '';
  }
  if (promptWrap) {
    promptWrap.hidden = !showPrompt;
  }
  if (promptInput) {
    promptInput.disabled = Boolean(options.promptDisabled);
    if (options.promptText !== undefined) {
      promptInput.value = String(options.promptText || '');
    }
  }
  if (generateBtn) {
    generateBtn.hidden = !showPrompt;
    generateBtn.disabled = Boolean(options.generateDisabled);
  }
  panel.hidden = false;
  if (showPrompt && options.focusPrompt && promptInput && typeof promptInput.focus === 'function') {
    setTimeout(() => promptInput.focus(), 0);
  }
}

function closeDraftPanel() {
  cancelPendingDraftPromptCapture('draft reply cancelled by user');
  const panel = document.querySelector('[data-mail-draft-panel]');
  if (!panel) return;
  const textarea = panel.querySelector('[data-mail-draft-text]');
  const source = panel.querySelector('[data-mail-draft-source]');
  const promptWrap = panel.querySelector('[data-mail-draft-prompt-wrap]');
  const promptInput = panel.querySelector('[data-mail-draft-prompt]');
  const generateBtn = panel.querySelector('button[data-mail-action="draft-generate"]');
  if (textarea) {
    textarea.value = '';
  }
  if (source) {
    source.textContent = '';
  }
  if (promptWrap) {
    promptWrap.hidden = true;
  }
  if (promptInput) {
    promptInput.value = '';
    promptInput.disabled = false;
  }
  if (generateBtn) {
    generateBtn.hidden = true;
    generateBtn.disabled = false;
  }
  panel.hidden = true;
}

function resetMailAssistDraftContext(context) {
  closeDraftPanel();
  setMailAssistState(context, MAIL_ASSIST_STATE.IDLE, { actionId: '', messageId: '', error: '' });
}

function registerDefaultMailAssistActions() {
  registerMailAssistAction('mail.draft_reply', {
    onCapturing(invocation) {
      openDraftPanel('', 'Preparing draft assist...', {
        showPrompt: true,
        focusPrompt: true,
        promptText: '',
        promptDisabled: false,
        generateDisabled: false,
      });
      setMailAssistStatus(invocation.context, invocation.row, invocation.inDetail, 'Capturing assist context...', 'info');
    },
    async prepare({ context, row, inDetail, messageID, actionId, selectionText }) {
      const header = findMailHeader(context, messageID);
      const capture = await waitForDraftPromptCapture(context, row, inDetail, messageID, actionId);
      const promptText = String(capture?.text || selectionText || '').trim();
      return {
        context,
        message: {
          id: messageID,
          sender: header?.sender || '',
          subject: header?.subject || '',
          selectionText: promptText,
          promptText,
          inputIntent: String(capture?.intent || MAIL_DRAFT_INTENT.PROMPT).trim().toLowerCase(),
          intentFallbackApplied: Boolean(capture?.fallbackApplied),
          intentFallbackPolicy: String(capture?.fallbackPolicy || MAIL_DRAFT_INTENT_FALLBACK_POLICY).trim().toLowerCase() || MAIL_DRAFT_INTENT_FALLBACK_POLICY,
          intentReason: String(capture?.reason || '').trim(),
        },
      };
    },
    onGenerating({ prepared }) {
      const promptText = prepared?.message?.promptText || '';
      const sourceLabel = prepared?.message?.inputIntent === MAIL_DRAFT_INTENT.DICTATION
        ? 'Applying dictation transcript...'
        : 'Generating...';
      openDraftPanel('', sourceLabel, {
        showPrompt: true,
        promptText,
        promptDisabled: true,
        generateDisabled: true,
      });
    },
    execute(prepared) {
      if (prepared?.message?.inputIntent === MAIL_DRAFT_INTENT.DICTATION) {
        return Promise.resolve({
          source: 'dictation',
          draft_text: String(prepared?.message?.promptText || ''),
          intent: MAIL_DRAFT_INTENT.DICTATION,
        });
      }
      return callDraftReply(prepared.context, prepared.message);
    },
    onReady(payload, prepared, invocation) {
      const draftText = String(payload?.draft_text || '').trim();
      const source = String(payload?.source || 'llm').trim();
      let sourceLabel = source === 'llm' ? 'Generated by LLM (unsent)' : 'Fallback draft (unsent)';
      if (source === 'dictation') {
        sourceLabel = 'Captured dictation draft (editable, unsent)';
      } else if (prepared?.message?.intentFallbackApplied && prepared?.message?.intentFallbackPolicy === MAIL_DRAFT_INTENT_FALLBACK_POLICY) {
        sourceLabel = 'Generated by LLM after ambiguous intent fallback (unsent)';
      }
      openDraftPanel(draftText, sourceLabel, {
        showPrompt: true,
        promptText: prepared?.message?.promptText || '',
        promptDisabled: false,
        generateDisabled: false,
      });
      setMailAssistStatus(invocation.context, invocation.row, invocation.inDetail, 'Draft ready. Review and edit before sending.', 'success');
    },
    onError(message, invocation) {
      closeDraftPanel();
      setMailAssistStatus(invocation.context, invocation.row, invocation.inDetail, message, 'error');
    },
  });
}

registerDefaultMailAssistActions();

function firstMailField(message, keys) {
  if (!message || typeof message !== 'object') return '';
  for (const key of keys) {
    const value = message[key];
    if (value === null || value === undefined) continue;
    if (Array.isArray(value)) {
      const joined = value.map((v) => String(v || '').trim()).filter(Boolean).join(', ');
      if (joined) return joined;
      continue;
    }
    const text = String(value).trim();
    if (text && text !== '<nil>') return text;
  }
  return '';
}

function renderMailRecordingControls() {
  return `
    <div class="mail-record-controls" data-mail-record-controls>
      <div class="mail-record-mode" role="group" aria-label="Recording mode">
        <button type="button" data-mail-record-mode="hold" aria-pressed="true">Hold</button>
        <button type="button" data-mail-record-mode="toggle" aria-pressed="false">Toggle</button>
      </div>
      <button type="button" data-mail-record-action="trigger">Hold to Push To Prompt</button>
      <button type="button" data-mail-record-action="stop" hidden disabled>Stop</button>
      <span class="mail-record-indicator" data-mail-record-indicator>Ready for Push To Prompt (hold mode)</span>
    </div>
  `;
}

function renderMailListHtml(context) {
  const provider = context.provider || 'default';
  const folder = context.folder || '-';
  const rows = context.headers.map((header, idx) => `
    <tr data-message-id="${escapeHtml(header.id)}" data-mail-index="${idx}">
      <td>${escapeHtml(formatMailDate(header.date))}</td>
      <td>${escapeHtml(header.sender || '(no sender)')}</td>
      <td>${escapeHtml(header.subject || '(no subject)')}</td>
      <td class="mail-row-actions">
        <div class="mail-action-buttons">
          <button type="button" data-mail-action="open">Open</button>
          <button type="button" data-mail-action="archive">Archive</button>
          <button type="button" data-mail-action="delete">Delete</button>
          <button type="button" data-mail-action="defer">Defer</button>
          <button type="button" data-mail-action="draft-reply" data-mail-action-id="mail.draft_reply">Draft Reply</button>
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
  const hasRows = context.headers.length > 0;
  const body = hasRows
    ? rows
    : '<tr><td colspan="4"><em>No messages left in this list.</em></td></tr>';
  return `
    <div class="mail-triage-head">
      <div><strong>Provider:</strong> ${escapeHtml(provider)}</div>
      <div><strong>Folder:</strong> ${escapeHtml(folder)}</div>
      <div><strong>Count:</strong> ${escapeHtml(String(context.count))}</div>
      <div class="mail-capability-hint" data-mail-capability-hint>Provider ${escapeHtml(provider)}: checking defer capability...</div>
    </div>
    ${renderMailRecordingControls()}
    <table class="mail-triage-table">
      <thead>
        <tr>
          <th>Date</th>
          <th>Sender</th>
          <th>Subject</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>${body}</tbody>
    </table>
    <div class="mail-draft-panel" data-mail-draft-panel hidden>
      <div class="mail-draft-head">
        <strong>Draft Reply</strong>
        <span data-mail-draft-source></span>
      </div>
      <div class="mail-draft-prompt" data-mail-draft-prompt-wrap hidden>
        <label>Prompt</label>
        <textarea data-mail-draft-prompt placeholder="Add context or intent for this reply"></textarea>
      </div>
      <textarea data-mail-draft-text placeholder="Draft reply will appear here"></textarea>
      <div class="mail-draft-actions">
        <button type="button" data-mail-action="draft-generate" hidden>Generate</button>
        <button type="button" data-mail-action="draft-copy">Copy</button>
        <button type="button" data-mail-action="draft-cancel">Cancel</button>
      </div>
    </div>
  `;
}

function renderMailDetailHtml(context) {
  const state = getMailViewState(context);
  const idx = Math.max(0, Math.min(state.currentIndex, context.headers.length - 1));
  const header = context.headers[idx] || { id: '', date: '', sender: '', subject: '' };
  const message = state.detailMessage || {};
  const provider = context.provider || 'default';
  const folder = context.folder || '-';
  const subject = firstMailField(message, ['Subject', 'subject']) || header.subject || '(no subject)';
  const from = firstMailField(message, ['Sender', 'sender']) || header.sender || '-';
  const to = firstMailField(message, ['Recipients', 'recipients']) || '-';
  const date = formatMailDate(firstMailField(message, ['Date', 'date']) || header.date || '');
  const body = firstMailField(message, ['BodyText', 'body_text', 'plain', 'text', 'Snippet', 'snippet']) || '(no body text available)';
  const isFirst = idx <= 0;
  const isLast = idx >= context.headers.length - 1;

  return `
    <div class="mail-detail-view" data-mail-detail-root data-message-id="${escapeHtml(header.id)}">
      <div class="mail-detail-toolbar">
        <button type="button" data-mail-action="detail-back">Back to list</button>
        <div class="mail-detail-nav">
          <button type="button" data-mail-action="detail-prev" ${isFirst ? 'disabled' : ''}>Prev</button>
          <span class="mail-detail-position">${escapeHtml(String(idx + 1))} / ${escapeHtml(String(context.headers.length))}</span>
          <button type="button" data-mail-action="detail-next" ${isLast ? 'disabled' : ''}>Next</button>
        </div>
      </div>
      <div class="mail-triage-head">
        <div><strong>Provider:</strong> ${escapeHtml(provider)}</div>
        <div><strong>Folder:</strong> ${escapeHtml(folder)}</div>
        <div class="mail-capability-hint" data-mail-capability-hint>Provider ${escapeHtml(provider)}: checking defer capability...</div>
      </div>
      ${renderMailRecordingControls()}
      <h3 class="mail-detail-subject">${escapeHtml(subject)}</h3>
      <div class="mail-detail-meta">
        <div><strong>From:</strong> ${escapeHtml(from)}</div>
        <div><strong>To:</strong> ${escapeHtml(to)}</div>
        <div><strong>Date:</strong> ${escapeHtml(date || '-')}</div>
      </div>
      <div class="mail-detail-actions">
        <button type="button" data-mail-action="archive">Archive</button>
        <button type="button" data-mail-action="delete">Delete</button>
        <button type="button" data-mail-action="defer">Defer</button>
        <button type="button" data-mail-action="draft-reply" data-mail-action-id="mail.draft_reply">Draft Reply</button>
      </div>
      <div class="mail-defer-controls" data-mail-detail-defer-controls hidden>
        <input type="datetime-local" data-mail-detail-defer-input>
        <button type="button" data-mail-action="defer-apply">Apply</button>
        <button type="button" data-mail-action="defer-cancel">Cancel</button>
      </div>
      <div class="mail-detail-status ${state.detailStatusTone ? `mail-row-status-${state.detailStatusTone}` : ''}" data-mail-detail-status>${escapeHtml(state.detailStatus || '')}</div>
      <pre class="mail-detail-body" data-mail-detail-body>${escapeHtml(body)}</pre>
      <div class="mail-draft-panel" data-mail-draft-panel hidden>
        <div class="mail-draft-head">
          <strong>Draft Reply</strong>
          <span data-mail-draft-source></span>
        </div>
        <div class="mail-draft-prompt" data-mail-draft-prompt-wrap hidden>
          <label>Prompt</label>
          <textarea data-mail-draft-prompt placeholder="Add context or intent for this reply"></textarea>
        </div>
        <textarea data-mail-draft-text placeholder="Draft reply will appear here"></textarea>
        <div class="mail-draft-actions">
          <button type="button" data-mail-action="draft-generate" hidden>Generate</button>
          <button type="button" data-mail-action="draft-copy">Copy</button>
          <button type="button" data-mail-action="draft-cancel">Cancel</button>
        </div>
      </div>
    </div>
  `;
}

function lockMailRowActions(row) {
  row.querySelectorAll('button').forEach((button) => {
    button.dataset.mailLocked = '1';
    button.disabled = true;
  });
}

function unlockMailRowActions(row) {
  row.querySelectorAll('button').forEach((button) => {
    delete button.dataset.mailLocked;
    button.disabled = false;
  });
}

function closeMailDetailDeferControls() {
  const controls = document.querySelector('[data-mail-detail-defer-controls]');
  if (!controls) return;
  controls.hidden = true;
}

function openMailDetailDeferControls() {
  const controls = document.querySelector('[data-mail-detail-defer-controls]');
  const input = document.querySelector('[data-mail-detail-defer-input]');
  if (!controls || !input) return;
  controls.hidden = false;
  input.value = formatLocalDateTimeInput(new Date(Date.now() + 60 * 60 * 1000));
  if (typeof input.showPicker === 'function') {
    input.showPicker();
  } else {
    input.focus();
  }
}

function setMailDetailStatus(context, text, tone = 'info') {
  const state = getMailViewState(context);
  state.detailStatus = text || '';
  state.detailStatusTone = tone || 'info';
  const status = document.querySelector('[data-mail-detail-status]');
  if (!status) return;
  status.textContent = state.detailStatus;
  status.className = `mail-detail-status ${state.detailStatusTone ? `mail-row-status-${state.detailStatusTone}` : ''}`;
}

function setMailDetailBusy(busy) {
  const root = document.querySelector('[data-mail-detail-root]');
  if (!root) return;
  root.classList.toggle('mail-row-busy', Boolean(busy));
  root.querySelectorAll('button').forEach((button) => {
    if (busy) {
      if (!Object.prototype.hasOwnProperty.call(button.dataset, 'mailPrevDisabled')) {
        button.dataset.mailPrevDisabled = button.disabled ? '1' : '0';
      }
      button.disabled = true;
      return;
    }
    if (Object.prototype.hasOwnProperty.call(button.dataset, 'mailPrevDisabled')) {
      button.disabled = button.dataset.mailPrevDisabled === '1';
      delete button.dataset.mailPrevDisabled;
    }
  });
}

function updateMailCount(context) {
  context.count = context.headers.length;
}

function backToMailList(eventId, context) {
  const e = getEls();
  const state = getMailViewState(context);
  state.mode = 'list';
  state.detailMessage = null;
  state.detailStatus = '';
  state.detailStatusTone = 'info';
  closeMailDetailDeferControls();
  resetMailAssistDraftContext(context);
  renderMailArtifact(eventId, context);
  requestAnimationFrame(() => {
    e.text.scrollTop = Math.max(0, state.listScrollTop || 0);
    const header = context.headers[state.currentIndex];
    if (!header || typeof CSS === 'undefined' || typeof CSS.escape !== 'function') return;
    const openBtn = e.text.querySelector(`tr[data-message-id="${CSS.escape(header.id)}"] button[data-mail-action="open"]`);
    if (openBtn && typeof openBtn.focus === 'function') {
      openBtn.focus();
    }
  });
}

async function openMailDetailAtIndex(eventId, context, index, row) {
  if (index < 0 || index >= context.headers.length) return;
  const state = getMailViewState(context);
  const header = context.headers[index];
  if (!header) return;
  resetMailAssistDraftContext(context);

  if (row) {
    state.listScrollTop = getEls().text.scrollTop;
    setMailRowBusy(row, true);
    setMailRowStatus(row, 'Opening...', 'info');
  } else {
    setMailDetailBusy(true);
    setMailDetailStatus(context, 'Opening...', 'info');
  }

  try {
    const message = await callMailRead(context, header.id);
    if (activeTextEventId !== eventId) return;
    state.mode = 'detail';
    state.currentIndex = index;
    state.detailMessage = message || {};
    state.detailStatus = 'Opened.';
    state.detailStatusTone = 'success';
    renderMailArtifact(eventId, context);

    try {
      await callMailMarkRead(context, header.id);
      if (activeTextEventId !== eventId) return;
      setMailDetailStatus(context, 'Opened. Marked as read.', 'success');
    } catch (markErr) {
      if (activeTextEventId !== eventId) return;
      setMailDetailStatus(context, String(markErr?.message || markErr || 'mark-read failed'), 'warning');
    }
  } catch (err) {
    if (activeTextEventId !== eventId) return;
    if (row && row.isConnected) {
      setMailRowStatus(row, String(err?.message || err || 'open failed'), 'error');
    } else {
      setMailDetailStatus(context, String(err?.message || err || 'open failed'), 'error');
    }
  } finally {
    if (activeTextEventId !== eventId) return;
    if (row && row.isConnected) {
      setMailRowBusy(row, false);
    }
    setMailDetailBusy(false);
  }
}

function applyMailActionState(row, action, result, untilAt) {
  if (result && result.status === 'stub_not_supported') {
    setMailRowStatus(row, 'Defer is not supported for this provider yet.', 'warning');
    return;
  }
  switch (action) {
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

function resolveSwipeAction(dx) {
  if (dx <= SWIPE_LEFT_DELETE_THRESHOLD_PX) return 'delete';
  if (dx <= SWIPE_LEFT_ARCHIVE_THRESHOLD_PX) return 'archive';
  if (dx >= SWIPE_RIGHT_DEFER_THRESHOLD_PX) return 'defer';
  return '';
}

function updateSwipePreview(row, dx) {
  const clamped = Math.max(-SWIPE_MAX_TRANSLATE_PX, Math.min(SWIPE_MAX_TRANSLATE_PX, dx));
  row.style.transform = `translateX(${clamped}px)`;
  row.classList.add('mail-row-swipe-active');
  const action = resolveSwipeAction(clamped);
  row.classList.toggle('mail-row-swipe-archive', action === 'archive');
  row.classList.toggle('mail-row-swipe-delete', action === 'delete');
  row.classList.toggle('mail-row-swipe-defer', action === 'defer');
}

function resetSwipePreview(row) {
  row.style.transform = '';
  row.classList.remove('mail-row-swipe-active', 'mail-row-swipe-archive', 'mail-row-swipe-delete', 'mail-row-swipe-defer');
}

function queueUndoableMailAction(eventId, context, row, action, messageID) {
  flushPendingUndoAction();
  const actionLabel = action === 'delete' ? 'Delete' : 'Archive';
  lockMailRowActions(row);
  row.classList.add(action === 'delete' ? 'mail-row-deleted' : 'mail-row-archived');
  setMailRowStatus(row, `${actionLabel} queued. Undo available for 5 seconds.`, 'info');

  const execute = async () => {
    if (activeTextEventId !== eventId) return;
    setMailRowStatus(row, `Running ${action}...`, 'info');
    try {
      const result = await callMailAction(context, action, messageID, '');
      if (activeTextEventId !== eventId) return;
      applyMailActionState(row, action, result, '');
    } catch (err) {
      if (activeTextEventId !== eventId) return;
      row.classList.remove('mail-row-archived', 'mail-row-deleted');
      unlockMailRowActions(row);
      setMailRowStatus(row, String(err?.message || err || `${action} failed`), 'error');
    }
  };

  const restore = () => {
    if (activeTextEventId !== eventId) return;
    row.classList.remove('mail-row-archived', 'mail-row-deleted');
    unlockMailRowActions(row);
    setMailRowStatus(row, `${actionLabel} canceled.`, 'info');
  };

  const undoID = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  const timerId = setTimeout(() => {
    if (!pendingUndoAction || pendingUndoAction.id !== undoID) return;
    pendingUndoAction = null;
    hideUndoToast();
    void execute();
  }, UNDO_TIMEOUT_MS);

  pendingUndoAction = { id: undoID, timerId, execute, restore };
  showUndoToast(`${actionLabel} scheduled`, () => {
    if (!pendingUndoAction || pendingUndoAction.id !== undoID) return;
    clearTimeout(pendingUndoAction.timerId);
    pendingUndoAction = null;
    hideUndoToast();
    restore();
  });
}

function runImmediateMailAction(eventId, context, row, action, messageID, untilAt) {
  setMailRowBusy(row, true);
  setMailRowStatus(row, `Running ${action}...`, 'info');
  void callMailAction(context, action, messageID, untilAt)
    .then((result) => {
      if (activeTextEventId !== eventId) return;
      applyMailActionState(row, action, result, untilAt);
    })
    .catch((err) => {
      if (activeTextEventId !== eventId) return;
      setMailRowStatus(row, String(err?.message || err || `${action} failed`), 'error');
    })
    .finally(() => {
      if (activeTextEventId !== eventId) return;
      setMailRowBusy(row, false);
    });
}

function setupMailGestureHandlers(eventId, context) {
  const e = getEls();
  let swipe = null;

  const onPointerDown = (ev) => {
    if (ev.button !== 0) return;
    if (ev.target.closest('button, input, textarea, .mail-defer-controls, [data-mail-detail-defer-controls]')) return;
    if (typeof window.getSelection === 'function') {
      const selection = window.getSelection();
      if (selection && selection.rangeCount > 0) {
        selection.removeAllRanges();
      }
    }
    closeReviewCommentPopover();
    ev.preventDefault();
    const state = getMailViewState(context);
    if (state.mode === 'detail') {
      swipe = {
        kind: 'detail',
        pointerId: ev.pointerId,
        startX: ev.clientX,
        dx: 0,
      };
      return;
    }
    const row = ev.target.closest('tr[data-message-id]');
    if (!row) return;
    if (row.classList.contains('mail-row-busy')) return;
    if (row.querySelector('[data-mail-defer-controls]:not([hidden])')) return;
    swipe = {
      kind: 'row',
      row,
      pointerId: ev.pointerId,
      startX: ev.clientX,
      dx: 0,
    };
  };

  const onPointerMove = (ev) => {
    if (!swipe || ev.pointerId !== swipe.pointerId) return;
    swipe.dx = ev.clientX - swipe.startX;
    if (swipe.kind === 'row') {
      updateSwipePreview(swipe.row, swipe.dx);
    }
  };

  const onPointerEnd = (ev) => {
    if (!swipe || ev.pointerId !== swipe.pointerId) return;
    const done = swipe;
    swipe = null;

    if (done.kind === 'detail') {
      const state = getMailViewState(context);
      if (state.mode !== 'detail') return;
      if (done.dx <= -DETAIL_SWIPE_NAV_THRESHOLD_PX) {
        const next = state.currentIndex + 1;
        if (next < context.headers.length) {
          void openMailDetailAtIndex(eventId, context, next, null);
        }
        return;
      }
      if (done.dx >= DETAIL_SWIPE_NAV_THRESHOLD_PX) {
        const prev = state.currentIndex - 1;
        if (prev >= 0) {
          void openMailDetailAtIndex(eventId, context, prev, null);
        }
      }
      return;
    }

    const { row, dx } = done;
    const action = resolveSwipeAction(dx);
    resetSwipePreview(row);
    if (!action) return;
    const messageID = row.dataset.messageId || '';
    if (!messageID) return;
    if (action === 'defer') {
      const supportsNative = context.capabilities ? Boolean(context.capabilities.supports_native_defer) : true;
      if (!supportsNative) {
        setMailRowStatus(row, 'Defer is currently a stub for this provider.', 'warning');
        return;
      }
      openMailDeferControls(row);
      return;
    }
    queueUndoableMailAction(eventId, context, row, action, messageID);
  };

  e.text._mailPointerDownHandler = onPointerDown;
  e.text._mailPointerMoveHandler = onPointerMove;
  e.text._mailPointerUpHandler = onPointerEnd;

  e.text.addEventListener('pointerdown', onPointerDown);
  window.addEventListener('pointermove', onPointerMove);
  window.addEventListener('pointerup', onPointerEnd);
  window.addEventListener('pointercancel', onPointerEnd);
}

function setupMailDetailKeyboardHandlers(eventId, context) {
  const e = getEls();
  if (e.text._mailDetailKeyDownHandler) {
    document.removeEventListener('keydown', e.text._mailDetailKeyDownHandler);
  }
  const onKeyDown = (ev) => {
    if (activeTextEventId !== eventId) return;
    const state = getMailViewState(context);
    if (state.mode !== 'detail') return;
    const tag = String(ev.target?.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select' || ev.target?.isContentEditable) return;

    if (ev.key === 'Escape') {
      ev.preventDefault();
      backToMailList(eventId, context);
      return;
    }
    if (ev.key === 'ArrowLeft' || ev.key === 'k' || ev.key === 'K') {
      ev.preventDefault();
      const prev = state.currentIndex - 1;
      if (prev >= 0) {
        void openMailDetailAtIndex(eventId, context, prev, null);
      }
      return;
    }
    if (ev.key === 'ArrowRight' || ev.key === 'j' || ev.key === 'J') {
      ev.preventDefault();
      const next = state.currentIndex + 1;
      if (next < context.headers.length) {
        void openMailDetailAtIndex(eventId, context, next, null);
      }
    }
  };
  e.text._mailDetailKeyDownHandler = onKeyDown;
  document.addEventListener('keydown', onKeyDown);
}

function setupMailRecordingHandlers(eventId, context) {
  const e = getEls();
  if (e.text._mailRecordingClickHandler) {
    e.text.removeEventListener('click', e.text._mailRecordingClickHandler);
  }
  if (e.text._mailRecordingPointerDownHandler) {
    e.text.removeEventListener('pointerdown', e.text._mailRecordingPointerDownHandler);
  }
  if (e.text._mailRecordingPointerUpHandler) {
    window.removeEventListener('pointerup', e.text._mailRecordingPointerUpHandler);
    window.removeEventListener('pointercancel', e.text._mailRecordingPointerUpHandler);
  }
  if (e.text._mailRecordingKeyDownHandler) {
    document.removeEventListener('keydown', e.text._mailRecordingKeyDownHandler);
  }
  if (e.text._mailRecordingKeyUpHandler) {
    document.removeEventListener('keyup', e.text._mailRecordingKeyUpHandler);
  }

  const isTextInputTarget = (target) => {
    const tag = String(target?.tagName || '').toLowerCase();
    return tag === 'input' || tag === 'textarea' || tag === 'select' || Boolean(target?.isContentEditable);
  };

  const onClick = (ev) => {
    if (activeTextEventId !== eventId) return;
    const modeButton = ev.target.closest('button[data-mail-record-mode]');
    if (modeButton) {
      setMailRecordingMode(context, modeButton.dataset.mailRecordMode);
      return;
    }

    const actionButton = ev.target.closest('button[data-mail-record-action]');
    if (!actionButton) return;
    const action = String(actionButton.dataset.mailRecordAction || '').trim();
    const recording = getMailRecordingState(context);

    if (action === 'stop') {
      stopMailRecording(context, 'click');
      return;
    }
    if (action !== 'trigger') return;

    if (recording.mode === MAIL_RECORDING_MODE.TOGGLE) {
      if (recording.state === MAIL_RECORDING_STATE.RECORDING) {
        stopMailRecording(context, 'click');
      } else {
        startMailRecording(context, MAIL_RECORDING_ORIGIN.TOGGLE_BUTTON);
      }
      return;
    }

    if (recording.state === MAIL_RECORDING_STATE.RECORDING) {
      stopMailRecording(context, 'click');
    }
  };

  const onPointerDown = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (ev.button !== 0) return;
    const trigger = ev.target.closest('button[data-mail-record-action="trigger"]');
    if (!trigger) return;
    const recording = getMailRecordingState(context);
    if (recording.mode !== MAIL_RECORDING_MODE.HOLD) return;
    if (recording.state === MAIL_RECORDING_STATE.RECORDING) return;
    ev.preventDefault();
    recording.holdPointerId = ev.pointerId;
    startMailRecording(context, MAIL_RECORDING_ORIGIN.HOLD_POINTER);
  };

  const onPointerUp = (ev) => {
    if (activeTextEventId !== eventId) return;
    const recording = getMailRecordingState(context);
    if (recording.mode !== MAIL_RECORDING_MODE.HOLD) return;
    if (recording.state !== MAIL_RECORDING_STATE.RECORDING) return;
    if (recording.origin !== MAIL_RECORDING_ORIGIN.HOLD_POINTER) return;
    if (recording.holdPointerId !== null && ev.pointerId !== recording.holdPointerId) return;
    stopMailRecording(context, 'release');
  };

  const onKeyDown = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (ev.key !== ' ') return;
    if (isTextInputTarget(ev.target)) return;
    const recording = getMailRecordingState(context);
    if (recording.state === MAIL_RECORDING_STATE.RECORDING) {
      ev.preventDefault();
      stopMailRecording(context, 'space');
      return;
    }
    if (recording.mode !== MAIL_RECORDING_MODE.HOLD || ev.repeat) return;
    ev.preventDefault();
    startMailRecording(context, MAIL_RECORDING_ORIGIN.HOLD_KEYBOARD);
  };

  const onKeyUp = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (ev.key !== ' ') return;
    if (isTextInputTarget(ev.target)) return;
    const recording = getMailRecordingState(context);
    if (recording.mode !== MAIL_RECORDING_MODE.HOLD) return;
    if (recording.state !== MAIL_RECORDING_STATE.RECORDING) return;
    if (recording.origin !== MAIL_RECORDING_ORIGIN.HOLD_KEYBOARD) return;
    ev.preventDefault();
    stopMailRecording(context, 'release');
  };

  e.text._mailRecordingClickHandler = onClick;
  e.text._mailRecordingPointerDownHandler = onPointerDown;
  e.text._mailRecordingPointerUpHandler = onPointerUp;
  e.text._mailRecordingKeyDownHandler = onKeyDown;
  e.text._mailRecordingKeyUpHandler = onKeyUp;

  e.text.addEventListener('click', onClick);
  e.text.addEventListener('pointerdown', onPointerDown);
  window.addEventListener('pointerup', onPointerUp);
  window.addEventListener('pointercancel', onPointerUp);
  document.addEventListener('keydown', onKeyDown);
  document.addEventListener('keyup', onKeyUp);
}

function setupMailActionHandlers(eventId, context) {
  const e = getEls();
  if (e.text._mailClickHandler) {
    e.text.removeEventListener('click', e.text._mailClickHandler);
  }

  const onClick = (ev) => {
    const button = ev.target.closest('button[data-mail-action]');
    if (!button) return;
    const action = button.dataset.mailAction;
    const state = getMailViewState(context);

    if (action === 'detail-back') {
      backToMailList(eventId, context);
      return;
    }
    if (action === 'detail-prev') {
      const prev = state.currentIndex - 1;
      if (prev >= 0) {
        void openMailDetailAtIndex(eventId, context, prev, null);
      }
      return;
    }
    if (action === 'detail-next') {
      const next = state.currentIndex + 1;
      if (next < context.headers.length) {
        void openMailDetailAtIndex(eventId, context, next, null);
      }
      return;
    }
    if (action === 'draft-cancel') {
      resetMailAssistDraftContext(context);
      return;
    }
    if (action === 'draft-generate') {
      submitPendingDraftPromptCapture();
      return;
    }
    if (action === 'draft-copy') {
      const panel = document.querySelector('[data-mail-draft-panel]');
      const textarea = panel ? panel.querySelector('[data-mail-draft-text]') : null;
      const text = textarea ? textarea.value : '';
      if (text && navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
        void navigator.clipboard.writeText(text);
      }
      return;
    }

    const row = button.closest('tr[data-message-id]');
    const detailRoot = e.text.querySelector('[data-mail-detail-root]');
    const inDetail = Boolean(detailRoot && !row);
    const messageID = row ? (row.dataset.messageId || '') : String(detailRoot?.dataset.messageId || '');
    if (!messageID) return;

    const assistActionId = resolveMailAssistActionID(button, action);
    if (assistActionId) {
      void dispatchMailAssistAction(eventId, context, { actionId: assistActionId, row, inDetail, messageID });
      return;
    }

    if (action === 'defer-cancel') {
      if (inDetail) {
        closeMailDetailDeferControls();
      } else if (row) {
        closeMailDeferControls(row);
      }
      return;
    }

    if (action === 'defer') {
      const supportsNative = context.capabilities ? Boolean(context.capabilities.supports_native_defer) : true;
      if (!supportsNative) {
        if (row) {
          setMailRowStatus(row, 'Defer is currently a stub for this provider.', 'warning');
        } else {
          setMailDetailStatus(context, 'Defer is currently a stub for this provider.', 'warning');
        }
        return;
      }
      if (inDetail) {
        openMailDetailDeferControls();
      } else if (row) {
        openMailDeferControls(row);
      }
      return;
    }

    if (action === 'defer-apply') {
      if (inDetail) {
        const input = document.querySelector('[data-mail-detail-defer-input]');
        if (!input || !input.value) {
          setMailDetailStatus(context, 'Choose a defer date/time first.', 'error');
          return;
        }
        const parsed = new Date(input.value);
        if (Number.isNaN(parsed.getTime())) {
          setMailDetailStatus(context, 'Invalid defer date/time.', 'error');
          return;
        }
        const untilAt = parsed.toISOString();
        setMailDetailBusy(true);
        setMailDetailStatus(context, 'Running defer...', 'info');
        void callMailAction(context, 'defer', messageID, untilAt)
          .then((result) => {
            if (activeTextEventId !== eventId) return;
            if (result && result.status === 'stub_not_supported') {
              setMailDetailStatus(context, 'Defer is not supported for this provider yet.', 'warning');
              return;
            }
            closeMailDetailDeferControls();
            const when = result?.deferred_until_at || untilAt;
            setMailDetailStatus(context, `Deferred until ${formatMailDate(when)}.`, 'success');
          })
          .catch((err) => {
            if (activeTextEventId !== eventId) return;
            setMailDetailStatus(context, String(err?.message || err || 'defer failed'), 'error');
          })
          .finally(() => {
            if (activeTextEventId !== eventId) return;
            setMailDetailBusy(false);
          });
        return;
      }

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
      runImmediateMailAction(eventId, context, row, 'defer', messageID, untilAt);
      return;
    }

    if (action === 'open') {
      if (!row) return;
      const parsedIndex = Number.parseInt(row.dataset.mailIndex || '', 10);
      const idx = Number.isInteger(parsedIndex) ? parsedIndex : findMailHeaderIndex(context, messageID);
      if (idx < 0 || idx >= context.headers.length) {
        setMailRowStatus(row, 'Message not found in current list.', 'error');
        return;
      }
      void openMailDetailAtIndex(eventId, context, idx, row);
      return;
    }

    if (action !== 'archive' && action !== 'delete') {
      return;
    }

    if (inDetail) {
      setMailDetailBusy(true);
      setMailDetailStatus(context, `Running ${action}...`, 'info');
      let navigated = false;
      void callMailAction(context, action, messageID, '')
        .then((result) => {
          if (activeTextEventId !== eventId) return;
          if (result && result.status === 'stub_not_supported') {
            setMailDetailStatus(context, 'Action is not supported for this provider.', 'warning');
            return;
          }
          const currentIndex = state.currentIndex;
          if (currentIndex < 0 || currentIndex >= context.headers.length) {
            setMailDetailStatus(context, 'Action completed.', 'success');
            return;
          }
          context.headers.splice(currentIndex, 1);
          updateMailCount(context);
          if (context.headers.length === 0) {
            state.mode = 'list';
            state.currentIndex = 0;
            state.detailMessage = null;
            renderMailArtifact(eventId, context);
            return;
          }
          const nextIndex = Math.min(currentIndex, context.headers.length - 1);
          state.currentIndex = nextIndex;
          state.detailMessage = null;
          navigated = true;
          void openMailDetailAtIndex(eventId, context, nextIndex, null);
        })
        .catch((err) => {
          if (activeTextEventId !== eventId) return;
          setMailDetailStatus(context, String(err?.message || err || `${action} failed`), 'error');
        })
        .finally(() => {
          if (activeTextEventId !== eventId) return;
          if (!navigated) {
            setMailDetailBusy(false);
          }
        });
      return;
    }

    queueUndoableMailAction(eventId, context, row, action, messageID);
  };

  e.text._mailClickHandler = onClick;
  e.text.addEventListener('click', onClick);
}

function renderMailArtifact(eventId, context) {
  const e = getEls();
  const state = getMailViewState(context);
  clearSubmittedDraftMarksForEvent(eventId);
  e.text.classList.add('mail-artifact');
  if (!context.headers.length) {
    state.mode = 'list';
    state.currentIndex = 0;
  }
  if (state.mode === 'detail') {
    if (state.currentIndex < 0 || state.currentIndex >= context.headers.length) {
      state.currentIndex = Math.max(0, Math.min(state.currentIndex, context.headers.length - 1));
    }
    e.text.innerHTML = renderMailDetailHtml(context);
    setMailAssistDomState(context);
    setMailRecordingDomState(context);
    setupMailRecordingHandlers(eventId, context);
    setupMailActionHandlers(eventId, context);
    setupMailGestureHandlers(eventId, context);
    setupMailDetailKeyboardHandlers(eventId, context);
    setCapabilityHint(context);
    void fetchMailCapabilities(eventId, context);
    return;
  }
  e.text.innerHTML = renderMailListHtml(context);
  setMailAssistDomState(context);
  setMailRecordingDomState(context);
  setupMailRecordingHandlers(eventId, context);
  setupMailActionHandlers(eventId, context);
  setupMailGestureHandlers(eventId, context);
  setCapabilityHint(context);
  void fetchMailCapabilities(eventId, context);
}
function setupTextSelection(eventId) {
  const e = getEls();
  clearSelectionInteractionHandlers();
  let autoPopoverSelectionKey = '';
  let pendingSelectionFinalize = false;

  const clearDraftSelection = () => {
    if (draftMark && draftMark.event_id === eventId) {
      draftMark = null;
      renderDraftOverlay();
      const state = window._tabulaApp?.getState?.();
      sendSelectionFeedback({
        kind: 'mark_clear_draft',
        session_id: state?.sessionId || '',
        artifact_id: eventId,
      });
      closeReviewCommentPopover();
    }
    autoPopoverSelectionKey = '';
  };

  const handleSelection = (finalizeSelection) => {
    if (e.text._reviewPopoverEl && e.text._reviewPopoverSource === 'selection') {
      return;
    }
    const selection = window.getSelection();
    const popover = e.text._reviewPopoverEl;
    const anchorNode = selection?.anchorNode || null;
    if (popover && anchorNode && popover.contains(anchorNode)) {
      return;
    }
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

    if (markType === 'highlight' && finalizeSelection) {
      const key = `${eventId}:${startOffset}:${endOffset}:${text}`;
      if (autoPopoverSelectionKey !== key) {
        autoPopoverSelectionKey = key;
        openReviewCommentPopover(eventId, {
          source: 'selection',
          onCancel: clearDraftSelection,
        });
      }
    }
  };

  const handler = (ev) => {
    if (e.text._selectionRaf) {
      cancelAnimationFrame(e.text._selectionRaf);
    }
    const triggerType = ev?.type || '';
    if (triggerType === 'mouseup' || triggerType === 'keyup') {
      pendingSelectionFinalize = true;
    }
    e.text._selectionRaf = requestAnimationFrame(() => {
      e.text._selectionRaf = null;
      handleSelection(pendingSelectionFinalize);
      pendingSelectionFinalize = false;
    });
  };

  document.addEventListener('selectionchange', handler);
  e.text._selectionHandler = handler;
  e.text._mouseUpSelectionHandler = handler;
  e.text._keyUpSelectionHandler = handler;
  e.text.addEventListener('mouseup', handler);
  e.text.addEventListener('keyup', handler);

  const canUseReviewVoiceCapture = () => (
    Boolean(window.MediaRecorder)
    && Boolean(navigator.mediaDevices)
    && typeof navigator.mediaDevices.getUserMedia === 'function'
  );

  const isInteractiveTarget = (target) => {
    if (!(target instanceof Element)) return true;
    if (!e.text.contains(target)) return true;
    if (target.closest('[data-review-popover]')) return true;
    if (target.closest('button,input,textarea,select,[contenteditable="true"]')) return true;
    if (target.closest('.mail-triage-root,.mail-detail-root,.mail-draft-panel,.mail-record-controls')) return true;
    return false;
  };

  const isTextInputTarget = (target) => {
    if (!(target instanceof Element)) return false;
    return Boolean(target.closest('input,textarea,select,[contenteditable="true"]'));
  };

  const updateReviewAnchorFromPoint = (clientX, clientY) => {
    if (!Number.isFinite(clientX) || !Number.isFinite(clientY)) return;
    const rect = e.text.getBoundingClientRect();
    if (clientX < rect.left || clientX > rect.right || clientY < rect.top || clientY > rect.bottom) return;
    e.text._reviewLastAnchor = { clientX, clientY };
  };

  const resolveReviewVoiceAnchorPoint = () => {
    const prior = e.text._reviewLastAnchor;
    if (prior && Number.isFinite(prior.clientX) && Number.isFinite(prior.clientY)) {
      updateReviewAnchorFromPoint(prior.clientX, prior.clientY);
      if (e.text._reviewLastAnchor) {
        return e.text._reviewLastAnchor;
      }
    }

    const selection = window.getSelection();
    if (selection && selection.isCollapsed && selection.rangeCount > 0) {
      const range = selection.getRangeAt(0);
      if (isSelectionInside(e.text, selection)) {
        const rect = range.getBoundingClientRect();
        if (rect && Number.isFinite(rect.left) && Number.isFinite(rect.top)) {
          const x = rect.left + Math.max(1, Math.min(12, rect.width || 1));
          const y = rect.top + Math.max(1, Math.min(12, rect.height || 1));
          updateReviewAnchorFromPoint(x, y);
          if (e.text._reviewLastAnchor) {
            return e.text._reviewLastAnchor;
          }
        }
      }
    }

    const rect = e.text.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return null;
    const fallback = {
      clientX: rect.left + Math.min(rect.width - 1, Math.max(1, rect.width * 0.5)),
      clientY: rect.top + Math.min(rect.height - 1, Math.max(1, rect.height * 0.5)),
    };
    updateReviewAnchorFromPoint(fallback.clientX, fallback.clientY);
    return e.text._reviewLastAnchor || fallback;
  };

  const createReviewVoiceCapture = (cfg) => ({
    pointerId: cfg.pointerId ?? null,
    source: String(cfg.source || 'long_press'),
    clientX: Number(cfg.clientX || 0),
    clientY: Number(cfg.clientY || 0),
    startX: Number(cfg.startX ?? cfg.clientX ?? 0),
    startY: Number(cfg.startY ?? cfg.clientY ?? 0),
    target: cfg.target || pointTargetFromClientPoint(e.text, cfg.clientX, cfg.clientY),
    longPressTimer: null,
    active: false,
    cancelled: false,
    stopping: false,
    stopRequested: false,
    mediaStream: null,
    mediaRecorder: null,
    sttSessionID: '',
    appendSeq: 0,
    appendChain: Promise.resolve(),
    appendError: '',
    captureBackend: '',
  });

  const stopReviewVoiceMedia = (capture) => {
    if (!capture) return;
    if (capture.mediaRecorder) {
      try {
        if (capture.mediaRecorder.state !== 'inactive') {
          capture.mediaRecorder.stop();
        }
      } catch (_) {
        // no-op
      }
    }
    if (capture.mediaStream && typeof capture.mediaStream.getTracks === 'function') {
      capture.mediaStream.getTracks().forEach((track) => {
        if (track && typeof track.stop === 'function') {
          track.stop();
        }
      });
    }
    capture.mediaRecorder = null;
    capture.mediaStream = null;
  };

  const stopReviewVoiceMediaAndFlush = (capture) => {
    if (!capture?.mediaRecorder) {
      stopReviewVoiceMedia(capture);
      return Promise.resolve();
    }
    const recorder = capture.mediaRecorder;
    if (recorder.state === 'inactive') {
      stopReviewVoiceMedia(capture);
      return Promise.resolve();
    }
    return new Promise((resolve) => {
      const onStop = () => {
        recorder.removeEventListener('error', onError);
        stopReviewVoiceMedia(capture);
        resolve();
      };
      const onError = () => {
        recorder.removeEventListener('stop', onStop);
        stopReviewVoiceMedia(capture);
        resolve();
      };
      recorder.addEventListener('stop', onStop, { once: true });
      recorder.addEventListener('error', onError, { once: true });
      try {
        recorder.stop();
      } catch (_) {
        recorder.removeEventListener('stop', onStop);
        recorder.removeEventListener('error', onError);
        stopReviewVoiceMedia(capture);
        resolve();
      }
    });
  };

  const cleanupReviewVoiceCapture = (capture, cancelRemote) => {
    if (!capture) return;
    if (capture.longPressTimer) {
      clearTimeout(capture.longPressTimer);
      capture.longPressTimer = null;
    }
    if (cancelRemote && capture.sttSessionID) {
      void callPushToPromptAction(null, sttActionCancel, {
        session_id: capture.sttSessionID,
      }).catch(() => {});
    }
    stopReviewVoiceMedia(capture);
    if (e.text._reviewVoiceCapture === capture) {
      e.text._reviewVoiceCapture = null;
    }
  };

  const appendReviewVoiceChunk = async (capture, chunkBlob) => {
    if (!capture?.sttSessionID) return;
    if (!chunkBlob || typeof chunkBlob.arrayBuffer !== 'function' || chunkBlob.size <= 0) return;
    const bytes = new Uint8Array(await chunkBlob.arrayBuffer());
    if (!bytes.length) return;
    const seq = Number(capture.appendSeq || 0);
    capture.appendSeq = seq + 1;
    const payload = {
      session_id: capture.sttSessionID,
      seq,
      audio_base64: base64FromBytes(bytes),
    };
    const chain = capture.appendChain || Promise.resolve();
    capture.appendChain = chain.then(() => callPushToPromptAction(null, sttActionAppend, payload));
    await capture.appendChain;
  };

  const beginReviewVoiceCapture = async (capture) => {
    if (!capture || e.text._reviewVoiceCapture !== capture || capture.cancelled) return;
    capture.longPressTimer = null;
    capture.active = true;
    if (e.text._reviewPopoverEl) {
      closeReviewCommentPopover();
    }
    const recordingHint = capture.source === 'keyboard_ctrl'
      ? 'Recording voice note... release Ctrl to submit.'
      : 'Recording voice note... release to submit.';
    showReviewVoiceHint(recordingHint, 'recording');

    capture.sttSessionID = createPushToPromptSessionID();
    capture.appendSeq = 0;
    capture.appendChain = Promise.resolve();
    capture.appendError = '';
    capture.captureBackend = '';
    const startResp = await callPushToPromptAction(null, sttActionStart, {
      session_id: capture.sttSessionID,
      mime_type: 'audio/webm',
    });
    const captureBackend = String(startResp?.capture_backend || '').trim().toLowerCase();
    capture.captureBackend = captureBackend || 'buffered';
    if (capture.captureBackend === 'daemon') {
      return;
    }
    if (!canUseReviewVoiceCapture()) {
      throw new Error('Microphone capture is unavailable in this browser.');
    }
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    if (e.text._reviewVoiceCapture !== capture || capture.cancelled) {
      if (typeof stream.getTracks === 'function') {
        stream.getTracks().forEach((track) => {
          if (track && typeof track.stop === 'function') {
            track.stop();
          }
        });
      }
      return;
    }
    const recorder = newMediaRecorder(stream);
    capture.mediaStream = stream;
    capture.mediaRecorder = recorder;
    recorder.addEventListener('dataavailable', (ev) => {
      if (!ev?.data || ev.data.size <= 0) return;
      const chain = appendReviewVoiceChunk(capture, ev.data);
      capture.appendChain = chain.catch((err) => {
        capture.appendError = String(err?.message || err || 'audio chunk append failed');
        throw err;
      });
      void capture.appendChain.catch(() => {});
    });
    recorder.start(300);
    if (capture.stopRequested) {
      await stopReviewVoiceMediaAndFlush(capture);
    }
  };

  const stopAndSubmitReviewVoiceCapture = async (capture) => {
    if (!capture || capture.stopping || capture.cancelled) return;
    capture.stopping = true;
    capture.stopRequested = true;
    let stoppedRemotely = false;
    try {
      if (!capture.active) {
        cleanupReviewVoiceCapture(capture, true);
        clearReviewVoiceHint();
        return;
      }
      await stopReviewVoiceMediaAndFlush(capture);
      await (capture.appendChain || Promise.resolve());
      if (capture.appendError) {
        throw new Error(capture.appendError);
      }
      if (!capture.sttSessionID) {
        throw new Error('missing transcription session');
      }
      const stt = await callPushToPromptAction(null, sttActionStop, {
        session_id: capture.sttSessionID,
      });
      stoppedRemotely = true;
      const transcript = String(stt?.text || '').trim();
      if (!transcript) {
        throw new Error('speech recognizer returned empty text');
      }
      submitPointDraftMark(eventId, capture.target, transcript);
      showReviewVoiceHint('Voice note added to draft annotations.', 'ok');
      window.setTimeout(() => {
        if (!e.text._reviewVoiceCapture) {
          clearReviewVoiceHint();
        }
      }, 900);
    } catch (err) {
      const message = String(err?.message || err || 'voice capture failed');
      showReviewVoiceHint(`Voice capture failed: ${message}`, 'error');
      openReviewCommentPopover(eventId, {
        source: 'point',
        contextmenuEvent: { clientX: capture.clientX, clientY: capture.clientY },
      });
    } finally {
      cleanupReviewVoiceCapture(capture, !stoppedRemotely);
    }
  };

  const onReviewLongPressPointerDown = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (e.text.classList.contains('mail-artifact')) return;
    if (!(ev.button === 0 || ev.pointerType === 'touch')) return;
    const target = ev.target;
    if (isInteractiveTarget(target)) return;
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed && isSelectionInside(e.text, selection)) {
      return;
    }
    if (e.text._reviewVoiceCapture) return;
    const capture = createReviewVoiceCapture({
      pointerId: ev.pointerId,
      source: 'long_press',
      clientX: ev.clientX,
      clientY: ev.clientY,
      startX: ev.clientX,
      startY: ev.clientY,
      target: pointTargetFromClientPoint(e.text, ev.clientX, ev.clientY),
    });
    capture.longPressTimer = window.setTimeout(() => {
      void beginReviewVoiceCapture(capture).catch((err) => {
        const message = String(err?.message || err || 'voice capture failed');
        showReviewVoiceHint(`Voice capture failed: ${message}`, 'error');
        cleanupReviewVoiceCapture(capture, true);
      });
    }, REVIEW_LONG_PRESS_MS);
    e.text._reviewVoiceCapture = capture;
  };

  const onReviewLongPressPointerMove = (ev) => {
    const capture = e.text._reviewVoiceCapture;
    if (!capture || capture.pointerId !== ev.pointerId) return;
    if (!capture.longPressTimer) return;
    const dx = ev.clientX - capture.startX;
    const dy = ev.clientY - capture.startY;
    const distance = Math.hypot(dx, dy);
    if (distance < REVIEW_LONG_PRESS_MOVE_TOLERANCE_PX) return;
    capture.cancelled = true;
    cleanupReviewVoiceCapture(capture, false);
    clearReviewVoiceHint();
  };

  const onReviewLongPressPointerUp = (ev) => {
    const capture = e.text._reviewVoiceCapture;
    if (!capture) return;
    if (capture.source !== 'long_press' || capture.pointerId !== ev.pointerId) return;
    if (capture.longPressTimer) {
      capture.cancelled = true;
      cleanupReviewVoiceCapture(capture, false);
      clearReviewVoiceHint();
      return;
    }
    if (!capture.active) {
      capture.cancelled = true;
      cleanupReviewVoiceCapture(capture, false);
      clearReviewVoiceHint();
      return;
    }
    void stopAndSubmitReviewVoiceCapture(capture);
  };

  e.text._reviewLongPressPointerDownHandler = onReviewLongPressPointerDown;
  e.text._reviewLongPressPointerMoveHandler = onReviewLongPressPointerMove;
  e.text._reviewLongPressPointerUpHandler = onReviewLongPressPointerUp;
  e.text.addEventListener('pointerdown', onReviewLongPressPointerDown);
  window.addEventListener('pointermove', onReviewLongPressPointerMove);
  window.addEventListener('pointerup', onReviewLongPressPointerUp);
  window.addEventListener('pointercancel', onReviewLongPressPointerUp);

  const onReviewAnchorPointer = (ev) => {
    if (activeTextEventId !== eventId) return;
    const target = ev.target instanceof Element ? ev.target : null;
    if (!target || !e.text.contains(target)) return;
    if (target.closest('[data-review-popover]')) return;
    updateReviewAnchorFromPoint(ev.clientX, ev.clientY);
  };
  e.text._reviewAnchorPointerHandler = onReviewAnchorPointer;
  e.text.addEventListener('mousemove', onReviewAnchorPointer);
  e.text.addEventListener('pointerdown', onReviewAnchorPointer);

  const onReviewCtrlKeyDown = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (ev.key !== 'Control' || ev.repeat) return;
    if (e.text.classList.contains('mail-artifact')) return;
    if (e.text._reviewVoiceCapture) return;
    if (ev.metaKey || ev.altKey) return;
    if (isTextInputTarget(ev.target instanceof Element ? ev.target : null)) return;
    const anchor = resolveReviewVoiceAnchorPoint();
    if (!anchor) return;

    const capture = createReviewVoiceCapture({
      pointerId: null,
      source: 'keyboard_ctrl',
      clientX: anchor.clientX,
      clientY: anchor.clientY,
      startX: anchor.clientX,
      startY: anchor.clientY,
      target: pointTargetFromClientPoint(e.text, anchor.clientX, anchor.clientY),
    });
    e.text._reviewVoiceCapture = capture;
    ev.preventDefault();
    void beginReviewVoiceCapture(capture).catch((err) => {
      const message = String(err?.message || err || 'voice capture failed');
      showReviewVoiceHint(`Voice capture failed: ${message}`, 'error');
      cleanupReviewVoiceCapture(capture, true);
    });
  };

  const onReviewCtrlKeyUp = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (ev.key !== 'Control') return;
    const capture = e.text._reviewVoiceCapture;
    if (!capture || capture.source !== 'keyboard_ctrl') return;
    ev.preventDefault();
    if (capture.longPressTimer || !capture.active) {
      capture.cancelled = true;
      cleanupReviewVoiceCapture(capture, false);
      clearReviewVoiceHint();
      return;
    }
    void stopAndSubmitReviewVoiceCapture(capture);
  };

  const onReviewCtrlWindowBlur = () => {
    const capture = e.text._reviewVoiceCapture;
    if (!capture || capture.source !== 'keyboard_ctrl') return;
    if (capture.active) {
      void stopAndSubmitReviewVoiceCapture(capture);
      return;
    }
    capture.cancelled = true;
    cleanupReviewVoiceCapture(capture, true);
    clearReviewVoiceHint();
  };
  e.text._reviewCtrlKeyDownHandler = onReviewCtrlKeyDown;
  e.text._reviewCtrlKeyUpHandler = onReviewCtrlKeyUp;
  e.text._reviewCtrlBlurHandler = onReviewCtrlWindowBlur;
  document.addEventListener('keydown', onReviewCtrlKeyDown, true);
  document.addEventListener('keyup', onReviewCtrlKeyUp, true);
  window.addEventListener('blur', onReviewCtrlWindowBlur);

  const onContextMenu = (ev) => {
    if (activeTextEventId !== eventId) return;
    const target = ev.target;
    if (!(target instanceof Element)) return;
    if (!e.text.contains(target)) return;
    if (target.closest('[data-review-popover]')) return;
    if (target.closest('button,input,textarea,select,[contenteditable="true"]')) return;
    ev.preventDefault();
    ev.stopPropagation();
    const activeCapture = e.text._reviewVoiceCapture;
    if (activeCapture) {
      activeCapture.cancelled = true;
      cleanupReviewVoiceCapture(activeCapture, true);
      clearReviewVoiceHint();
    }
    if (e.text._reviewPopoverEl) {
      closeReviewCommentPopover();
    }
    if (shouldSuppressNextReviewOpen(e.text)) {
      return;
    }
    const hit = findSubmittedMarkAtPoint(e.text, eventId, ev.clientX, ev.clientY);
    if (hit && hit.local_id) {
      openReviewCommentPopover(eventId, {
        source: 'existing',
        existingMark: hit,
      });
      return;
    }
    openReviewCommentPopover(eventId, {
      source: 'point',
      contextmenuEvent: ev,
    });
  };
  e.text._reviewContextMenuHandler = onContextMenu;
  e.text.addEventListener('contextmenu', onContextMenu, true);

  const onExistingMarkClick = (ev) => {
    if (activeTextEventId !== eventId) return;
    if (ev.button !== 0) return;
    const target = ev.target;
    if (!(target instanceof Element)) return;
    if (target.closest('[data-review-popover]')) return;
    if (shouldSuppressNextReviewOpen(e.text)) {
      ev.preventDefault();
      ev.stopPropagation();
      return;
    }
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed && isSelectionInside(e.text, selection)) {
      return;
    }
    const hit = findSubmittedMarkAtPoint(e.text, eventId, ev.clientX, ev.clientY);
    if (!hit) return;
    ev.preventDefault();
    ev.stopPropagation();
    if (e.text._reviewPopoverEl) {
      closeReviewCommentPopover();
      return;
    }
    openReviewCommentPopover(eventId, {
      source: 'existing',
      existingMark: hit,
    });
  };
  e.text._reviewExistingMarkClickHandler = onExistingMarkClick;
  e.text.addEventListener('click', onExistingMarkClick);

  const onReviewHoverMove = (ev) => {
    if (activeTextEventId !== eventId) {
      e.text.style.cursor = '';
      return;
    }
    const hit = findSubmittedMarkAtPoint(e.text, eventId, ev.clientX, ev.clientY);
    e.text.style.cursor = hit ? 'pointer' : '';
  };
  const onReviewHoverLeave = () => {
    e.text.style.cursor = '';
  };
  e.text._reviewHoverMoveHandler = onReviewHoverMove;
  e.text._reviewHoverLeaveHandler = onReviewHoverLeave;
  e.text.addEventListener('mousemove', onReviewHoverMove);
  e.text.addEventListener('mouseleave', onReviewHoverLeave);

  if (e.text._scrollHandler) {
    e.text.removeEventListener('scroll', e.text._scrollHandler);
  }
  e.text._scrollHandler = () => {
    renderDraftOverlay();
    closeReviewCommentPopover();
  };
  e.text.addEventListener('scroll', e.text._scrollHandler);
}

function setupPdfOverlay() {
  const e = getEls();
  const hitRoot = e.pdf._pdfHitLayer || e.pdf;
  if (e.pdf._pdfClickHandler) {
    hitRoot.removeEventListener('click', e.pdf._pdfClickHandler);
    e.pdf._pdfClickHandler = null;
  }
  if (e.pdf._pdfHoverMoveHandler) {
    hitRoot.removeEventListener('mousemove', e.pdf._pdfHoverMoveHandler);
    e.pdf._pdfHoverMoveHandler = null;
  }
  if (e.pdf._pdfHoverLeaveHandler) {
    hitRoot.removeEventListener('mouseleave', e.pdf._pdfHoverLeaveHandler);
    e.pdf._pdfHoverLeaveHandler = null;
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
    const localMarkID = nextSubmittedDraftMarkID();
    const half = Math.round(POINT_COMMENT_MARK_SIZE_PX / 2);
    const markRect = [x - half, y - half, POINT_COMMENT_MARK_SIZE_PX, POINT_COMMENT_MARK_SIZE_PX];

    sendSelectionFeedback({
      kind: 'mark_set',
      session_id: (window._tabulaApp?.getState?.().sessionId) || '',
      artifact_id: activePdfEvent.event_id,
      mark_id: localMarkID,
      intent: 'draft',
      type: 'comment_point',
      target_kind: 'pdf_point',
      target: { page, x, y, rect: markRect },
      comment,
    });

    addSubmittedDraftMark({
      local_id: localMarkID,
      event_id: activePdfEvent.event_id,
      type: 'comment_point',
      line_start: 1,
      line_end: 1,
      start_offset: 0,
      end_offset: 0,
      rects: [markRect],
      comment,
    });

    const marker = document.createElement('div');
    marker.className = 'canvas-mark-rect canvas-mark-comment_point';
    marker.style.left = `${x - half}px`;
    marker.style.top = `${y - half}px`;
    marker.style.width = `${POINT_COMMENT_MARK_SIZE_PX}px`;
    marker.style.height = `${POINT_COMMENT_MARK_SIZE_PX}px`;
    marker.style.position = 'absolute';
    marker.style.pointerEvents = 'none';
    if (window.getComputedStyle(e.pdf).position === 'static') {
      e.pdf.style.position = 'relative';
    }
    e.pdf.appendChild(marker);
  };
  const hoverMoveHandler = (ev) => {
    if (!activePdfEvent) {
      hitRoot.style.cursor = '';
      return;
    }
    const hit = findSubmittedMarkAtPoint(e.pdf, activePdfEvent.event_id, ev.clientX, ev.clientY);
    hitRoot.style.cursor = hit ? 'pointer' : '';
  };
  const hoverLeaveHandler = () => {
    hitRoot.style.cursor = '';
  };
  e.pdf._pdfClickHandler = clickHandler;
  e.pdf._pdfHoverMoveHandler = hoverMoveHandler;
  e.pdf._pdfHoverLeaveHandler = hoverLeaveHandler;
  hitRoot.addEventListener('click', clickHandler);
  hitRoot.addEventListener('mousemove', hoverMoveHandler);
  hitRoot.addEventListener('mouseleave', hoverLeaveHandler);
}

function renderPdfSurface(event) {
  const e = getEls();
  const pdfState = (window._tabulaApp || {}).getState ? window._tabulaApp.getState() : {};
  const pdfSid = pdfState.sessionId || '';
  const pdfURL = `/api/files/${encodeURIComponent(pdfSid)}/${encodeURIComponent(event.path)}`;
  e.pdf.innerHTML = '';

  const surface = document.createElement('div');
  surface.className = 'canvas-pdf-surface';
  const objectEl = document.createElement('object');
  objectEl.className = 'canvas-pdf-object';
  objectEl.type = 'application/pdf';
  objectEl.data = pdfURL;
  const fallback = document.createElement('div');
  fallback.className = 'canvas-pdf-fallback';
  fallback.innerHTML = `PDF preview unavailable. <a href="${pdfURL}" target="_blank" rel="noopener noreferrer">Open PDF</a>`;
  const hitLayer = document.createElement('div');
  hitLayer.className = 'canvas-pdf-hit-layer';
  objectEl.appendChild(fallback);
  surface.appendChild(objectEl);
  surface.appendChild(hitLayer);
  e.pdf.appendChild(surface);
  e.pdf._pdfHitLayer = hitLayer;
}

function clearPdfMarkers() {
  const e = getEls();
  if (!e.pdf) return;
  e.pdf.querySelectorAll('.canvas-mark-rect').forEach((node) => node.remove());
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
    clearSubmittedDraftMarksForEvent(event.event_id);
    const mailContext = normalizeMailHeadersContext(event);
    if (mailContext) {
      activeMailContext = mailContext;
      renderMailArtifact(event.event_id, mailContext);
      setupTextSelection(event.event_id);
      renderDraftOverlay();
      return;
    }
    activeMailContext = null;
    const { text: markdownText, stash: mathSegments } = extractMathSegments(event.text || '');
    const renderedMarkdownHtml = marked.parse(markdownText);
    e.text.innerHTML = restoreMathSegments(sanitizeHtml(renderedMarkdownHtml), mathSegments);
    typesetMarkdownMath(e.text);
    setupTextSelection(event.event_id);
    renderDraftOverlay();
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
    submittedDraftMarks = [];
    clearOverlay();
  } else if (event.kind === 'pdf_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.pdf.style.display = '';
    void renderPdfSurface(event);
    e.title.textContent = event.title || 'PDF';
    e.mode.textContent = 'review';
    e.mode.className = 'badge review';
    activeTextEventId = null;
    activePdfEvent = event;
    draftMark = null;
    submittedDraftMarks = [];
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
  submittedDraftMarks = [];
  clearOverlay();
}

export function initCanvasControls() {
  const e = getEls();
  const commitBtn = document.getElementById('btn-canvas-commit');
  const clearBtn = document.getElementById('btn-canvas-clear-draft');
  const runCommit = async () => {
    if (!commitBtn) return;
    if (commitBtn.dataset.busy === '1') return;
    const originalText = commitBtn.textContent || 'Commit';
    commitBtn.dataset.busy = '1';
    commitBtn.disabled = true;
    commitBtn.textContent = 'Committing...';
    try {
      await commitCanvasDraft();
      commitBtn.textContent = 'Committed';
    } catch (err) {
      console.error('canvas commit failed:', err);
      commitBtn.textContent = 'Commit failed';
    } finally {
      window.setTimeout(() => {
        delete commitBtn.dataset.busy;
        commitBtn.disabled = false;
        commitBtn.textContent = originalText;
      }, 900);
    }
  };

  if (commitBtn) {
    commitBtn.addEventListener('click', runCommit);

	    if (!document.__tabulaCommitShortcutHandler) {
	      const shortcutHandler = (ev) => {
	        const isCommitShortcut = ev.key === 'Enter' && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && !ev.altKey;
	        if (!isCommitShortcut) return;

	        if (document.querySelector('[data-review-popover="true"]')) {
	          return;
	        }
	        const activeEl = document.activeElement;
	        if (activeEl && activeEl.matches('input,textarea,select,[contenteditable="true"]')) {
	          return;
	        }
	        if (activeEl && document.getElementById('terminal-container')?.contains(activeEl)) {
	          return;
	        }
        const artifactID = activeArtifactIDForCommit();
        if (!artifactID) return;

        ev.preventDefault();
        void runCommit();
      };
      document.addEventListener('keydown', shortcutHandler, true);
      document.__tabulaCommitShortcutHandler = shortcutHandler;
    }
  }

  if (clearBtn) {
    clearBtn.addEventListener('click', () => {
      const { getState } = window._tabulaApp || {};
      if (!getState) return;
      const state = getState();
      if (!state.canvasWs || state.canvasWs.readyState !== WebSocket.OPEN) return;
      const artifactID = activeArtifactIDForCommit();
      state.canvasWs.send(JSON.stringify({
        kind: 'mark_clear_draft',
        session_id: state.sessionId,
        artifact_id: artifactID,
      }));
      draftMark = null;
      clearSubmittedDraftMarksForEvent(artifactID);
      if (activePdfEvent && artifactID === activePdfEvent.event_id) {
        clearPdfMarkers();
      }
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
