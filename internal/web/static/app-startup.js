import { apiURL, clearCanvas } from './app-env.js';
import { refs, state } from './app-context.js';
import { bindUi } from './app-init.js';

const showStatus = (...args) => refs.showStatus(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const setYoloModeLocal = (...args) => refs.setYoloModeLocal(...args);
const readYoloModePreference = (...args) => refs.readYoloModePreference(...args);
const readSomedayReviewNudgePreference = (...args) => refs.readSomedayReviewNudgePreference(...args);
const setSomedayReviewNudgeEnabled = (...args) => refs.setSomedayReviewNudgeEnabled(...args);
const renderInkControls = (...args) => refs.renderInkControls(...args);
const switchProject = (...args) => refs.switchProject(...args);
const activeProject = (...args) => refs.activeProject(...args);
const hideCanvasColumn = (...args) => refs.hideCanvasColumn(...args);
const setTTSSilentMode = (...args) => refs.setTTSSilentMode(...args);
const isMobileSilent = (...args) => refs.isMobileSilent(...args);
const restoreRuntimeReloadContext = (...args) => refs.restoreRuntimeReloadContext(...args);
const consumeRuntimeReloadContext = (...args) => refs.consumeRuntimeReloadContext(...args);
const fetchRuntimeMeta = (...args) => refs.fetchRuntimeMeta(...args);
const applyRuntimePreferences = (...args) => refs.applyRuntimePreferences(...args);
const initHotwordLifecycle = (...args) => refs.initHotwordLifecycle(...args);
const resolveInitialProjectID = (...args) => refs.resolveInitialProjectID(...args);
const applyRuntimeReasoningEffortOptions = (...args) => refs.applyRuntimeReasoningEffortOptions(...args);
const fetchProjects = (...args) => refs.fetchProjects(...args);
const startRuntimeReloadWatcher = (...args) => refs.startRuntimeReloadWatcher(...args);
const startAssistantActivityWatcher = (...args) => refs.startAssistantActivityWatcher(...args);
const syncInteractionBodyState = (...args) => refs.syncInteractionBodyState(...args);
const showDisclaimerModal = (...args) => refs.showDisclaimerModal(...args);
const applyIPhoneFrameCorners = (...args) => refs.applyIPhoneFrameCorners(...args);
const initPanelMotionMode = (...args) => refs.initPanelMotionMode(...args);
const syncInkLayerSize = (...args) => refs.syncInkLayerSize(...args);

let bootstrapStarted = false;
let bootstrapErrorShown = false;

function showBootstrapError(message) {
  const text = String(message || 'Unknown error');
  if (bootstrapErrorShown) return;
  bootstrapErrorShown = true;
  const loginErr = document.getElementById('login-error');
  if (loginErr) loginErr.textContent = `Initialization failed: ${text}`;
  const loginView = document.getElementById('view-login');
  if (loginView) loginView.style.display = '';
  const mainView = document.getElementById('view-main');
  if (mainView) mainView.style.display = 'none';
}

function installBootstrapErrorHandlers() {
  window.addEventListener('error', (event) => {
    const msg = String(event?.error?.message || event?.message || '').trim();
    if (!msg) return;
    if (msg.includes('ResizeObserver loop limit exceeded')) return;
    showBootstrapError(msg);
  });

  window.addEventListener('unhandledrejection', (event) => {
    const reason = event?.reason;
    const msg = String(reason?.message || reason || '').trim();
    if (!msg) return;
    showBootstrapError(msg);
  });
}

function showSplash() {
  const project = activeProject();
  const name = project?.name || '';
  if (!name) return;
  const splash = document.createElement('div');
  splash.className = 'splash';
  splash.textContent = name;
  document.getElementById('view-main')?.appendChild(splash);
  window.setTimeout(() => splash.classList.add('fade-out'), 100);
  window.setTimeout(() => splash.remove(), 1700);
}

async function init() {
  state.pendingRuntimeReloadContext = consumeRuntimeReloadContext();
  setSomedayReviewNudgeEnabled(readSomedayReviewNudgePreference(), { persist: false });
  applyIPhoneFrameCorners();
  window.addEventListener('resize', () => {
    if (document.body.classList.contains('keyboard-open')) return;
    applyIPhoneFrameCorners();
    syncInkLayerSize();
    renderInkControls();
  });
  bindUi();
  syncInkLayerSize();
  renderInkControls();
  syncInteractionBodyState();
  updateAssistantActivityIndicator();
  startRuntimeReloadWatcher();
  startAssistantActivityWatcher();
  clearCanvas();
  hideCanvasColumn();
  showStatus('starting...');

  try {
    const runtime = await fetchRuntimeMeta();
    applyRuntimePreferences(runtime);
    renderInkControls();
    state.ttsEnabled = Boolean(runtime?.tts_enabled);
    applyRuntimeReasoningEffortOptions(runtime?.available_reasoning_efforts);
  } catch (_) {
    state.ttsEnabled = false;
    setYoloModeLocal(readYoloModePreference(), { persist: false, render: false });
  }
  await showDisclaimerModal().catch(() => {});
  setTTSSilentMode(state.ttsSilent, { persist: false, pinPanel: false });
  await initHotwordLifecycle();

  await fetchProjects();
  const initialProjectID = resolveInitialProjectID();
  if (!initialProjectID) throw new Error('no projects available');
  await switchProject(initialProjectID);
  if (isMobileSilent()) {
    const edgeRight = document.getElementById('edge-right');
    if (edgeRight) edgeRight.classList.add('edge-pinned');
  }
  restoreRuntimeReloadContext();
  showSplash();
  requestAnimationFrame(() => requestAnimationFrame(initPanelMotionMode));
}

async function authGate() {
  const loginView = document.getElementById('view-login');
  const mainView = document.getElementById('view-main');
  const resp = await fetch(apiURL('setup'));
  const data = await resp.json();
  if (data.authenticated) {
    if (loginView) loginView.style.display = 'none';
    return;
  }
  const loginForm = document.getElementById('login-form');
  const loginPassword = document.getElementById('login-password');
  const loginError = document.getElementById('login-error');
  const loginBtn = document.getElementById('btn-login');

  if (!data.has_password) {
    loginPassword.style.display = 'none';
    loginView.style.display = '';
    mainView.style.display = 'none';
    return new Promise(() => {});
  }

  loginView.style.display = '';
  mainView.style.display = 'none';

  await new Promise(() => {
    loginForm.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      loginError.textContent = '';
      const pw = loginPassword.value;
      if (!pw) return;
      if (loginBtn) loginBtn.disabled = true;
      try {
        const r = await fetch(apiURL('login'), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'same-origin',
          body: JSON.stringify({ password: pw }),
        });
        if (!r.ok) {
          const msg = (await r.text()).trim();
          loginError.textContent = msg || `Error ${r.status}`;
          return;
        }
        loginPassword.value = '';
        window.location.replace(window.location.href);
      } catch (err) {
        loginError.textContent = String(err?.message || err);
      } finally {
        if (loginBtn) loginBtn.disabled = false;
      }
    });
  });

  loginView.style.display = 'none';
  mainView.style.display = '';
}

export function bootstrapApp() {
  if (bootstrapStarted) return;
  bootstrapStarted = true;
  installBootstrapErrorHandlers();
  authGate()
    .then(() => {
      document.getElementById('view-main').style.display = '';
      return init();
    })
    .catch((err) => {
      showBootstrapError(String(err?.message || err));
    });
}
