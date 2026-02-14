const MAX_BUFFER_SIZE = 200_000;
const STICK_THRESHOLD = 24;

let container = null;
let pre = null;
let probe = null;
let inputCapture = null;
let resizeObserver = null;
let buffer = "";
let frameText = "";
let cols = 120;
let rows = 40;
let cellMetrics = { width: 8.4, height: 18 };
let stickToBottom = true;
let dataCallback = null;
let resizeCallback = null;
let renderScheduled = false;
let isComposing = false;
let compositionCommitPending = false;
let touchStartX = 0;
let touchStartY = 0;
let touchTracking = false;
let touchMoved = false;
const decoder = new TextDecoder();

function keyEventToTerminalData(event) {
  if (event.metaKey) {
    return null;
  }
  if (event.ctrlKey && event.key.length === 1) {
    const lower = event.key.toLowerCase();
    if (lower >= "a" && lower <= "z") {
      return String.fromCharCode(lower.charCodeAt(0) - 96);
    }
    if (lower === " ") {
      return "\u0000";
    }
  }

  switch (event.key) {
    case "Enter": return "\r";
    case "Backspace": return "\u007f";
    case "Tab": return "\t";
    case "Escape": return "\u001b";
    case "ArrowUp": return "\u001b[A";
    case "ArrowDown": return "\u001b[B";
    case "ArrowRight": return "\u001b[C";
    case "ArrowLeft": return "\u001b[D";
    case "Home": return "\u001b[H";
    case "End": return "\u001b[F";
    case "Delete": return "\u001b[3~";
    case "PageUp": return "\u001b[5~";
    case "PageDown": return "\u001b[6~";
    default: break;
  }

  if (event.altKey && event.key.length === 1) {
    return `\u001b${event.key}`;
  }

  return null;
}

function measureTerminalSize() {
  if (!container || !probe) {
    return;
  }
  const probeRect = probe.getBoundingClientRect();
  if (probeRect.width > 2 && probeRect.height > 4) {
    cellMetrics = { width: probeRect.width, height: probeRect.height };
  }
  const rect = container.getBoundingClientRect();
  const styles = window.getComputedStyle(container);
  const paddingX = parseFloat(styles.paddingLeft || "0") + parseFloat(styles.paddingRight || "0");
  const paddingY = parseFloat(styles.paddingTop || "0") + parseFloat(styles.paddingBottom || "0");
  const contentWidth = Math.max(80, rect.width - paddingX);
  const contentHeight = Math.max(80, rect.height - paddingY);
  const glyphWidth = Math.max(4, cellMetrics.width);
  const glyphHeight = Math.max(10, cellMetrics.height);
  const newCols = Math.max(40, Math.floor(contentWidth / glyphWidth));
  const newRows = Math.max(10, Math.floor(contentHeight / glyphHeight));
  const changed = newCols !== cols || newRows !== rows;
  cols = newCols;
  rows = newRows;
  if (changed && resizeCallback) {
    resizeCallback({ cols, rows });
  }
}

function isNearBottom() {
  if (!container) return true;
  return container.scrollHeight - container.scrollTop - container.clientHeight <= STICK_THRESHOLD;
}

function scrollToBottom() {
  requestAnimationFrame(() => {
    if (container) {
      container.scrollTop = container.scrollHeight;
    }
  });
}

function renderNow() {
  if (!pre) return;
  pre.textContent = frameText || buffer;
  if (stickToBottom) {
    scrollToBottom();
  }
}

function scheduleRender() {
  if (renderScheduled) {
    return;
  }
  renderScheduled = true;
  requestAnimationFrame(() => {
    renderScheduled = false;
    renderNow();
  });
}

function focusInputCapture() {
  if (!inputCapture) {
    return;
  }
  try {
    inputCapture.focus({ preventScroll: true });
  } catch {
    inputCapture.focus();
  }
}

function sendData(text) {
  if (!text || !dataCallback) {
    return;
  }
  dataCallback(text);
}

function flushInputCaptureValue() {
  if (!inputCapture) {
    return false;
  }
  const text = inputCapture.value;
  if (!text) {
    return false;
  }
  sendData(text);
  inputCapture.value = "";
  return true;
}

