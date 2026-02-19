import { initTerminal, destroyTerminal, writeToTerminal } from './terminal.js';
import { renderCanvas, clearCanvas, initCanvasControls } from './canvas.js';
import { loadHosts, initHostsView } from './hosts.js';
import { initAuth } from './auth.js';
import { initMcpLog, logEvent } from './mcp-log.js';

const state = {
  authenticated: false,
  sessionId: null,
  hostId: null,
  terminalWs: null,
  canvasWs: null,
  tunnelPort: null,
  connected: false,
  localSession: null,
  mcpUrl: null,
  mobileTerminalMinimized: false,
  terminalReconnectAttempt: 0,
  terminalReconnectTimer: null,
  runtimeBootId: null,
  runtimePollTimer: null,
};

const DESKTOP_CANVAS_ONLY = location.pathname === '/canvas' || new URLSearchParams(location.search).get('desktop') === '1';

export function getState() { return state; }

window._tabulaApp = { getState };
const SAVED_REMOTE_SESSION_KEY = 'tabula.remoteSession.v1';

const MOBILE_BREAKPOINT_PX = 768;
const MOBILE_KEY_PAYLOADS = {
  tab: '\t',
  esc: '\u001b',
  up: '\u001b[A',
  down: '\u001b[B',
  left: '\u001b[D',
  right: '\u001b[C',
};

function showView(viewId) {
  document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
  document.getElementById(viewId).style.display = '';
}

function applyDesktopMode() {
  if (!DESKTOP_CANVAS_ONLY) return;
  document.body.classList.add('desktop-canvas-only');
}

function clearTerminalReconnectTimer() {
  if (state.terminalReconnectTimer) {
    clearTimeout(state.terminalReconnectTimer);
    state.terminalReconnectTimer = null;
  }
}

function scheduleTerminalReconnect() {
  if (DESKTOP_CANVAS_ONLY) return;
  if (!state.connected || !state.sessionId) return;
  if (state.terminalReconnectTimer) return;
  const attempt = Math.max(0, Number(state.terminalReconnectAttempt) || 0);
  const delayMs = Math.min(5000, 500 + attempt * 400);
  state.terminalReconnectTimer = setTimeout(() => {
    state.terminalReconnectTimer = null;
    if (!state.connected || !state.sessionId || state.terminalWs) return;
    state.terminalReconnectAttempt = attempt + 1;
    openTerminal();
  }, delayMs);
}

function stopRuntimePolling() {
  if (state.runtimePollTimer) {
    clearInterval(state.runtimePollTimer);
    state.runtimePollTimer = null;
  }
}

async function pollRuntimeStatus() {
  try {
    const resp = await fetch('/api/runtime');
    if (!resp.ok) return;
    const payload = await resp.json();
    if (!payload || typeof payload !== 'object') return;
    if (payload.dev_mode !== true) {
      stopRuntimePolling();
      state.runtimeBootId = null;
      return;
    }
    const bootId = typeof payload.boot_id === 'string' ? payload.boot_id : null;
    if (!bootId) return;
    if (!state.runtimeBootId) {
      state.runtimeBootId = bootId;
      return;
    }
    if (state.runtimeBootId !== bootId) {
      location.reload();
    }
  } catch (_) {
    // ignore transient errors while service restarts
  }
}

function startRuntimePolling() {
  if (state.runtimePollTimer) return;
  void pollRuntimeStatus();
  state.runtimePollTimer = setInterval(() => {
    void pollRuntimeStatus();
  }, 1500);
}

