import { marked } from './vendor/marked.esm.js';
import { AnnotationLayer, GlobalWorkerOptions, TextLayer, getDocument } from './vendor/pdf.mjs';
import { apiURL } from './paths.js';

const SOURCE_LANGUAGE_BY_EXT = {
  c: 'c',
  h: 'c',
  cc: 'cpp',
  cxx: 'cpp',
  cpp: 'cpp',
  hpp: 'cpp',
  hh: 'cpp',
  go: 'go',
  rs: 'rust',
  py: 'python',
  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  ts: 'typescript',
  jsx: 'jsx',
  tsx: 'tsx',
  java: 'java',
  kt: 'kotlin',
  scala: 'scala',
  rb: 'ruby',
  php: 'php',
  swift: 'swift',
  cs: 'csharp',
  lua: 'lua',
  r: 'r',
  sql: 'sql',
  sh: 'bash',
  bash: 'bash',
  zsh: 'bash',
  ps1: 'powershell',
  json: 'json',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'toml',
  ini: 'ini',
  xml: 'xml',
  html: 'xml',
  css: 'css',
  scss: 'scss',
  f: 'fortran',
  for: 'fortran',
  f77: 'fortran',
  f90: 'fortran',
  f95: 'fortran',
  f03: 'fortran',
  f08: 'fortran',
};

const SOURCE_LANGUAGE_BY_BASENAME = {
  makefile: 'makefile',
  dockerfile: 'dockerfile',
  cmakelists: 'cmake',
};

const MARKDOWN_EXTENSIONS = new Set(['md', 'markdown', 'mdown', 'mkdn', 'mkd', 'mdx']);
const PDFJS_WORKER_URL = new URL('./vendor/pdf.worker.mjs', import.meta.url).toString();
const PDFJS_STANDARD_FONTS_URL = new URL('./vendor/standard_fonts/', import.meta.url).toString();

