import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const refreshWorkspaceBrowser = (...args) => refs.refreshWorkspaceBrowser(...args);
const loadItemSidebarView = (...args) => refs.loadItemSidebarView(...args);
const setPrReviewDrawerOpen = (...args) => refs.setPrReviewDrawerOpen(...args);
const renderPrReviewFileList = (...args) => refs.renderPrReviewFileList(...args);
const hideCanvasColumn = (...args) => refs.hideCanvasColumn(...args);
const applyIPhoneFrameCorners = (...args) => refs.applyIPhoneFrameCorners(...args);
const isFocusedTextInput = (...args) => refs.isFocusedTextInput(...args);
const isIPhoneStandalone = (...args) => refs.isIPhoneStandalone(...args);
const setSyncKeyboardStateNow = (...args) => refs.setSyncKeyboardStateNow(...args);
const stepCanvasFile = (...args) => refs.stepCanvasFile(...args);
const suppressSyntheticClick = (...args) => refs.suppressSyntheticClick(...args);

// Edge panel logic
let edgeTopTimer = null;
let edgeRightTimer = null;
let edgeTouchStart = null;
const EDGE_TAP_SIZE_PX = 30;
const EDGE_TAP_SIZE_SMALL_PX = 30;
const EDGE_TOP_TAP_SIZE_PX = 56;
const EDGE_TOP_TAP_SIZE_SMALL_PX = 52;
const EDGE_TAP_SIZE_SMALL_MEDIA_QUERY = '(max-width: 768px)';

function clearEdgeHideTimer(timer) {
  if (timer) clearTimeout(timer);
  return null;
}

function isPointerInsideElement(element, clientX, clientY) {
  if (!(element instanceof HTMLElement)) return false;
  const rect = element.getBoundingClientRect();
  return clientX >= rect.left
    && clientX <= rect.right
    && clientY >= rect.top
    && clientY <= rect.bottom;
}

function topEdgeTriggerBlockedByFileSidebar() {
  if (!state.prReviewDrawerOpen) return false;
  const pane = document.getElementById('pr-file-pane');
  return pane instanceof HTMLElement && pane.classList.contains('is-open');
}

function scheduleEdgePanelHide(element, timerName) {
  if (!(element instanceof HTMLElement)) return timerName === 'top' ? edgeTopTimer : edgeRightTimer;
  const currentTimer = timerName === 'top' ? edgeTopTimer : edgeRightTimer;
  if (currentTimer) return currentTimer;
  const nextTimer = window.setTimeout(() => {
    element.classList.remove('edge-active');
    if (timerName === 'top') {
      edgeTopTimer = null;
    } else {
      edgeRightTimer = null;
    }
  }, 300);
  if (timerName === 'top') {
    edgeTopTimer = nextTimer;
  } else {
    edgeRightTimer = nextTimer;
  }
  return nextTimer;
}

export function getEdgeTapSizePx() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return EDGE_TAP_SIZE_PX;
  }
  try {
    return window.matchMedia(EDGE_TAP_SIZE_SMALL_MEDIA_QUERY).matches
      ? EDGE_TAP_SIZE_SMALL_PX
      : EDGE_TAP_SIZE_PX;
  } catch (_) {
    return EDGE_TAP_SIZE_PX;
  }
}

export function getTopEdgeTapSizePx() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return EDGE_TOP_TAP_SIZE_PX;
  }
  try {
    return window.matchMedia(EDGE_TAP_SIZE_SMALL_MEDIA_QUERY).matches
      ? EDGE_TOP_TAP_SIZE_SMALL_PX
      : EDGE_TOP_TAP_SIZE_PX;
  } catch (_) {
    return EDGE_TOP_TAP_SIZE_PX;
  }
}

export function edgePanelsAreOpen() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  const topOpen = Boolean(edgeTop && (edgeTop.classList.contains('edge-active') || edgeTop.classList.contains('edge-pinned')));
  const rightOpen = Boolean(edgeRight && (edgeRight.classList.contains('edge-active') || edgeRight.classList.contains('edge-pinned')));
  return topOpen || rightOpen || state.prReviewDrawerOpen;
}

