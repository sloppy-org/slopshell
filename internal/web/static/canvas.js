import { marked } from './vendor/marked.esm.js';
import {
  normalizeMailHeadersContext,
  renderMailArtifact,
  clearMailInteractionHandlers,
  setActiveMailContext,
} from './canvas-mail.js';

const FORTRAN_KEYWORDS = [
  'program', 'module', 'contains', 'implicit', 'none',
  'integer', 'real', 'double', 'precision', 'complex', 'logical', 'character', 'type',
  'subroutine', 'function', 'call',
  'if', 'then', 'else', 'elseif', 'select', 'case', 'where',
  'do', 'enddo', 'end', 'stop', 'return', 'cycle', 'exit',
  'allocate', 'deallocate', 'parameter', 'intent', 'in', 'out', 'inout',
  'use', 'only', 'private', 'public', 'interface', 'elemental', 'pure', 'recursive',
];

export function escapeHtml(text) {
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

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_MATH_SEGMENT_';

export function getEls() {
  if (!els.text) {
    els.text = document.getElementById('canvas-text');
    els.image = document.getElementById('canvas-image');
    els.img = document.getElementById('canvas-img');
    els.pdf = document.getElementById('canvas-pdf');
  }
  return els;
}

export function sanitizeHtml(html) {
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

export function hideAll() {
  const e = getEls();
  if (e.text) e.text.style.display = 'none';
  if (e.image) e.image.style.display = 'none';
  if (e.pdf) e.pdf.style.display = 'none';
  if (e.text) e.text.classList.remove('is-active');
  if (e.image) e.image.classList.remove('is-active');
  if (e.pdf) e.pdf.classList.remove('is-active');
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

function getActiveArtifactTitle() {
  const state = window._taburaApp?.getState?.();
  if (!state?.artifactTabs) return '';
  const tab = state.artifactTabs.find(t => t.id === activeTextEventId);
  return tab?.title || '';
}

export function getActiveTextEventId() {
  return activeTextEventId;
}

export function getLocationFromPoint(clientX, clientY) {
  const e = getEls();
  if (!e.text || !activeTextEventId) return null;
  const range = textRangeFromClientPoint(clientX, clientY);
  if (!range || !e.text.contains(range.startContainer)) return null;
  try {
    const startProbe = range.cloneRange();
    startProbe.selectNodeContents(e.text);
    startProbe.setEnd(range.startContainer, range.startOffset);
    const offset = startProbe.toString().length;
    const lines = (e.text.textContent || '').split('\n');
    const line = lineFromOffset(lines, offset);
    const title = getActiveArtifactTitle();
    return { line, title };
  } catch (_) {
    return null;
  }
}

export function getLocationFromSelection() {
  const e = getEls();
  if (!e.text || !activeTextEventId) return null;
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !isSelectionInside(e.text, selection)) return null;
  const selectedText = selection.toString().trim();
  if (!selectedText) return null;
  const range = selection.getRangeAt(0);
  const { startOffset } = getSelectionOffsets(e.text, range);
  const lines = (e.text.textContent || '').split('\n');
  const line = lineFromOffset(lines, startOffset);
  const title = getActiveArtifactTitle();
  return { line, selectedText, title };
}

export function showTransientMarker(clientX, clientY) {
  clearTransientMarker();
  const e = getEls();
  if (!e.text) return;
  const rootRect = e.text.getBoundingClientRect();
  const x = clientX - rootRect.left + e.text.scrollLeft;
  const y = clientY - rootRect.top + e.text.scrollTop;
  const marker = document.createElement('div');
  marker.className = 'transient-marker';
  marker.style.left = `${x - 4}px`;
  marker.style.top = `${y - 4}px`;
  if (window.getComputedStyle(e.text).position === 'static') {
    e.text.style.position = 'relative';
  }
  e.text.appendChild(marker);
}

export function clearTransientMarker() {
  const e = getEls();
  if (!e.text) return;
  e.text.querySelectorAll('.transient-marker').forEach(el => el.remove());
}

function clearTextInteractionHandlers() {
  clearMailInteractionHandlers();
}

function renderPdfSurface(event) {
  const e = getEls();
  const pdfState = (window._taburaApp || {}).getState ? window._taburaApp.getState() : {};
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

export function renderCanvas(event) {
  const e = getEls();

  if (event.kind === 'text_artifact') {
    hideAll();
    e.text.style.display = '';
    e.text.classList.add('is-active');
    clearTextInteractionHandlers();
    activeTextEventId = event.event_id;
    activePdfEvent = null;
    const mailContext = normalizeMailHeadersContext(event);
    if (mailContext) {
      setActiveMailContext(mailContext);
      renderMailArtifact(event.event_id, mailContext);
      return;
    }
    const { text: markdownText, stash: mathSegments } = extractMathSegments(event.text || '');
    const renderedMarkdownHtml = marked.parse(markdownText);
    e.text.innerHTML = restoreMathSegments(sanitizeHtml(renderedMarkdownHtml), mathSegments);
    typesetMarkdownMath(e.text);
  } else if (event.kind === 'image_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.image.style.display = '';
    e.image.classList.add('is-active');
    const state = (window._taburaApp || {}).getState ? window._taburaApp.getState() : {};
    const sid = state.sessionId || '';
    e.img.src = `/api/files/${encodeURIComponent(sid)}/${encodeURIComponent(event.path)}`;
    e.img.alt = event.title || 'Image';
    activeTextEventId = null;
    activePdfEvent = null;
  } else if (event.kind === 'pdf_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.pdf.style.display = '';
    e.pdf.classList.add('is-active');
    void renderPdfSurface(event);
    activeTextEventId = null;
    activePdfEvent = event;
  } else if (event.kind === 'clear_canvas') {
    clearTextInteractionHandlers();
    clearCanvas();
  }
}

export function clearCanvas() {
  clearTextInteractionHandlers();
  hideAll();
  activeTextEventId = null;
  activePdfEvent = null;
}

export function initCanvasControls() {
  // No-op: mark type selector removed.
  // Artifact interaction is wired in app.js bindUi().
}
