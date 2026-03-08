import { expect, test } from '@playwright/test';

test.describe('capture page', () => {
  test('saves a typed note and stays outside the canvas shell', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/tests/playwright/capture-harness.html');

    await expect(page.locator('#capture-page')).toBeVisible();
    await expect(page.locator('#workspace')).toHaveCount(0);
    await expect(page.locator('#edge-left-tap')).toHaveCount(0);
    await expect(page.locator('#capture-save')).toBeDisabled();

    await page.locator('#capture-note').fill('Follow up with the review queue tomorrow morning. Capture the blockers too.');
    await expect(page.locator('#capture-save')).toBeEnabled();

    await page.locator('#capture-save').click();
    await expect(page.locator('#capture-note')).toHaveValue('');
    await expect(page.locator('#capture-status')).toContainText('Saved:');
    await expect(page.locator('#capture-save')).toBeDisabled();

    const requests = await page.evaluate(() => (window as any).__captureRequests);
    expect(requests).toHaveLength(1);
    expect(requests[0].title).toBe('Follow up with the review queue tomorrow morning.');
  });

  test('transcribes a voice memo and saves an artifact-backed inbox item', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/tests/playwright/capture-harness.html');

    await page.locator('#capture-record').click({ force: true });
    await page.locator('#capture-record').click({ force: true });

    await expect(page.locator('#capture-status')).toContainText('Saved: Voice memo from capture harness.');
    await expect(page.locator('#capture-retry')).toBeHidden();

    const transcribeRequests = await page.evaluate(() => (window as any).__captureTranscribeRequests);
    expect(transcribeRequests).toHaveLength(1);

    const artifactRequests = await page.evaluate(() => (window as any).__captureArtifactRequests);
    expect(artifactRequests).toHaveLength(1);
    expect(artifactRequests[0].kind).toBe('idea_note');
    expect(artifactRequests[0].title).toBe('Voice memo from capture harness.');
    expect(JSON.parse(String(artifactRequests[0].meta_json)).transcript).toBe(
      'Voice memo from capture harness. Follow up tomorrow morning.',
    );

    const itemRequests = await page.evaluate(() => (window as any).__captureRequests);
    expect(itemRequests).toHaveLength(1);
    expect(itemRequests[0].title).toBe('Voice memo from capture harness.');
    expect(itemRequests[0].artifact_id).toBe(1);

    const fetchRequests = await page.evaluate(() => (window as any).__captureFetchRequests);
    expect(fetchRequests).toHaveLength(3);
    expect(fetchRequests.some((request: { url: string }) => request.url.includes('/api/chat/'))).toBe(false);
  });

  test('keeps the memo available for retry after transcription failure', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/tests/playwright/capture-harness.html');

    await page.evaluate(() => {
      (window as any).__setCaptureTranscribeResponses([
        { status: 502, body: { error: 'sidecar unavailable' } },
        { status: 200, body: { text: 'Retry worked after the STT sidecar came back.' } },
      ]);
    });

    await page.locator('#capture-record').click({ force: true });
    await page.locator('#capture-record').click({ force: true });

    await expect(page.locator('#capture-status')).toContainText('Transcription failed. Retry this memo.');
    await expect(page.locator('#capture-retry')).toBeVisible();

    await page.locator('#capture-retry').click();

    await expect(page.locator('#capture-status')).toContainText('Saved: Retry worked after the STT sidecar came back.');
    await expect(page.locator('#capture-retry')).toBeHidden();

    const transcribeRequests = await page.evaluate(() => (window as any).__captureTranscribeRequests);
    expect(transcribeRequests).toHaveLength(2);

    const itemRequests = await page.evaluate(() => (window as any).__captureRequests);
    expect(itemRequests).toHaveLength(1);
    expect(itemRequests[0].title).toBe('Retry worked after the STT sidecar came back.');
  });

  test('toggles record state with the large capture button', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/tests/playwright/capture-harness.html');

    await expect(page.locator('body')).toHaveAttribute('data-capture-state', 'idle');
    await page.locator('#capture-record').click({ force: true });
    await expect(page.locator('body')).toHaveAttribute('data-capture-state', 'recording');
    await expect(page.locator('#capture-record')).toHaveAttribute('aria-pressed', 'true');

    await page.locator('#capture-record').click({ force: true });
    await expect(page.locator('body')).toHaveAttribute('data-capture-state', 'idle');
    await expect(page.locator('#capture-record')).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('#capture-status')).toContainText('Saved: Voice memo from capture harness.');
  });
});