function scheduleCompositionFallbackCommit() {
  queueMicrotask(() => {
    if (!compositionCommitPending || isComposing) {
      return;
    }
    compositionCommitPending = false;
    if (flushInputCaptureValue()) {
      focusInputCapture();
    }
  });
}

function onContainerScroll() {
  stickToBottom = isNearBottom();
}

function onContainerActivateInput() {
  if (container) {
    try {
      container.focus({ preventScroll: true });
    } catch {
      container.focus();
    }
  }
  focusInputCapture();
}

function onTouchStart(event) {
  if (!event.touches || event.touches.length !== 1) {
    touchTracking = false;
    touchMoved = false;
    return;
  }
  const t = event.touches[0];
  touchStartX = t.clientX;
  touchStartY = t.clientY;
  touchTracking = true;
  touchMoved = false;
}

function onTouchMove(event) {
  if (!touchTracking || !event.touches || event.touches.length !== 1) {
    return;
  }
  const t = event.touches[0];
  const dx = Math.abs(t.clientX - touchStartX);
  const dy = Math.abs(t.clientY - touchStartY);
  if (dx > 8 || dy > 8) {
    touchMoved = true;
  }
}

function onTouchEnd() {
  if (touchTracking && !touchMoved) {
    onContainerActivateInput();
  }
  touchTracking = false;
  touchMoved = false;
}

function onTouchCancel() {
  touchTracking = false;
  touchMoved = false;
}

function onKeyDown(event) {
  if (event.isComposing || isComposing) {
    return;
  }

  const encoded = keyEventToTerminalData(event);
  if (encoded) {
    event.preventDefault();
    sendData(encoded);
    return;
  }
  if (!event.ctrlKey && !event.altKey && !event.metaKey && event.key.length === 1) {
    event.preventDefault();
    sendData(event.key);
  }
}

function onBeforeInput(event) {
  const inputType = event.inputType || "";

  if (event.isComposing || isComposing || inputType === "insertCompositionText") {
    return;
  }

  if (inputType === "insertLineBreak" || inputType === "insertParagraph") {
    event.preventDefault();
    sendData("\r");
    if (inputCapture) {
      inputCapture.value = "";
    }
    return;
  }

  if (inputType === "deleteContentBackward") {
    event.preventDefault();
    sendData("\u007f");
    if (inputCapture) {
      inputCapture.value = "";
    }
    return;
  }

  if (inputType === "deleteContentForward") {
    event.preventDefault();
    sendData("\u001b[3~");
    if (inputCapture) {
      inputCapture.value = "";
    }
  }
}

function onInput(event) {
  if (!inputCapture) {
    return;
  }

  if (event.isComposing || isComposing) {
    return;
  }

  compositionCommitPending = false;
  if (flushInputCaptureValue()) {
    focusInputCapture();
  }
}

function onCompositionStart() {
  isComposing = true;
  compositionCommitPending = false;
}

function onCompositionEnd() {
  isComposing = false;
  compositionCommitPending = true;
  scheduleCompositionFallbackCommit();
}

function onPaste(event) {
  const pasted = event.clipboardData.getData("text");
  if (!pasted) return;
  event.preventDefault();
  sendData(pasted);
}

function onInputBlur() {
  if (!inputCapture || isComposing) {
    return;
  }
  inputCapture.value = "";
  compositionCommitPending = false;
}

