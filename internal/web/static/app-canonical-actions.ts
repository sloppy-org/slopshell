import { refs, state } from './app-context.js';
import { apiURL } from './paths.js';
import {
  artifactKindPreferredTool,
  artifactKindSpec,
  artifactSupportsMailActions,
  canonicalActionLabel,
  normalizeArtifactKind,
} from './artifact-taxonomy.js';

const showStatus = (...args) => refs.showStatus(...args);
const openItemSidebarView = (...args) => refs.openItemSidebarView(...args);
const openSidebarItem = (...args) => refs.openSidebarItem(...args);
const selectInteractionTool = (...args) => refs.selectInteractionTool(...args);
const openComposerAt = (...args) => refs.openComposerAt(...args);
const launchNewMailAuthoring = (...args) => refs.launchNewMailAuthoring(...args);
const launchReplyAuthoring = (...args) => refs.launchReplyAuthoring(...args);
const sendActiveMailDraft = (...args) => refs.sendActiveMailDraft(...args);
const showItemSidebarReviewMenu = (...args) => refs.showItemSidebarReviewMenu(...args);
const showItemSidebarDelegateMenu = (...args) => refs.showItemSidebarDelegateMenu(...args);
const performItemSidebarTriage = (...args) => refs.performItemSidebarTriage(...args);

function activeSidebarItem() {
  const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
  if (items.length === 0) return null;
  const activeID = Number(state.itemSidebarActiveItemID || 0);
  return items.find((item) => Number(item?.id || 0) === activeID) || items[0] || null;
}

function currentCanvasItem() {
  const itemID = Number(state.currentCanvasArtifact?.itemID || 0);
  if (itemID <= 0) return null;
  const items = Array.isArray(state.itemSidebarItems) ? state.itemSidebarItems : [];
  return items.find((item) => Number(item?.id || 0) === itemID) || null;
}

type CanonicalActionMenuOptions = {
  sourceElement?: HTMLElement | null;
};

export function currentCanonicalActionContext() {
  const item = currentCanvasItem() || activeSidebarItem();
  const artifactKind = normalizeArtifactKind(
    state.currentCanvasArtifact?.artifactKind
    || item?.artifact_kind
    || '',
  );
  const artifactID = Number(state.currentCanvasArtifact?.artifactID || item?.artifact_id || 0);
  const itemID = Number(state.currentCanvasArtifact?.itemID || item?.id || 0);
  const title = String(
    state.currentCanvasArtifact?.title
    || item?.artifact_title
    || item?.title
    || '',
  ).trim();
  return {
    item,
    itemID: itemID > 0 ? itemID : 0,
    artifactKind,
    artifactID: artifactID > 0 ? artifactID : 0,
    title,
  };
}

export function currentCanonicalActions() {
  const context = currentCanonicalActionContext();
  if (!context.artifactKind) return [];
  return artifactKindSpec(context.artifactKind).actions;
}

function menuPointFromOptions(options: CanonicalActionMenuOptions = {}) {
  const source = options?.sourceElement;
  if (source instanceof HTMLElement) {
    const rect = source.getBoundingClientRect();
    return {
      x: Math.round(rect.left + (rect.width / 2)),
      y: Math.round(rect.bottom + 8),
    };
  }
  return {
    x: Math.round(window.innerWidth / 2),
    y: Math.round(window.innerHeight / 2),
  };
}

function composerAnchorFromContext(context) {
  const item = context?.item;
  return {
    view: 'artifact',
    title: context?.title || '',
    item_id: Number(item?.id || context?.itemID || 0),
    item_title: String(item?.title || '').trim(),
    item_state: String(item?.state || '').trim(),
  };
}

async function trackCurrentArtifactAsItem(context) {
  const title = String(context?.title || '').trim() || 'Follow up';
  const payload: Record<string, any> = {
    title,
    sphere: String(state.activeSphere || 'private').trim().toLowerCase() === 'work' ? 'work' : 'private',
  };
  const artifactID = Number(context?.artifactID || 0);
  if (artifactID > 0) {
    payload.artifact_id = artifactID;
  }
  const resp = await fetch(apiURL('items'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const body = await resp.json();
  const item = body?.item && typeof body.item === 'object' ? body.item : null;
  const itemID = Number(item?.id || 0);
  if (itemID > 0) {
    state.currentCanvasArtifact.itemID = itemID;
    state.itemSidebarActiveItemID = itemID;
  }
  await openItemSidebarView('inbox');
  showStatus('tracked as inbox item');
  return item;
}

export async function executeCurrentCanonicalAction(action, options: Record<string, any> = {}) {
  const normalizedAction = String(action || '').trim();
  const context = currentCanonicalActionContext();
  const item = context.item;
  const point = menuPointFromOptions(options);
  if (!normalizedAction) return false;

  if (normalizedAction === 'open_show') {
    if (item) {
      state.itemSidebarActiveItemID = Number(item?.id || 0);
      await openItemSidebarView(String(item?.state || state.itemSidebarView || 'inbox'));
      await openSidebarItem(item);
      return true;
    }
    showStatus('artifact already open');
    return true;
  }

  if (normalizedAction === 'annotate_capture') {
    await selectInteractionTool(artifactKindPreferredTool(context.artifactKind));
    return true;
  }

  if (normalizedAction === 'compose') {
    if (item && artifactSupportsMailActions(context.artifactKind)) {
      await launchReplyAuthoring(item);
      return true;
    }
    if (!item && artifactSupportsMailActions(context.artifactKind)) {
      await launchNewMailAuthoring();
      return true;
    }
    openComposerAt(point.x, point.y, composerAnchorFromContext(context));
    showStatus('composer ready');
    return true;
  }

  if (normalizedAction === 'bundle_review') {
    openComposerAt(point.x, point.y, composerAnchorFromContext(context));
    showStatus('review notes ready');
    return true;
  }

  if (normalizedAction === 'dispatch_execute') {
    if (context.artifactKind === 'github_pr' && item) {
      return Boolean(showItemSidebarReviewMenu(item, point.x, point.y));
    }
    const mailDraftMatchesContext = Number(state.mailDraft?.artifactId || 0) === context.artifactID
      || Number(state.mailDraft?.itemId || 0) === context.itemID;
    if (mailDraftMatchesContext && (state.mailDraft?.artifactId || state.mailDraft?.itemId)) {
      await sendActiveMailDraft();
      return true;
    }
    if (item && artifactSupportsMailActions(context.artifactKind)) {
      await launchReplyAuthoring(item);
      showStatus('draft a reply before dispatching');
      return true;
    }
    if (item) {
      return Boolean(await performItemSidebarTriage(item, 'done'));
    }
    showStatus('nothing ready to dispatch');
    return false;
  }

  if (normalizedAction === 'track_item') {
    if (item) {
      state.itemSidebarActiveItemID = Number(item?.id || 0);
      showStatus('already tracked as item');
      return true;
    }
    if (context.artifactID > 0 || context.title) {
      await trackCurrentArtifactAsItem(context);
      return true;
    }
    showStatus('nothing to track');
    return false;
  }

  if (normalizedAction === 'delegate_actor') {
    if (!item) {
      showStatus('track this first to delegate it');
      return false;
    }
    return Boolean(await showItemSidebarDelegateMenu(item, point.x, point.y));
  }

  showStatus(`${canonicalActionLabel(normalizedAction) || normalizedAction} is not available`);
  return false;
}