function loadSavedRemoteSession() {
  try {
    const raw = window.localStorage.getItem(SAVED_REMOTE_SESSION_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return null;
    if (typeof parsed.sessionId !== 'string' || !parsed.sessionId) return null;
    return parsed;
  } catch (_) {
    return null;
  }
}

function saveRemoteSession() {
  if (state.localSession || !state.connected || !state.sessionId || !state.hostId) {
    window.localStorage.removeItem(SAVED_REMOTE_SESSION_KEY);
    return;
  }
  const sel = document.getElementById('host-select');
  const selected = sel?.selectedOptions?.[0];
  const payload = {
    sessionId: state.sessionId,
    hostId: Number(state.hostId),
    hostLabel: selected ? selected.textContent : '',
  };
  window.localStorage.setItem(SAVED_REMOTE_SESSION_KEY, JSON.stringify(payload));
}

function clearSavedRemoteSession() {
  window.localStorage.removeItem(SAVED_REMOTE_SESSION_KEY);
}

function shellSingleQuote(value) {
  return `'${String(value).replace(/'/g, `'\"'\"'`)}'`;
}

function buildClaudeCommand(mcpUrl) {
  const bridge = `exec tabula mcp-http-bridge --mcp-url ${shellSingleQuote(mcpUrl)}`;
  const cfg = JSON.stringify({
    mcpServers: {
      'tabula': {
        command: 'bash',
        args: ['-lc', bridge],
      },
    },
  });
  return `claude --dangerously-skip-permissions --mcp-config ${shellSingleQuote(cfg)}\n`;
}

function isMobileViewport() {
  return window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX}px)`).matches;
}

function terminalApi() {
  return window._tabulaTerminal || null;
}

function updateCtrlButtonState(armed) {
  const btn = document.getElementById('btn-key-ctrl');
  if (!btn) return;
  const next = Boolean(armed);
  btn.classList.toggle('is-active', next);
  btn.setAttribute('aria-pressed', next ? 'true' : 'false');
}

function sendTerminalInput(text) {
  if (state.terminalWs && state.terminalWs.readyState === WebSocket.OPEN) {
    state.terminalWs.send(text);
    return true;
  }
  const term = terminalApi();
  if (term && typeof term.send === 'function' && state.connected) {
    term.send(text);
    return true;
  }
  return false;
}

function syncMobileTerminalUi() {
  if (DESKTOP_CANVAS_ONLY) {
    const keybar = document.getElementById('mobile-keybar');
    const popRow = document.getElementById('terminal-pop-row');
    const panel = document.getElementById('panel-terminal');
    const workspace = document.getElementById('workspace');
    const minBtn = document.getElementById('btn-terminal-minimize');
    if (keybar) keybar.style.display = 'none';
    if (popRow) popRow.style.display = 'none';
    if (panel) panel.classList.remove('mobile-minimized');
    if (workspace) workspace.classList.remove('terminal-minimized');
    if (minBtn) minBtn.style.display = 'none';
    return;
  }
  const mobile = isMobileViewport();
  const terminalActive = Boolean(state.connected && state.terminalWs);
  if (!mobile || !terminalActive) {
    state.mobileTerminalMinimized = false;
  }

  const minimized = Boolean(mobile && terminalActive && state.mobileTerminalMinimized);
  const keybar = document.getElementById('mobile-keybar');
  const popRow = document.getElementById('terminal-pop-row');
  const panel = document.getElementById('panel-terminal');
  const workspace = document.getElementById('workspace');
  const minBtn = document.getElementById('btn-terminal-minimize');

  if (keybar) {
    keybar.style.display = mobile && terminalActive ? 'flex' : 'none';
  }
  if (popRow) {
    popRow.style.display = minimized ? 'block' : 'none';
  }
  if (minBtn) {
    minBtn.style.display = mobile && terminalActive ? '' : 'none';
  }
  if (panel) {
    panel.classList.toggle('mobile-minimized', minimized);
  }
  if (workspace) {
    workspace.classList.toggle('terminal-minimized', minimized);
  }

  if (!mobile || !terminalActive) {
    const term = terminalApi();
    if (term && typeof term.setCtrlArmed === 'function') {
      term.setCtrlArmed(false);
    }
    updateCtrlButtonState(false);
  }
}

function sendCurrentTerminalSize() {
  const term = terminalApi();
  if (!term || !state.terminalWs || state.terminalWs.readyState !== WebSocket.OPEN) {
    return;
  }
  state.terminalWs.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
}

function setTerminalMinimized(minimized) {
  state.mobileTerminalMinimized = Boolean(minimized);
  syncMobileTerminalUi();
  if (!state.mobileTerminalMinimized) {
    const container = document.getElementById('terminal-container');
    if (container) {
      container.click();
    }
    requestAnimationFrame(() => sendCurrentTerminalSize());
  }
}

function handleMobileTerminalKey(event) {
  const key = event.currentTarget?.dataset?.terminalKey;
  if (!key) return;

  const term = terminalApi();
  if (key === 'ctrl') {
    if (!term || typeof term.toggleCtrlArmed !== 'function') return;
    const armed = term.toggleCtrlArmed();
    updateCtrlButtonState(armed);
    return;
  }

  const payload = MOBILE_KEY_PAYLOADS[key];
  if (!payload) return;
  sendTerminalInput(payload);
  const container = document.getElementById('terminal-container');
  if (container) container.click();
}

export function showMain() {
  showView('view-main');
  applyDesktopMode();
  syncMobileTerminalUi();
  startRuntimePolling();
  if (state.localSession) {
    clearSavedRemoteSession();
    connectLocalSession();
  } else {
    void restoreRemoteSessionOrHosts();
  }
}

async function restoreRemoteSessionOrHosts() {
  const restored = await tryRestoreRemoteSession();
  if (!restored) {
    await refreshHosts();
  }
}

async function tryRestoreRemoteSession() {
  if (DESKTOP_CANVAS_ONLY) {
    return false;
  }
  const saved = loadSavedRemoteSession();
  if (!saved) {
    return false;
  }

  try {
    await refreshHosts();
    const sessionsResp = await fetch('/api/sessions');
    if (!sessionsResp.ok) {
      return false;
    }
    const sessionsData = await sessionsResp.json();
    const sessions = Array.isArray(sessionsData.sessions) ? sessionsData.sessions : [];
    if (!sessions.includes(saved.sessionId)) {
      clearSavedRemoteSession();
      return false;
    }

    state.sessionId = saved.sessionId;
    state.hostId = Number(saved.hostId) || null;
    state.connected = true;

    const sel = document.getElementById('host-select');
    if (state.hostId && sel && sel.querySelector(`option[value="${state.hostId}"]`)) {
      sel.value = String(state.hostId);
    }

    const selected = sel?.selectedOptions?.[0];
    const label = selected ? selected.textContent : saved.hostLabel || saved.sessionId;
    setStatus(`connected: ${label}`, 'connected');
    document.getElementById('btn-connect').style.display = 'none';
    document.getElementById('btn-disconnect').style.display = '';
    document.getElementById('btn-launch-ai').disabled = false;
    document.getElementById('host-select').disabled = true;

    if (!DESKTOP_CANVAS_ONLY) {
      openTerminal();
    }
    openCanvasWs();
    syncMobileTerminalUi();
    return true;
  } catch (e) {
    console.error('remote session restore failed:', e);
    return false;
  }
}

async function connectLocalSession() {
  try {
    const resp = await fetch('/api/sessions');
    if (!resp.ok) { refreshHosts(); return; }
    const data = await resp.json();
    if (!data.local_session) { refreshHosts(); return; }

    state.sessionId = data.local_session.session_id;
    state.mcpUrl = data.local_session.mcp_url;
    state.connected = true;
    state.localSession = data.local_session;

    document.getElementById('host-select').style.display = 'none';
    document.getElementById('btn-connect').style.display = 'none';
    document.getElementById('btn-disconnect').style.display = 'none';
    document.getElementById('btn-launch-ai').disabled = false;
    setStatus(`local: ${data.local_session.project_dir}`, 'connected');

    if (!DESKTOP_CANVAS_ONLY) {
      openTerminal();
    }
    openCanvasWs();
    syncMobileTerminalUi();
  } catch (e) {
    console.error('local session connect failed:', e);
    refreshHosts();
    syncMobileTerminalUi();
  }
}

export function showHosts() {
  if (DESKTOP_CANVAS_ONLY) return;
  showView('view-hosts');
  loadHosts();
}

export function showAuth() {
  showView('view-auth');
  syncMobileTerminalUi();
}

function setStatus(text, statusClass) {
  const dot = document.getElementById('status-indicator');
  const label = document.getElementById('status-text');
  dot.className = 'status-dot ' + (statusClass || '');
  label.textContent = text;
}

async function refreshHosts() {
  try {
    const resp = await fetch('/api/hosts');
    if (!resp.ok) return;
    const hosts = await resp.json();
    const sel = document.getElementById('host-select');
    const current = sel.value;
    sel.innerHTML = '<option value="">-- select host --</option>';
    hosts.forEach(h => {
      const opt = document.createElement('option');
      opt.value = h.id;
      opt.textContent = `${h.name} (${h.username}@${h.hostname})`;
      sel.appendChild(opt);
    });
    if (current) sel.value = current;
    document.getElementById('btn-connect').disabled = !sel.value;
  } catch (e) {
    console.error('failed to refresh hosts:', e);
  }
}

async function connect() {
  const hostId = parseInt(document.getElementById('host-select').value);
  if (!hostId) return;

  setStatus('connecting...', 'connecting');
  document.getElementById('btn-connect').disabled = true;

  try {
    const resp = await fetch('/api/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host_id: hostId }),
    });
    if (!resp.ok) {
      const text = await resp.text();
      setStatus('failed: ' + text, '');
      document.getElementById('btn-connect').disabled = false;
      return;
    }
    const data = await resp.json();
    state.sessionId = data.session_id;
    state.hostId = hostId;
    state.connected = true;

    setStatus('connected', 'connected');
    document.getElementById('btn-connect').style.display = 'none';
    document.getElementById('btn-disconnect').style.display = '';
    document.getElementById('btn-launch-ai').disabled = false;
    document.getElementById('host-select').disabled = true;
    saveRemoteSession();

    if (!DESKTOP_CANVAS_ONLY) {
      openTerminal();
    }
    syncMobileTerminalUi();
  } catch (e) {
    setStatus('error: ' + e.message, '');
    document.getElementById('btn-connect').disabled = false;
    syncMobileTerminalUi();
  }
}

async function disconnect() {
  if (state.terminalWs) {
    state.terminalWs.close();
    state.terminalWs = null;
  }
  if (state.canvasWs) {
    state.canvasWs.close();
    state.canvasWs = null;
  }
  destroyTerminal();
  clearCanvas();

  if (state.sessionId) {
    try {
      await fetch('/api/disconnect', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: state.sessionId }),
      });
    } catch (e) {
      console.error('disconnect error:', e);
    }
  }

  state.sessionId = null;
  state.hostId = null;
  state.connected = false;
  state.mobileTerminalMinimized = false;
  state.terminalReconnectAttempt = 0;
  clearTerminalReconnectTimer();
  stopRuntimePolling();
  state.runtimeBootId = null;
  clearSavedRemoteSession();
  setStatus('disconnected', '');
  document.getElementById('btn-connect').style.display = '';
  document.getElementById('btn-connect').disabled = false;
  document.getElementById('btn-disconnect').style.display = 'none';
  document.getElementById('btn-launch-ai').disabled = true;
  document.getElementById('host-select').disabled = false;
  updateCtrlButtonState(false);
  syncMobileTerminalUi();
}

function openTerminal() {
  if (DESKTOP_CANVAS_ONLY) {
    return;
  }
  if (state.terminalWs && (state.terminalWs.readyState === WebSocket.OPEN || state.terminalWs.readyState === WebSocket.CONNECTING)) {
    return;
  }
  const container = document.getElementById('terminal-container');
  const term = terminalApi() || initTerminal(container);
  clearTerminalReconnectTimer();
  updateCtrlButtonState(false);
  syncMobileTerminalUi();

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/terminal/${state.sessionId}`;
  const ws = new WebSocket(wsUrl);
  ws.binaryType = 'arraybuffer';
  state.terminalWs = ws;

  ws.onopen = () => {
    state.terminalReconnectAttempt = 0;
    const sendResize = () => {
      if (ws.readyState !== WebSocket.OPEN) return;
      const safeCols = Math.max(40, Number(term.cols) || 120);
      const safeRows = Math.max(10, Number(term.rows) || 40);
      ws.send(JSON.stringify({ type: 'resize', cols: safeCols, rows: safeRows }));
    };

    term.onData(data => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols, rows }));
      }
    });
    // Force an initial resize so stale PTY dimensions from prior sessions
    // cannot leak into a newly attached browser terminal.
    sendResize();
    setTimeout(sendResize, 100);
    syncMobileTerminalUi();
  };

  ws.onmessage = (event) => {
    if (typeof event.data === 'string') {
      try {
        const payload = JSON.parse(event.data);
        if (payload && payload.type === 'terminal_frame') {
          writeToTerminal(payload);
          return;
        }
      } catch (_) {
        // Keep backward compatibility with plain text terminal streams.
      }
    }
    writeToTerminal(event.data);
  };

  ws.onclose = () => {
    state.terminalWs = null;
    syncMobileTerminalUi();
    if (state.connected) {
      scheduleTerminalReconnect();
    }
  };
}