export function toggleFileSidebarFromEdge() {
  if (!state.prReviewMode && !state.activeProjectId) return;
  if (!state.prReviewMode) {
    if (state.fileSidebarMode === 'workspace') {
      if (!state.workspaceBrowserLoading && state.workspaceBrowserEntries.length === 0 && !state.workspaceBrowserError) {
        void refreshWorkspaceBrowser(false);
      }
    } else if (!state.itemSidebarLoading && state.itemSidebarItems.length === 0 && !state.itemSidebarError) {
      void loadItemSidebarView(state.itemSidebarView);
    }
  }
  setPrReviewDrawerOpen(!state.prReviewDrawerOpen);
  renderPrReviewFileList();
}

export function toggleRightEdgeDrawer(edgeRight) {
  if (!(edgeRight instanceof HTMLElement)) return;
  if (edgeRight.classList.contains('edge-pinned')) {
    edgeRight.classList.remove('edge-pinned', 'edge-active');
    return;
  }
  edgeRight.classList.add('edge-active', 'edge-pinned');
}

export function handleRasaEdgeTap() {
  const hadOpenPanels = edgePanelsAreOpen();
  closeEdgePanels();
  if (hadOpenPanels) return;
  if (state.hasArtifact) {
    clearCanvas();
    hideCanvasColumn();
  }
}

export function isLeftEdgeTapCoordinate(clientX) {
  const edgeTapSize = getEdgeTapSizePx();
  if (!state.prReviewDrawerOpen) {
    return clientX < edgeTapSize;
  }
  const pane = document.getElementById('pr-file-pane');
  if (!(pane instanceof HTMLElement) || !pane.classList.contains('is-open')) {
    return clientX < edgeTapSize;
  }
  const rect = pane.getBoundingClientRect();
  const zoneStart = Math.max(0, rect.right - edgeTapSize);
  const zoneEnd = Math.min(window.innerWidth, rect.right);
  return clientX >= zoneStart && clientX <= zoneEnd;
}

