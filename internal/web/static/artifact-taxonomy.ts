import { apiURL } from './paths.js';

const IMAGE_EXTENSIONS = new Set([
  '.apng',
  '.avif',
  '.bmp',
  '.gif',
  '.ico',
  '.jpeg',
  '.jpg',
  '.png',
  '.svg',
  '.tif',
  '.tiff',
  '.webp',
]);

const EMPTY_TAXONOMY = Object.freeze({
  canonical_action_order: Object.freeze([]),
  actions: Object.freeze({}),
  kinds: Object.freeze({}),
});

const DEFAULT_CANVAS_SURFACE = 'text_artifact';
const DEFAULT_SPEC = Object.freeze({
  family: 'artifact',
  canvas_surface: DEFAULT_CANVAS_SURFACE,
  interaction_model: 'canonical_canvas',
  actions: Object.freeze([
    'open_show',
    'annotate_capture',
    'compose',
    'bundle_review',
    'track_item',
  ]),
  preferred_tool: 'pointer',
  mail_actions: false,
});

async function loadArtifactTaxonomy() {
  try {
    const resp = await fetch(apiURL('artifacts/taxonomy'), {
      cache: 'no-store',
    });
    if (!resp.ok) {
      throw new Error(`HTTP ${resp.status}`);
    }
    const payload = await resp.json();
    const actions = payload?.actions && typeof payload.actions === 'object' ? payload.actions : {};
    const kinds = payload?.kinds && typeof payload.kinds === 'object' ? payload.kinds : {};
    const order = Array.isArray(payload?.canonical_action_order) ? payload.canonical_action_order : [];
    return {
      canonical_action_order: Object.freeze(order.map((value) => String(value || '').trim()).filter(Boolean)),
      actions: Object.freeze(actions),
      kinds: Object.freeze(kinds),
    };
  } catch (err) {
    console.warn('artifact taxonomy unavailable:', err);
    return EMPTY_TAXONOMY;
  }
}

const TAXONOMY_DATA = await loadArtifactTaxonomy();

export const CANONICAL_ACTION_SEMANTICS = Object.freeze(
  TAXONOMY_DATA.canonical_action_order.length > 0
    ? [...TAXONOMY_DATA.canonical_action_order]
    : [...DEFAULT_SPEC.actions],
);

export const CANONICAL_ACTION_SPECS = Object.freeze(TAXONOMY_DATA.actions);
export const ARTIFACT_KIND_TAXONOMY = Object.freeze(TAXONOMY_DATA.kinds);

export function normalizeArtifactKind(kind) {
  return String(kind || '').trim().toLowerCase();
}

export function canonicalActionSpec(action) {
  const normalized = String(action || '').trim();
  const spec = CANONICAL_ACTION_SPECS[normalized];
  if (spec && typeof spec === 'object') {
    return spec;
  }
  return {
    label: normalized.replace(/_/g, ' ').trim(),
    prompt_label: normalized.replace(/_/g, ' ').trim(),
    description: '',
  };
}

export function canonicalActionLabel(action) {
  return String(canonicalActionSpec(action)?.label || '').trim();
}

export function canonicalActionPromptLabel(action) {
  const spec = canonicalActionSpec(action);
  return String(spec?.prompt_label || spec?.label || action || '').trim();
}

export function artifactKindSpec(kind) {
  const normalized = normalizeArtifactKind(kind);
  const spec = ARTIFACT_KIND_TAXONOMY[normalized];
  if (spec && typeof spec === 'object') return spec;
  return DEFAULT_SPEC;
}

export function artifactKindActionLabels(kind) {
  return artifactKindSpec(kind).actions.map((action) => canonicalActionLabel(action) || action);
}

export function artifactKindPreferredTool(kind) {
  const normalized = String(artifactKindSpec(kind).preferred_tool || '').trim().toLowerCase();
  if (normalized === 'highlight' || normalized === 'ink' || normalized === 'text_note' || normalized === 'prompt') {
    return normalized;
  }
  return 'pointer';
}

function imageExtensionFromPath(refPath) {
  const normalized = String(refPath || '').trim().toLowerCase();
  if (!normalized) return '';
  const dot = normalized.lastIndexOf('.');
  if (dot < 0) return '';
  return normalized.slice(dot);
}

export function artifactCanvasEventKind(kind, refPath = '') {
  const normalized = normalizeArtifactKind(kind);
  if (normalized === 'email_draft') return DEFAULT_CANVAS_SURFACE;
  if (normalized === 'pdf') return 'pdf_artifact';
  if (normalized === 'image') return 'image_artifact';
  if (String(refPath || '').trim().toLowerCase().endsWith('.pdf')) return 'pdf_artifact';
  if (IMAGE_EXTENSIONS.has(imageExtensionFromPath(refPath))) return 'image_artifact';
  return artifactKindSpec(normalized).canvas_surface || DEFAULT_CANVAS_SURFACE;
}

export function artifactSupportsMailActions(kind) {
  return artifactKindSpec(kind).mail_actions === true;
}

export function artifactUsesThreadHTML(kind) {
  return normalizeArtifactKind(kind) === 'email_thread';
}
