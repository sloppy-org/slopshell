import { expect, test, type Page } from '@playwright/test';

type Header = {
  id: string;
  date: string;
  sender: string;
  subject: string;
};

type HarnessMessage = Record<string, unknown>;

function plainTextEvent(eventID: string, text: string) {
  return {
    kind: 'text_artifact',
    event_id: eventID,
    title: 'Notes',
    text,
    meta: {},
  };
}

function mailEvent(eventID: string, provider: string, headers: Header[]) {
  return {
    kind: 'text_artifact',
    event_id: eventID,
    title: 'Mail Headers',
    text: '# Mail Headers',
    meta: {
      producer_mcp_url: 'http://127.0.0.1:8090/mcp',
      message_triage_v1: {
        provider,
        folder: 'INBOX',
        count: headers.length,
        headers,
      },
    },
  };
}

function imageEvent(eventID: string) {
  return {
    kind: 'image_artifact',
    event_id: eventID,
    title: 'Image',
    path: 'missing.png',
  };
}

async function renderArtifact(page: Page, event: Record<string, unknown>) {
  await page.waitForFunction(() => typeof (window as any).renderHarnessArtifact === 'function');
  await page.evaluate((payload) => {
    // @ts-expect-error injected by harness module
    window.renderHarnessArtifact(payload);
  }, event);
}

async function clearHarnessMessages(page: Page) {
  await page.waitForFunction(() => typeof (window as any).clearHarnessMessages === 'function');
  await page.evaluate(() => {
    // @ts-expect-error injected by harness module
    window.clearHarnessMessages();
  });
}

async function getHarnessMessages(page: Page): Promise<HarnessMessage[]> {
  await page.waitForFunction(() => typeof (window as any).getHarnessMessages === 'function');
  return page.evaluate(() => {
    // @ts-expect-error injected by harness module
    return window.getHarnessMessages();
  });
}

async function selectTextFromSelector(page: Page, selector: string) {
  const selected = await page.evaluate((sel) => {
    const root = document.querySelector(sel);
    if (!root) return false;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    let node = walker.nextNode();
    while (node && !String(node.textContent || '').trim()) {
      node = walker.nextNode();
    }
    if (!node) return false;
    const text = String(node.textContent || '');
    const start = Math.max(0, text.search(/\S/));
    const range = document.createRange();
    range.setStart(node, start);
    range.setEnd(node, text.length);
    const selection = window.getSelection();
    if (!selection) return false;
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event('selectionchange'));
    root.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
    return true;
  }, selector);
  if (!selected) throw new Error(`unable to select text from ${selector}`);
}

async function waitForLastSelectionMessage(page: Page): Promise<HarnessMessage> {
  await expect.poll(async () => {
    const messages = await getHarnessMessages(page);
    return messages.filter((m) => m.kind === 'text_selection').length;
  }).toBeGreaterThan(0);

  const messages = await getHarnessMessages(page);
  const selections = messages.filter((m) => m.kind === 'text_selection');
  return selections[selections.length - 1];
}

async function waitForLastMessageOfKind(page: Page, kind: string): Promise<HarnessMessage> {
  await expect.poll(async () => {
    const messages = await getHarnessMessages(page);
    return messages.filter((m) => m.kind === kind).length;
  }).toBeGreaterThan(0);

  const messages = await getHarnessMessages(page);
  const matches = messages.filter((m) => m.kind === kind);
  return matches[matches.length - 1];
}

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/playwright/harness.html');
  await clearHarnessMessages(page);
});

test('non-mail text artifacts enable review selection payloads', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-text-1', '# Header\nAlpha Beta'));
  await selectTextFromSelector(page, '#canvas-text');
  const msg = await waitForLastSelectionMessage(page);

  expect(msg.event_id).toBe('evt-text-1');
  expect(msg.artifact_id).toBe('evt-text-1');
  expect(String(msg.text || '')).toContain('Header');
  expect(Number(msg.line_start)).toBeGreaterThanOrEqual(1);
  expect(Number(msg.line_end)).toBeGreaterThanOrEqual(Number(msg.line_start));
});

test('mail text artifacts keep the same review selection behavior', async ({ page }) => {
  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'gmail',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: true,
        },
      },
    });
  });

  await renderArtifact(page, mailEvent('evt-mail-1', 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'Quarterly Review' },
  ]));
  await selectTextFromSelector(page, 'tr[data-message-id="m1"] td:nth-child(3)');
  const msg = await waitForLastSelectionMessage(page);

  expect(msg.event_id).toBe('evt-mail-1');
  expect(msg.artifact_id).toBe('evt-mail-1');
  expect(String(msg.text || '')).toContain('Quarterly Review');
  expect(Number(msg.line_start)).toBeGreaterThanOrEqual(1);
});