export function initEdgePanels() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  const edgeLeftTap = document.getElementById('edge-left-tap');

  // Desktop: hover near edge
  document.addEventListener('mousemove', (ev) => {
    const edgeTapSize = getEdgeTapSizePx();
    const topEdgeTapSize = getTopEdgeTapSizePx();
    // Top edge
    if (edgeTop && !edgeTop.classList.contains('edge-pinned')) {
      const inTopTrigger = ev.clientY < topEdgeTapSize
        && !topEdgeTriggerBlockedByFileSidebar();
      const insideTopPanel = isPointerInsideElement(edgeTop, ev.clientX, ev.clientY);
      if (inTopTrigger) {
        edgeTop.classList.add('edge-active');
        edgeTopTimer = clearEdgeHideTimer(edgeTopTimer);
      } else if (insideTopPanel) {
        edgeTopTimer = clearEdgeHideTimer(edgeTopTimer);
      } else if (edgeTop.classList.contains('edge-active')) {
        scheduleEdgePanelHide(edgeTop, 'top');
      }
    }
    // Right edge
    if (edgeRight && !edgeRight.classList.contains('edge-pinned')) {
      const inRightTrigger = ev.clientX > window.innerWidth - edgeTapSize;
      const insideRightPanel = isPointerInsideElement(edgeRight, ev.clientX, ev.clientY);
      if (inRightTrigger) {
        edgeRight.classList.add('edge-active');
        edgeRightTimer = clearEdgeHideTimer(edgeRightTimer);
      } else if (insideRightPanel) {
        edgeRightTimer = clearEdgeHideTimer(edgeRightTimer);
      } else if (edgeRight.classList.contains('edge-active')) {
        scheduleEdgePanelHide(edgeRight, 'right');
      }
    }
  });

  // Leave panels
  if (edgeTop) {
    edgeTop.addEventListener('mouseleave', () => {
      if (edgeTop.classList.contains('edge-pinned')) return;
      scheduleEdgePanelHide(edgeTop, 'top');
    });
    edgeTop.addEventListener('mouseenter', () => {
      edgeTopTimer = clearEdgeHideTimer(edgeTopTimer);
    });
  }

  if (edgeRight) {
    edgeRight.addEventListener('mouseleave', () => {
      if (edgeRight.classList.contains('edge-pinned')) return;
      scheduleEdgePanelHide(edgeRight, 'right');
    });
    edgeRight.addEventListener('mouseenter', () => {
      edgeRightTimer = clearEdgeHideTimer(edgeRightTimer);
    });
  }

  // Click to pin
  if (edgeTop) {
    edgeTop.addEventListener('click', (ev) => {
      if (ev.target instanceof Element && ev.target.closest('button')) return;
      edgeTop.classList.add('edge-pinned');
    });
  }
  if (edgeRight) {
    edgeRight.addEventListener('click', (ev) => {
      if (ev.target instanceof Element && ev.target.closest('button')) return;
      edgeRight.classList.add('edge-pinned');
    });
  }

  // Tabula Rasa button
  const rasaBtn = document.getElementById('btn-edge-rasa');
  if (rasaBtn) {
    rasaBtn.addEventListener('click', () => {
      clearInkDraft();
      clearCanvas();
      hideCanvasColumn();
      if (edgeTop) {
        edgeTop.classList.remove('edge-active', 'edge-pinned');
      }
    });
  }

  // Desktop: button clicks for left/right/bottom edge taps
  if (edgeLeftTap) {
    let edgeLeftLastTouchAt = 0;
    let edgeLeftTouchStartX = 0;
    let edgeLeftTouchStartY = 0;
    let edgeLeftTouchFlipHandled = false;
    edgeLeftTap.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (Date.now() - edgeLeftLastTouchAt < 700) return;
      toggleFileSidebarFromEdge();
    });
    edgeLeftTap.addEventListener('touchstart', (ev) => {
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      edgeLeftTouchStartX = touch.clientX;
      edgeLeftTouchStartY = touch.clientY;
      edgeLeftTouchFlipHandled = false;
    }, { passive: true });
    edgeLeftTap.addEventListener('touchmove', (ev) => {
      if (!state.hasArtifact || edgeLeftTouchFlipHandled) return;
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      const dx = touch.clientX - edgeLeftTouchStartX;
      const dy = touch.clientY - edgeLeftTouchStartY;
      const absDx = Math.abs(dx);
      const absDy = Math.abs(dy);
      if (dx > 30 && absDx > absDy * 1.1) {
        if (stepCanvasFile(-1)) {
          edgeLeftTouchFlipHandled = true;
          edgeLeftLastTouchAt = Date.now();
          ev.preventDefault();
        }
      }
    }, { passive: false });
    edgeLeftTap.addEventListener('touchend', (ev) => {
      if (edgeLeftTouchFlipHandled) {
        ev.preventDefault();
        edgeLeftTouchFlipHandled = false;
        return;
      }
      ev.preventDefault();
      edgeLeftLastTouchAt = Date.now();
      state.sidebarEdgeTapAt = Date.now();
      toggleFileSidebarFromEdge();
    }, { passive: false });
  }

  const edgeRightTap = document.getElementById('edge-right-tap');
  if (edgeRightTap) {
    let edgeRightLastTouchAt = 0;
    let edgeRightTouchStartX = 0;
    let edgeRightTouchStartY = 0;
    let edgeRightTouchFlipHandled = false;
    edgeRightTap.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (Date.now() - edgeRightLastTouchAt < 700) return;
      toggleRightEdgeDrawer(edgeRight);
    });
    edgeRightTap.addEventListener('touchstart', (ev) => {
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      edgeRightTouchStartX = touch.clientX;
      edgeRightTouchStartY = touch.clientY;
      edgeRightTouchFlipHandled = false;
    }, { passive: true });
    edgeRightTap.addEventListener('touchmove', (ev) => {
      if (!state.hasArtifact || edgeRightTouchFlipHandled) return;
      const touch = ev.touches && ev.touches[0];
      if (!touch) return;
      const dx = touch.clientX - edgeRightTouchStartX;
      const dy = touch.clientY - edgeRightTouchStartY;
      const absDx = Math.abs(dx);
      const absDy = Math.abs(dy);
      if (dx < -30 && absDx > absDy * 1.1) {
        if (stepCanvasFile(1)) {
          edgeRightTouchFlipHandled = true;
          edgeRightLastTouchAt = Date.now();
          ev.preventDefault();
        }
      }
    }, { passive: false });
    // Direct touch handler: iOS system gesture recognizer can intercept
    // document-level touch events near screen edges. Handle on the button
    // itself with touch-action:manipulation to bypass system gestures.
    edgeRightTap.addEventListener('touchend', (ev) => {
      if (edgeRightTouchFlipHandled) {
        ev.preventDefault();
        edgeRightTouchFlipHandled = false;
        return;
      }
      ev.preventDefault();
      edgeRightLastTouchAt = Date.now();
      toggleRightEdgeDrawer(edgeRight);
    }, { passive: false });
  }

  const prDrawerBackdrop = document.getElementById('pr-file-drawer-backdrop');
  if (prDrawerBackdrop) {
    prDrawerBackdrop.addEventListener('click', () => {
      setPrReviewDrawerOpen(false);
    });
  }
  // Mobile: touch tap and swipe from edges and open panels.
  // Buttons don't reliably fire click on iOS, so handle everything here.
  let edgeTouchHandled = false;
  document.addEventListener('touchstart', (ev) => {
    if (ev.touches.length !== 1) return;
    if (ev.target instanceof Element && ev.target.closest('#edge-left-tap,#edge-right-tap')) {
      edgeTouchStart = null;
      return;
    }
    const target = ev.target instanceof Element ? ev.target : null;
    const t = ev.touches[0];
    const edgeTapSize = getEdgeTapSizePx();
    const topEdgeTapSize = getTopEdgeTapSizePx();
    edgeTouchHandled = false;
    const startsInCanvasViewport = Boolean(target && target.closest('#canvas-viewport'));
    // When a canvas artifact is visible, prioritize horizontal swipe-to-flip
    // over left/right edge-open gestures.
    const preserveCanvasHorizontalSwipe = Boolean(state.hasArtifact && startsInCanvasViewport);
    const topOpen = Boolean(edgeTop && (edgeTop.classList.contains('edge-active') || edgeTop.classList.contains('edge-pinned')));
    const rightOpen = Boolean(edgeRight && (edgeRight.classList.contains('edge-active') || edgeRight.classList.contains('edge-pinned')));
    const leftOpen = Boolean(state.prReviewDrawerOpen);
    if (leftOpen && target && target.closest('#pr-file-pane')) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'left-open' };
    } else if (rightOpen && target && target.closest('#edge-right')) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'right-open' };
    } else if (topOpen && target && target.closest('#edge-top')) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'top-open' };
    } else if (!preserveCanvasHorizontalSwipe && isLeftEdgeTapCoordinate(t.clientX)) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'left' };
    } else if (!preserveCanvasHorizontalSwipe && t.clientX > window.innerWidth - edgeTapSize) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'right' };
    } else if (t.clientY < topEdgeTapSize) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'top' };
    } else if (t.clientY > window.innerHeight - edgeTapSize) {
      edgeTouchStart = { x: t.clientX, y: t.clientY, edge: 'bottom' };
    } else {
      edgeTouchStart = null;
    }
  }, { passive: true });

  document.addEventListener('touchmove', (ev) => {
    if (!edgeTouchStart || edgeTouchHandled || ev.touches.length !== 1) return;
    const t = ev.touches[0];
    const dx = t.clientX - edgeTouchStart.x;
    const dy = t.clientY - edgeTouchStart.y;
    const absDx = Math.abs(dx);
    const absDy = Math.abs(dy);
    if (edgeTouchStart.edge === 'right' && dx < -30 && absDx > absDy * 1.1 && edgeRight) {
      edgeRight.classList.add('edge-active');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'top' && dy > 30 && absDy > absDx * 1.1 && edgeTop) {
      edgeTop.classList.add('edge-active');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'left-open' && dx < -30 && absDx > absDy * 1.1 && state.prReviewDrawerOpen) {
      setPrReviewDrawerOpen(false);
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'right-open' && dx > 30 && absDx > absDy * 1.1 && edgeRight) {
      edgeRight.classList.remove('edge-active', 'edge-pinned');
      edgeTouchHandled = true;
    } else if (edgeTouchStart.edge === 'top-open' && dy < -30 && absDy > absDx * 1.1 && edgeTop) {
      edgeTop.classList.remove('edge-active', 'edge-pinned');
      edgeTouchHandled = true;
    }
  }, { passive: true });

  document.addEventListener('touchend', (ev) => {
    if (!edgeTouchStart || edgeTouchHandled) {
      edgeTouchStart = null;
      return;
    }
    // Tap (not swipe): small movement from start point
    const touch = ev.changedTouches && ev.changedTouches[0];
    if (touch) {
      const dx = Math.abs(touch.clientX - edgeTouchStart.x);
      const dy = Math.abs(touch.clientY - edgeTouchStart.y);
      if (dx < 20 && dy < 20) {
        let handledTapAction = false;
        switch (edgeTouchStart.edge) {
          case 'left':
            toggleFileSidebarFromEdge();
            handledTapAction = true;
            break;
          case 'bottom':
            handleRasaEdgeTap();
            handledTapAction = true;
            break;
          case 'right':
            toggleRightEdgeDrawer(edgeRight);
            handledTapAction = true;
            break;
          case 'top':
            if (edgeTop) {
              edgeTop.classList.add('edge-pinned');
              handledTapAction = true;
            }
            break;
        }
        if (handledTapAction) {
          // Prevent iOS from synthesizing a click after edge tap — the
          // panel pin above can cause the click to land inside the
          // newly-visible panel (e.g. chatHistory) and start recording.
          ev.preventDefault();
          suppressSyntheticClick();
        }
      }
    }
    edgeTouchStart = null;
  }, { passive: false });

  // Blur chat input when app goes to background so iOS does not
  // restore keyboard focus on resume.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      const cpInput = document.getElementById('chat-pane-input');
      if (cpInput && document.activeElement === cpInput) {
        cpInput.blur();
      }
    }
  });

  // Toggle safe-area bottom padding and keyboard state on mobile.
  // iOS can report changing viewport metrics while the keyboard opens;
  // keep a baseline "fully open" viewport and restore frame corners
  // once the keyboard is dismissed.
  if (window.visualViewport) {
    const inputRow = document.querySelector('.chat-pane-input-row');
    if (inputRow) {
      const root = document.documentElement;

      const setKeyboardOpen = (keyboardOpen) => {
        inputRow.classList.toggle('keyboard-open', keyboardOpen);
        document.body.classList.toggle('keyboard-open', keyboardOpen);
        if (!isIPhoneStandalone()) return;
        if (keyboardOpen) {
          root.style.setProperty('--cue-corner-radius', '0 0 0 0');
        } else {
          applyIPhoneFrameCorners();
        }
      };

      let baselineHeight = Math.max(
        window.innerHeight,
        window.visualViewport.height + Math.max(0, window.visualViewport.offsetTop || 0),
      );
      const syncKeyboardState = () => {
        const vv = window.visualViewport;
        if (!vv) return;
        const offsetTop = Math.max(0, Number(vv.offsetTop) || 0);
        const viewportExtent = vv.height + offsetTop;
        if (viewportExtent > baselineHeight) baselineHeight = viewportExtent;
        const focused = isFocusedTextInput();
        const shifted = offsetTop > 1;
        const shrunkenWhileFocused = focused && viewportExtent < baselineHeight - 100;
        const keyboardOpen = shifted || shrunkenWhileFocused;
        setKeyboardOpen(keyboardOpen);
        if (!keyboardOpen) {
          baselineHeight = Math.max(window.innerHeight, viewportExtent);
        }
      };

      window.visualViewport.addEventListener('resize', syncKeyboardState);
      window.visualViewport.addEventListener('scroll', syncKeyboardState);
      window.addEventListener('orientationchange', () => {
        baselineHeight = Math.max(
          window.innerHeight,
          window.visualViewport
            ? (window.visualViewport.height + Math.max(0, window.visualViewport.offsetTop || 0))
            : window.innerHeight,
        );
        window.setTimeout(syncKeyboardState, 80);
      });
      document.addEventListener('focusin', syncKeyboardState, true);
      document.addEventListener('focusout', () => {
        window.setTimeout(syncKeyboardState, 80);
        window.setTimeout(syncKeyboardState, 260);
      }, true);
      setSyncKeyboardStateNow(syncKeyboardState);
      syncKeyboardState();
    }
  }
}

export function closeEdgePanels() {
  const edgeTop = document.getElementById('edge-top');
  const edgeRight = document.getElementById('edge-right');
  if (edgeTop) edgeTop.classList.remove('edge-active', 'edge-pinned');
  if (edgeRight) edgeRight.classList.remove('edge-active', 'edge-pinned');
  if (state.prReviewDrawerOpen) {
    setPrReviewDrawerOpen(false);
  }
}
