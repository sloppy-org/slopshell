import { marked } from './vendor/marked.esm.js';
import {
  getLocationFromPoint,
  getLocationFromSelection,
  escapeHtml,
  sanitizeHtml,
} from './canvas.js';

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_MATH_SEGMENT_';

const uiState = {
  mode: 'rasa',
  recording: false,
  overlayVisible: false,
  overlayTurnId: null,
  inputAnchor: null,
  inputVisible: false,
  indicatorVisible: false,
  indicatorMode: '',
  lastInputX: 0,
  lastInputY: 0,
  lastInputPaneId: '',
  lastInputPaneLocalX: 0,
  lastInputPaneLocalY: 0,
};

const renderer = new marked.Renderer();
renderer.code = ({ text, lang }) => {
  const safeLang = escapeHtml((lang || 'plaintext').toLowerCase());
  return `<pre><code class="language-${safeLang}">${escapeHtml(text || '')}</code></pre>\n`;
};
marked.setOptions({ breaks: true, renderer });

function extractMathSegments(source) {
  const s = String(source || '');
  const stash = [];
  let text = s;
  const patterns = [
    /\$\$[\s\S]+?\$\$/g,
    /\\\[[\s\S]+?\\\]/g,
    /\\\([\s\S]+?\\\)/g,
  ];
  for (const p of patterns) {
    text = text.replace(p, (seg) => {
      const token = `${MATH_SEGMENT_TOKEN_PREFIX}${stash.length}@@`;
      stash.push(seg);
      return token;
    });
  }
  return { text, stash };
}

function restoreMathSegments(html, stash) {
  let out = String(html || '');
  for (let i = 0; i < stash.length; i++) {
    out = out.replaceAll(`${MATH_SEGMENT_TOKEN_PREFIX}${i}@@`, escapeHtml(String(stash[i] || '')));
  }
  return out;
}

function renderMarkdown(md) {
  const { text, stash } = extractMathSegments(md);
  const rendered = marked.parse(text || '');
  return restoreMathSegments(sanitizeHtml(rendered), stash);
}

export function getUiState() {
  return uiState;
}

export function setUiMode(mode) {
  uiState.mode = mode === 'artifact' ? 'artifact' : 'rasa';
}

function indicatorEl() {
  return document.getElementById('indicator');
}

function inputEl() {
  return document.getElementById('floating-input');
}

function activeCanvasPaneEl() {
  return document.querySelector('#canvas-viewport .canvas-pane.is-active');
}

function setPaneAnchor(x, y) {
  const pane = activeCanvasPaneEl();
  if (!(pane instanceof HTMLElement)) {
    uiState.lastInputPaneId = '';
    return;
  }
  const rect = pane.getBoundingClientRect();
  if (x < rect.left || x > rect.right || y < rect.top || y > rect.bottom) {
    uiState.lastInputPaneId = '';
    return;
  }
  uiState.lastInputPaneId = pane.id || '';
  uiState.lastInputPaneLocalX = x - rect.left + pane.scrollLeft;
  uiState.lastInputPaneLocalY = y - rect.top + pane.scrollTop;
}

function paneAnchoredPosition() {
  const paneID = String(uiState.lastInputPaneId || '').trim();
  if (!paneID) return null;
  const pane = document.getElementById(paneID);
  if (!(pane instanceof HTMLElement)) return null;
  const rect = pane.getBoundingClientRect();
  return {
    x: rect.left + uiState.lastInputPaneLocalX - pane.scrollLeft,
    y: rect.top + uiState.lastInputPaneLocalY - pane.scrollTop,
  };
}

function overlayEl() {
  return document.getElementById('overlay');
}

function overlayContentEl() {
  const ol = overlayEl();
  return ol ? ol.querySelector('.overlay-content') : null;
}

export function showIndicatorMode(mode, x, y) {
  const el = indicatorEl();
  if (!el) return;
  const normalizedMode = String(mode || '').trim().toLowerCase();
  let nextMode;
  if (normalizedMode === 'recording') nextMode = 'recording';
  else if (normalizedMode === 'listening') nextMode = 'listening';
  else if (normalizedMode === 'paused') nextMode = 'paused';
  else nextMode = 'play';
  const body = document.body;
  el.classList.remove('is-recording', 'is-working', 'is-listening', 'is-paused');
  if (nextMode === 'recording') {
    el.classList.add('is-recording');
  } else if (nextMode === 'listening') {
    el.classList.add('is-listening');
  } else if (nextMode === 'paused') {
    el.classList.add('is-paused');
  } else {
    el.classList.add('is-working');
  }
  el.style.display = '';
  el.style.position = 'fixed';
  el.style.inset = '';
  el.style.width = '';
  el.style.height = '';
  el.style.left = '';
  el.style.top = '';
  el.style.right = '';
  el.style.bottom = '';
  el.style.maxWidth = '';
  el.style.maxHeight = '';
  el.style.transform = '';
  el.style.translate = '';
  // Recording dot appears at tap point; stop square stays in top-right (CSS default).
  const dot = el.querySelector('.record-dot');
  if (dot) {
    if (nextMode === 'recording' && Number.isFinite(x) && Number.isFinite(y)) {
      dot.style.top = `${Math.round(y)}px`;
      dot.style.right = '';
      dot.style.left = `${Math.round(x)}px`;
      dot.style.transform = 'translate(-50%, -50%)';
    } else {
      dot.style.top = '';
      dot.style.right = '';
      dot.style.left = '';
      dot.style.transform = '';
    }
  }
  if (body) {
    const isCueVisible = nextMode === 'recording' || nextMode === 'play' || nextMode === 'listening' || nextMode === 'paused';
    body.classList.toggle('cue-active', isCueVisible);
  }
  uiState.indicatorVisible = true;
  uiState.indicatorMode = nextMode;
}

