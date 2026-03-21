const toolIcons = {
  pointer: 'arrow',
  highlight: 'marker',
  ink: 'pen_nib',
  text_note: 'sticky_note',
  prompt: 'mic',
};

const toolGlyphs = {
  pointer: '↗',
  highlight: '▰',
  ink: '✒',
  text_note: '▣',
  prompt: '●',
};

const indicatorLabels = {
  idle: 'Idle',
  listening: 'Listening',
  paused: 'Meeting paused',
  recording: 'Recording',
  working: 'Working',
};

const state = {
  tool: 'pointer',
  session: 'none',
  silent: false,
  circleExpanded: false,
  indicatorOverride: '',
};
let ignoreOutsideCollapseUntil = 0;
let lastTouchActivationAt = 0;

const body = document.body;
const circle = document.getElementById('tabura-circle');
const dot = document.getElementById('tabura-circle-dot');
const menu = document.getElementById('tabura-circle-menu');
const indicator = document.getElementById('indicator');
const indicatorLabel = document.getElementById('indicator-label');
const indicatorBorder = document.getElementById('indicator-border');

function normalizeTool(value) {
  return ['pointer', 'highlight', 'ink', 'text_note', 'prompt'].includes(value) ? value : 'pointer';
}

function normalizeSession(value) {
  return ['none', 'dialogue', 'meeting'].includes(value) ? value : 'none';
}

function normalizeIndicator(value) {
  return ['idle', 'listening', 'paused', 'recording', 'working'].includes(value) ? value : '';
}

function derivedIndicatorState() {
  if (state.session === 'dialogue') return 'listening';
  if (state.session === 'meeting') return 'paused';
  return 'idle';
}

function currentIndicatorState() {
  return state.indicatorOverride || derivedIndicatorState();
}

function currentCursorClass() {
  return `tool-${state.tool}`;
}

function syncSegments() {
  const segments = menu.querySelectorAll('[data-segment]');
  for (const segment of segments) {
    const name = segment.getAttribute('data-segment') || '';
    const kind = segment.getAttribute('data-kind') || '';
    if (kind === 'tool') {
      segment.setAttribute('aria-pressed', String(name === state.tool));
    } else if (kind === 'session') {
      segment.setAttribute('aria-pressed', String(name === state.session));
    } else if (kind === 'toggle') {
      segment.setAttribute('aria-pressed', String(state.silent));
    }
  }
}

function sync() {
  const indicatorState = currentIndicatorState();
  body.className = [
    `tool-${state.tool}`,
    `session-${state.session}`,
    `indicator-${indicatorState}`,
    state.silent ? 'silent-on' : 'silent-off',
    state.circleExpanded ? 'circle-expanded' : 'circle-collapsed',
  ].join(' ');
  body.dataset.tool = state.tool;
  body.dataset.session = state.session;
  body.dataset.silent = String(state.silent);
  body.dataset.circle = state.circleExpanded ? 'expanded' : 'collapsed';
  body.dataset.indicatorState = indicatorState;
  body.dataset.dotInnerIcon = toolIcons[state.tool];
  body.dataset.cursorClass = currentCursorClass();

  circle.dataset.state = body.dataset.circle;
  circle.classList.toggle('is-expanded', state.circleExpanded);
  circle.classList.toggle('is-collapsed', !state.circleExpanded);
  dot.dataset.icon = toolIcons[state.tool];
  dot.dataset.sessionLabel = state.session === 'none' ? '' : state.session;
  dot.textContent = toolGlyphs[state.tool];

  indicator.dataset.state = indicatorState;
  indicatorLabel.textContent = indicatorLabels[indicatorState];
  const canvas = document.getElementById('canvas-viewport');
  if (canvas) {
    canvas.dataset.cursorClass = currentCursorClass();
  }
  syncSegments();
}

function setTool(tool) {
  state.tool = normalizeTool(tool);
}

function setSession(session) {
  const next = normalizeSession(session);
  state.session = state.session === next ? 'none' : next;
  state.indicatorOverride = '';
}

function setSilent(next) {
  state.silent = Boolean(next);
}

function toggleCircle() {
  state.circleExpanded = !state.circleExpanded;
  sync();
}

function collapseCircle() {
  state.circleExpanded = false;
  sync();
}

function stopIndicator() {
  state.session = 'none';
  state.indicatorOverride = '';
  sync();
}

function setIndicatorOverride(next) {
  state.indicatorOverride = normalizeIndicator(next);
  sync();
}

function isTouchPointer(event) {
  return typeof event.pointerType === 'string' && event.pointerType === 'touch';
}

function markTouchActivation() {
  lastTouchActivationAt = performance.now();
}

function ignoreSyntheticClick() {
  return lastTouchActivationAt > 0 && performance.now() - lastTouchActivationAt < 320;
}

function handleSegmentActivation(target) {
  if (!(target instanceof HTMLElement)) return;
  const segment = target.closest('[data-segment]');
  if (!(segment instanceof HTMLButtonElement)) return;
  const name = segment.dataset.segment || '';
  const kind = segment.dataset.kind || '';
  if (kind === 'tool') {
    setTool(name);
  } else if (kind === 'session') {
    setSession(name);
  } else if (kind === 'toggle') {
    setSilent(!state.silent);
  }
  sync();
}

