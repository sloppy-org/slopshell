import * as env from './app-env.js';
import * as context from './app-context.js';

const { marked, apiURL, wsURL, renderCanvas, clearCanvas, getLocationFromSelection, clearLineHighlight, escapeHtml, sanitizeHtml, getActiveArtifactTitle, getActiveTextEventId, getPreviousArtifactText, getUiState, setUiMode, showIndicatorMode, hideIndicator, showTextInput, hideTextInput, showOverlay, hideOverlay, updateOverlay, isOverlayVisible, isTextInputVisible, isRecording, setRecording, getInputAnchor, setInputAnchor, pinCursorAnchor, getAnchorFromPoint, buildContextPrefix, getLastInputPosition, setLastInputPosition, configureLiveSession, getLiveSessionSnapshot, handleLiveSessionMessage, isLiveSessionListenActive, LIVE_SESSION_HOTWORD_DEFAULT, LIVE_SESSION_MODE_DIALOGUE, LIVE_SESSION_MODE_MEETING, onLiveSessionTTSPlaybackComplete, cancelLiveSessionListen, startLiveSession, stopLiveSession, initHotword, startHotwordMonitor, stopHotwordMonitor, isHotwordActive, onHotwordDetected, setHotwordThreshold, setHotwordAudioContext, getPreRollAudio, getHotwordMicStream, initVAD, ensureVADLoaded, float32ToWav } = env;
const { refs, state, getState, isVoiceTurn, COMPANION_VIEW_PATH_PREFIX, COMPANION_TRANSCRIPT_VIEW_PATH, COMPANION_SUMMARY_VIEW_PATH, COMPANION_REFERENCES_VIEW_PATH, MEETING_TRANSCRIPT_LABEL, MEETING_SUMMARY_LABEL, MEETING_REFERENCES_LABEL, MEETING_SUMMARY_ITEMS_PANEL_ID, CHAT_CTRL_LONG_PRESS_MS, ARTIFACT_EDIT_LONG_TAP_MS, ITEM_SIDEBAR_VIEWS, ITEM_SIDEBAR_GESTURE_CANCEL_PX, ITEM_SIDEBAR_GESTURE_COMMIT_PX, ITEM_SIDEBAR_GESTURE_LONG_PX, ITEM_SIDEBAR_DEFAULT_LATER_HOUR_UTC, ITEM_SIDEBAR_MENU_ID, DEV_UI_RELOAD_POLL_MS, ASSISTANT_ACTIVITY_POLL_MS, CHAT_WS_STALE_THRESHOLD_MS, ACTIVE_TURN_NO_ID_CLEAR_GRACE_MS, ACTIVE_TURN_ACTIVITY_CLEAR_GRACE_MS, PROJECT_CHAT_MODEL_ALIASES, PROJECT_CHAT_MODEL_REASONING_EFFORTS, TTS_SILENT_STORAGE_KEY, YOLO_MODE_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_ENABLED_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_LAST_SHOWN_STORAGE_KEY, SOMEDAY_REVIEW_NUDGE_INTERVAL_MS, ACTIVE_PROJECT_STORAGE_KEY, LAST_VIEW_STORAGE_KEY, RUNTIME_RELOAD_CONTEXT_STORAGE_KEY, SIDEBAR_IMAGE_EXTENSIONS, PANEL_MOTION_WATCH_QUERIES, VOICE_LIFECYCLE, COMPANION_IDLE_SURFACES, COMPANION_RUNTIME_STATES, TOOL_PALETTE_MODES } = context;

