import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const state = app.getState();
    return state.chatWs && state.chatWs.readyState === (window as any).WebSocket.OPEN;
  }, null, { timeout: 5_000 });
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    const sessionId = String(app?.getState?.().chatSessionId || '');
    const sessions = (window as any).__mockWsSessions || [];
    const chatWs = sessions.find((ws: any) => typeof ws.url === 'string'
      && ws.url.includes('/ws/chat/')
      && (!sessionId || ws.url.includes(`/ws/chat/${sessionId}`)));
    if (chatWs?.injectEvent) {
      chatWs.injectEvent(eventPayload);
    }
  }, payload);
}

test('assistant output shows provider label and model metadata', async ({ page }) => {
  await waitReady(page);

  await injectChatEvent(page, { type: 'turn_started', turn_id: 'provider-turn-1' });
  await injectChatEvent(page, {
    type: 'assistant_output',
    role: 'assistant',
    turn_id: 'provider-turn-1',
    message: 'Using the cloud tier.',
    provider: 'openai',
    provider_label: 'OpenAI',
    provider_model: 'gpt-5.3-codex-spark',
  });

  const row = page.locator('.chat-message.chat-assistant').last();
  const label = row.locator('.chat-assistant-label');
  await expect(label).toHaveText('OpenAI');
  await expect(label).toHaveAttribute('title', 'gpt-5.3-codex-spark');
  await expect(row).toHaveAttribute('data-provider', 'openai');
});

test('unknown provider falls back to Assistant', async ({ page }) => {
  await waitReady(page);

  await injectChatEvent(page, {
    type: 'assistant_output',
    role: 'assistant',
    turn_id: 'provider-turn-2',
    message: 'Fallback label works.',
  });

  const label = page.locator('.chat-message.chat-assistant .chat-assistant-label').last();
  await expect(label).toHaveText('Assistant');
});