test('right-click inline comment popover submits a comment_point draft mark', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-comment-1', '# Notes\nInline comment target text'));
  await page.click('#canvas-text', { button: 'right', position: { x: 80, y: 64 } });

  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();
  await popover.locator('input').fill('Check this sentence.');
  await popover.locator('button[type="submit"]').click();
  await expect(popover).toHaveCount(0);

  const markSet = await waitForLastMessageOfKind(page, 'mark_set');
  expect(markSet.artifact_id).toBe('evt-comment-1');
  expect(markSet.intent).toBe('draft');
  expect(markSet.type).toBe('comment_point');
  expect(markSet.target_kind).toBe('text_range');
  expect(markSet.comment).toBe('Check this sentence.');
  expect(Number((markSet.target as any).line_start)).toBeGreaterThanOrEqual(1);
  expect(Number((markSet.target as any).start_offset)).toBeGreaterThanOrEqual(0);
});

test('highlight selection popover submits with Enter key as a highlight draft mark', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-highlight-1', '# Notes\nHighlight this sentence now'));
  await selectTextFromSelector(page, '#canvas-text');

  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();
  await popover.locator('input').fill('Add context to this highlight.');
  await popover.locator('input').press('Enter');
  await expect(popover).toHaveCount(0);

  const markSet = await waitForLastMessageOfKind(page, 'mark_set');
  expect(markSet.artifact_id).toBe('evt-highlight-1');
  expect(markSet.intent).toBe('draft');
  expect(markSet.type).toBe('highlight');
  expect(markSet.target_kind).toBe('text_range');
  expect(markSet.comment).toBe('Add context to this highlight.');
  expect(Number((markSet.target as any).line_start)).toBeGreaterThanOrEqual(1);
  expect(Number((markSet.target as any).end_offset)).toBeGreaterThan(Number((markSet.target as any).start_offset));
});

test('highlight review flow emits mark_set with comment and mark_commit for persistence', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-highlight-persist', '# Notes\nPersist this highlighted sentence'));
  await selectTextFromSelector(page, '#canvas-text');

  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();
  await popover.locator('input').fill('Persist this note.');
  await popover.locator('input').press('Enter');
  await expect(popover).toHaveCount(0);

  const draftMarkSet = await waitForLastMessageOfKind(page, 'mark_set');
  expect(draftMarkSet.artifact_id).toBe('evt-highlight-persist');
  expect(draftMarkSet.intent).toBe('draft');
  expect(draftMarkSet.type).toBe('highlight');
  expect(draftMarkSet.comment).toBe('Persist this note.');

  await page.evaluate(() => {
    const btn = document.getElementById('btn-canvas-commit') as HTMLButtonElement | null;
    btn?.click();
  });

  const commitMessage = await waitForLastMessageOfKind(page, 'mark_commit');
  expect(commitMessage.session_id).toBe('local');
  expect(commitMessage.include_draft).toBe(true);
});

test('popover Escape cancels and returns focus to the review canvas', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-comment-esc', '# Notes\nEscape key should cancel this popover'));
  await page.evaluate(() => {
    const existing = document.getElementById('review-focus-anchor');
    if (existing) existing.remove();
    const focusAnchor = document.createElement('button');
    focusAnchor.id = 'review-focus-anchor';
    focusAnchor.type = 'button';
    focusAnchor.textContent = 'focus anchor';
    document.body.appendChild(focusAnchor);
    focusAnchor.focus();
  });

  await page.click('#canvas-text', { button: 'right', position: { x: 88, y: 72 } });
  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();
  await expect(popover.locator('input')).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(popover).toHaveCount(0);
  await page.waitForTimeout(50);

  const activeElementId = await page.evaluate(() => document.activeElement?.id || '');
  expect(activeElementId).toBe('canvas-text');
  const messages = await getHarnessMessages(page);
  expect(messages.filter((m) => m.kind === 'mark_set')).toHaveLength(0);
});

test('highlight selection cancel clears draft without creating a mark', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-highlight-2', '# Notes\nCancel this highlighted draft'));
  await selectTextFromSelector(page, '#canvas-text');

  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();
  await popover.locator('button[data-review-cancel]').click();
  await expect(popover).toHaveCount(0);

  await page.waitForTimeout(50);
  const messages = await getHarnessMessages(page);
  expect(messages.filter((m) => m.kind === 'mark_set')).toHaveLength(0);
  const clearDrafts = messages.filter((m) => m.kind === 'mark_clear_draft');
  expect(clearDrafts.length).toBeGreaterThan(0);
  expect((clearDrafts[clearDrafts.length - 1] as any).artifact_id).toBe('evt-highlight-2');
});

