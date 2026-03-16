import { AnnotationLayer, GlobalWorkerOptions, TextLayer, getDocument } from './vendor/pdf.mjs';
import { apiURL } from './paths.js';
import { renderTextArtifact, sanitizeHtml } from './canvas-content.js';
import { openMailDraftArtifact, renderMailDraftArtifact } from './app-mail-drafts.js';
import { renderMailTriageArtifact } from './app-mail-triage.js';
import { buildEmailThreadHTML } from './app-item-sidebar-artifacts.js';
import {
  renderCanvasApprovalActions,
  renderCanvasArtifactActions,
} from './canvas-actions.js';

export { escapeHtml, sanitizeHtml } from './canvas-content.js';
export { resolveCanvasApprovalRequest } from './canvas-actions.js';

const PDFJS_WORKER_URL = new URL('./vendor/pdf.worker.mjs', import.meta.url).toString();
const PDFJS_STANDARD_FONTS_URL = new URL('./vendor/standard_fonts/', import.meta.url).toString();

const els: Record<string, HTMLElement | null> = {};
let activeTextEventId = null;
let activeArtifactTitle = '';
let activePdfEvent = null;
let previousArtifactText = '';
let previousBlockTexts = [];
let previousArtifactTitle = '';
const PDF_MIN_RENDER_WIDTH_PX = 240;
const PDF_MAX_RENDER_SCALE = 3;
const PDF_RESIZE_DEBOUNCE_MS = 120;

GlobalWorkerOptions.workerSrc = PDFJS_WORKER_URL;

const pdfRenderState = {
  generation: 0,
  key: '',
  doc: null,
  loadingTask: null,
  resizeObserver: null,
  resizeTimer: null,
  lastWidth: 0,
  renderTasks: new Set<any>(),
  textLayers: new Set<any>(),
};

function dispatchCanvasRendered(event) {
  document.dispatchEvent(new CustomEvent('tabura:canvas-rendered', {
    detail: {
      kind: event?.kind || '',
      title: event?.title || '',
      path: event?.path || '',
      event_id: event?.event_id || '',
    },
  }));
}

function dispatchCanvasCleared() {
  document.dispatchEvent(new CustomEvent('tabura:canvas-cleared'));
}

export function getEls() {
  if (!els.text) {
    els.text = document.getElementById('canvas-text');
    els.image = document.getElementById('canvas-image');
    els.img = document.getElementById('canvas-img');
    els.pdf = document.getElementById('canvas-pdf');
    ensurePdfResizeObserver();
  }
  return els;
}

function clearPdfResizeTimer() {
  if (pdfRenderState.resizeTimer) {
    window.clearTimeout(pdfRenderState.resizeTimer);
    pdfRenderState.resizeTimer = null;
  }
}

function clearPdfRenderArtifacts() {
  for (const task of pdfRenderState.renderTasks) {
    if (task && typeof task.cancel === 'function') {
      try { task.cancel(); } catch (_) {}
    }
  }
  pdfRenderState.renderTasks.clear();
  for (const layer of pdfRenderState.textLayers) {
    if (layer && typeof layer.cancel === 'function') {
      try { layer.cancel(); } catch (_) {}
    }
  }
  pdfRenderState.textLayers.clear();
}

function cancelPdfRender({ destroyDocument = false } = {}) {
  pdfRenderState.generation += 1;
  clearPdfResizeTimer();
  clearPdfRenderArtifacts();
  if (pdfRenderState.loadingTask && typeof pdfRenderState.loadingTask.destroy === 'function') {
    try {
      void pdfRenderState.loadingTask.destroy();
    } catch (_) {}
  }
  pdfRenderState.loadingTask = null;
  if (destroyDocument && pdfRenderState.doc && typeof pdfRenderState.doc.destroy === 'function') {
    try {
      void pdfRenderState.doc.destroy();
    } catch (_) {}
    pdfRenderState.doc = null;
    pdfRenderState.key = '';
  }
}

function getPdfContainerWidth(container) {
  if (!(container instanceof HTMLElement)) return PDF_MIN_RENDER_WIDTH_PX;
  const rect = container.getBoundingClientRect();
  const measured = Number.isFinite(rect.width) ? rect.width : container.clientWidth;
  return Math.max(PDF_MIN_RENDER_WIDTH_PX, Math.floor(measured || PDF_MIN_RENDER_WIDTH_PX));
}

