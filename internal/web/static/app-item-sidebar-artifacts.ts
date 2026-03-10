import * as env from './app-env.js';
import * as context from './app-context.js';
import {
  artifactCanvasEventKind,
  artifactSupportsMailActions,
  artifactUsesThreadHTML,
} from './artifact-taxonomy.js';

const { apiURL } = env;
const { refs, SIDEBAR_IMAGE_EXTENSIONS } = context;

const applyCanvasArtifactEvent = (...args) => refs.applyCanvasArtifactEvent(...args);
const normalizeDisplayText = (...args) => refs.normalizeDisplayText(...args);
const openMailDraftArtifact = (...args) => refs.openMailDraftArtifact(...args);

export function parseSidebarArtifactMeta(raw) {
  const text = String(raw || '').trim();
  if (!text) return {};
  try {
    const parsed = JSON.parse(text);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch (_) {
    return {};
  }
}

export function ideaRefinementHeading(entry) {
  const explicit = String(entry?.heading || '').trim();
  if (explicit) return explicit;
  const kind = String(entry?.kind || '').trim().toLowerCase();
  if (kind === 'expand') return 'Expansion';
  if (kind === 'pros_cons') return 'Pros and Cons';
  if (kind === 'alternatives') return 'Alternatives';
  if (kind === 'implementation') return 'Implementation Outline';
  return 'Idea Notes';
}

export function appendIdeaPromotionPreview(detail, preview) {
  const target = String(preview?.target || '').trim().toLowerCase();
  if (!target) return;
  detail.push('', '## Promotion Review', '');
  if (target === 'task') {
    detail.push('- Pending: task draft');
    detail.push('- Confirm with: `create this idea task`');
  } else if (target === 'items') {
    detail.push('- Pending: item proposals');
    detail.push('- Confirm with: `create these idea items` or `create selected idea items 1,2`');
  } else if (target === 'github') {
    detail.push('- Pending: GitHub issue draft');
    detail.push('- Confirm with: `create this idea GitHub issue`');
  }
  detail.push('- Optional: add `and mark this idea done` or `and keep this idea`');
  if (target === 'github') {
    const title = String(preview?.issue?.title || '').trim();
    const body = String(preview?.issue?.body || '').trim();
    if (title) {
      detail.push('', `### ${title}`);
    }
    if (body) {
      detail.push('', body);
    }
    return;
  }
  const candidates = Array.isArray(preview?.candidates) ? preview.candidates : [];
  candidates.forEach((entry, offset) => {
    const title = String(entry?.title || '').trim();
    if (!title) return;
    const index = Number(entry?.index || offset + 1) || (offset + 1);
    detail.push('', `### ${index}. ${title}`);
    const body = String(entry?.details || '').trim();
    if (body) {
      detail.push('', body);
    }
  });
}

export function appendIdeaPromotions(detail, promotions) {
  const records = Array.isArray(promotions) ? promotions : [];
  if (records.length === 0) return;
  detail.push('', '## Promotions');
  records.forEach((entry) => {
    const target = String(entry?.target || '').trim().toLowerCase();
    if (!target) return;
    let label = target;
    if (target === 'github') label = 'GitHub issue';
    let line = `- ${label}`;
    const count = Number(entry?.count || 0);
    if (count > 0) line += ` x${count}`;
    const createdAt = String(entry?.created_at || '').trim();
    if (createdAt) line += ` on ${createdAt}`;
    const refsList = Array.isArray(entry?.refs)
      ? entry.refs.map((ref) => String(ref || '').trim()).filter(Boolean)
      : [];
    if (refsList.length > 0) line += ` [${refsList.join(', ')}]`;
    detail.push(line);
  });
}

export function buildIdeaNoteMarkdown(title, artifactMeta) {
  const noteTitle = String(artifactMeta?.title || title || 'Idea').trim() || 'Idea';
  const notes = Array.isArray(artifactMeta?.notes)
    ? artifactMeta.notes.map((entry) => String(entry || '').trim()).filter(Boolean)
    : [];
  const transcript = String(artifactMeta?.transcript || '').trim();
  if (notes.length === 0 && transcript) {
    notes.push(transcript);
  }
  const detail = [
    `# ${noteTitle}`,
    '',
    '## Notes',
  ];
  if (notes.length > 0) {
    notes.forEach((note) => {
      detail.push(`- ${note}`);
    });
  } else {
    detail.push('- No notes yet.');
  }
  detail.push('', '## Context');
  const captureMode = String(artifactMeta?.capture_mode || '').trim();
  if (captureMode) detail.push(`- Captured: ${captureMode}`);
  const workspace = String(artifactMeta?.workspace || '').trim();
  if (workspace) detail.push(`- Workspace: ${workspace}`);
  const capturedAt = String(artifactMeta?.captured_at || '').trim();
  if (capturedAt) detail.push(`- Date: ${capturedAt}`);
  if (detail[detail.length - 1] === '## Context') {
    detail.push('- Date: unavailable');
  }
  const refinements = Array.isArray(artifactMeta?.refinements) ? artifactMeta.refinements : [];
  refinements.forEach((entry) => {
    const body = String(entry?.body || '').trim();
    if (!body) return;
    detail.push('', `## ${ideaRefinementHeading(entry)}`, '', body);
  });
  appendIdeaPromotionPreview(detail, artifactMeta?.promotion_preview);
  appendIdeaPromotions(detail, artifactMeta?.promotions);
  return detail.join('\n');
}

function sidebarEmailBody(meta: Record<string, any> = {}) {
  return String(
    meta.body
    || meta.body_text
    || meta.text
    || meta.summary
    || meta.content
    || meta.snippet
    || '',
  ).trim();
}

function sidebarJoinList(values) {
  if (!Array.isArray(values)) return '';
  return values
    .map((value) => String(value || '').trim())
    .filter(Boolean)
    .join(', ');
}

function buildEmailArtifactMarkdown(title, artifactMeta) {
  const subject = String(artifactMeta?.subject || title || 'Email').trim() || 'Email';
  const detail = [`# ${subject}`];
  const sender = String(artifactMeta?.sender || '').trim();
  if (sender) detail.push('', `From: ${sender}`);
  const recipients = sidebarJoinList(artifactMeta?.recipients);
  if (recipients) detail.push(`To: ${recipients}`);
  const date = String(artifactMeta?.date || '').trim();
  if (date) detail.push(`Date: ${date}`);
  const labels = sidebarJoinList(artifactMeta?.labels);
  if (labels) detail.push(`Contexts: ${labels}`);
  const body = sidebarEmailBody(artifactMeta);
  if (body) {
    detail.push('', body);
  }
  return detail.join('\n');
}

function buildEmailThreadArtifactMarkdown(title, artifactMeta) {
  const subject = String(artifactMeta?.subject || title || 'Email thread').trim() || 'Email thread';
  const detail = [`# ${subject}`];
  const participants = sidebarJoinList(artifactMeta?.participants);
  if (participants) detail.push('', `Participants: ${participants}`);
  const messageCount = Number(artifactMeta?.message_count || 0);
  if (messageCount > 0) detail.push(`Messages: ${messageCount}`);
  const messages = Array.isArray(artifactMeta?.messages) ? artifactMeta.messages : [];
  if (messages.length === 0) {
    const fallbackBody = sidebarEmailBody(artifactMeta);
    if (fallbackBody) detail.push('', fallbackBody);
    return detail.join('\n');
  }
  messages.forEach((entry, index) => {
    const sender = String(entry?.sender || '').trim() || `Message ${index + 1}`;
    const date = String(entry?.date || '').trim();
    const heading = date ? `## ${sender} (${date})` : `## ${sender}`;
    detail.push('', heading);
    const recipients = sidebarJoinList(entry?.recipients);
    if (recipients) detail.push('', `To: ${recipients}`);
    const body = sidebarEmailBody(entry);
    if (body) {
      detail.push('', body);
    }
  });
  return detail.join('\n');
}

const escapeHtml = (...args: any[]) => (env.escapeHtml as any)(...args);

export function buildEmailThreadHTML(title, artifactMeta) {
  const subject = String(artifactMeta?.subject || title || 'Email thread').trim() || 'Email thread';
  const messages = Array.isArray(artifactMeta?.messages) ? artifactMeta.messages : [];
  if (messages.length === 0) {
    return null;
  }
  const parts = [];
  parts.push(`<div class="thread-subject"><h1>${escapeHtml(subject)}</h1></div>`);
  const participants = sidebarJoinList(artifactMeta?.participants);
  if (participants) {
    parts.push(`<div class="thread-participants">Participants: ${escapeHtml(participants)}</div>`);
  }
  messages.forEach((entry, index) => {
    const sender = String(entry?.sender || '').trim() || `Message ${index + 1}`;
    const date = String(entry?.date || '').trim();
    const recipients = sidebarJoinList(entry?.recipients);
    const body = sidebarEmailBody(entry);
    const snippet = body ? String(body).slice(0, 80).replace(/\n/g, ' ').trim() : '';
    const isLast = index === messages.length - 1;
    const openAttr = isLast ? ' open' : '';
    const sentClass = String(entry?.sent || '').trim() === 'true' ? ' thread-message-sent' : '';
    parts.push(`<details class="thread-message${sentClass}" data-message-index="${escapeHtml(String(index))}"${openAttr}>`);
    parts.push(`<summary class="thread-message-summary">`);
    parts.push(`<strong>${escapeHtml(sender)}</strong>`);
    if (date) parts.push(` <span class="thread-date">${escapeHtml(date)}</span>`);
    if (snippet && !isLast) parts.push(` <span class="thread-snippet">${escapeHtml(snippet)}</span>`);
    parts.push(`</summary>`);
    parts.push(`<div class="thread-message-body">`);
    parts.push(`<div class="thread-message-headers">`);
    parts.push(`From: ${escapeHtml(sender)}`);
    if (recipients) parts.push(`<br>To: ${escapeHtml(recipients)}`);
    if (date) parts.push(`<br>Date: ${escapeHtml(date)}`);
    parts.push(`</div>`);
    if (body) {
      parts.push(`<div class="thread-message-content">${escapeHtml(body).replace(/\n/g, '<br>')}</div>`);
    }
    parts.push(`</div>`);
    parts.push(`</details>`);
  });
  const activeDrafts = Array.isArray(artifactMeta?.active_drafts) ? artifactMeta.active_drafts : [];
  activeDrafts.forEach((draftInfo) => {
    const draftId = Number(draftInfo?.artifact_id || draftInfo || 0);
    if (draftId <= 0) return;
    const draftSubject = String(draftInfo?.subject || '').trim() || 'Draft';
    const safeDraftId = escapeHtml(String(draftId));
    parts.push(`<details class="thread-message thread-message-draft" data-draft-id="${safeDraftId}">`);
    parts.push(`<summary class="thread-message-summary">`);
    parts.push(`<strong>${escapeHtml(draftSubject)}</strong>`);
    parts.push(` <span class="thread-draft-badge">Draft</span>`);
    parts.push(`</summary>`);
    parts.push(`<div class="thread-message-body">`);
    parts.push(`<p><a href="#" class="thread-draft-open" data-draft-id="${safeDraftId}">Open draft</a></p>`);
    parts.push(`</div>`);
    parts.push(`</details>`);
  });
  return parts.join('\n');
}

function buildSidebarCanvasMeta(item, artifactKind) {
  const normalizedKind = String(artifactKind || item?.artifact_kind || '').trim().toLowerCase();
  if (!artifactSupportsMailActions(normalizedKind)) {
    return undefined;
  }
  return {
    surface_default: 'annotate',
    item_id: Number(item?.id || 0),
    artifact_kind: normalizedKind,
  };
}

export function buildSidebarItemFallbackText(item, artifact = null) {
  const artifactMeta = parseSidebarArtifactMeta(artifact?.meta_json || '');
  const title = String(artifact?.title || item?.artifact_title || item?.title || 'Item').trim() || 'Item';
  const artifactKind = String(artifact?.kind || item?.artifact_kind || '').trim().toLowerCase();
  if (artifactKind === 'idea_note') {
    return buildIdeaNoteMarkdown(title, artifactMeta);
  }
  if (artifactKind === 'email') {
    return buildEmailArtifactMarkdown(title, artifactMeta);
  }
  if (artifactKind === 'email_thread') {
    return buildEmailThreadArtifactMarkdown(title, artifactMeta);
  }
  const detail = [
    `# ${title}`,
    '',
    `- Item: ${String(item?.title || title).trim() || title}`,
    `- Kind: ${normalizeDisplayText(artifact?.kind || item?.artifact_kind || 'note') || 'note'}`,
  ];
  const sourceRef = String(item?.source_ref || '').trim();
  if (sourceRef) detail.push(`- Source: ${sourceRef}`);
  const refURL = String(artifact?.ref_url || '').trim();
  if (refURL) detail.push(`- Link: ${refURL}`);
  const body = String(
    artifactMeta.transcript
    || artifactMeta.text
    || artifactMeta.body
    || artifactMeta.summary
    || artifactMeta.content
    || '',
  ).trim();
  if (body) {
    detail.push('', '## Details', '', body);
  }
  return detail.join('\n');
}

export async function openSidebarArtifactItem(item) {
  const artifactID = Number(item?.artifact_id || 0);
  const fallbackArtifactKind = String(item?.artifact_kind || '').trim().toLowerCase();
  if (artifactID > 0 && fallbackArtifactKind === 'email_draft') {
    return openMailDraftArtifact(artifactID);
  }
  if (artifactID <= 0) {
    return false;
  }
  const resp = await fetch(apiURL(`artifacts/${encodeURIComponent(String(artifactID))}`), { cache: 'no-store' });
  if (!resp.ok) {
    const detail = (await resp.text()).trim() || `HTTP ${resp.status}`;
    throw new Error(detail);
  }
  const payload = await resp.json();
  const artifact = payload?.artifact || {};
  const refPath = String(artifact?.ref_path || '').trim();
  const artifactKind = String(artifact?.kind || item?.artifact_kind || '').trim().toLowerCase();
  const canvasKind = artifactCanvasEventKind(artifactKind, refPath);
  if (refPath && !refPath.startsWith('/')) {
    if (canvasKind === 'pdf_artifact') {
      applyCanvasArtifactEvent({
        kind: canvasKind,
        event_id: `sidebar-item-${artifactID}-${Date.now()}`,
        title: String(artifact?.title || item?.artifact_title || item?.title || refPath),
        path: refPath,
      });
      return true;
    }
    if (canvasKind === 'image_artifact' || SIDEBAR_IMAGE_EXTENSIONS.has(`.${String(refPath.split('.').pop() || '').toLowerCase()}`)) {
      applyCanvasArtifactEvent({
        kind: 'image_artifact',
        event_id: `sidebar-item-${artifactID}-${Date.now()}`,
        title: String(artifact?.title || item?.artifact_title || item?.title || refPath),
        path: refPath,
      });
      return true;
    }
  }
  const textEvent: Record<string, any> = {
    kind: 'text_artifact',
    event_id: `sidebar-item-${artifactID}-${Date.now()}`,
    title: String(artifact?.title || item?.artifact_title || item?.title || 'Item'),
    text: buildSidebarItemFallbackText(item, artifact),
    meta: buildSidebarCanvasMeta(item, artifactKind),
  };
  if (artifactUsesThreadHTML(artifactKind)) {
    textEvent.threadMeta = parseSidebarArtifactMeta(artifact?.meta_json || '');
  }
  applyCanvasArtifactEvent(textEvent);
  return true;
}
