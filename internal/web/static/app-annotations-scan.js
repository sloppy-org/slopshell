import { apiURL } from './app-env.js';
import { refs, state } from './app-context.js';

const openSidebarArtifactItem = (...args) => refs.openSidebarArtifactItem(...args);

function activeSidebarItemForScan() {
  const activeID = Number(state.itemSidebarActiveItemID || 0);
  if (activeID <= 0) return null;
  const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
  return items.find((entry) => Number(entry?.id || 0) === activeID) || null;
}

function normalizeScanBounds(bounds, clamp01) {
  if (!bounds || typeof bounds !== 'object') return null;
  return {
    x: clamp01(Number(bounds.x)),
    y: clamp01(Number(bounds.y)),
    width: clamp01(Number(bounds.width)),
    height: clamp01(Number(bounds.height)),
  };
}

function normalizedRectFromClientRect(rect, rootRect, options = {}, clamp01) {
  const width = Math.max(Number(options.width || rootRect.width || 1), 1);
  const height = Math.max(Number(options.height || rootRect.height || 1), 1);
  return {
    x: clamp01((rect.left - rootRect.left + Number(options.scrollLeft || 0)) / width),
    y: clamp01((rect.top - rootRect.top + Number(options.scrollTop || 0)) / height),
    width: clamp01(rect.width / width),
    height: clamp01(rect.height / height),
  };
}

function findTextAnchorRects(anchorText, safeText, collectNormalizedClientRects) {
  const pane = document.getElementById('canvas-text');
  const query = safeText(anchorText);
  if (!(pane instanceof HTMLElement) || !pane.classList.contains('is-active') || !query) return [];
  const walker = document.createTreeWalker(pane, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const text = String(node.textContent || '');
    const index = text.toLowerCase().indexOf(query.toLowerCase());
    if (index >= 0) {
      const range = document.createRange();
      range.setStart(node, index);
      range.setEnd(node, Math.min(text.length, index + query.length));
      const rects = collectNormalizedClientRects(range, pane, { scrollable: true });
      range.detach?.();
      if (rects.length > 0) return rects;
    }
    node = walker.nextNode();
  }
  return [];
}

function approximateTextRectsForLine(line, clamp01) {
  const pane = document.getElementById('canvas-text');
  const lineNumber = Number.parseInt(String(line || ''), 10);
  if (!(pane instanceof HTMLElement) || !pane.classList.contains('is-active') || !Number.isFinite(lineNumber) || lineNumber <= 0) {
    return [];
  }
  const sourceNode = pane.querySelector(`[data-source-line="${lineNumber}"]`);
  if (sourceNode instanceof HTMLElement) {
    return [normalizedRectFromClientRect(sourceNode.getBoundingClientRect(), pane.getBoundingClientRect(), {
      scrollLeft: pane.scrollLeft,
      scrollTop: pane.scrollTop,
      width: Math.max(pane.scrollWidth, pane.clientWidth, 1),
      height: Math.max(pane.scrollHeight, pane.clientHeight, 1),
    }, clamp01)];
  }
  const text = String(pane.textContent || '');
  const lineCount = Math.max(1, text.split('\n').length);
  const anchor = pane.querySelector('pre, code, p, li, blockquote');
  const lineHeight = Math.max(18, parseFloat(window.getComputedStyle(anchor || pane).lineHeight) || 22);
  const height = Math.max(pane.scrollHeight, lineCount * lineHeight, 1);
  const top = Math.max(0, Math.min(height - lineHeight, (lineNumber - 1) * lineHeight));
  return [{
    x: 0.02,
    y: clamp01(top / height),
    width: 0.96,
    height: clamp01(lineHeight / height),
  }];
}