export function initTerminal(containerEl) {
  container = containerEl;
  container.innerHTML = "";
  buffer = "";
  frameText = "";
  stickToBottom = true;
  dataCallback = null;
  resizeCallback = null;
  renderScheduled = false;
  isComposing = false;
  compositionCommitPending = false;

  pre = document.createElement("pre");
  pre.className = "terminal-text";
  container.appendChild(pre);

  probe = document.createElement("span");
  probe.className = "terminal-measure-probe";
  probe.setAttribute("aria-hidden", "true");
  probe.textContent = "M";
  container.appendChild(probe);

  inputCapture = document.createElement("textarea");
  inputCapture.className = "terminal-input-capture";
  inputCapture.setAttribute("tabindex", "0");
  inputCapture.setAttribute("role", "textbox");
  inputCapture.setAttribute("aria-label", "Terminal input capture");
  inputCapture.setAttribute("autocomplete", "off");
  inputCapture.setAttribute("autocapitalize", "none");
  inputCapture.setAttribute("autocorrect", "off");
  inputCapture.setAttribute("spellcheck", "false");
  inputCapture.setAttribute("data-gramm", "false");
  inputCapture.setAttribute("data-lpignore", "true");
  inputCapture.setAttribute("data-1p-ignore", "true");
  inputCapture.setAttribute("data-form-type", "other");
  container.appendChild(inputCapture);

  container.setAttribute("tabindex", "0");
  container.addEventListener("scroll", onContainerScroll);
  container.addEventListener("keydown", onKeyDown);
  container.addEventListener("paste", onPaste);
  container.addEventListener("click", onContainerActivateInput);
  container.addEventListener("touchstart", onTouchStart, { passive: true });
  container.addEventListener("touchmove", onTouchMove, { passive: true });
  container.addEventListener("touchend", onTouchEnd, { passive: true });
  container.addEventListener("touchcancel", onTouchCancel, { passive: true });

  inputCapture.addEventListener("beforeinput", onBeforeInput);
  inputCapture.addEventListener("input", onInput);
  inputCapture.addEventListener("compositionstart", onCompositionStart);
  inputCapture.addEventListener("compositionend", onCompositionEnd);
  inputCapture.addEventListener("blur", onInputBlur);

  measureTerminalSize();

  resizeObserver = new ResizeObserver(() => {
    measureTerminalSize();
    scheduleRender();
  });
  resizeObserver.observe(container);

  window._tabulaTerminal = {
    cols,
    rows,
    onData(cb) { dataCallback = cb; },
    onBinary() {},
    onResize(cb) { resizeCallback = cb; },
  };

  focusInputCapture();
  return window._tabulaTerminal;
}

export function destroyTerminal() {
  if (resizeObserver) {
    resizeObserver.disconnect();
    resizeObserver = null;
  }
  if (inputCapture) {
    inputCapture.removeEventListener("beforeinput", onBeforeInput);
    inputCapture.removeEventListener("input", onInput);
    inputCapture.removeEventListener("compositionstart", onCompositionStart);
    inputCapture.removeEventListener("compositionend", onCompositionEnd);
    inputCapture.removeEventListener("blur", onInputBlur);
  }
  if (container) {
    container.removeEventListener("scroll", onContainerScroll);
    container.removeEventListener("keydown", onKeyDown);
    container.removeEventListener("paste", onPaste);
    container.removeEventListener("click", onContainerActivateInput);
    container.removeEventListener("touchstart", onTouchStart);
    container.removeEventListener("touchmove", onTouchMove);
    container.removeEventListener("touchend", onTouchEnd);
    container.removeEventListener("touchcancel", onTouchCancel);
    container.innerHTML = "";
  }
  container = null;
  pre = null;
  probe = null;
  inputCapture = null;
  buffer = "";
  frameText = "";
  dataCallback = null;
  resizeCallback = null;
  renderScheduled = false;
  isComposing = false;
  compositionCommitPending = false;
  touchTracking = false;
  touchMoved = false;
  window._tabulaTerminal = null;
}

export function writeToTerminal(data) {
  if (data && typeof data === "object" && data.type === "terminal_frame") {
    const screen = data.screen || {};
    if (typeof screen.text === "string") {
      frameText = screen.text;
    }
    if (Number.isFinite(screen.cols) && Number.isFinite(screen.rows)) {
      cols = Math.max(40, Math.floor(screen.cols));
      rows = Math.max(10, Math.floor(screen.rows));
      if (window._tabulaTerminal) {
        window._tabulaTerminal.cols = cols;
        window._tabulaTerminal.rows = rows;
      }
    }
    scheduleRender();
    return;
  }

  let text;
  if (data instanceof ArrayBuffer || data instanceof Uint8Array) {
    text = decoder.decode(data instanceof ArrayBuffer ? data : data.buffer, { stream: true });
  } else {
    text = data;
  }
  if (typeof text !== "string") {
    return;
  }
  if (frameText) {
    // Keep legacy fallback path available without mixing paradigms.
    return;
  }
  buffer += text;
  if (buffer.length > MAX_BUFFER_SIZE) {
    buffer = buffer.slice(buffer.length - MAX_BUFFER_SIZE);
  }
  scheduleRender();
}
