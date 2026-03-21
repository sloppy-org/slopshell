import { apiURL, appURL } from './paths.js';

function activeSection() {
  const path = window.location.pathname.replace(/\/+$/, '');
  if (path.endsWith('/manage/hotword')) return 'hotword';
  if (path.endsWith('/manage/models')) return 'models';
  if (path.endsWith('/manage/voices')) return 'voices';
  return 'manage';
}

function setActiveNavigation(section: string) {
  document.querySelectorAll('[data-manage-link]').forEach((node) => {
    if (!(node instanceof HTMLAnchorElement)) return;
    const active = node.dataset.manageLink === section;
    node.setAttribute('aria-current', active ? 'page' : 'false');
  });
  document.querySelectorAll('[data-manage-section]').forEach((node) => {
    if (!(node instanceof HTMLElement)) return;
    node.hidden = node.dataset.manageSection !== section;
  });
}

function card(label: string, value: string) {
  const node = document.createElement('article');
  node.className = 'manage-card';
  node.innerHTML = `
    <p class="manage-card-label">${label}</p>
    <p class="manage-card-value">${value}</p>
  `;
  return node;
}

function row(title: string, detail: string, actionLabel = '', actionHref = '') {
  const node = document.createElement('div');
  node.className = 'manage-row';
  const info = document.createElement('div');
  info.innerHTML = `<strong>${title}</strong><span>${detail}</span>`;
  node.appendChild(info);
  if (actionLabel && actionHref) {
    const link = document.createElement('a');
    link.className = 'manage-link';
    link.href = actionHref;
    link.textContent = actionLabel;
    node.appendChild(link);
  }
  return node;
}

async function loadData() {
  const [runtimeResp, hotwordResp] = await Promise.all([
    fetch(apiURL('runtime'), { cache: 'no-store' }),
    fetch(apiURL('hotword/status'), { cache: 'no-store' }),
  ]);
  if (!runtimeResp.ok) {
    throw new Error(`runtime metadata failed: HTTP ${runtimeResp.status}`);
  }
  if (!hotwordResp.ok) {
    throw new Error(`hotword status failed: HTTP ${hotwordResp.status}`);
  }
  return {
    runtime: await runtimeResp.json(),
    hotword: await hotwordResp.json(),
  };
}

function renderDashboard(runtime: Record<string, any>, hotword: Record<string, any>) {
  const host = document.getElementById('manage-dashboard-cards');
  if (!(host instanceof HTMLElement)) return;
  host.replaceChildren(
    card('Version', String(runtime?.version || 'unknown')),
    card('Live policy', String(runtime?.live_policy || 'manual')),
    card('Silent mode', runtime?.silent_mode ? 'On' : 'Off'),
    card('Hotword', hotword?.ready ? 'Ready' : 'Missing assets'),
  );
}

function renderHotword(hotword: Record<string, any>) {
  const host = document.getElementById('manage-hotword-status');
  if (!(host instanceof HTMLElement)) return;
  const missing = Array.isArray(hotword?.missing) && hotword.missing.length
    ? hotword.missing.join(', ')
    : 'All runtime assets are present.';
  const model = hotword?.model && typeof hotword.model === 'object'
    ? String(hotword.model.file || 'sloppy.onnx')
    : 'sloppy.onnx';
  host.replaceChildren(
    row('Runtime status', hotword?.ready ? 'The browser hotword runtime can start immediately.' : `Missing: ${missing}`),
    row('Model file', model, 'Open hotword test', appURL('static/hotword-test.html')),
    row('Training entry', 'Wake word management is split onto this management surface so training and model rollout stay out of the canvas.', 'Open dashboard', appURL('manage')),
  );
}

function renderModels(runtime: Record<string, any>) {
  const host = document.getElementById('manage-models-status');
  if (!(host instanceof HTMLElement)) return;
  const efforts = Array.isArray(runtime?.available_reasoning_efforts)
    ? runtime.available_reasoning_efforts.join(', ')
    : 'low, medium, high, xhigh';
  host.replaceChildren(
    row('Runtime version', String(runtime?.version || 'unknown')),
    row('Live policy', String(runtime?.live_policy || 'dialogue')),
    row('Reasoning efforts', efforts),
  );
}

function renderVoices(runtime: Record<string, any>) {
  const host = document.getElementById('manage-voices-status');
  if (!(host instanceof HTMLElement)) return;
  host.replaceChildren(
    row('TTS service', runtime?.tts_enabled ? 'Enabled' : 'Disabled'),
    row('Voice output', runtime?.silent_mode ? 'Silent mode is active.' : 'Voice playback is active.'),
    row('Hotword monitor', 'Use the hotword page to inspect wake-word readiness and open the current browser-side test harness.', 'Hotword tools', appURL('manage/hotword')),
  );
}

async function bootstrapManage() {
  const section = activeSection();
  setActiveNavigation(section);
  try {
    const { runtime, hotword } = await loadData();
    renderDashboard(runtime, hotword);
    renderHotword(hotword);
    renderModels(runtime);
    renderVoices(runtime);
  } catch (err) {
    const message = String(err?.message || err || 'unknown error');
    const host = document.getElementById(`manage-${section === 'manage' ? 'dashboard-cards' : `${section}-status`}`);
    if (host instanceof HTMLElement) {
      host.replaceChildren(row('Load failed', message));
    }
  }
}

void bootstrapManage();