function approximateTextRectsForParagraph(paragraph, clamp01) {
  const pane = document.getElementById('canvas-text');
  const paragraphNumber = Number.parseInt(String(paragraph || ''), 10);
  if (!(pane instanceof HTMLElement) || !pane.classList.contains('is-active') || !Number.isFinite(paragraphNumber) || paragraphNumber <= 0) {
    return [];
  }
  const blockNodes = Array.from(pane.querySelectorAll('p, li, blockquote, article, section')).filter((node) => node instanceof HTMLElement);
  const block = blockNodes[paragraphNumber - 1];
  if (block instanceof HTMLElement) {
    return [normalizedRectFromClientRect(block.getBoundingClientRect(), pane.getBoundingClientRect(), {
      scrollLeft: pane.scrollLeft,
      scrollTop: pane.scrollTop,
      width: Math.max(pane.scrollWidth, pane.clientWidth, 1),
      height: Math.max(pane.scrollHeight, pane.clientHeight, 1),
    }, clamp01)];
  }
  const paragraphs = String(pane.textContent || '').split(/\n\s*\n+/).map((entry) => entry.trim()).filter(Boolean);
  if (paragraphs.length === 0) {
    return [];
  }
  const height = Math.max(pane.scrollHeight, pane.clientHeight, 1);
  const segmentHeight = height / paragraphs.length;
  const top = Math.max(0, Math.min(height - segmentHeight, (paragraphNumber - 1) * segmentHeight));
  return [{
    x: 0.02,
    y: clamp01(top / height),
    width: 0.96,
    height: clamp01(segmentHeight / height),
  }];
}

function buildImportedScanAnnotation(entry, index, payload, deps) {
  const {
    clamp01,
    collectNormalizedClientRects,
    createAnnotationID,
    highlightColor,
    safeText,
  } = deps;
  const currentKind = safeText(state.currentCanvasArtifact?.kind || '');
  const page = Number.parseInt(safeText(entry?.page), 10);
  const line = Number.parseInt(safeText(entry?.line), 10);
  const paragraph = Number.parseInt(safeText(entry?.paragraph), 10);
  const anchorText = safeText(entry?.anchor_text);
  const text = safeText(entry?.content || entry?.text || 'Scanned note');
  const bounds = normalizeScanBounds(entry?.bounds, clamp01);
  const base = {
    id: createAnnotationID(),
    text,
    anchor_text: anchorText,
    line: Number.isFinite(line) && line > 0 ? line : 0,
    paragraph: Number.isFinite(paragraph) && paragraph > 0 ? paragraph : 0,
    color: highlightColor,
    notes: [],
    confidence: clamp01(Number(entry?.confidence)),
    source: 'scan_upload',
    scan_artifact_id: Number(payload?.scan_artifact?.id || payload?.scan_artifact_id || 0),
    scan_item_id: Number(payload?.item_id || 0),
    source_artifact_id: Number(payload?.artifact_id || 0),
    project_id: safeText(payload?.project_id),
  };
  if (currentKind === 'pdf_artifact') {
    return {
      ...base,
      type: bounds ? 'highlight' : 'sticky_note',
      target: 'pdf',
      page: Number.isFinite(page) && page > 0 ? page : 1,
      rects: bounds ? [bounds] : [{ x: 0.08, y: clamp01(0.08 + (index * 0.06)), width: 0, height: 0 }],
    };
  }
  const rects = findTextAnchorRects(anchorText, safeText, collectNormalizedClientRects) || [];
  const mappedRects = rects.length > 0
    ? rects
    : (Number.isFinite(line) && line > 0
      ? approximateTextRectsForLine(line, clamp01)
      : (Number.isFinite(paragraph) && paragraph > 0 ? approximateTextRectsForParagraph(paragraph, clamp01) : []));
  return {
    ...base,
    type: 'highlight',
    target: 'text',
    rects: mappedRects.length > 0 ? mappedRects : [{ x: 0.02, y: clamp01(0.06 + (index * 0.08)), width: 0.96, height: 0.05 }],
  };
}