function schedulePdfRerender() {
  if (!activePdfEvent) return;
  clearPdfResizeTimer();
  pdfRenderState.resizeTimer = window.setTimeout(() => {
    pdfRenderState.resizeTimer = null;
    if (!activePdfEvent) return;
    const e = getEls();
    if (!e.pdf || !e.pdf.classList.contains('is-active')) return;
    void renderPdfSurface(activePdfEvent, { reuseDocument: true });
  }, PDF_RESIZE_DEBOUNCE_MS);
}

function ensurePdfResizeObserver() {
  const pdfPane = els.pdf;
  if (!pdfPane || pdfRenderState.resizeObserver) return;
  if (typeof ResizeObserver !== 'function') {
    window.addEventListener('resize', schedulePdfRerender);
    return;
  }
  pdfRenderState.resizeObserver = new ResizeObserver((entries) => {
    if (!Array.isArray(entries) || entries.length === 0) return;
    const width = getPdfContainerWidth(entries[0].target);
    if (Math.abs(width - pdfRenderState.lastWidth) < 4) return;
    pdfRenderState.lastWidth = width;
    schedulePdfRerender();
  });
  pdfRenderState.resizeObserver.observe(pdfPane);
}

export function hideAll() {
  cancelPdfRender({ destroyDocument: false });
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

function compactAnchorText(raw, maxChars = 240) {
  const text = String(raw || '').trim();
  if (!text) return '';
  const collapsed = text.replace(/\s+/g, ' ').trim();
  if (collapsed.length <= maxChars) return collapsed;
  return `${collapsed.slice(0, maxChars)}...`;
}

function surroundingTextForLine(lines, line) {
  const lineNumber = Number.parseInt(String(line || ''), 10);
  if (!Array.isArray(lines) || lines.length === 0 || !Number.isFinite(lineNumber) || lineNumber <= 0) {
    return '';
  }
  const start = Math.max(0, lineNumber - 2);
  const end = Math.min(lines.length, lineNumber + 1);
  return lines
    .slice(start, end)
    .map((entry, index) => `${start + index + 1}: ${String(entry || '')}`)
    .join('\n')
    .trim();
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

function textRangeFromPointInRoot(root, clientX, clientY) {
  const direct = textRangeFromClientPoint(clientX, clientY);
  if (!(root instanceof HTMLElement)) return direct;
  if (direct && root.contains(direct.startContainer)) return direct;

  const rect = root.getBoundingClientRect();
  if (clientX < rect.left || clientX > rect.right || clientY < rect.top || clientY > rect.bottom) {
    return direct;
  }

  const probeY = Math.max(rect.top + 1, Math.min(clientY, rect.bottom - 1));
  const probeXs = [
    Math.max(rect.left + 1, Math.min(clientX, rect.right - 1)),
    Math.max(rect.left + 1, Math.min(rect.left + 8, rect.right - 1)),
  ];
  for (const probeX of probeXs) {
    const probe = textRangeFromClientPoint(probeX, probeY);
    if (probe && root.contains(probe.startContainer)) {
      return probe;
    }
  }
  return direct;
}

function estimateTextLineAtPoint(root, clientY) {
  if (!(root instanceof HTMLElement)) return null;
  const range = document.createRange();
  range.selectNodeContents(root);
  const rects = Array.from(range.getClientRects()).filter((rect) => rect.width > 0 && rect.height > 0);
  if (rects.length === 0) return null;

  const lineRects = [];
  const topEpsilonPx = 1.5;
  for (const rect of rects) {
    const existing = lineRects.find((line) => Math.abs(line.top - rect.top) <= topEpsilonPx);
    if (existing) {
      existing.top = Math.min(existing.top, rect.top);
      existing.bottom = Math.max(existing.bottom, rect.bottom);
      existing.height = Math.max(existing.height, rect.height);
      continue;
    }
    lineRects.push({
      top: rect.top,
      bottom: rect.bottom,
      height: rect.height,
    });
  }
  lineRects.sort((a, b) => a.top - b.top);

  let nearestIndex = -1;
  let nearestDistance = Infinity;
  for (let i = 0; i < lineRects.length; i += 1) {
    const rect = lineRects[i];
    let distance = 0;
    if (clientY < rect.top) distance = rect.top - clientY;
    else if (clientY > rect.bottom) distance = clientY - rect.bottom;
    if (distance < nearestDistance) {
      nearestDistance = distance;
      nearestIndex = i;
    }
  }
  if (nearestIndex < 0) return null;
  return {
    line: nearestIndex + 1,
    top: lineRects[nearestIndex].top,
    height: lineRects[nearestIndex].height,
  };
}

export function getActiveArtifactTitle() {
  return activeArtifactTitle;
}

export function getActiveTextEventId() {
  return activeTextEventId;
}

function parsePositiveInt(raw) {
  const n = Number.parseInt(String(raw || '').trim(), 10);
  if (!Number.isFinite(n) || n <= 0) return null;
  return n;
}

function getPdfPageNodeFromNode(node) {
  const start = node instanceof Element ? node : node?.parentElement;
  if (!(start instanceof Element)) return null;
  const page = start.closest('.canvas-pdf-page');
  return page instanceof HTMLElement ? page : null;
}

function getPdfPageNodeFromPoint(pdfRoot, clientX, clientY) {
  if (!(pdfRoot instanceof HTMLElement)) return null;
  const hit = document.elementFromPoint(clientX, clientY);
  if (hit instanceof Element && pdfRoot.contains(hit)) {
    const page = hit.closest('.canvas-pdf-page');
    if (page instanceof HTMLElement) return page;
  }
  const pages = pdfRoot.querySelectorAll('.canvas-pdf-page');
  for (const page of pages) {
    if (!(page instanceof HTMLElement)) continue;
    const rect = page.getBoundingClientRect();
    if (clientX < rect.left || clientX > rect.right || clientY < rect.top || clientY > rect.bottom) continue;
    return page;
  }
  return null;
}

function estimatePdfLineAtPoint(pageNode, clientX, clientY) {
  if (!(pageNode instanceof HTMLElement)) return null;
  const textLayer = pageNode.querySelector('.textLayer');
  if (!(textLayer instanceof HTMLElement)) return null;
  const spans = textLayer.querySelectorAll('span');
  if (spans.length === 0) return null;

  const lineTops = [];
  let nearestTop = null;
  let nearestDistance = Infinity;
  const topEpsilonPx = 1.5;

  for (const span of spans) {
    if (!(span instanceof HTMLElement)) continue;
    if (!(span.textContent || '').trim()) continue;
    const rect = span.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) continue;

    const clampedX = Math.max(rect.left, Math.min(clientX, rect.right));
    const clampedY = Math.max(rect.top, Math.min(clientY, rect.bottom));
    const distance = ((clampedX - clientX) ** 2) + ((clampedY - clientY) ** 2);
    if (distance < nearestDistance) {
      nearestDistance = distance;
      nearestTop = rect.top;
    }

    if (!lineTops.some((top) => Math.abs(top - rect.top) <= topEpsilonPx)) {
      lineTops.push(rect.top);
    }
  }

  if (!Number.isFinite(nearestTop) || lineTops.length === 0) return null;
  lineTops.sort((a, b) => a - b);
  const index = lineTops.findIndex((top) => Math.abs(top - nearestTop) <= topEpsilonPx);
  if (index < 0) return null;
  return index + 1;
}

function getPdfAnchorFromPoint(clientX, clientY) {
  const e = getEls();
  if (!e.pdf || !activePdfEvent || !e.pdf.classList.contains('is-active')) return null;

  const pageNode = getPdfPageNodeFromPoint(e.pdf, clientX, clientY);
  if (!(pageNode instanceof HTMLElement)) return null;
  const page = parsePositiveInt(pageNode.dataset.page);
  if (!page) return null;

  const title = getActiveArtifactTitle();
  const line = estimatePdfLineAtPoint(pageNode, clientX, clientY);
  if (line) {
    return { page, line, title };
  }
  return { page, title };
}

function getPdfAnchorFromRange(range) {
  if (!(range instanceof Range)) return null;
  const pageNode = getPdfPageNodeFromNode(range.startContainer);
  const page = parsePositiveInt(pageNode?.dataset?.page || '');
  if (!page) return null;
  const title = getActiveArtifactTitle();
  const rangeRect = range.getBoundingClientRect();
  if (rangeRect.width > 0 || rangeRect.height > 0) {
    const x = rangeRect.left + Math.max(1, rangeRect.width / 2);
    const y = rangeRect.top + Math.max(1, rangeRect.height / 2);
    const line = estimatePdfLineAtPoint(pageNode, x, y);
    if (line) {
      return { page, line, title };
    }
  }
  return { page, title };
}

function getDiffAnchorContext(node) {
  const start = node instanceof Element ? node : node?.parentElement;
  if (!(start instanceof Element)) return null;
  const lineEl = start.closest('.hl-diff-line');
  if (!(lineEl instanceof HTMLElement)) return null;
  const fileLineRaw = String(lineEl.dataset.fileLine || '').trim();
  const fileLine = Number.parseInt(fileLineRaw, 10);
  const diffLineRaw = String(lineEl.dataset.diffLine || '').trim();
  const diffLine = Number.parseInt(diffLineRaw, 10);
  const resolvedLine = Number.isFinite(fileLine) && fileLine > 0
    ? fileLine
    : (Number.isFinite(diffLine) && diffLine > 0 ? diffLine : null);
  if (!resolvedLine) return null;
  const path = String(lineEl.dataset.filePath || '').trim();
  return {
    line: resolvedLine,
    title: path || getActiveArtifactTitle(),
    surroundingText: compactAnchorText(lineEl.textContent || ''),
  };
}

function getMarkdownSourceAnchorContext(node) {
  const start = node instanceof Element ? node : node?.parentElement;
  if (!(start instanceof Element)) return null;
  const sourceEl = start.closest('[data-source-line]');
  if (!(sourceEl instanceof HTMLElement)) return null;
  const lineRaw = String(sourceEl.dataset.sourceLine || '').trim();
  const line = Number.parseInt(lineRaw, 10);
  if (!Number.isFinite(line) || line <= 0) return null;
  return {
    line,
    title: getActiveArtifactTitle(),
    surroundingText: compactAnchorText(sourceEl.textContent || ''),
  };
}

export function getLocationFromPoint(clientX, clientY) {
  const e = getEls();
  const range = textRangeFromPointInRoot(e.text, clientX, clientY);
  if (e.text && activeTextEventId && range && e.text.contains(range.startContainer)) {
    const diffAnchor = getDiffAnchorContext(range.startContainer);
    if (diffAnchor) return diffAnchor;
    const markdownAnchor = getMarkdownSourceAnchorContext(range.startContainer);
    if (markdownAnchor) return markdownAnchor;
    try {
      const startProbe = range.cloneRange();
      startProbe.selectNodeContents(e.text);
      startProbe.setEnd(range.startContainer, range.startOffset);
      const offset = startProbe.toString().length;
      const lines = (e.text.textContent || '').split('\n');
      const line = lineFromOffset(lines, offset);
      const title = getActiveArtifactTitle();
      return { line, title, surroundingText: surroundingTextForLine(lines, line) };
    } catch (_) {
      return null;
    }
  }
  if (e.text && activeTextEventId) {
    const rect = e.text.getBoundingClientRect();
    if (clientX >= rect.left && clientX <= rect.right && clientY >= rect.top && clientY <= rect.bottom) {
      const estimate = estimateTextLineAtPoint(e.text, clientY);
      if (estimate) {
        const lines = (e.text.textContent || '').split('\n');
        return {
          line: estimate.line,
          title: getActiveArtifactTitle(),
          surroundingText: surroundingTextForLine(lines, estimate.line),
        };
      }
    }
  }
  if (e.image && e.image.classList.contains('is-active') && e.img instanceof HTMLImageElement) {
    const rect = e.img.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0 && clientX >= rect.left && clientX <= rect.right && clientY >= rect.top && clientY <= rect.bottom) {
      return {
        title: getActiveArtifactTitle(),
        relativeX: (clientX - rect.left) / rect.width,
        relativeY: (clientY - rect.top) / rect.height,
      };
    }
  }
  return getPdfAnchorFromPoint(clientX, clientY);
}