menu.addEventListener('click', (event) => {
  if (ignoreSyntheticClick()) return;
  event.stopPropagation();
  handleSegmentActivation(event.target);
});

menu.addEventListener('pointerup', (event) => {
  if (!isTouchPointer(event)) return;
  markTouchActivation();
  event.preventDefault();
  event.stopPropagation();
  handleSegmentActivation(event.target);
});

menu.addEventListener('touchend', (event) => {
  markTouchActivation();
  event.preventDefault();
  event.stopPropagation();
  handleSegmentActivation(event.target);
}, { passive: false });

function handleDotActivation(event) {
  event.stopPropagation();
  ignoreOutsideCollapseUntil = performance.now() + 220;
  toggleCircle();
}

dot.addEventListener('click', (event) => {
  if (ignoreSyntheticClick()) return;
  handleDotActivation(event);
});

dot.addEventListener('pointerup', (event) => {
  if (!isTouchPointer(event)) return;
  markTouchActivation();
  event.preventDefault();
  handleDotActivation(event);
});

dot.addEventListener('touchend', (event) => {
  markTouchActivation();
  event.preventDefault();
  handleDotActivation(event);
}, { passive: false });

indicatorBorder.addEventListener('click', (event) => {
  if (ignoreSyntheticClick()) return;
  event.stopPropagation();
  stopIndicator();
});

indicatorBorder.addEventListener('pointerup', (event) => {
  if (!isTouchPointer(event)) return;
  markTouchActivation();
  event.preventDefault();
  event.stopPropagation();
  stopIndicator();
});

indicatorBorder.addEventListener('touchend', (event) => {
  markTouchActivation();
  event.preventDefault();
  event.stopPropagation();
  stopIndicator();
}, { passive: false });

function bindTouchAwareButton(id, handler) {
  const element = document.getElementById(id);
  if (!(element instanceof HTMLButtonElement)) return;
  element.addEventListener('click', () => {
    if (ignoreSyntheticClick()) return;
    handler();
  });
  element.addEventListener('pointerup', (event) => {
    if (!isTouchPointer(event)) return;
    markTouchActivation();
    event.preventDefault();
    handler();
  });
  element.addEventListener('touchend', (event) => {
    markTouchActivation();
    event.preventDefault();
    handler();
  }, { passive: false });
}

bindTouchAwareButton('indicator-simulate-recording', () => {
  setIndicatorOverride('recording');
});

bindTouchAwareButton('indicator-simulate-working', () => {
  setIndicatorOverride('working');
});

bindTouchAwareButton('indicator-override-clear', () => {
  state.indicatorOverride = '';
  sync();
});

document.addEventListener('click', (event) => {
  if (!state.circleExpanded) return;
  if (performance.now() < ignoreOutsideCollapseUntil) return;
  const target = event.target;
  if (!(target instanceof Node)) return;
  if (circle.contains(target)) return;
  collapseCircle();
});

document.addEventListener('keydown', (event) => {
  if (event.key !== 'Escape' || !state.circleExpanded) return;
  collapseCircle();
});

window.__flowHarness = {
  activateTarget(target) {
    switch (String(target || '')) {
      case 'tabura_circle_dot':
        ignoreOutsideCollapseUntil = performance.now() + 220;
        toggleCircle();
        return true;
      case 'tabura_circle_segment_pointer':
        setTool('pointer');
        sync();
        return true;
      case 'tabura_circle_segment_highlight':
        setTool('highlight');
        sync();
        return true;
      case 'tabura_circle_segment_ink':
        setTool('ink');
        sync();
        return true;
      case 'tabura_circle_segment_text_note':
        setTool('text_note');
        sync();
        return true;
      case 'tabura_circle_segment_prompt':
        setTool('prompt');
        sync();
        return true;
      case 'tabura_circle_segment_dialogue':
        setSession('dialogue');
        sync();
        return true;
      case 'tabura_circle_segment_meeting':
        setSession('meeting');
        sync();
        return true;
      case 'tabura_circle_segment_silent':
        setSilent(!state.silent);
        sync();
        return true;
      case 'indicator_border':
        stopIndicator();
        return true;
      case 'indicator_simulate_recording':
        setIndicatorOverride('recording');
        return true;
      case 'indicator_simulate_working':
        setIndicatorOverride('working');
        return true;
      case 'indicator_override_clear':
        state.indicatorOverride = '';
        sync();
        return true;
      default:
        return false;
    }
  },
  reset(next = {}) {
    state.tool = normalizeTool(next.tool || 'pointer');
    state.session = normalizeSession(next.session || 'none');
    state.silent = Boolean(next.silent);
    state.circleExpanded = false;
    state.indicatorOverride = normalizeIndicator(next.indicator_state);
    sync();
    return this.snapshot();
  },
  snapshot() {
    return {
      active_tool: state.tool,
      session: state.session,
      silent: state.silent,
      tabura_circle: state.circleExpanded ? 'expanded' : 'collapsed',
      dot_inner_icon: toolIcons[state.tool],
      indicator_state: currentIndicatorState(),
      body_class: body.className,
      cursor_class: currentCursorClass(),
    };
  },
};

sync();
