import type { Page } from '@playwright/test';
import { expect, openLiveApp, test } from './live';
import { authenticate, clearLiveChat } from './helpers';
import {
  assistantReplyCount,
  browserWsTtsToStt,
  resetCircleRuntimeState,
  setCircleToggle,
  setLiveSession,
  submitVoiceTranscript,
  submitPrompt,
  waitForAssistantReply,
  waitForLiveAppReady,
} from './live-ui';

async function sendPromptAndExpect(page: Page, prompt: string, needle: string) {
  const before = await page.locator('#chat-history .chat-message.chat-assistant:not(.is-pending)').count();
  await submitPrompt(page, prompt);
  const reply = await waitForAssistantReply(page, before, needle, 120_000);
  expect(reply.toLowerCase()).toContain(needle.toLowerCase());
}

async function expectCanvasFlowchart(page: Page, expectedTerms: string[]) {
  await expect.poll(async () => {
    const text = String(await page.locator('#canvas-text').textContent() || '').trim();
    if (!text) return '';
    return text;
  }, {
    timeout: 120_000,
    intervals: [500, 1_000, 2_000, 4_000],
  }).not.toBe('');
  const canvasText = String(await page.locator('#canvas-text').textContent() || '').trim();
  expect(canvasText.length).toBeGreaterThan(60);
  expect(canvasText.split('\n').filter((line) => line.trim().length > 0).length).toBeGreaterThanOrEqual(4);
  const lower = canvasText.toLowerCase();
  for (const term of expectedTerms) {
    expect(lower).toContain(term.toLowerCase());
  }
}

test.describe('local llm conversation flows @local-only', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('usual typed chat returns a local model answer', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await sendPromptAndExpect(page, 'Reply with the single word ORBIT.', 'orbit');
  });

  test('fast typed chat returns a local model answer', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'fast', true);
    await sendPromptAndExpect(page, 'Reply with the single word RIVET.', 'rivet');
  });

  test('silent typed chat returns a local model answer', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await expect(page.locator('body')).toHaveClass(/silent-mode/);
    await sendPromptAndExpect(page, 'Reply with the single word KESTREL.', 'kestrel');
  });

  test('dialogue typed flow returns a local model answer while dialogue stays active', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'dialogue', true);
    await sendPromptAndExpect(page, 'Reply with the single word HARBOR.', 'harbor');
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
  });

  test('meeting typed flow returns a local model answer while meeting stays active', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'meeting', true);
    await sendPromptAndExpect(page, 'Reply with the single word LANTERN.', 'lantern');
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  });

  test('usual local mode can open the README on canvas through the local tool path', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);

    const before = await page.locator('#chat-history .chat-message.chat-assistant:not(.is-pending)').count();
    await submitPrompt(page, 'Display the README on canvas.');
    await waitForAssistantReply(page, before, '', 120_000);
    await expect(page.locator('#canvas-text')).toContainText('Tabura', { timeout: 120_000 });
  });

  test('meeting typed flow creates German canvas flowchart content', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'meeting', true);

    const before = await assistantReplyCount(page);
    await submitPrompt(page, 'Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas.');
    await waitForAssistantReply(page, before, '', 120_000);
    await expectCanvasFlowchart(page, ['fusion', 'plasma']);
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  });

  test('meeting typed flow creates English canvas flowchart content', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'meeting', true);

    const before = await assistantReplyCount(page);
    await submitPrompt(page, 'Please draw a flowchart on the canvas showing how a fusion reactor works.');
    await waitForAssistantReply(page, before, '', 120_000);
    await expectCanvasFlowchart(page, ['fusion', 'reactor']);
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  });

  test('meeting voice flow creates German canvas flowchart content through Piper and STT', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'meeting', true);

    const before = await assistantReplyCount(page);
    const roundtrip = await browserWsTtsToStt(
      page,
      'Computer, bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas.',
      'http',
      'de',
    );
    expect(String(roundtrip.transcript || '').trim().length).toBeGreaterThan(10);
    const submit = await submitVoiceTranscript(page, String(roundtrip.transcript || '').trim(), { silent: true });
    expect(submit.status).toBe(200);
    await waitForAssistantReply(page, before, '', 120_000);
    await expectCanvasFlowchart(page, ['fusion', 'plasma']);
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  });

  test('meeting voice flow creates English canvas flowchart content through Piper and STT', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'meeting', true);

    const before = await assistantReplyCount(page);
    const roundtrip = await browserWsTtsToStt(
      page,
      'Computer, please draw a flowchart on the canvas showing how a fusion reactor works.',
      'ws',
      'en',
    );
    expect(String(roundtrip.transcript || '').trim().length).toBeGreaterThan(10);
    const submit = await submitVoiceTranscript(page, String(roundtrip.transcript || '').trim(), { silent: true });
    expect(submit.status).toBe(200);
    await waitForAssistantReply(page, before, '', 120_000);
    await expectCanvasFlowchart(page, ['fusion', 'reactor']);
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  });
});
