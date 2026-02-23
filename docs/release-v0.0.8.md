# Release v0.0.8

## Scope

`v0.0.8` replaces the two-column layout with a zen canvas: a full-viewport surface with no visible chrome. Chat is invisible, responses stream as ephemeral overlays, and all interaction happens through tap, voice, and keyboard on a blank canvas or artifact.

## Highlights

### Zen Canvas

- **Tabula rasa**: blank white screen when no artifact is loaded. Nothing visible.
- **Artifact mode**: document (text, image, PDF, mail) fills the entire viewport.
- No toolbar, no prompt bar, no chat column. All chrome is hidden behind edge panels.

### Interaction Model

- **Tap/left-click** anywhere toggles voice recording. A red recording dot appears at the tap position.
- **Right-click** opens a floating text input at the cursor position.
- **Keyboard typing** (when nothing is focused) auto-activates text input.
- **Ctrl long-press** (300ms) starts push-to-talk; release stops and sends.
- **Enter** sends the message; input is cleared.
- **Escape** dismisses overlay/input; clears artifact back to tabula rasa.
- On artifact: tap/right-click captures line context (`[Line N of "title"]`).

### Ephemeral Response Overlay

- Responses stream live into a floating overlay.
- Click outside the overlay to dismiss.
- Document edits update the canvas in place with diff highlighting (block-level comparison, 2s yellow highlight, smooth scroll to change).
- Errors and cancellations auto-dismiss after a short delay.

### Edge Panels

- **Top edge** (hover/swipe): project list with "Tabula Rasa" button.
- **Right edge** (hover/swipe): chat log / diagnostics panel.
- Click to pin, Escape to close.

### New Module: zen.js

- Extracted interaction state, recording indicator, floating text input, and response overlay into a dedicated module.
- `canvas.js` gains block-level diff highlighting with `previousBlockTexts` comparison.

### Cleanup

- Removed: `#prompt-bar`, `#chat-column`, `#toolbar` from visible DOM.
- Removed: `sendChatMessage`, `focusChatInput`, `renderProjectTabs`, `setPromptContext`, `clearPromptContext`, prompt bar event listeners, two-column toggle logic.
- Preserved: all backend APIs, WebSocket layer, canvas rendering, mail triage UI, STT protocol.

## No Backend Changes

All Go code, API endpoints, WebSocket handlers, MCP tools, and SQLite schema remain identical. This is a pure frontend redesign.

## Tests

54 Playwright tests pass (14 new zen interaction tests, updated existing tests for new DOM).

## Traceability

For publication metadata, associate this release with:

- release label: `v0.0.8`
- repository: `https://github.com/krystophny/tabura`
- exact source revision: tag target commit hash