const showStatus = (...args) => refs.showStatus(...args);
const updateAssistantActivityIndicator = (...args) => refs.updateAssistantActivityIndicator(...args);
const setYoloModeLocal = (...args) => refs.setYoloModeLocal(...args);
const readYoloModePreference = (...args) => refs.readYoloModePreference(...args);
const readSomedayReviewNudgePreference = (...args) => refs.readSomedayReviewNudgePreference(...args);
const setSomedayReviewNudgeEnabled = (...args) => refs.setSomedayReviewNudgeEnabled(...args);
const clearInkDraft = (...args) => refs.clearInkDraft(...args);
const renderInkControls = (...args) => refs.renderInkControls(...args);
const switchProject = (...args) => refs.switchProject(...args);
const setPrReviewDrawerOpen = (...args) => refs.setPrReviewDrawerOpen(...args);
const activeProject = (...args) => refs.activeProject(...args);
const handleStopAction = (...args) => refs.handleStopAction(...args);
const hideCanvasColumn = (...args) => refs.hideCanvasColumn(...args);
const submitMessage = (...args) => refs.submitMessage(...args);
const stopVoiceCaptureAndSend = (...args) => refs.stopVoiceCaptureAndSend(...args);
const cancelChatVoiceCapture = (...args) => refs.cancelChatVoiceCapture(...args);
const handleItemSidebarKeyboardShortcut = (...args) => refs.handleItemSidebarKeyboardShortcut(...args);
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
const closeEdgePanels = (...args) => refs.closeEdgePanels(...args);
const syncInputModeBodyState = (...args) => refs.syncInputModeBodyState(...args);
const settleKeyboardAfterSubmit = (...args) => refs.settleKeyboardAfterSubmit(...args);
const ensureArtifactEditor = (...args) => refs.ensureArtifactEditor(...args);
const exitArtifactEditMode = (...args) => refs.exitArtifactEditMode(...args);
const enterArtifactEditMode = (...args) => refs.enterArtifactEditMode(...args);
const canEnterArtifactEditModeFromTarget = (...args) => refs.canEnterArtifactEditModeFromTarget(...args);
const showDisclaimerModal = (...args) => refs.showDisclaimerModal(...args);
const applyIPhoneFrameCorners = (...args) => refs.applyIPhoneFrameCorners(...args);
const initPanelMotionMode = (...args) => refs.initPanelMotionMode(...args);
const initEdgePanels = (...args) => refs.initEdgePanels(...args);
const setPenInkingState = (...args) => refs.setPenInkingState(...args);
const syncInkLayerSize = (...args) => refs.syncInkLayerSize(...args);
const beginVoiceCapture = (...args) => refs.beginVoiceCapture(...args);
const openComposerAt = (...args) => refs.openComposerAt(...args);
const suppressSyntheticClick = (...args) => refs.suppressSyntheticClick(...args);
const isSuppressedClick = (...args) => refs.isSuppressedClick(...args);
const isPenInputMode = (...args) => refs.isPenInputMode(...args);
const isEditableTarget = (...args) => refs.isEditableTarget(...args);
const isUiStopGestureActive = (...args) => refs.isUiStopGestureActive(...args);
const isLikelyIOS = (...args) => refs.isLikelyIOS(...args);
const isMobileViewport = (...args) => refs.isMobileViewport(...args);
const stepCanvasFile = (...args) => refs.stepCanvasFile(...args);
const beginInkStroke = (...args) => refs.beginInkStroke(...args);
const extendInkStroke = (...args) => refs.extendInkStroke(...args);
const resetInkDraftState = (...args) => refs.resetInkDraftState(...args);
const getEdgeTapSizePx = (...args) => refs.getEdgeTapSizePx(...args);
const getTopEdgeTapSizePx = (...args) => refs.getTopEdgeTapSizePx(...args);
const isKeyboardInputMode = (...args) => refs.isKeyboardInputMode(...args);
const submitInkDraft = (...args) => refs.submitInkDraft(...args);
const shouldStopInUiClick = (...args) => refs.shouldStopInUiClick(...args);
const hideItemSidebarMenu = (...args) => refs.hideItemSidebarMenu(...args);
const stepPrReviewFile = (...args) => refs.stepPrReviewFile(...args);

let bootstrapStarted = false;
let bootstrapErrorShown = false;