function getTextSelectionLocation(e, selection, selectedText) {
  const range = selection.getRangeAt(0);
  const diffAnchor = getDiffAnchorContext(range.startContainer);
  if (diffAnchor) {
    return { ...diffAnchor, selectedText };
  }
  const markdownAnchor = getMarkdownSourceAnchorContext(range.startContainer);
  if (markdownAnchor) {
    return { ...markdownAnchor, selectedText };
  }
  const { startOffset } = getSelectionOffsets(e.text, range);
  const lines = (e.text.textContent || '').split('\n');
  const line = lineFromOffset(lines, startOffset);
  const title = getActiveArtifactTitle();
  return {
    line,
    selectedText,
    title,
    surroundingText: surroundingTextForLine(lines, line),
  };
}

export function getLocationFromSelection() {
  const e = getEls();
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed) return null;
  const selectedText = selection.toString().trim();
  if (!selectedText) return null;
  if (e.text && activeTextEventId && isSelectionInside(e.text, selection)) {
    return getTextSelectionLocation(e, selection, selectedText);
  }
  if (e.pdf && activePdfEvent && isSelectionInside(e.pdf, selection)) {
    const range = selection.getRangeAt(0);
    const pdfAnchor = getPdfAnchorFromRange(range);
    if (pdfAnchor) {
      return { ...pdfAnchor, selectedText };
    }
  }
  return null;
}