export function escapeHtml(text) {
  return String(text)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function normalizeLanguage(langRaw) {
  const lang = String(langRaw || '').trim().toLowerCase();
  if (!lang) return '';
  const aliases = {
    js: 'javascript',
    ts: 'typescript',
    py: 'python',
    rs: 'rust',
    golang: 'go',
    shell: 'bash',
    sh: 'bash',
    zsh: 'bash',
    ps: 'powershell',
    f90: 'fortran',
    f95: 'fortran',
    f03: 'fortran',
    f08: 'fortran',
    for: 'fortran',
  };
  return aliases[lang] || lang;
}

function languageFromArtifactTitle(titleRaw) {
  const title = String(titleRaw || '').trim();
  if (!title) return '';
  const base = title.split(/[\\/]/).pop() || '';
  const lowerBase = base.toLowerCase();
  if (lowerBase === 'cmakelists.txt') return 'cmake';
  if (SOURCE_LANGUAGE_BY_BASENAME[lowerBase]) return SOURCE_LANGUAGE_BY_BASENAME[lowerBase];
  const dot = lowerBase.lastIndexOf('.');
  if (dot < 0 || dot === lowerBase.length - 1) return '';
  const ext = lowerBase.slice(dot + 1);
  return SOURCE_LANGUAGE_BY_EXT[ext] || '';
}

function highlightCode(code, langRaw) {
  const input = String(code || '');
  const lang = normalizeLanguage(langRaw);
  const hljs = window.hljs;
  if (!hljs || typeof hljs.highlight !== 'function') {
    return escapeHtml(input);
  }
  if (lang && typeof hljs.getLanguage === 'function' && hljs.getLanguage(lang)) {
    try {
      return hljs.highlight(input, { language: lang, ignoreIllegals: true }).value;
    } catch (_) {}
  }
  if (typeof hljs.highlightAuto === 'function') {
    try {
      return hljs.highlightAuto(input).value;
    } catch (_) {}
  }
  return escapeHtml(input);
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

function parseDiffHunkHeader(line) {
  const match = /^@@\s*-(\d+)(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s*@@/.exec(line);
  if (!match) return null;
  return {
    oldStart: Number.parseInt(match[1], 10),
    newStart: Number.parseInt(match[2], 10),
  };
}

function parseDiffPathFromHeader(line) {
  const match = /^diff --git a\/(.+?) b\/(.+)$/.exec(line);
  if (!match) return '';
  const right = String(match[2] || '').trim();
  const left = String(match[1] || '').trim();
  if (right && right !== '/dev/null') return right;
  return left;
}

function parseDiffPathFromMarker(line, marker) {
  if (!line.startsWith(marker)) return '';
  const raw = String(line.slice(marker.length)).trim();
  if (!raw || raw === '/dev/null') return '';
  if (raw.startsWith('a/') || raw.startsWith('b/')) {
    return raw.slice(2);
  }
  return raw;
}

function highlightDiffCodeLine(line, langRaw) {
  const lang = normalizeLanguage(langRaw);
  if (!lang) {
    return escapeHtml(line);
  }
  if (line.startsWith('+') && !line.startsWith('+++')) {
    return `${escapeHtml('+')}${highlightCode(line.slice(1), lang)}`;
  }
  if (line.startsWith('-') && !line.startsWith('---')) {
    return `${escapeHtml('-')}${highlightCode(line.slice(1), lang)}`;
  }
  if (line.startsWith(' ')) {
    return `${escapeHtml(' ')}${highlightCode(line.slice(1), lang)}`;
  }
  return escapeHtml(line);
}

function isMarkdownPath(pathRaw) {
  const path = String(pathRaw || '').trim();
  if (!path) return false;
  const base = path.split(/[\\/]/).pop() || '';
  const lowerBase = base.toLowerCase();
  const dot = lowerBase.lastIndexOf('.');
  if (dot < 0 || dot === lowerBase.length - 1) return false;
  return MARKDOWN_EXTENSIONS.has(lowerBase.slice(dot + 1));
}

function inferDiffPath(diffTextRaw) {
  const lines = String(diffTextRaw || '').replaceAll('\r\n', '\n').split('\n');
  let path = '';
  for (const line of lines) {
    if (line.startsWith('diff --git ')) {
      const headerPath = parseDiffPathFromHeader(line);
      if (headerPath) return headerPath;
    } else if (line.startsWith('+++ ')) {
      const plusPath = parseDiffPathFromMarker(line, '+++ ');
      if (plusPath) return plusPath;
    } else if (line.startsWith('--- ')) {
      const minusPath = parseDiffPathFromMarker(line, '--- ');
      if (minusPath) path = minusPath;
    }
  }
  return path;
}

function tokenizeInlineDiffWords(textRaw) {
  return String(textRaw || '').match(/(\s+|[A-Za-z0-9_]+|[^A-Za-z0-9_\s])/g) || [];
}

function renderInlineMarkdownDiff(oldLineRaw, newLineRaw) {
  const oldLine = String(oldLineRaw || '');
  const newLine = String(newLineRaw || '');
  if (!oldLine || !newLine || oldLine === newLine) {
    return escapeHtml(newLine);
  }
  if (oldLine.length > 2000 || newLine.length > 2000) {
    return escapeHtml(newLine);
  }

  const oldTokens = tokenizeInlineDiffWords(oldLine);
  const newTokens = tokenizeInlineDiffWords(newLine);
  if (oldTokens.length === 0 || newTokens.length === 0) {
    return escapeHtml(newLine);
  }
  if (oldTokens.length > 320 || newTokens.length > 320) {
    return escapeHtml(newLine);
  }

  const lcs = Array.from({ length: oldTokens.length + 1 }, () => new Uint16Array(newTokens.length + 1));
  for (let i = 1; i <= oldTokens.length; i += 1) {
    for (let j = 1; j <= newTokens.length; j += 1) {
      if (oldTokens[i - 1] === newTokens[j - 1]) {
        lcs[i][j] = lcs[i - 1][j - 1] + 1;
      } else {
        lcs[i][j] = Math.max(lcs[i - 1][j], lcs[i][j - 1]);
      }
    }
  }

  const opsReversed = [];
  let i = oldTokens.length;
  let j = newTokens.length;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldTokens[i - 1] === newTokens[j - 1]) {
      opsReversed.push({ type: 'equal', text: oldTokens[i - 1] });
      i -= 1;
      j -= 1;
      continue;
    }
    if (j > 0 && (i === 0 || lcs[i][j - 1] >= lcs[i - 1][j])) {
      opsReversed.push({ type: 'add', text: newTokens[j - 1] });
      j -= 1;
      continue;
    }
    if (i > 0) {
      opsReversed.push({ type: 'del', text: oldTokens[i - 1] });
      i -= 1;
      continue;
    }
  }

  const ops = opsReversed.reverse();
  const merged = [];
  for (const op of ops) {
    if (merged.length > 0 && merged[merged.length - 1].type === op.type) {
      merged[merged.length - 1].text += op.text;
    } else {
      merged.push({ type: op.type, text: op.text });
    }
  }
  if (!merged.some((op) => op.type !== 'equal')) {
    return escapeHtml(newLine);
  }

  return merged.map((op) => {
    const safe = escapeHtml(op.text);
    if (op.type === 'add') {
      return `<ins class="md-diff-ins">${safe}</ins>`;
    }
    if (op.type === 'del') {
      return `<del class="md-diff-del-inline">${safe}</del>`;
    }
    return safe;
  }).join('');
}

function renderRemovedMarkdownLine(lineRaw) {
  const text = String(lineRaw || '');
  const safe = text ? escapeHtml(text) : '&nbsp;';
  return `<div class="md-diff-del-line"><del>${safe}</del></div>`;
}

function buildMarkdownDiffPreview(diffTextRaw, artifactTitleRaw) {
  const diffText = String(diffTextRaw || '').replaceAll('\r\n', '\n');
  if (!diffText.trim()) return null;
  const artifactTitle = String(artifactTitleRaw || '').trim();
  const inferredPath = inferDiffPath(diffText);
  const markdownPath = isMarkdownPath(artifactTitle)
    ? artifactTitle
    : (isMarkdownPath(inferredPath) ? inferredPath : '');
  if (!markdownPath) {
    return null;
  }
  if (!(diffText.startsWith('diff --git ') || diffText.includes('\ndiff --git '))) {
    return null;
  }

  const lines = diffText.split('\n');
  const previewLines = [];
  const lineMap = [];
  const changedMap = [];
  let sawHunk = false;
  let inHunk = false;
  let pendingDeletionLines = [];
  let newLine = null;
  let lastMappedLine = null;

  const appendLine = (text, fileLine, changed) => {
    previewLines.push(String(text || ''));
    if (Number.isFinite(fileLine) && fileLine > 0) {
      const resolved = Math.trunc(fileLine);
      lineMap.push(resolved);
      lastMappedLine = resolved;
    } else {
      lineMap.push(null);
    }
    changedMap.push(Boolean(changed));
  };

  // Keep the active file path visible in PR review mode while preserving
  // markdown diff rendering for file content changes.
  appendLine(`\`${markdownPath}\``, null, false);
  appendLine('', null, false);

  const flushPendingDeletions = () => {
    if (pendingDeletionLines.length === 0) return;
    for (const deletedLine of pendingDeletionLines) {
      appendLine(renderRemovedMarkdownLine(deletedLine), null, true);
    }
    pendingDeletionLines = [];
  };

  for (const line of lines) {
    if (line.startsWith('diff --git ')) {
      flushPendingDeletions();
      inHunk = false;
      continue;
    }

    const hunk = parseDiffHunkHeader(line);
    if (hunk) {
      sawHunk = true;
      inHunk = true;
      flushPendingDeletions();
      const hunkStart = Number.isFinite(hunk.newStart) ? hunk.newStart : null;
      if (
        Number.isFinite(lastMappedLine)
        && Number.isFinite(hunkStart)
        && hunkStart > lastMappedLine + 1
        && previewLines.length > 0
      ) {
        appendLine('', null, false);
        appendLine('[...]', null, false);
        appendLine('', null, false);
      }
      newLine = Number.isFinite(hunkStart) ? hunkStart : null;
      continue;
    }

    if (!inHunk) continue;
    if (line.startsWith('\\ No newline at end of file')) continue;

    if (line.startsWith('+') && !line.startsWith('+++')) {
      const nextText = line.slice(1);
      if (pendingDeletionLines.length > 0) {
        const deletedText = pendingDeletionLines.shift() || '';
        appendLine(renderInlineMarkdownDiff(deletedText, nextText), newLine, true);
      } else {
        appendLine(nextText, newLine, true);
      }
      if (Number.isFinite(newLine)) newLine += 1;
      continue;
    }

    if (line.startsWith('-') && !line.startsWith('---')) {
      pendingDeletionLines.push(line.slice(1));
      continue;
    }

    if (line.startsWith(' ')) {
      flushPendingDeletions();
      appendLine(line.slice(1), newLine, false);
      if (Number.isFinite(newLine)) newLine += 1;
      continue;
    }
  }
  flushPendingDeletions();

  if (!sawHunk || previewLines.length === 0) return null;
  if (!lineMap.some((line) => Number.isFinite(line) && line > 0)) return null;

  return {
    markdown: previewLines.join('\n'),
    lineMap,
    changedMap,
  };
}

function countNewlines(textRaw) {
  const text = String(textRaw || '');
  let count = 0;
  for (let i = 0; i < text.length; i += 1) {
    if (text.charCodeAt(i) === 10) count += 1;
  }
  return count;
}

function resolveTokenSourceMeta(lineMap, changedMap, startLine, endLine) {
  const start = Math.max(1, Math.trunc(startLine || 1));
  const end = Math.max(start, Math.trunc(endLine || start));
  let sourceLine = null;
  let changed = false;
  for (let line = start; line <= end; line += 1) {
    const mapped = lineMap[line - 1];
    if (sourceLine === null && Number.isFinite(mapped) && mapped > 0) {
      sourceLine = mapped;
    }
    if (changedMap[line - 1]) {
      changed = true;
    }
    if (sourceLine !== null && changed) break;
  }
  return { sourceLine, changed };
}

function annotateToken(token, startLine, endLine, lineMap, changedMap) {
  if (!token || typeof token !== 'object') return;
  const meta = resolveTokenSourceMeta(lineMap, changedMap, startLine, endLine);
  if (Number.isFinite(meta.sourceLine) && meta.sourceLine > 0) {
    token.taburaSourceLine = Math.trunc(meta.sourceLine);
  } else {
    delete token.taburaSourceLine;
  }
  token.taburaDiffChanged = Boolean(meta.changed);
}

function annotateListItems(token, listStartLine, lineMap, changedMap) {
  if (!token || token.type !== 'list' || !Array.isArray(token.items) || typeof token.raw !== 'string') return;
  let localCursor = 0;
  for (const item of token.items) {
    const raw = typeof item?.raw === 'string' ? item.raw : '';
    if (!raw) continue;
    const found = token.raw.indexOf(raw, localCursor);
    const index = found >= 0 ? found : localCursor;
    const start = listStartLine + countNewlines(token.raw.slice(0, index));
    const end = start + countNewlines(raw);
    annotateToken(item, start, end, lineMap, changedMap);
    localCursor = index + raw.length;
  }
}

function annotateMarkdownTokens(tokens, sourceTextRaw, lineMap, changedMap) {
  if (!Array.isArray(tokens) || tokens.length === 0) return;
  const sourceText = String(sourceTextRaw || '');
  let cursor = 0;
  for (const token of tokens) {
    const raw = typeof token?.raw === 'string' ? token.raw : '';
    if (!raw) continue;
    const found = sourceText.indexOf(raw, cursor);
    const index = found >= 0 ? found : cursor;
    const start = 1 + countNewlines(sourceText.slice(0, index));
    const end = start + countNewlines(raw);
    annotateToken(token, start, end, lineMap, changedMap);
    annotateListItems(token, start, lineMap, changedMap);
    cursor = index + raw.length;
  }
}

function markdownTokenAttrs(token) {
  if (!token || typeof token !== 'object') return '';
  const attrs = [];
  if (Number.isFinite(token.taburaSourceLine) && token.taburaSourceLine > 0) {
    attrs.push(`data-source-line="${Math.trunc(token.taburaSourceLine)}"`);
  }
  if (token.taburaDiffChanged) {
    attrs.push('class="md-diff-changed"');
  }
  if (attrs.length === 0) return '';
  return ` ${attrs.join(' ')}`;
}

function injectAttrsIntoOpeningTag(htmlRaw, tagNameRaw, attrs) {
  const html = String(htmlRaw || '');
  const tagName = String(tagNameRaw || '').trim();
  if (!attrs || !tagName || !html) return html;
  const open = `<${tagName}`;
  const index = html.indexOf(open);
  if (index < 0) return html;
  const insertAt = index + open.length;
  return `${html.slice(0, insertAt)}${attrs}${html.slice(insertAt)}`;
}

function highlightDiff(code) {
  const lines = code.split('\n');
  let oldLine = null;
  let newLine = null;
  let filePath = '';
  let fileLang = '';
  return lines.map((line, index) => {
    const kind = classifyDiffLine(line);
    const hunk = parseDiffHunkHeader(line);
    if (hunk) {
      oldLine = Number.isFinite(hunk.oldStart) ? hunk.oldStart : null;
      newLine = Number.isFinite(hunk.newStart) ? hunk.newStart : null;
    }

    if (line.startsWith('diff --git ')) {
      const nextPath = parseDiffPathFromHeader(line);
      if (nextPath) {
        filePath = nextPath;
        fileLang = languageFromArtifactTitle(filePath);
      }
      oldLine = null;
      newLine = null;
    } else if (line.startsWith('+++ ')) {
      const plusPath = parseDiffPathFromMarker(line, '+++ ');
      if (plusPath) {
        filePath = plusPath;
        fileLang = languageFromArtifactTitle(filePath);
      }
    } else if (line.startsWith('--- ') && !filePath) {
      const minusPath = parseDiffPathFromMarker(line, '--- ');
      if (minusPath) {
        filePath = minusPath;
        fileLang = languageFromArtifactTitle(filePath);
      }
    }

    let oldAtLine = null;
    let newAtLine = null;
    if (hunk) {
      // hunk header sets counters; no source line number at the header itself.
    } else if (Number.isFinite(oldLine) || Number.isFinite(newLine)) {
      if (line.startsWith('+') && !line.startsWith('+++')) {
        if (Number.isFinite(newLine)) {
          newAtLine = newLine;
          newLine += 1;
        }
      } else if (line.startsWith('-') && !line.startsWith('---')) {
        if (Number.isFinite(oldLine)) {
          oldAtLine = oldLine;
          oldLine += 1;
        }
      } else if (line.startsWith(' ')) {
        if (Number.isFinite(oldLine)) {
          oldAtLine = oldLine;
          oldLine += 1;
        }
        if (Number.isFinite(newLine)) {
          newAtLine = newLine;
          newLine += 1;
        }
      }
    }

    const fileLine = Number.isFinite(newAtLine)
      ? newAtLine
      : (Number.isFinite(oldAtLine) ? oldAtLine : null);
    const attrs = [`class="hl-diff-line hl-diff-${kind}"`, `data-diff-line="${index + 1}"`];
    if (filePath) {
      attrs.push(`data-file-path="${escapeHtml(filePath)}"`);
    }
    if (Number.isFinite(fileLine)) {
      attrs.push(`data-file-line="${fileLine}"`);
    }
    if (Number.isFinite(oldAtLine)) {
      attrs.push(`data-old-line="${oldAtLine}"`);
    }
    if (Number.isFinite(newAtLine)) {
      attrs.push(`data-new-line="${newAtLine}"`);
    }
    if (!line) {
      return `<span ${attrs.join(' ')}></span>`;
    }
    return `<span ${attrs.join(' ')}>${highlightDiffCodeLine(line, fileLang)}</span>`;
  }).join('');
}

function renderHighlightedCodeBlock(code, langRaw) {
  const normalized = normalizeLanguage(langRaw) || 'plaintext';
  const highlighted = highlightCode(code, normalized);
  return `<pre><code class="hljs language-${escapeHtml(normalized)}">${highlighted}</code></pre>\n`;
}

function renderCodeBlock(code, langRaw) {
  const lang = normalizeLanguage(langRaw);
  if (lang === 'fortran') {
    return renderHighlightedCodeBlock(code, 'fortran');
  }
  if (lang === 'diff' || lang === 'patch' || lang === 'git') {
    return `<pre><code class="hljs language-${escapeHtml(lang)}">${highlightDiff(code)}</code></pre>\n`;
  }
  return renderHighlightedCodeBlock(code, lang || 'plaintext');
}

const baseRenderer = new marked.Renderer();
const renderer = new marked.Renderer();
renderer.code = function codeRenderer(token) {
  const rendered = renderCodeBlock(token?.text || '', token?.lang || '');
  return injectAttrsIntoOpeningTag(rendered, 'pre', markdownTokenAttrs(token));
};
renderer.heading = function headingRenderer(token) {
  const html = baseRenderer.heading.call(this, token);
  return injectAttrsIntoOpeningTag(html, `h${token?.depth || 1}`, markdownTokenAttrs(token));
};
renderer.paragraph = function paragraphRenderer(token) {
  const html = baseRenderer.paragraph.call(this, token);
  return injectAttrsIntoOpeningTag(html, 'p', markdownTokenAttrs(token));
};
renderer.blockquote = function blockquoteRenderer(token) {
  const html = baseRenderer.blockquote.call(this, token);
  return injectAttrsIntoOpeningTag(html, 'blockquote', markdownTokenAttrs(token));
};
renderer.list = function listRenderer(token) {
  const html = baseRenderer.list.call(this, token);
  const tag = token?.ordered ? 'ol' : 'ul';
  return injectAttrsIntoOpeningTag(html, tag, markdownTokenAttrs(token));
};
renderer.listitem = function listItemRenderer(token) {
  const html = baseRenderer.listitem.call(this, token);
  return injectAttrsIntoOpeningTag(html, 'li', markdownTokenAttrs(token));
};
renderer.table = function tableRenderer(token) {
  const html = baseRenderer.table.call(this, token);
  return injectAttrsIntoOpeningTag(html, 'table', markdownTokenAttrs(token));
};
renderer.hr = function hrRenderer(token) {
  const html = baseRenderer.hr.call(this, token);
  return injectAttrsIntoOpeningTag(html, 'hr', markdownTokenAttrs(token));
};

marked.setOptions({
  breaks: true,
  renderer,
});

const els = {};
let activeTextEventId = null;
let activeArtifactTitle = '';
let activePdfEvent = null;
let previousArtifactText = '';
let previousBlockTexts = [];
let previousArtifactTitle = '';

const MATH_SEGMENT_TOKEN_PREFIX = '@@TABURA_MATH_SEGMENT_';
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
  renderTasks: new Set(),
  textLayers: new Set(),
};

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
      return { line, title };
    } catch (_) {
      return null;
    }
  }
  if (e.text && activeTextEventId) {
    const rect = e.text.getBoundingClientRect();
    if (clientX >= rect.left && clientX <= rect.right && clientY >= rect.top && clientY <= rect.bottom) {
      const estimate = estimateTextLineAtPoint(e.text, clientY);
      if (estimate) {
        return { line: estimate.line, title: getActiveArtifactTitle() };
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
  return { line, selectedText, title };
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

async function renderPdfSurface(event, options = {}) {
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
  } catch (err) {
    if (token !== pdfRenderState.generation) return;
    if (isPdfCancellationError(err)) return;
    console.warn('PDF render failed:', err);
    pdfRenderState.lastWidth = getPdfContainerWidth(e.pdf);
    renderPdfFallback(e.pdf, url, 'PDF preview unavailable.');
  }
}

export function renderCanvas(event) {
  const e = getEls();

  if (event.kind === 'text_artifact') {
    hideAll();
    e.text.style.display = '';
    e.text.classList.add('is-active');
    clearTextInteractionHandlers();
    activeTextEventId = event.event_id;
    const nextArtifactTitle = String(event.title || '');
    const shouldHighlightChanges = previousArtifactTitle === nextArtifactTitle && previousBlockTexts.length > 0;
    activeArtifactTitle = nextArtifactTitle;
    activePdfEvent = null;
    const oldBlockTexts = shouldHighlightChanges ? previousBlockTexts.slice() : [];
    const textBody = String(event.text || '');
    const isUnifiedDiff = textBody.startsWith('diff --git ') || textBody.includes('\ndiff --git ');
    const diffPreview = isUnifiedDiff ? buildMarkdownDiffPreview(textBody, activeArtifactTitle) : null;
    const sourceLang = languageFromArtifactTitle(activeArtifactTitle);
    if (diffPreview) {
      const markdownSource = diffPreview ? diffPreview.markdown : textBody;
      const { text: markdownText, stash: mathSegments } = extractMathSegments(markdownSource);
      let renderedMarkdownHtml = '';
      const tokens = marked.lexer(markdownText);
      annotateMarkdownTokens(tokens, markdownText, diffPreview.lineMap, diffPreview.changedMap);
      renderedMarkdownHtml = marked.parser(tokens);
      e.text.innerHTML = restoreMathSegments(sanitizeHtml(renderedMarkdownHtml), mathSegments);
      typesetMarkdownMath(e.text);
    } else if (isUnifiedDiff) {
      e.text.innerHTML = sanitizeHtml(renderCodeBlock(textBody, 'diff'));
    } else if (sourceLang) {
      e.text.innerHTML = sanitizeHtml(renderHighlightedCodeBlock(textBody, sourceLang));
    } else {
      const { text: markdownText, stash: mathSegments } = extractMathSegments(textBody);
      const renderedMarkdownHtml = marked.parse(markdownText);
      e.text.innerHTML = restoreMathSegments(sanitizeHtml(renderedMarkdownHtml), mathSegments);
      typesetMarkdownMath(e.text);
    }
    previousArtifactText = event.text || '';
    previousBlockTexts = captureBlockTexts(e.text);
    previousArtifactTitle = nextArtifactTitle;
    applyDiffHighlight(e.text, oldBlockTexts);
  } else if (event.kind === 'image_artifact') {
    clearTextInteractionHandlers();
    hideAll();
    e.image.style.display = '';
    e.image.classList.add('is-active');
    const state = (window._taburaApp || {}).getState ? window._taburaApp.getState() : {};
    const sid = state.sessionId || '';
    e.img.src = apiURL(`files/${encodeURIComponent(sid)}/${encodeURIComponent(event.path)}`);
    e.img.alt = event.title || 'Image';
    activeTextEventId = null;
    activeArtifactTitle = event.title || '';
    activePdfEvent = null;
  } else if (event.kind === 'pdf_artifact') {
    clearTextInteractionHandlers();
    hideAll();
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
  cancelPdfRender({ destroyDocument: true });
  activeTextEventId = null;
  activeArtifactTitle = '';
  activePdfEvent = null;
  previousArtifactText = '';
  previousBlockTexts = [];
  previousArtifactTitle = '';
}

function getBlockSelector() {
  return 'p, h1, h2, h3, h4, h5, h6, pre, ul, ol, table, blockquote, hr';
}

function captureBlockTexts(root) {
  const blocks = root.querySelectorAll(getBlockSelector());
  const texts = [];
  for (let i = 0; i < blocks.length; i++) {
    texts.push(blocks[i].textContent || '');
  }
  return texts;
}

function applyDiffHighlight(root, oldBlockTexts) {
  if (!oldBlockTexts || oldBlockTexts.length === 0 || !root) return;
  const blocks = root.querySelectorAll(getBlockSelector());
  const changedBlocks = [];
  const maxLen = Math.max(oldBlockTexts.length, blocks.length);
  for (let i = 0; i < maxLen; i++) {
    if (i >= blocks.length) break;
    const oldContent = i < oldBlockTexts.length ? oldBlockTexts[i] : '';
    const newContent = blocks[i].textContent || '';
    if (oldContent !== newContent) {
      blocks[i].classList.add('diff-highlight');
      changedBlocks.push(blocks[i]);
    }
  }
  // New blocks beyond old length
  for (let i = oldBlockTexts.length; i < blocks.length; i++) {
    blocks[i].classList.add('diff-highlight');
    changedBlocks.push(blocks[i]);
  }
  if (changedBlocks.length === 0) return;
  if (changedBlocks[0] && changedBlocks[0].isConnected) {
    changedBlocks[0].scrollIntoView({ behavior: 'smooth', block: 'center' });
  }
}

export function getPreviousArtifactText() {
  return previousArtifactText;
}