export function showBootstrapError(message) {
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

export function bindUi() {
  const canvasText = document.getElementById('canvas-text');
  const canvasViewport = document.getElementById('canvas-viewport');
  const artifactEditor = ensureArtifactEditor();
  const indicatorNode = document.getElementById('indicator');
  if (indicatorNode && indicatorNode.parentElement !== document.body) {
    document.body.appendChild(indicatorNode);
  }
  if (artifactEditor) {
    artifactEditor.addEventListener('keydown', (ev) => {
      if (ev.key !== 'Escape') return;
      ev.preventDefault();
      ev.stopPropagation();
      exitArtifactEditMode({ applyChanges: true });
    }, true);
  }
  let lastMouseX = Math.floor(window.innerWidth / 2);
  let lastMouseY = Math.floor(window.innerHeight / 2);
  let hasLastMousePosition = false;
  const isInEdgeZone = (x, y) => {
    const s = getEdgeTapSizePx();
    const top = getTopEdgeTapSizePx();
    return x < s || x > window.innerWidth - s || y < top || y > window.innerHeight - s;
  };
  const isVoiceInteractionTarget = (target, x, y) => (
    isInEdgeZone(x, y)
    || (target instanceof Element
      && target.closest('button,a,input,textarea,select,[contenteditable="true"],.overlay,.floating-input,.edge-panel,#canvas-pdf .canvas-pdf-page,#canvas-pdf .textLayer,#canvas-pdf .annotationLayer'))
  );
  const rememberMousePosition = (x, y) => {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return;
    lastMouseX = Number(x);
    lastMouseY = Number(y);
    hasLastMousePosition = true;
  };
  const getCtrlVoiceCapturePoint = () => {
    if (hasLastMousePosition) {
      return { x: lastMouseX, y: lastMouseY };
    }
    const lastPos = getLastInputPosition();
    if (Number.isFinite(lastPos?.x) && Number.isFinite(lastPos?.y)) {
      return { x: Number(lastPos.x), y: Number(lastPos.y) };
    }
    return {
      x: Math.floor(window.innerWidth / 2),
      y: Math.floor(window.innerHeight / 2),
    };
  };
  const beginVoiceCaptureFromPoint = (x, y) => {
    let anchor = null;
    if (state.hasArtifact && canvasText) {
      anchor = getAnchorFromPoint(x, y);
    }
    return beginVoiceCapture(x, y, anchor);
  };

  document.addEventListener('mousemove', (ev) => {
    rememberMousePosition(ev.clientX, ev.clientY);
  }, { passive: true });
  document.addEventListener('pointerdown', (ev) => {
    if (ev.pointerType !== 'mouse') return;
    rememberMousePosition(ev.clientX, ev.clientY);
  }, true);

  if (indicatorNode) {
    const isIndicatorArmed = () => (
      indicatorNode.classList.contains('is-working')
      || indicatorNode.classList.contains('is-recording')
      || indicatorNode.classList.contains('is-listening')
    );
    const pointHitsIndicatorChip = (x, y) => {
      const chips = indicatorNode.querySelectorAll('.record-dot, .stop-square');
      for (const chip of chips) {
        if (!(chip instanceof HTMLElement)) continue;
        const style = window.getComputedStyle(chip);
        if (style.display === 'none' || style.visibility === 'hidden') continue;
        const rect = chip.getBoundingClientRect();
        if (x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom) {
          return true;
        }
      }
      return false;
    };
    const isTapOnInteractiveUi = (ev) => {
      const t = ev.target;
      if (!(t instanceof Element)) return false;
      return Boolean(t.closest('button, a, input, textarea, select, #edge-left-tap, #edge-right-tap, #edge-top, #edge-right, #pr-file-pane, #pr-file-drawer-backdrop'));
    };
    const handleIndicatorTap = (ev, x, y, isTouch = false) => {
      if (!isIndicatorArmed()) return;
      if (!isTouch && isSuppressedClick()) return;
      const stopGestureActive = isUiStopGestureActive();
      const hitsChip = pointHitsIndicatorChip(x, y);
      if (!hitsChip && isTouch && stopGestureActive && isTapOnInteractiveUi(ev)) return;
      if (!hitsChip && !(isTouch && stopGestureActive)) return;
      ev.preventDefault();
      ev.stopPropagation();
      if (isTouch) suppressSyntheticClick();
      void handleStopAction();
    };
    document.addEventListener('click', (ev) => {
      handleIndicatorTap(ev, ev.clientX, ev.clientY, false);
    }, true);
    document.addEventListener('touchend', (ev) => {
      const touch = ev.changedTouches && ev.changedTouches.length > 0 ? ev.changedTouches[0] : null;
      if (!touch) return;
      handleIndicatorTap(ev, touch.clientX, touch.clientY, true);
    }, { passive: false, capture: true });
  }

  // Left-click/tap on canvas -> toggle voice recording
  const clickTarget = canvasViewport || document.getElementById('workspace');
  const syncIndicatorOnViewportChange = () => {
    updateAssistantActivityIndicator();
  };
  if (canvasViewport instanceof HTMLElement) {
    syncInkLayerSize();
    canvasViewport.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true, capture: true });
    let canvasSwipeStart = null;
    let canvasSwipeHandled = false;
    let horizontalWheelAccum = 0;
    let horizontalWheelLastAt = 0;
    const resetCanvasSwipe = () => {
      canvasSwipeStart = null;
      canvasSwipeHandled = false;
    };
    canvasViewport.addEventListener('touchstart', (ev) => {
      if (!isMobileViewport() && !isLikelyIOS()) return;
      if (state.prReviewDrawerOpen || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      canvasSwipeStart = { x: touch.clientX, y: touch.clientY };
      canvasSwipeHandled = false;
    }, { passive: true });
    canvasViewport.addEventListener('touchmove', (ev) => {
      if (!canvasSwipeStart || canvasSwipeHandled || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      const dx = touch.clientX - canvasSwipeStart.x;
      const dy = touch.clientY - canvasSwipeStart.y;
      if (!state.hasArtifact) return;
      if (Math.abs(dx) < 48) return;
      if (Math.abs(dx) <= Math.abs(dy) * 1.25) return;
      const stepped = stepCanvasFile(dx < 0 ? 1 : -1);
      if (!stepped) return;
      canvasSwipeHandled = true;
      ev.preventDefault();
    }, { passive: false });
    canvasViewport.addEventListener('touchend', resetCanvasSwipe, { passive: true });
    canvasViewport.addEventListener('touchcancel', resetCanvasSwipe, { passive: true });
    canvasViewport.addEventListener('wheel', (ev) => {
      if (!state.hasArtifact) return;
      const absX = Math.abs(ev.deltaX);
      const absY = Math.abs(ev.deltaY);
      if (absX < 0.8) return;
      if (absX <= absY * 1.15) return;
      ev.preventDefault();
      const now = Date.now();
      if (now - horizontalWheelLastAt > 260) {
        horizontalWheelAccum = 0;
      }
      horizontalWheelAccum += ev.deltaX;
      if (Math.abs(horizontalWheelAccum) < 48) return;
      const stepped = stepCanvasFile(horizontalWheelAccum > 0 ? 1 : -1);
      if (!stepped) return;
      horizontalWheelAccum = 0;
      horizontalWheelLastAt = now;
    }, { passive: false });
    canvasViewport.addEventListener('pointerdown', (ev) => {
      if (!isPenInputMode()) return;
      if (ev.pointerType !== 'pen') return;
      if (isEditableTarget(ev.target)) return;
      if (ev.target instanceof Element && ev.target.closest('.edge-panel,#pr-file-pane,#pr-file-drawer-backdrop')) return;
      if (beginInkStroke(ev)) {
        try { window.getSelection()?.removeAllRanges(); } catch (_) {}
        setPenInkingState(true);
        ev.preventDefault();
        try { canvasViewport.setPointerCapture(ev.pointerId); } catch (_) {}
      }
    }, true);
    canvasViewport.addEventListener('pointermove', (ev) => {
      if (!isPenInputMode()) return;
      if (state.inkDraft.activePointerId !== ev.pointerId) return;
      if (extendInkStroke(ev)) {
        ev.preventDefault();
      }
    }, true);
    const finishInkPointer = (ev) => {
      if (state.inkDraft.activePointerId !== ev.pointerId) return;
      extendInkStroke(ev);
      resetInkDraftState();
      setPenInkingState(false);
      renderInkControls();
      ev.preventDefault();
    };
    canvasViewport.addEventListener('pointerup', finishInkPointer, true);
    canvasViewport.addEventListener('pointercancel', finishInkPointer, true);
    canvasViewport.addEventListener('selectstart', (ev) => {
      if (!isPenInputMode()) return;
      ev.preventDefault();
    }, true);
  }
  window.addEventListener('scroll', syncIndicatorOnViewportChange, { passive: true });
  window.addEventListener('resize', syncIndicatorOnViewportChange);

  if (clickTarget) {
    let touchTapStartX = 0;
    let touchTapStartY = 0;
    let touchTapTracking = false;
    let touchTapMoved = false;
    let touchLongTapTriggered = false;
    let touchEditTimer = null;
    const TOUCH_TAP_MOVE_THRESHOLD = 10;
    const clearTouchEditTimer = () => {
      if (touchEditTimer !== null) {
        clearTimeout(touchEditTimer);
        touchEditTimer = null;
      }
    };

    const handleWorkspaceTap = (target, x, y) => {
      const liveSessionPointerMode = state.liveSessionActive
        && (state.liveSessionMode === LIVE_SESSION_MODE_DIALOGUE || state.liveSessionMode === LIVE_SESSION_MODE_MEETING);
      if (liveSessionPointerMode) {
        if (isVoiceInteractionTarget(target, x, y)) return;
        const sel = window.getSelection();
        if (sel && !sel.isCollapsed) return;
        rememberMousePosition(x, y);
        const anchor = state.hasArtifact && canvasText ? getAnchorFromPoint(x, y) : null;
        pinCursorAnchor(x, y, anchor);
        updateAssistantActivityIndicator();
        return;
      }
      if (isLiveSessionListenActive()) {
        if (isVoiceInteractionTarget(target, x, y)) return;
        cancelLiveSessionListen();
        if (isKeyboardInputMode()) {
          const anchor = state.hasArtifact && canvasText ? getAnchorFromPoint(x, y) : null;
          openComposerAt(x, y, anchor);
        } else {
          void beginVoiceCaptureFromPoint(x, y);
        }
        return;
      }
      if (isUiStopGestureActive()) {
        void handleStopAction();
        return;
      }
      if (isVoiceInteractionTarget(target, x, y)) return;
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;
      rememberMousePosition(x, y);
      if (isRecording()) {
        void stopVoiceCaptureAndSend();
        return;
      }
      if (isKeyboardInputMode()) {
        const anchor = state.hasArtifact && canvasText ? getAnchorFromPoint(x, y) : null;
        openComposerAt(x, y, anchor);
        return;
      }
      void beginVoiceCaptureFromPoint(x, y);
    };

    clickTarget.addEventListener('touchstart', (ev) => {
      if (ev.touches.length !== 1) {
        touchTapTracking = false;
        touchTapMoved = false;
        touchLongTapTriggered = false;
        clearTouchEditTimer();
        return;
      }
      const touch = ev.touches[0];
      if (isEditableTarget(ev.target)) {
        touchTapTracking = false;
        touchTapMoved = false;
        touchLongTapTriggered = false;
        clearTouchEditTimer();
        return;
      }
      touchTapStartX = touch.clientX;
      touchTapStartY = touch.clientY;
      touchTapTracking = !isVoiceInteractionTarget(ev.target, touch.clientX, touch.clientY);
      touchTapMoved = false;
      touchLongTapTriggered = false;
      clearTouchEditTimer();
      if (touchTapTracking && canEnterArtifactEditModeFromTarget(ev.target)) {
        touchEditTimer = window.setTimeout(() => {
          touchEditTimer = null;
          touchTapTracking = false;
          touchTapMoved = false;
          touchLongTapTriggered = enterArtifactEditMode(touchTapStartX, touchTapStartY);
          if (touchLongTapTriggered) suppressSyntheticClick();
        }, ARTIFACT_EDIT_LONG_TAP_MS);
      }
    }, { passive: true });

    clickTarget.addEventListener('touchmove', (ev) => {
      if ((!touchTapTracking && touchEditTimer === null) || touchTapMoved || ev.touches.length !== 1) return;
      const touch = ev.touches[0];
      if (Math.hypot(touch.clientX - touchTapStartX, touch.clientY - touchTapStartY) > TOUCH_TAP_MOVE_THRESHOLD) {
        touchTapMoved = true;
        clearTouchEditTimer();
      }
    }, { passive: true });

    clickTarget.addEventListener('touchend', (ev) => {
      if (touchLongTapTriggered) {
        touchLongTapTriggered = false;
        touchTapTracking = false;
        touchTapMoved = false;
        clearTouchEditTimer();
        ev.preventDefault();
        suppressSyntheticClick();
        return;
      }
      if (!touchTapTracking) return;
      touchTapTracking = false;
      if (touchTapMoved) {
        touchTapMoved = false;
        clearTouchEditTimer();
        return;
      }
      const touch = ev.changedTouches && ev.changedTouches.length > 0 ? ev.changedTouches[0] : null;
      if (!touch) return;
      clearTouchEditTimer();
      ev.preventDefault();
      suppressSyntheticClick();
      handleWorkspaceTap(ev.target, touch.clientX, touch.clientY);
    }, { passive: false });

    clickTarget.addEventListener('touchcancel', () => {
      touchTapTracking = false;
      touchTapMoved = false;
      touchLongTapTriggered = false;
      clearTouchEditTimer();
    }, { passive: true });

    clickTarget.addEventListener('click', (ev) => {
      if (isSuppressedClick()) return;
      if (ev.button !== 0) return;
      handleWorkspaceTap(ev.target, ev.clientX, ev.clientY);
    });
  }

  // Right-click -> artifact editor (text artifacts) or floating text input
  if (clickTarget) {
    clickTarget.addEventListener('contextmenu', (ev) => {
      if (state.artifactEditMode) {
        ev.preventDefault();
        return;
      }
      if (ev.target instanceof Element && ev.target.closest('.edge-panel')) return;
      if (canEnterArtifactEditModeFromTarget(ev.target)) {
        ev.preventDefault();
        enterArtifactEditMode(ev.clientX, ev.clientY);
        return;
      }
      ev.preventDefault();
      cancelLiveSessionListen();
      let anchor = null;
      if (state.hasArtifact && canvasText) {
        anchor = getAnchorFromPoint(ev.clientX, ev.clientY);
      }
      openComposerAt(ev.clientX, ev.clientY, anchor);
    });
  }

  // Text input Enter -> send
  const floatingInput = document.getElementById('floating-input');
  if (floatingInput instanceof HTMLTextAreaElement) {
    floatingInput.addEventListener('focus', () => {
      cancelLiveSessionListen();
    });
    floatingInput.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        const text = floatingInput.value.trim();
        if (text) {
          state.lastInputOrigin = 'text';
          floatingInput.value = '';
          floatingInput.blur();
          hideTextInput();
          settleKeyboardAfterSubmit();
          void submitMessage(text);
        }
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        hideTextInput();
      }
    });
    floatingInput.addEventListener('input', () => {
      floatingInput.style.height = 'auto';
      floatingInput.style.height = `${Math.min(floatingInput.scrollHeight, 240)}px`;
    });
  }

  // Chat pane input: Enter sends, Escape blurs, auto-resize
  const chatPaneInput = document.getElementById('chat-pane-input');
  if (chatPaneInput instanceof HTMLTextAreaElement) {
    chatPaneInput.addEventListener('focus', () => {
      cancelLiveSessionListen();
    });
    chatPaneInput.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        const text = chatPaneInput.value.trim();
        if (text) {
          state.lastInputOrigin = 'text';
          chatPaneInput.value = '';
          chatPaneInput.style.height = '';
          chatPaneInput.blur();
          settleKeyboardAfterSubmit();
          void submitMessage(text);
        }
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        chatPaneInput.value = '';
        chatPaneInput.style.height = '';
        chatPaneInput.blur();
        settleKeyboardAfterSubmit();
      }
    });
    chatPaneInput.addEventListener('input', () => {
      chatPaneInput.style.height = 'auto';
      chatPaneInput.style.height = `${Math.min(chatPaneInput.scrollHeight, 240)}px`;
    });

  }

  const inkClear = document.getElementById('ink-clear');
  if (inkClear instanceof HTMLButtonElement) {
    inkClear.addEventListener('click', () => {
      clearInkDraft();
      showStatus('ink cleared');
    });
  }
  const inkSubmit = document.getElementById('ink-submit');
  if (inkSubmit instanceof HTMLButtonElement) {
    inkSubmit.addEventListener('click', () => {
      void submitInkDraft();
    });
  }

  // Voice tap on chat history (only when panel is pinned, not just hover-active)
  const chatHistory = document.getElementById('chat-history');
  if (chatHistory) {
    chatHistory.addEventListener('click', (ev) => {
      if (isKeyboardInputMode()) return;
      if (ev.button !== 0) return;
      if (ev.target instanceof Element && ev.target.closest('a,button,input,textarea,select,[contenteditable="true"]')) return;
      if (isInEdgeZone(ev.clientX, ev.clientY)) return;
      const edgeR = chatHistory.closest('.edge-panel');
      if (edgeR && !edgeR.classList.contains('edge-pinned')) return;
      if (isLiveSessionListenActive()) {
        cancelLiveSessionListen();
        void beginVoiceCaptureFromPoint(ev.clientX, ev.clientY);
        return;
      }
      if (shouldStopInUiClick()) { void handleStopAction(); return; }
      if (isRecording()) { void stopVoiceCaptureAndSend(); return; }
      void beginVoiceCaptureFromPoint(ev.clientX, ev.clientY);
    });
  }

  // Click outside overlay/input -> dismiss
  document.addEventListener('mousedown', (ev) => {
    if (!(ev.target instanceof Element)) return;
    const sidebarMenu = document.getElementById(ITEM_SIDEBAR_MENU_ID);
    if (state.itemSidebarMenuOpen && sidebarMenu instanceof HTMLElement && !sidebarMenu.contains(ev.target)) {
      hideItemSidebarMenu();
    }
    // Dismiss overlay on click outside
    if (isOverlayVisible()) {
      const overlay = document.getElementById('overlay');
      if (overlay && !overlay.contains(ev.target)) {
        hideOverlay();
      }
    }
    // Dismiss text input on click outside
    if (isTextInputVisible()) {
      const input = document.getElementById('floating-input');
      if (input && !input.contains(ev.target) && ev.button === 0) {
        hideTextInput();
      }
    }
  });

  // Keyboard typing auto-activates text input (rasa mode)
  document.addEventListener('keydown', (ev) => {
    // Escape handling
    if (ev.key === 'Escape' && !ev.metaKey && !ev.ctrlKey && !ev.altKey) {
      if (state.artifactEditMode) {
        ev.preventDefault();
        exitArtifactEditMode({ applyChanges: true });
        return;
      }
      if (isRecording()) {
        cancelChatVoiceCapture();
        showStatus('ready');
        return;
      }
      if (isOverlayVisible()) {
        hideOverlay();
        return;
      }
      if (isTextInputVisible()) {
        hideTextInput();
        return;
      }
      if (state.itemSidebarMenuOpen) {
        hideItemSidebarMenu();
        return;
      }
      if (state.inkDraft.dirty) {
        clearInkDraft();
        showStatus('ink cleared');
        return;
      }
      if (state.prReviewDrawerOpen) {
        setPrReviewDrawerOpen(false);
        return;
      }
      closeEdgePanels();
      if (state.hasArtifact) {
        clearCanvas();
        hideCanvasColumn();
        return;
      }
      void handleStopAction();
      return;
    }

    // Enter stops recording
    if (ev.key === 'Enter' && isRecording()) {
      ev.preventDefault();
      void stopVoiceCaptureAndSend();
      return;
    }
    if (ev.key === 'Enter' && isPenInputMode() && state.inkDraft.dirty) {
      ev.preventDefault();
      void submitInkDraft();
      return;
    }

    // Control long-press for PTT
    if (ev.key === 'Control' && !ev.repeat) {
      if (state.chatCtrlHoldTimer || state.chatVoiceCapture) return;
      if (isLiveSessionListenActive()) {
        cancelLiveSessionListen();
      }
      state.chatCtrlHoldTimer = window.setTimeout(() => {
        state.chatCtrlHoldTimer = null;
        const point = getCtrlVoiceCapturePoint();
        void beginVoiceCaptureFromPoint(point.x, point.y);
      }, CHAT_CTRL_LONG_PRESS_MS);
      return;
    }

    if (ev.ctrlKey && ev.key !== 'Control') {
      if (state.chatCtrlHoldTimer) {
        clearTimeout(state.chatCtrlHoldTimer);
        state.chatCtrlHoldTimer = null;
      }
      if (state.chatVoiceCapture) {
        cancelChatVoiceCapture();
        showStatus('ready');
      }
      return;
    }

    if (ev.metaKey || ev.ctrlKey || ev.altKey) return;
    if (isEditableTarget(ev.target)) return;
    if (state.artifactEditMode) return;
    if (handleItemSidebarKeyboardShortcut(ev)) return;

    if (ev.key === 'ArrowRight') {
      if (stepCanvasFile(1)) {
        ev.preventDefault();
      }
      return;
    }
    if (ev.key === 'ArrowLeft') {
      if (stepCanvasFile(-1)) {
        ev.preventDefault();
      }
      return;
    }

    if (state.prReviewMode) {
      if (ev.key === 'j' || ev.key === 'J') {
        ev.preventDefault();
        stepPrReviewFile(1);
        return;
      }
      if (ev.key === 'k' || ev.key === 'K') {
        ev.preventDefault();
        stepPrReviewFile(-1);
        return;
      }
    }

    // Auto-activate text input on printable key
    if (ev.key.length === 1 && !isTextInputVisible()) {
      // Route to chat pane input when chat pane is open (desktop only)
      const edgeR = document.getElementById('edge-right');
      const cpInput = document.getElementById('chat-pane-input');
      const chatPaneOpen = edgeR && (edgeR.classList.contains('edge-active') || edgeR.classList.contains('edge-pinned'));
      if (chatPaneOpen && cpInput instanceof HTMLTextAreaElement && !window.matchMedia('(max-width: 767px)').matches) {
        cancelLiveSessionListen();
        cpInput.focus();
        cpInput.value = ev.key;
        const caret = ev.key.length;
        cpInput.setSelectionRange(caret, caret);
        cpInput.dispatchEvent(new Event('input', { bubbles: true }));
        ev.preventDefault();
        return;
      }
      if (!isKeyboardInputMode()) {
        return;
      }
      const cx = window.innerWidth / 2 - 130;
      const cy = window.innerHeight / 2;
      cancelLiveSessionListen();
      openComposerAt(cx, cy, null, ev.key);
      ev.preventDefault();
      return;
    }

    // Enter when text input is NOT visible but could send
    if (ev.key === 'Enter' && !isTextInputVisible()) {
      ev.preventDefault();
    }
  }, true);

  document.addEventListener('keyup', (ev) => {
    if (ev.key !== 'Control') return;
    if (state.chatCtrlHoldTimer) {
      clearTimeout(state.chatCtrlHoldTimer);
      state.chatCtrlHoldTimer = null;
      return;
    }
    if (state.chatVoiceCapture) {
      void stopVoiceCaptureAndSend();
    }
  }, true);

  window.addEventListener('blur', () => {
    if (state.chatCtrlHoldTimer) {
      clearTimeout(state.chatCtrlHoldTimer);
      state.chatCtrlHoldTimer = null;
    }
    // Keep active capture alive on transient browser blur; hard stop is
    // handled by visibilitychange when the page is actually hidden.
    if (state.chatVoiceCapture && document.hidden) {
      cancelChatVoiceCapture();
      showStatus('ready');
    }
  });

  // Text selection on artifact sets anchor
  if (canvasText) {
    canvasText.addEventListener('mouseup', () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) return;
      const loc = getLocationFromSelection();
      if (loc) {
        setInputAnchor({ line: loc.line, title: loc.title, selectedText: loc.selectedText });
      }
    });
  }

  initEdgePanels();
}