export function showLineHighlight(clientX, clientY) {
  clearLineHighlight();
  const e = getEls();
  if (!e.text) return;
  const range = textRangeFromPointInRoot(e.text, clientX, clientY);
  let rangeRect = null;
  if (range && e.text.contains(range.startContainer)) {
    rangeRect = range.getBoundingClientRect();
  } else {
    const estimate = estimateTextLineAtPoint(e.text, clientY);
    if (!estimate) return;
    const rootRect = e.text.getBoundingClientRect();
    rangeRect = {
      top: estimate.top,
      height: estimate.height,
      bottom: estimate.top + estimate.height,
      left: rootRect.left,
      right: rootRect.right,
    };
  }
  const rootRect = e.text.getBoundingClientRect();
  const top = rangeRect.top - rootRect.top + e.text.scrollTop;
  const lineHeight = rangeRect.height || parseFloat(window.getComputedStyle(e.text).lineHeight) || 22;
  if (window.getComputedStyle(e.text).position === 'static') {
    e.text.style.position = 'relative';
  }
  const highlight = document.createElement('div');
  highlight.className = 'review-line-highlight';
  highlight.style.top = `${top}px`;
  highlight.style.height = `${lineHeight}px`;
  e.text.appendChild(highlight);
}

export function clearLineHighlight() {
  const e = getEls();
  if (!e.text) return;
  e.text.querySelectorAll('.review-line-highlight').forEach(el => el.remove());
}

