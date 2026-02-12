import { initTerminal, destroyTerminal, writeToTerminal } from './terminal.js';
import { renderCanvas, clearCanvas } from './canvas.js';
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
};

export function getState() { return state; }

window._tabulaApp = { getState };

function showView(viewId) {
  document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
  document.getElementById(viewId).style.display = '';
}

export function showMain() {
  showView('view-main');
  if (state.localSession) {
    connectLocalSession();
  } else {
    refreshHosts();
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

    openTerminal();
    openCanvasWs();
  } catch (e) {
    console.error('local session connect failed:', e);
    refreshHosts();
  }
}

export function showHosts() {
  showView('view-hosts');
  loadHosts();
}

export function showAuth() {
  showView('view-auth');
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

    openTerminal();
  } catch (e) {
    setStatus('error: ' + e.message, '');
    document.getElementById('btn-connect').disabled = false;
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
  setStatus('disconnected', '');
  document.getElementById('btn-connect').style.display = '';
  document.getElementById('btn-connect').disabled = false;
  document.getElementById('btn-disconnect').style.display = 'none';
  document.getElementById('btn-launch-ai').disabled = true;
  document.getElementById('host-select').disabled = false;
}

function openTerminal() {
  const container = document.getElementById('terminal-container');
  const term = initTerminal(container);

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/terminal/${state.sessionId}`;
  const ws = new WebSocket(wsUrl);
  ws.binaryType = 'arraybuffer';
  state.terminalWs = ws;

  ws.onopen = () => {
    term.onData(data => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });
    term.onBinary(data => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols, rows }));
      }
    });
  };

  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      writeToTerminal(new Uint8Array(event.data));
    } else {
      writeToTerminal(event.data);
    }
  };

  ws.onclose = () => {
    if (state.connected) {
      writeToTerminal('\r\n[connection closed]\r\n');
    }
  };
}

function openCanvasWs() {
  if (!state.sessionId) return;

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/canvas/${state.sessionId}`;
  const ws = new WebSocket(wsUrl);
  state.canvasWs = ws;

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

async function launchAI() {
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

  const mcpUrl = state.mcpUrl || `http://127.0.0.1:${state.tunnelPort}/mcp`;
  let cmd;
  if (assistant === 'claude') {
    const cfg = JSON.stringify({mcpServers: {'tabula-canvas': {url: mcpUrl}}});
    cmd = `claude --mcp-config '${cfg}'\n`;
  } else {
    cmd = `codex --no-alt-screen --yolo --search -c 'mcp_servers.tabula-canvas.url=${JSON.stringify(mcpUrl)}'\n`;
  }

  if (state.terminalWs.readyState === WebSocket.OPEN) {
    state.terminalWs.send(cmd);
  }
}

async function logout() {
  await disconnect();
  try {
    await fetch('/api/logout', { method: 'POST' });
  } catch (e) {
    console.error('logout error:', e);
  }
  state.authenticated = false;
  showAuth();
}

function initDivider() {
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
  initAuth();
  initHostsView();
  initDivider();
  initMcpLog();

  document.getElementById('host-select').addEventListener('change', (e) => {
    document.getElementById('btn-connect').disabled = !e.target.value;
  });
  document.getElementById('btn-connect').addEventListener('click', connect);
  document.getElementById('btn-disconnect').addEventListener('click', disconnect);
  document.getElementById('btn-launch-ai').addEventListener('click', launchAI);
  document.getElementById('btn-hosts').addEventListener('click', showHosts);
  document.getElementById('btn-logout').addEventListener('click', logout);

  try {
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
    }
  } catch (e) {
    showAuth();
  }
}

init();