export function showSplash() {
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

export async function init() {
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
  syncInputModeBodyState();
  updateAssistantActivityIndicator();
  startRuntimeReloadWatcher();
  startAssistantActivityWatcher();
  clearCanvas();
  hideCanvasColumn();
  showStatus('starting...');

  // Check TTS availability from runtime
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
  // Pin chat panel now that all startup state is settled.
  if (isMobileSilent()) {
    const edgeRight = document.getElementById('edge-right');
    if (edgeRight) edgeRight.classList.add('edge-pinned');
  }
  restoreRuntimeReloadContext();
  showSplash();
  // Enable panel slide transitions only after startup is fully painted.
  requestAnimationFrame(() => requestAnimationFrame(initPanelMotionMode));
}

export async function authGate() {
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
  const loginPrompt = document.getElementById('login-prompt');
  const loginBtn = document.getElementById('btn-login');

  if (!data.has_password) {
    loginPassword.style.display = 'none';
    loginView.style.display = '';
    mainView.style.display = 'none';
    return new Promise(() => {});
  }

  loginView.style.display = '';
  mainView.style.display = 'none';

  await new Promise((resolve) => {
    loginForm.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      loginError.textContent = '';
      const pw = loginPassword.value;
      if (!pw) return;
      try {
        const r = await fetch(apiURL('login'), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ password: pw }),
        });
        if (!r.ok) {
          const msg = (await r.text()).trim();
          loginError.textContent = msg || `Error ${r.status}`;
          return;
        }
        resolve();
      } catch (err) {
        loginError.textContent = String(err?.message || err);
      }
    });
  });

  loginView.style.display = 'none';
  mainView.style.display = '';
}

export function bootstrapApp() {
  if (bootstrapStarted) return;
  bootstrapStarted = true;
  void ensureVADLoaded();
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