function clearTextInteractionHandlers() {
}

function getPdfURL(event) {
  const pdfState = (window._taburaApp || {}).getState ? window._taburaApp.getState() : {};
  const pdfSid = String(pdfState.sessionId || '');
  const pdfPath = String(event?.path || '');
  return {
    sid: pdfSid,
    path: pdfPath,
    url: apiURL(`files/${encodeURIComponent(pdfSid)}/${encodeURIComponent(pdfPath)}`),
  };
}

function renderPdfFallback(host, pdfURL, message) {
  host.innerHTML = '';
  const fallback = document.createElement('div');
  fallback.className = 'canvas-pdf-fallback';
  fallback.append(`${message} `);
  const link = document.createElement('a');
  link.href = pdfURL;
  link.target = '_blank';
  link.rel = 'noopener noreferrer';
  link.textContent = 'Open PDF';
  fallback.appendChild(link);
  host.appendChild(fallback);
}

function isPdfCancellationError(err) {
  if (!err) return false;
  const name = String(err.name || '');
  if (name === 'AbortException' || name === 'RenderingCancelledException') return true;
  const message = String(err.message || '').toLowerCase();
  return message.includes('cancel');
}

function createPdfLinkService() {
  return {
    externalLinkEnabled: true,
    addLinkAttributes(link, url, newWindow = false) {
      if (!(link instanceof HTMLAnchorElement)) return;
      link.href = String(url || '');
      link.target = '_blank';
      link.rel = 'noopener noreferrer';
    },
    getDestinationHash(dest) {
      if (typeof dest === 'string') return `#${dest}`;
      return '#';
    },
    getAnchorUrl(anchor) {
      return String(anchor || '#');
    },
    goToDestination() {},
    goToPage() {},
    setHash() {},
    executeNamedAction() {},
  };
}

