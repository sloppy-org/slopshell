import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return s.chatWs?.readyState === wsOpen && s.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 8_000 });
}

async function openSidebarTab(page: Page, label: 'Inbox' | 'Waiting' | 'Someday' | 'Done') {
  const pane = page.locator('#pr-file-pane');
  const isOpen = await pane.evaluate((node) => node.classList.contains('is-open'));
  if (!isOpen) {
    await page.locator('#edge-left-tap').click();
  }
  await expect(pane).toHaveClass(/is-open/);
  await page.locator('.sidebar-tab', { hasText: label }).click();
  await expect(page.locator('.sidebar-tab.is-active')).toContainText(label);
}

async function expectCanonicalActions(page: Page, actions: string[]) {
  for (const action of actions) {
    await expect(page.locator(`#canvas-text [data-canonical-action="${action}"]`)).toBeVisible();
  }
}

test('artifact taxonomy keeps every stored kind on canonical canvas surfaces', async ({ page }) => {
  await waitReady(page);

  const taxonomy = await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/artifact-taxonomy.js');
    return {
      actions: mod.CANONICAL_ACTION_SEMANTICS,
      specs: mod.ARTIFACT_KIND_TAXONOMY,
    };
  });

  expect(Object.keys(taxonomy.specs).sort()).toEqual([
    'annotation',
    'document',
    'email',
    'email_thread',
    'external_note',
    'external_task',
    'github_issue',
    'github_pr',
    'idea_note',
    'image',
    'markdown',
    'pdf',
    'plan_note',
    'reference',
    'transcript',
  ]);

  for (const spec of Object.values(taxonomy.specs as Record<string, any>)) {
    expect(spec.interaction_model).toBe('canonical_canvas');
    expect(['text_artifact', 'pdf_artifact', 'image_artifact']).toContain(spec.canvas_surface);
    for (const action of spec.actions as string[]) {
      expect(taxonomy.actions).toContain(action);
    }
  }
});

test('plan notes and GitHub issues expose taxonomy-driven canonical actions without mail quick actions', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);

  await openSidebarTab(page, 'Someday');
  await page.locator('#pr-file-list .pr-file-item[data-item-id="301"]').click();
  await expect(page.locator('#canvas-text')).toContainText('Gesture backlog');
  await expectCanonicalActions(page, ['open_show', 'annotate_capture', 'compose', 'bundle_review', 'track_item']);
  await expect(page.locator('#canvas-new-mail-trigger')).toHaveCount(0);
  await expect(page.locator('#canvas-reply-mail-trigger')).toHaveCount(0);

  await openSidebarTab(page, 'Done');
  await page.locator('#pr-file-list .pr-file-item[data-item-id="401"]').click();
  await expect(page.locator('#canvas-text')).toContainText('Capture checklist');
  await expectCanonicalActions(page, ['open_show', 'annotate_capture', 'compose', 'bundle_review', 'dispatch_execute', 'track_item']);
  await expect(page.locator('#canvas-new-mail-trigger')).toHaveCount(0);
  await expect(page.locator('#canvas-reply-mail-trigger')).toHaveCount(0);
});

test('mail threads keep canonical text canvas rendering, taxonomy actions, and mail quick actions', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);

  await page.evaluate(() => {
    (window as any).__setItemSidebarData({
      inbox: [{
        id: 105,
        title: 'Urgent follow-up',
        state: 'inbox',
        sphere: 'private',
        artifact_id: 505,
        source: 'exchange',
        source_ref: 'thread-505',
        artifact_title: 'Urgent follow-up',
        artifact_kind: 'email_thread',
        actor_name: 'Ada',
        created_at: '2026-03-08 10:04:00',
        updated_at: '2026-03-08 10:05:00',
      }],
      waiting: [],
      someday: [],
      done: [],
    });
  });

  await openSidebarTab(page, 'Inbox');
  await page.locator('#pr-file-list .pr-file-item[data-item-id="105"]').click();
  await expect(page.locator('#canvas-text')).toContainText('Urgent follow-up');
  await expect(page.locator('#canvas-text')).toContainText('Need a response before tomorrow morning.');
  await expectCanonicalActions(page, ['open_show', 'annotate_capture', 'compose', 'bundle_review', 'dispatch_execute', 'track_item']);
  await expect(page.locator('#reply-mail-trigger')).toBeVisible();

  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);
  await expect(page.locator('#canvas-new-mail-trigger')).toBeVisible();
  await expect(page.locator('#canvas-reply-mail-trigger')).toBeVisible();
});