test('right-click inline comment popover cancel and outside click do not create marks', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-comment-2', '# Notes\nCancel path text'));

  await page.click('#canvas-text', { button: 'right', position: { x: 82, y: 66 } });
  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();
  await popover.locator('button[data-review-cancel]').click();
  await expect(popover).toHaveCount(0);
  await page.waitForTimeout(50);
  let messages = await getHarnessMessages(page);
  expect(messages.filter((m) => m.kind === 'mark_set')).toHaveLength(0);

  await page.click('#canvas-text', { button: 'right', position: { x: 96, y: 86 } });
  await expect(popover).toBeVisible();
  await page.click('#canvas-header');
  await expect(popover).toHaveCount(0);
  await page.waitForTimeout(50);
  messages = await getHarnessMessages(page);
  expect(messages.filter((m) => m.kind === 'mark_set')).toHaveLength(0);
});

test('popover opened near viewport edge stays within visible text canvas bounds', async ({ page }) => {
  await renderArtifact(page, plainTextEvent('evt-comment-edge', '# Notes\nEdge positioning test text'));

  const box = await page.locator('#canvas-text').boundingBox();
  if (!box) throw new Error('expected #canvas-text to have a bounding box');

  await page.click('#canvas-text', {
    button: 'right',
    position: { x: Math.max(2, box.width - 2), y: Math.max(2, box.height - 2) },
  });

  const popover = page.locator('[data-review-popover="true"]');
  await expect(popover).toBeVisible();

  const bounds = await page.evaluate(() => {
    const root = document.getElementById('canvas-text');
    const overlay = root?.querySelector('[data-review-popover="true"]');
    if (!root || !overlay) return null;
    const rootRect = root.getBoundingClientRect();
    const overlayRect = overlay.getBoundingClientRect();
    return {
      rootLeft: rootRect.left,
      rootTop: rootRect.top,
      rootRight: rootRect.right,
      rootBottom: rootRect.bottom,
      overlayLeft: overlayRect.left,
      overlayTop: overlayRect.top,
      overlayRight: overlayRect.right,
      overlayBottom: overlayRect.bottom,
    };
  });
  if (!bounds) throw new Error('expected popover bounds to be measurable');

  expect(bounds.overlayLeft).toBeGreaterThanOrEqual(bounds.rootLeft);
  expect(bounds.overlayTop).toBeGreaterThanOrEqual(bounds.rootTop);
  expect(bounds.overlayRight).toBeLessThanOrEqual(bounds.rootRight);
  expect(bounds.overlayBottom).toBeLessThanOrEqual(bounds.rootBottom);
});

test('switching artifacts tears down stale review and mail handlers', async ({ page }) => {
  await renderArtifact(page, mailEvent('evt-mail-2', 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'Switch Test' },
  ]));

  const before = await page.evaluate(() => {
    const root = document.getElementById('canvas-text') as any;
    return {
      hasSelectionHandler: Boolean(root?._selectionHandler),
      hasMailClickHandler: Boolean(root?._mailClickHandler),
      hasMailPointerDownHandler: Boolean(root?._mailPointerDownHandler),
      hasMailDetailKeyDownHandler: Boolean(root?._mailDetailKeyDownHandler),
      hasReviewContextMenuHandler: Boolean(root?._reviewContextMenuHandler),
      hasMailClass: root?.classList.contains('mail-artifact') || false,
    };
  });
  expect(before.hasSelectionHandler).toBe(true);
  expect(before.hasMailClickHandler).toBe(true);
  expect(before.hasMailPointerDownHandler).toBe(true);
  expect(before.hasReviewContextMenuHandler).toBe(true);
  expect(before.hasMailClass).toBe(true);

  await clearHarnessMessages(page);
  await renderArtifact(page, imageEvent('evt-image-1'));

  const after = await page.evaluate(() => {
    const root = document.getElementById('canvas-text') as any;
    return {
      hasSelectionHandler: Boolean(root?._selectionHandler),
      hasMailClickHandler: Boolean(root?._mailClickHandler),
      hasMailPointerDownHandler: Boolean(root?._mailPointerDownHandler),
      hasMailDetailKeyDownHandler: Boolean(root?._mailDetailKeyDownHandler),
      hasReviewContextMenuHandler: Boolean(root?._reviewContextMenuHandler),
      hasMailClass: root?.classList.contains('mail-artifact') || false,
    };
  });
  expect(after.hasSelectionHandler).toBe(false);
  expect(after.hasMailClickHandler).toBe(false);
  expect(after.hasMailPointerDownHandler).toBe(false);
  expect(after.hasMailDetailKeyDownHandler).toBe(false);
  expect(after.hasReviewContextMenuHandler).toBe(false);
  expect(after.hasMailClass).toBe(false);

  await page.evaluate(() => {
    const root = document.getElementById('canvas-text');
    if (!root) return;
    const cell = root.querySelector('tr[data-message-id="m1"] td');
    const text = cell?.firstChild;
    if (!text) return;
    const range = document.createRange();
    range.setStart(text, 0);
    range.setEnd(text, String(text.textContent || '').length);
    const selection = window.getSelection();
    if (!selection) return;
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event('selectionchange'));
  });
  await page.waitForTimeout(80);

  const messages = await getHarnessMessages(page);
  expect(messages).toHaveLength(0);
});