async function loadPdfDocument(pdfKey, pdfURL, token, reuseDocument) {
  if (reuseDocument && pdfRenderState.doc && pdfRenderState.key === pdfKey) {
    return pdfRenderState.doc;
  }
  if (pdfRenderState.loadingTask && typeof pdfRenderState.loadingTask.destroy === 'function') {
    try {
      await pdfRenderState.loadingTask.destroy();
    } catch (_) {}
  }
  pdfRenderState.loadingTask = null;
  if (pdfRenderState.doc && pdfRenderState.key !== pdfKey && typeof pdfRenderState.doc.destroy === 'function') {
    try {
      await pdfRenderState.doc.destroy();
    } catch (_) {}
    pdfRenderState.doc = null;
  }

  pdfRenderState.key = pdfKey;
  const loadingTask = getDocument({
    url: pdfURL,
    withCredentials: true,
    standardFontDataUrl: PDFJS_STANDARD_FONTS_URL,
    useSystemFonts: true,
    isEvalSupported: false,
  });
  pdfRenderState.loadingTask = loadingTask;

  const doc = await loadingTask.promise;
  if (token !== pdfRenderState.generation) {
    if (typeof doc.destroy === 'function') {
      try {
        await doc.destroy();
      } catch (_) {}
    }
    return null;
  }
  pdfRenderState.doc = doc;
  if (pdfRenderState.loadingTask === loadingTask) {
    pdfRenderState.loadingTask = null;
  }
  return doc;
}

async function renderPdfPages(pdfDoc, pagesHost, statusNode, token) {
  if (!pdfDoc || !pagesHost) return;
  const pageCount = Number(pdfDoc.numPages || 0);
  if (pageCount < 1) return;

  clearPdfRenderArtifacts();
  pagesHost.innerHTML = '';
  statusNode.textContent = 'Preparing PDF...';
  const firstPage = await pdfDoc.getPage(1);
  if (token !== pdfRenderState.generation) return;
  const baseViewport = firstPage.getViewport({ scale: 1 });
  const targetWidth = getPdfContainerWidth(pagesHost);
  pdfRenderState.lastWidth = targetWidth;
  const scale = Math.max(0.2, Math.min(PDF_MAX_RENDER_SCALE, targetWidth / Math.max(1, baseViewport.width)));

  const outputScale = Math.min(2, window.devicePixelRatio || 1);
  const linkService = createPdfLinkService();
  for (let pageIndex = 1; pageIndex <= pageCount; pageIndex += 1) {
    if (token !== pdfRenderState.generation) return;
    statusNode.textContent = `Rendering page ${pageIndex} / ${pageCount}...`;
    const page = pageIndex === 1 ? firstPage : await pdfDoc.getPage(pageIndex);
    const viewport = page.getViewport({ scale });
    const renderViewport = page.getViewport({ scale: scale * outputScale });
    const pageNode = document.createElement('section');
    pageNode.className = 'canvas-pdf-page';
    pageNode.dataset.page = String(pageIndex);

    const pageInner = document.createElement('div');
    pageInner.className = 'canvas-pdf-page-inner';
    pageInner.style.width = `${Math.floor(viewport.width)}px`;
    pageInner.style.height = `${Math.floor(viewport.height)}px`;

    const canvas = document.createElement('canvas');
    canvas.className = 'canvas-pdf-canvas';
    canvas.width = Math.max(1, Math.floor(renderViewport.width));
    canvas.height = Math.max(1, Math.floor(renderViewport.height));
    canvas.style.width = `${Math.floor(viewport.width)}px`;
    canvas.style.height = `${Math.floor(viewport.height)}px`;
    pageInner.appendChild(canvas);

    const textLayerNode = document.createElement('div');
    textLayerNode.className = 'textLayer canvas-pdf-text-layer';
    textLayerNode.style.setProperty('--scale-factor', `${viewport.scale}`);
    pageInner.appendChild(textLayerNode);

    const annotationLayerNode = document.createElement('div');
    annotationLayerNode.className = 'annotationLayer canvas-pdf-annotation-layer';
    annotationLayerNode.style.setProperty('--scale-factor', `${viewport.scale}`);
    pageInner.appendChild(annotationLayerNode);

    pageNode.appendChild(pageInner);
    pagesHost.appendChild(pageNode);

    const context = canvas.getContext('2d', { alpha: false });
    if (!context) continue;
    const renderTask = page.render({
      canvasContext: context,
      viewport: renderViewport,
      annotationMode: 0,
    });
    pdfRenderState.renderTasks.add(renderTask);
    try {
      await renderTask.promise;
    } catch (err) {
      if (!isPdfCancellationError(err)) throw err;
      return;
    } finally {
      pdfRenderState.renderTasks.delete(renderTask);
    }
    if (token !== pdfRenderState.generation) return;

    const textContent = await page.getTextContent({ includeMarkedContent: true });
    if (token !== pdfRenderState.generation) return;
    const textLayer = new TextLayer({
      textContentSource: textContent,
      container: textLayerNode,
      viewport,
    });
    pdfRenderState.textLayers.add(textLayer);
    try {
      await textLayer.render();
    } catch (err) {
      if (!isPdfCancellationError(err)) throw err;
      return;
    } finally {
      pdfRenderState.textLayers.delete(textLayer);
    }
    if (token !== pdfRenderState.generation) return;

    const annotations = await page.getAnnotations({ intent: 'display' });
    if (Array.isArray(annotations) && annotations.length > 0) {
      const annotationLayer = new AnnotationLayer({
        div: annotationLayerNode,
        accessibilityManager: null,
        annotationCanvasMap: null,
        annotationEditorUIManager: null,
        page,
        viewport: viewport.clone({ dontFlip: true }),
        structTreeLayer: null,
      });
      await annotationLayer.render({
        annotations,
        div: annotationLayerNode,
        page,
        viewport: viewport.clone({ dontFlip: true }),
        linkService,
        annotationStorage: pdfDoc.annotationStorage,
        renderForms: true,
        enableScripting: false,
      });
    } else {
      annotationLayerNode.hidden = true;
    }
    if (token !== pdfRenderState.generation) return;

    if (typeof page.cleanup === 'function') {
      page.cleanup();
    }
  }
  statusNode.classList.add('is-hidden');
}