export function createScanAnnotationController(deps) {
  const {
    clamp01,
    collectNormalizedClientRects,
    createAnnotationID,
    highlightColor,
    listActiveAnnotations,
    openAnnotationBubble,
    renderActiveAnnotations,
    safeText,
    saveActiveAnnotations,
    showStatus,
  } = deps;
  let scanUploadInFlight = false;

  function importScanAnnotations(payload = {}) {
    const incoming = Array.isArray(payload?.annotations) ? payload.annotations : [];
    if (incoming.length === 0) {
      showStatus('scan imported: no annotations found');
      return 0;
    }
    const annotations = listActiveAnnotations();
    incoming.forEach((entry, index) => {
      annotations.push(buildImportedScanAnnotation(entry, index, payload, {
        clamp01,
        collectNormalizedClientRects,
        createAnnotationID,
        highlightColor,
        safeText,
      }));
    });
    saveActiveAnnotations(annotations);
    renderActiveAnnotations();
    if (annotations.length > 0) {
      openAnnotationBubble(annotations[annotations.length - incoming.length].id);
    }
    showStatus(`scan imported: ${incoming.length} annotation${incoming.length === 1 ? '' : 's'}`);
    return incoming.length;
  }

  function openScanImportPicker() {
    if (!activeSidebarItemForScan()) {
      showStatus('select an item before importing a scan');
      return false;
    }
    let input = document.getElementById('scan-upload-input');
    if (!(input instanceof HTMLInputElement)) {
      input = document.createElement('input');
      input.id = 'scan-upload-input';
      input.type = 'file';
      input.accept = 'image/*';
      input.hidden = true;
      input.addEventListener('change', () => {
        const [file] = Array.from(input.files || []);
        input.value = '';
        if (file) {
          void uploadScanFile(file);
        }
      });
      document.body.appendChild(input);
    }
    input.click();
    return true;
  }

  async function uploadScanFile(file) {
    const item = activeSidebarItemForScan();
    if (!item) {
      showStatus('select an item before importing a scan');
      return false;
    }
    if (!safeText(state.activeProjectId)) {
      showStatus('scan import requires an active workspace');
      return false;
    }
    if (scanUploadInFlight) {
      showStatus('scan import already running');
      return false;
    }
    scanUploadInFlight = true;
    try {
      await openSidebarArtifactItem(item);
      const form = new FormData();
      form.set('project_id', safeText(state.activeProjectId));
      form.set('item_id', String(Number(item?.id || 0)));
      form.set('artifact_id', String(Number(item?.artifact_id || 0)));
      form.set('file', file, file.name || 'scan.png');
      const resp = await fetch(apiURL('scan/upload'), { method: 'POST', body: form });
      if (!resp.ok) {
        const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
        throw new Error(detail);
      }
      importScanAnnotations(await resp.json());
      return true;
    } catch (err) {
      showStatus(`scan import failed: ${safeText(err?.message || err) || 'unknown error'}`);
      return false;
    } finally {
      scanUploadInFlight = false;
    }
  }

  async function confirmImportedScanAnnotations(selected) {
    const pending = Array.isArray(selected)
      ? selected.filter((entry) => safeText(entry?.source) === 'scan_upload' && Number(entry?.scan_artifact_id || 0) > 0)
      : [];
    if (pending.length === 0) return true;
    const groups = new Map();
    pending.forEach((entry) => {
      const key = String(Number(entry?.scan_artifact_id || 0));
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(entry);
    });
    for (const group of groups.values()) {
      const first = group[0] || {};
      const resp = await fetch(apiURL('scan/confirm'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project_id: safeText(first?.project_id || state.activeProjectId),
          item_id: Number(first?.scan_item_id || 0),
          artifact_id: Number(first?.source_artifact_id || 0),
          scan_artifact_id: Number(first?.scan_artifact_id || 0),
          annotations: group.map((entry) => ({
            content: safeText(entry?.text),
            anchor_text: safeText(entry?.anchor_text),
            line: Number(entry?.line || 0),
            paragraph: Number(entry?.paragraph || 0),
            page: Number(entry?.page || 0),
            bounds: normalizeScanBounds(Array.isArray(entry?.rects) ? entry.rects[0] : null, clamp01),
            confidence: Number(entry?.confidence || 0),
          })),
        }),
      });
      if (!resp.ok) {
        const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
        throw new Error(detail);
      }
      await resp.json();
    }
    return true;
  }

  return {
    confirmImportedScanAnnotations,
    importScanAnnotations,
    openScanImportPicker,
    uploadScanFile,
  };
}