function openCanvasWs() {
  if (!state.sessionId) return;

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/canvas/${state.sessionId}`;
  const ws = new WebSocket(wsUrl);
  state.canvasWs = ws;

  ws.onopen = () => {
    void loadCanvasSnapshot();
  };

  ws.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data);
      logEvent('in', payload);
      renderCanvas(payload);
    } catch (e) {
      console.error('canvas ws parse error:', e);
    }
  };

  ws.onclose = () => {
    if (state.connected) {
      setTimeout(() => openCanvasWs(), 3000);
    }
  };
}

async function loadCanvasSnapshot() {
  if (!state.sessionId) return;
  try {
    const resp = await fetch(`/api/canvas/${encodeURIComponent(state.sessionId)}/snapshot`);
    if (!resp.ok) return;
    const payload = await resp.json();
    if (payload && payload.event) {
      logEvent('in', payload.event);
      renderCanvas(payload.event);
      return;
    }
    if (payload && payload.status && payload.status.mode === 'prompt') {
      clearCanvas();
    }
  } catch (e) {
    console.error('canvas snapshot fetch failed:', e);
  }
}

async function launchAI() {
  if (DESKTOP_CANVAS_ONLY) return;
  if (!state.sessionId) return;

  const assistant = document.getElementById('assistant-select').value;
  const term = window._tabulaTerminal;
  if (!term || !state.terminalWs) return;

  if (!state.localSession) {
    try {
      const resp = await fetch('/api/daemon/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: state.sessionId }),
      });
      if (!resp.ok) {
        const text = await resp.text();
        writeToTerminal(`\r\n[daemon start failed: ${text}]\r\n`);
        return;
      }
      const data = await resp.json();
      state.tunnelPort = data.tunnel_port;
    } catch (e) {
      writeToTerminal(`\r\n[daemon start error: ${e.message}]\r\n`);
      return;
    }
    openCanvasWs();
  }

  // Commands run inside the PTY host, so they should use the host-local daemon port.
  const mcpUrl = state.mcpUrl || 'http://127.0.0.1:9420/mcp';
  let cmd;
  if (assistant === 'claude') {
    cmd = buildClaudeCommand(mcpUrl);
  } else {
    cmd = `codex --no-alt-screen --yolo --search -c 'mcp_servers.tabula.url=${JSON.stringify(mcpUrl)}'\n`;
  }

  sendTerminalInput(cmd);
}

async function logout() {
  await disconnect();
  try {
    await fetch('/api/logout', { method: 'POST' });
  } catch (e) {
    console.error('logout error:', e);
  }
  clearSavedRemoteSession();
  state.authenticated = false;
  showAuth();
}

function initDivider() {
  if (DESKTOP_CANVAS_ONLY) return;
  const divider = document.getElementById('panel-divider');
  const workspace = document.getElementById('workspace');
  const left = document.getElementById('panel-terminal');
  const right = document.getElementById('panel-canvas');
  let dragging = false;

  divider.addEventListener('mousedown', (e) => {
    dragging = true;
    e.preventDefault();
  });

  document.addEventListener('mousemove', (e) => {
    if (!dragging) return;
    const rect = workspace.getBoundingClientRect();
    const pct = ((e.clientX - rect.left) / rect.width) * 100;
    const clamped = Math.max(20, Math.min(80, pct));
    left.style.flex = 'none';
    right.style.flex = 'none';
    left.style.width = clamped + '%';
    right.style.width = (100 - clamped) + '%';
  });

  document.addEventListener('mouseup', () => { dragging = false; });
}

async function init() {
  try {
    initAuth();
    initHostsView();
    initDivider();
    initMcpLog();
    initCanvasControls();

    document.getElementById('host-select').addEventListener('change', (e) => {
      document.getElementById('btn-connect').disabled = !e.target.value;
    });
    document.getElementById('btn-connect').addEventListener('click', connect);
    document.getElementById('btn-disconnect').addEventListener('click', disconnect);
    document.getElementById('btn-launch-ai').addEventListener('click', launchAI);
    document.getElementById('btn-hosts').addEventListener('click', showHosts);
    document.getElementById('btn-logout').addEventListener('click', logout);
    document.getElementById('btn-terminal-minimize').addEventListener('click', () => setTerminalMinimized(true));
    document.getElementById('btn-terminal-pop').addEventListener('click', () => setTerminalMinimized(false));
    document.querySelectorAll('#mobile-keybar [data-terminal-key]').forEach((btn) => {
      btn.addEventListener('click', handleMobileTerminalKey);
    });
    window.addEventListener('resize', syncMobileTerminalUi);
    window.addEventListener('orientationchange', syncMobileTerminalUi);
    window.addEventListener('tabula-terminal-ctrl', (event) => {
      updateCtrlButtonState(Boolean(event?.detail?.armed));
    });
    syncMobileTerminalUi();

    if (DESKTOP_CANVAS_ONLY) {
      const hideIds = [
        'host-select',
        'btn-connect',
        'btn-disconnect',
        'assistant-select',
        'btn-launch-ai',
        'btn-mcp-log',
        'btn-hosts',
        'btn-terminal-minimize',
        'mobile-keybar',
        'terminal-pop-row',
        'mcp-log-panel',
      ];
      hideIds.forEach((id) => {
        const el = document.getElementById(id);
        if (el) el.style.display = 'none';
      });
    }

    const resp = await fetch('/api/setup');
    const data = await resp.json();
    if (data.local_session) {
      state.localSession = { session_id: data.local_session };
    }
    if (data.authenticated) {
      state.authenticated = true;
      showMain();
    } else {
      showAuth();
      syncMobileTerminalUi();
    }
  } catch (e) {
    console.error('tabula web init failed:', e);
    const errorEl = document.getElementById('auth-error');
    if (errorEl) {
      errorEl.textContent = 'UI init error. Try hard-refreshing the page.';
    }
    showAuth();
    syncMobileTerminalUi();
  }
}

init();