async function renderPdfSurface(event, options: Record<string, any> = {}) {
  const e = getEls();
  if (!e.pdf) return;
  const { sid, path, url } = getPdfURL(event);
  if (!path) {
    pdfRenderState.lastWidth = getPdfContainerWidth(e.pdf);
    renderPdfFallback(e.pdf, url, 'PDF path missing.');
    return;
  }

  const token = pdfRenderState.generation + 1;
  pdfRenderState.generation = token;
  clearPdfResizeTimer();
  clearPdfRenderArtifacts();
  e.pdf.innerHTML = '';

  const surface = document.createElement('div');
  surface.className = 'canvas-pdf-surface';
  const status = document.createElement('div');
  status.className = 'canvas-pdf-status';
  status.textContent = 'Loading PDF...';
  const pagesHost = document.createElement('div');
  pagesHost.className = 'canvas-pdf-pages';
  surface.appendChild(status);
  surface.appendChild(pagesHost);
  e.pdf.appendChild(surface);

  const pdfKey = `${sid}:${path}`;
  const reuseDocument = options.reuseDocument !== false;
  try {
    const pdfDoc = await loadPdfDocument(pdfKey, url, token, reuseDocument);
    if (!pdfDoc || token !== pdfRenderState.generation) return;
    await renderPdfPages(pdfDoc, pagesHost, status, token);
    dispatchCanvasRendered(event);
  } catch (err) {
    if (token !== pdfRenderState.generation) return;
    if (isPdfCancellationError(err)) return;
    console.warn('PDF render failed:', err);
    pdfRenderState.lastWidth = getPdfContainerWidth(e.pdf);
    renderPdfFallback(e.pdf, url, 'PDF preview unavailable.');
    dispatchCanvasRendered(event);
  }
}

