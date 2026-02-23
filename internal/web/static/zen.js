import { marked } from './vendor/marked.esm.js';
import {
  getLocationFromPoint,
  getLocationFromSelection,
  showLineHighlight,
  clearLineHighlight,
  escapeHtml,
  sanitizeHtml,
  getActiveTextEventId,
} from './canvas.js';

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_ZEN_MATH_SEGMENT_';

const zenState = {
  mode: 'rasa',
  recording: false,
  overlayVisible: false,
  overlayTurnId: null,
  inputAnchor: null,
  inputVisible: false,
  indicatorVisible: false,
  lastInputX: 0,
  lastInputY: 0,
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

export function getZenState() {
  return zenState;
}

export function setZenMode(mode) {
  zenState.mode = mode === 'artifact' ? 'artifact' : 'rasa';
}

function indicatorEl() {
  return document.getElementById('zen-indicator');
}

function inputEl() {
  return document.getElementById('zen-input');
}

function overlayEl() {
  return document.getElementById('zen-overlay');
}

function overlayContentEl() {
  const ol = overlayEl();
  return ol ? ol.querySelector('.zen-overlay-content') : null;
}

export function showStopIndicator(x, y) {
  const el = indicatorEl();
  if (!el) return;
  el.style.display = '';
  el.style.left = `${x}px`;
  el.style.top = `${y}px`;
  zenState.indicatorVisible = true;
}

export function hideStopIndicator() {
  const el = indicatorEl();
  if (!el) return;
  el.style.display = 'none';
  zenState.indicatorVisible = false;
}

export function showTextInput(x, y, anchor) {
  const el = inputEl();
  if (!el) return;
  zenState.inputAnchor = anchor || null;
  zenState.inputVisible = true;
  zenState.lastInputX = x;
  zenState.lastInputY = y;
  el.style.display = '';
  el.style.left = `${Math.min(x, window.innerWidth - 280)}px`;
  el.style.top = `${Math.min(y, window.innerHeight - 60)}px`;
  el.value = '';
  el.focus();
}

export function hideTextInput() {
  const el = inputEl();
  if (!el) return;
  el.style.display = 'none';
  zenState.inputVisible = false;
  zenState.inputAnchor = null;
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
  zenState.overlayVisible = true;
}

export function hideOverlay() {
  const el = overlayEl();
  if (!el) return;
  el.style.display = 'none';
  zenState.overlayVisible = false;
  zenState.overlayTurnId = null;
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
  return zenState.overlayVisible;
}

export function isTextInputVisible() {
  return zenState.inputVisible;
}

export function isRecording() {
  return zenState.recording;
}

export function setRecording(active) {
  zenState.recording = Boolean(active);
}

export function getInputAnchor() {
  return zenState.inputAnchor;
}

export function setInputAnchor(anchor) {
  zenState.inputAnchor = anchor || null;
}

export function getAnchorFromPoint(clientX, clientY) {
  if (!getActiveTextEventId()) return null;
  const loc = getLocationFromPoint(clientX, clientY);
  if (!loc) return null;
  return { line: loc.line, title: loc.title, x: clientX, y: clientY };
}

export function getAnchorFromSelection() {
  if (!getActiveTextEventId()) return null;
  const loc = getLocationFromSelection();
  if (!loc) return null;
  return { line: loc.line, title: loc.title, selectedText: loc.selectedText };
}

export function buildContextPrefix(anchor) {
  if (!anchor) return '';
  if (anchor.selectedText) {
    return `[Line ${anchor.line} of "${anchor.title}": "${anchor.selectedText}"]`;
  }
  return `[Line ${anchor.line} of "${anchor.title}"]`;
}

export function getLastInputPosition() {
  return { x: zenState.lastInputX, y: zenState.lastInputY };
}
