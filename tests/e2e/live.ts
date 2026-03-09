import base, { expect, type Page, type TestInfo } from '@playwright/test';
import { mkdir, writeFile } from 'fs/promises';
import { SERVER_URL } from './helpers';

type PlaytestMeta = {
  tested?: string;
  expected?: string;
  steps?: string[];
};

async function collectPageState(page: Page): Promise<Record<string, unknown>> {
  return page.evaluate(async () => {
    const localStorageState: Record<string, string> = {};
    const sessionStorageState: Record<string, string> = {};
    try {
      for (let i = 0; i < window.localStorage.length; i += 1) {
        const key = window.localStorage.key(i);
        if (!key) continue;
        localStorageState[key] = window.localStorage.getItem(key) || '';
      }
    } catch {}
    try {
      for (let i = 0; i < window.sessionStorage.length; i += 1) {
        const key = window.sessionStorage.key(i);
        if (!key) continue;
        sessionStorageState[key] = window.sessionStorage.getItem(key) || '';
      }
    } catch {}
    const edgeTop = document.getElementById('edge-top');
    const edgeRight = document.getElementById('edge-right');
    return {
      url: window.location.href,
      title: document.title,
      bodyClass: document.body?.className || '',
      activeElement:
        document.activeElement instanceof HTMLElement
          ? {
              tag: document.activeElement.tagName,
              id: document.activeElement.id || '',
              className: document.activeElement.className || '',
            }
          : null,
      overlayVisible: Boolean(document.getElementById('overlay')),
      floatingInputVisible: Boolean(document.getElementById('floating-input')),
      fileSidebarOpen: document.body?.classList.contains('file-sidebar-open') || false,
      edgeTopClass: edgeTop?.className || '',
      edgeRightClass: edgeRight?.className || '',
      viewport: {
        width: window.innerWidth,
        height: window.innerHeight,
      },
      localStorage: localStorageState,
      sessionStorage: sessionStorageState,
    };
  });
}

async function attachFailureArtifacts(page: Page, testInfo: TestInfo, browserLogs: string[]) {
  const failed = testInfo.status !== testInfo.expectedStatus;
  if (!failed) return;

  const artifactDir = testInfo.outputPath('playtest-failure');
  await mkdir(artifactDir, { recursive: true });

  const screenshotPath = testInfo.outputPath('playtest-failure', 'screenshot.png');
  try {
    await page.screenshot({ path: screenshotPath, fullPage: true });
    await testInfo.attach('playtest-screenshot', {
      path: screenshotPath,
      contentType: 'image/png',
    });
  } catch {}

  const pageStatePath = testInfo.outputPath('playtest-failure', 'page-state.json');
  try {
    const pageState = await collectPageState(page);
    await writeFile(pageStatePath, `${JSON.stringify(pageState, null, 2)}\n`, 'utf8');
    await testInfo.attach('page-state', {
      path: pageStatePath,
      contentType: 'application/json',
    });
  } catch (error) {
    await writeFile(
      pageStatePath,
      `${JSON.stringify({ error: String(error || 'collect page state failed') }, null, 2)}\n`,
      'utf8',
    );
    await testInfo.attach('page-state', {
      path: pageStatePath,
      contentType: 'application/json',
    });
  }

  if (browserLogs.length > 0) {
    const browserLogPath = testInfo.outputPath('playtest-failure', 'browser-logs.txt');
    await writeFile(browserLogPath, `${browserLogs.join('\n')}\n`, 'utf8');
    await testInfo.attach('browser-logs', {
      path: browserLogPath,
      contentType: 'text/plain',
    });
  }
}

export const test = base.extend({
  page: async ({ page }, use, testInfo) => {
    const browserLogs: string[] = [];
    page.on('console', (message) => {
      const location = message.location();
      const suffix = location?.url
        ? ` (${location.url}:${location.lineNumber || 0}:${location.columnNumber || 0})`
        : '';
      browserLogs.push(`[console:${message.type()}] ${message.text()}${suffix}`);
    });
    page.on('pageerror', (error) => {
      browserLogs.push(`[pageerror] ${error.stack || error.message}`);
    });
    page.on('requestfailed', (request) => {
      browserLogs.push(
        `[requestfailed] ${request.method()} ${request.url()} ${request.failure()?.errorText || 'request failed'}`,
      );
    });
    await use(page);
    await attachFailureArtifacts(page, testInfo, browserLogs);
  },
});

export { expect };

export async function applySessionCookie(page: Page, sessionToken: string) {
  if (!sessionToken) return;
  await page.context().addCookies([{
    name: 'tabura_session',
    value: sessionToken,
    url: SERVER_URL,
  }]);
}

export async function openLiveApp(page: Page, sessionToken: string) {
  await applySessionCookie(page, sessionToken);
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  await page.waitForFunction(() => {
    const app = (window as { _taburaApp?: { getState?: () => { activeProjectId?: string } } })._taburaApp;
    if (!app || typeof app.getState !== 'function') return false;
    const state = app.getState();
    return Boolean(String(state?.activeProjectId || '').trim());
  }, null, { timeout: 10_000 }).catch(() => {});
}

export function annotatePlaytest(testInfo: TestInfo, meta: PlaytestMeta) {
  if (meta.tested) {
    testInfo.annotations.push({ type: 'playtest-tested', description: meta.tested });
  }
  if (meta.expected) {
    testInfo.annotations.push({ type: 'playtest-expected', description: meta.expected });
  }
  if (meta.steps && meta.steps.length > 0) {
    testInfo.annotations.push({
      type: 'playtest-steps',
      description: meta.steps.join(' || '),
    });
  }
}
