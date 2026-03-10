# Gesture Truth Table

This document is the explicit gesture matrix for Tabura's shipped runtime.

The table below describes visible runtime states, not hidden implementation flags:

- `Blank surface` means no artifact is visible and no live session is active.
- `Artifact visible` means an artifact is on canvas and no live session is active.
- `Annotation mode` means the annotation surface is active and the selected tool is part of the visible state (`pointer`, `highlight`, `ink`, `text_note`, or `prompt`).
- `Dialogue live` and `Meeting live` override ordinary prompt/annotation routing.

Canonical rule: no gesture may have two incompatible meanings in the same visible state.

Canonical tap-to-voice rule: when a tap resolves to voice, it always means `start local capture bound to the current context`. On artifact surfaces, that context includes the tapped cursor anchor. A follow-up tap or `Enter` while recording stops and sends that same local capture; it does not create a second start meaning.

Current authority for the shipped behavior:

- `internal/web/static/app-init.js`
- `internal/web/static/live-session.js`
- `tests/playwright/canvas.spec.ts`
- `tests/playwright/canvas-cursor-context.spec.ts`
- `tests/playwright/ui-system.spec.ts`
- `tests/playwright/artifact-context.spec.ts`

| Input | Blank surface | Artifact visible | Annotation mode | Dialogue live | Meeting live |
| --- | --- | --- | --- | --- | --- |
| Tap | Starts local capture from the tapped point in the default `pointer` / `prompt` path. A second tap stops and sends. | Starts local capture bound to the tapped artifact anchor in the default `pointer` / `prompt` path. A second tap stops and sends. | No generic tap-to-voice. The selected annotation tool owns the tap: `text_note` places a sticky note / annotation bubble, `ink` waits for pen contact, `highlight` preserves selection-driven annotation, and `prompt` uses the same local capture rule as the default path. | If Dialogue is in the post-TTS listen window, tap cancels that listen window and immediately starts local capture bound to the tapped context. Otherwise tap follows the same local-capture rule as the default path. | Never starts a new local capture. Tap pins or moves the cursor context and sends a `canvas_position` update only. |
| Tap+voice | Means the same thing everywhere it exists: start local capture tied to the current tap context, then speak into that local capture. | Means the same thing everywhere it exists: start local capture tied to the tapped artifact location, then speak into that local capture. | Exists only for the visible `prompt` tool. Other annotation tools keep tap local to annotation placement/editing and do not reinterpret it as voice. | Means cancel listen if needed, then start local capture tied to the tapped context. | Not available. Meeting taps stay cursor-only even if the user speaks after tapping. |
| Right-click | Opens the floating composer at the clicked point. | Opens the floating composer at the clicked point unless the target is an editable text artifact, in which case it enters artifact edit mode. | Editable text artifacts enter artifact edit mode first; otherwise the floating composer opens. | Cancels the Dialogue listen window, then opens the floating composer or artifact editor using the same precedence as the non-live path. | Opens the floating composer or artifact editor using the same precedence as the non-live path. The meeting session itself keeps running. |
| Typing | Printable keys open the chat-pane composer if the right panel is pinned; otherwise they open the floating composer only when the editor / `text_note` path is active. | Same as blank surface, but submitted text carries the active artifact context when one exists. | Printable keys route into the composer only for the visible text-composition path. Annotation tools otherwise keep keyboard input for shortcuts or focused editors. | Typing cancels the Dialogue listen window before routing into the relevant composer. | Typing routes into the same composer paths as the non-live UI; it does not start or stop meeting capture. |
| Ink/stylus | No annotation stroke is created because there is nothing to annotate. | Pen input is inert unless the annotation surface is active with the `ink` tool selected. | Pen contact with the `ink` tool draws on `#ink-layer`; `Enter` submits the draft and `Escape` clears it. Finger or mouse taps do not substitute for pen strokes. | Live state does not change pen semantics. Dialogue still requires an explicit tap-to-voice path for audio capture. | Live state does not change pen semantics. Meeting still treats taps as cursor updates, not voice capture. |
| Enter | If local capture is recording, stop and send. If a composer is focused, send and clear it. | Same as blank surface, with artifact context preserved on submission. | Submits the focused composer; when `ink` has a dirty draft, submits the ink draft instead of sending chat text. | If local capture is recording, stop and send. If a composer is focused, send and clear it. Dialogue listen itself is not started by `Enter`. | Sends focused composer text or other explicit submit actions, but does not control meeting capture. |
| Escape | Dismisses overlay/input first. If nothing transient is open, it falls through to stop/cancel behavior. | Dismisses overlay/input first, then closes panels, then clears the artifact back to tabula rasa if nothing else is open. | Exits artifact edit mode, clears dirty ink drafts, dismisses overlays/inputs, then falls through to the same panel/artifact clearing order. | Cancels recording if one is active; otherwise dismisses UI layers using the normal precedence. It does not invent a second tap meaning. | Dismisses UI layers using the normal precedence; if no transient UI is open and an artifact is visible, the artifact clears, but Escape is not the meeting start gesture. |

Precedence notes:

1. Live session state wins over ordinary prompt/annotation routing.
2. Inside `Annotation mode`, the selected tool is part of the visible state and therefore part of the gesture contract.
3. `Tap-to-voice` is not a second product model. It is only the local-capture branch of the main interaction grammar.