export function hideIndicator() {
  const el = indicatorEl();
  if (!el) return;
  el.classList.remove('is-recording', 'is-working', 'is-listening', 'is-paused');
  el.style.display = 'none';
  const body = document.body;
  if (body) {
    body.classList.remove('cue-active');
  }
  uiState.indicatorVisible = false;
  uiState.indicatorMode = '';
}

export function showTextInput(x, y, anchor) {
  const el = inputEl();
  if (!el) return;
  uiState.inputAnchor = anchor || null;
  uiState.inputVisible = true;
  setLastInputPosition(x, y);
  el.style.display = '';
  el.style.left = `${Math.min(x, window.innerWidth - 280)}px`;
  el.style.top = `${Math.min(y, window.innerHeight - 60)}px`;
  el.value = '';
  el.focus();
}

export function hideTextInput() {
  const el = inputEl();
  if (!el) return;
  if (document.activeElement === el) {
    try { el.blur(); } catch (_) {}
  }
  el.style.display = 'none';
  uiState.inputVisible = false;
  uiState.inputAnchor = null;
}

export function showOverlay(x, y) {
  const el = overlayEl();
  if (!el) return;
  const content = overlayContentEl();
  if (content) content.innerHTML = '';
  const cx = typeof x === 'number' ? x : window.innerWidth / 2 - 150;
  const cy = typeof y === 'number' ? y : window.innerHeight / 3;
  el.style.left = `${Math.max(8, Math.min(cx, window.innerWidth - 320))}px`;
  el.style.top = `${Math.max(8, Math.min(cy, window.innerHeight - 200))}px`;
  el.style.display = '';
  uiState.overlayVisible = true;
}

export function hideOverlay() {
  const el = overlayEl();
  if (!el) return;
  el.style.display = 'none';
  uiState.overlayVisible = false;
  uiState.overlayTurnId = null;
}

export function updateOverlay(markdownText) {
  const content = overlayContentEl();
  if (!content) return;
  content.innerHTML = renderMarkdown(markdownText);
  const ol = overlayEl();
  if (ol) {
    ol.scrollTop = ol.scrollHeight;
  }
  const mj = window.MathJax;
  if (mj && typeof mj.typesetPromise === 'function') {
    void mj.typesetPromise([content]).catch(() => {});
  }
}

export function isOverlayVisible() {
  return uiState.overlayVisible;
}

export function isTextInputVisible() {
  return uiState.inputVisible;
}

export function isRecording() {
  return uiState.recording;
}

export function setRecording(active) {
  const next = Boolean(active);
  uiState.recording = next;
  const body = document.body;
  if (body) {
    body.classList.toggle('cue-active', next);
  }
}

export function getInputAnchor() {
  return uiState.inputAnchor;
}

export function setInputAnchor(anchor) {
  uiState.inputAnchor = anchor || null;
}

export function getAnchorFromPoint(clientX, clientY) {
  const loc = getLocationFromPoint(clientX, clientY);
  if (!loc) return null;
  return { ...loc, x: clientX, y: clientY };
}

export function getAnchorFromSelection() {
  const loc = getLocationFromSelection();
  if (!loc) return null;
  return { ...loc, selectedText: loc.selectedText };
}

export function buildContextPrefix(anchor) {
  if (!anchor) return '';
  const page = Number.parseInt(String(anchor.page || ''), 10);
  const line = Number.parseInt(String(anchor.line || ''), 10);
  const hasPage = Number.isFinite(page) && page > 0;
  const hasLine = Number.isFinite(line) && line > 0;
  const title = String(anchor.title || '').trim();
  const quotedTitle = title || 'artifact';
  const selectionSuffix = anchor.selectedText ? `: "${anchor.selectedText}"` : '';

  if (hasPage && hasLine) {
    return `[Page ${page}, line ${line} of "${quotedTitle}"${selectionSuffix}]`;
  }
  if (hasPage) {
    return `[Page ${page} of "${quotedTitle}"${selectionSuffix}]`;
  }
  if (hasLine) {
    return `[Line ${line} of "${quotedTitle}"${selectionSuffix}]`;
  }
  return '';
}

export function getLastInputPosition() {
  const anchored = paneAnchoredPosition();
  if (anchored) return anchored;
  return { x: uiState.lastInputX, y: uiState.lastInputY };
}

export function setLastInputPosition(x, y) {
  uiState.lastInputX = x;
  uiState.lastInputY = y;
  setPaneAnchor(x, y);
}