export function renderCanvas(event) {
  const e = getEls();

  if (event.kind === 'text_artifact') {
    hideAll();
    e.text.style.display = '';
    e.text.classList.add('is-active');
    e.text.classList.remove('mail-draft-canvas');
    e.text.classList.remove('mail-triage-canvas');
    delete e.text.dataset.approvalRequestId;
    clearTextInteractionHandlers();
    activeTextEventId = event.event_id;
    activePdfEvent = null;
    const threadMeta = event.threadMeta && typeof event.threadMeta === 'object' ? event.threadMeta : null;
    const threadHtml = threadMeta ? buildEmailThreadHTML(event.title, threadMeta) : null;
    if (threadHtml) {
      e.text.innerHTML = sanitizeHtml(threadHtml);
      e.text.querySelectorAll('.thread-draft-open').forEach((link) => {
        link.addEventListener('click', (ev) => {
          ev.preventDefault();
          const draftId = Number((link as HTMLElement).dataset.draftId || 0);
          if (draftId > 0) void openMailDraftArtifact(draftId);
        });
      });
      activeArtifactTitle = String(event.title || '');
      previousArtifactText = event.text || '';
      previousBlockTexts = [];
      previousArtifactTitle = activeArtifactTitle;
    } else {
      const nextState = renderTextArtifact(e.text, event, {
        previousArtifactTitle,
        previousBlockTexts,
      });
      activeArtifactTitle = nextState.activeArtifactTitle;
      previousArtifactText = nextState.previousArtifactText;
      previousBlockTexts = nextState.previousBlockTexts;
      previousArtifactTitle = nextState.previousArtifactTitle;
    }
    renderCanvasArtifactActions(e.text, event);
    renderCanvasApprovalActions(e.text, event);
    dispatchCanvasRendered(event);
  } else if (event.kind === 'email_draft') {
    hideAll();
    e.text.style.display = '';
    e.text.classList.add('is-active');
    e.text.classList.remove('mail-triage-canvas');
    clearTextInteractionHandlers();
    activeTextEventId = null;
    activePdfEvent = null;
    activeArtifactTitle = event.title || '';
    previousArtifactText = String(event?.draft?.body || '');
    previousBlockTexts = [];
    previousArtifactTitle = activeArtifactTitle;
    renderMailDraftArtifact(e.text, event);
    dispatchCanvasRendered(event);
  } else if (event.kind === 'email_triage') {
    hideAll();
    e.text.style.display = '';
    e.text.classList.add('is-active');
    e.text.classList.remove('mail-draft-canvas');
    clearTextInteractionHandlers();
    activeTextEventId = null;
    activePdfEvent = null;
    activeArtifactTitle = event.title || '';
    previousArtifactText = '';
    previousBlockTexts = [];
    previousArtifactTitle = activeArtifactTitle;
    renderMailTriageArtifact(e.text, event);
    dispatchCanvasRendered(event);
  } else if (event.kind === 'image_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.text.classList.remove('mail-draft-canvas');
    e.text.classList.remove('mail-triage-canvas');
    e.image.style.display = '';
    e.image.classList.add('is-active');
    const state = (window._taburaApp || {}).getState ? window._taburaApp.getState() : {};
    const sid = state.sessionId || '';
    (e.img as HTMLImageElement).src = apiURL(`files/${encodeURIComponent(sid)}/${encodeURIComponent(event.path)}`);
    (e.img as HTMLImageElement).alt = event.title || 'Image';
    activeTextEventId = null;
    activeArtifactTitle = event.title || '';
    activePdfEvent = null;
    dispatchCanvasRendered(event);
  } else if (event.kind === 'pdf_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.text.classList.remove('mail-draft-canvas');
    e.text.classList.remove('mail-triage-canvas');
    e.pdf.style.display = '';
    e.pdf.classList.add('is-active');
    void renderPdfSurface(event);
    activeTextEventId = null;
    activeArtifactTitle = event.title || '';
    activePdfEvent = event;
  } else if (event.kind === 'clear_canvas') {
    clearTextInteractionHandlers();
    clearCanvas();
  }
}

export function clearCanvas() {
  clearTextInteractionHandlers();
  hideAll();
  const e = getEls();
  if (e.text) e.text.classList.remove('mail-draft-canvas');
  if (e.text) e.text.classList.remove('mail-triage-canvas');
  if (e.text) delete e.text.dataset.approvalRequestId;
  cancelPdfRender({ destroyDocument: true });
  activeTextEventId = null;
  activeArtifactTitle = '';
  activePdfEvent = null;
  previousArtifactText = '';
  previousBlockTexts = [];
  previousArtifactTitle = '';
  dispatchCanvasCleared();
}

export function getPreviousArtifactText() {
  return previousArtifactText;
}
