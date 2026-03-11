# Object-Scoped Intent UI

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

## Purpose

Define a calm, document-first AI interaction model optimized for high-latency and e-ink style environments.

Core constraints:
- No always-visible chat panel.
- No hidden autonomous actions.
- Invocation is local to selected object.
- User approval is required before applied changes.

## Invocation Primitive

Tap (left-click) anywhere on the canvas toggles voice recording. A recording dot appears at the tap position.
Desktop equivalents: tap to toggle voice capture, or hold `Ctrl` (300ms) for push-to-talk.
Right-click opens a floating text input at the cursor position.
Keyboard typing (when nothing is focused) auto-activates text input.
Direct ink is available as an annotate-surface tool; dirty ink shows explicit submit/clear controls and saves artifacts under `.tabura/artifacts/ink/`.

On artifact: tap or right-click captures line context prepended to the message.

No always-visible prompt bar or chat panel. Chrome is edge-revealed. Responses stream as ephemeral overlays.

## Email Reply Interaction

### Normal tap on Reply

- Opens standard reply editor.

### Long press on Reply

- Captures intent (voice or prompt).
- Routes intent to either prompt-generation path or dictation path.
- Produces editable draft text.
- Keeps draft unsent until explicit user action.

Mandatory invariants:
- MUST NOT auto-send mail.
- MUST NOT auto-apply irreversible change.
- MUST present explicit user control before apply.

## Review Interaction Model

Selection types:
- Text selection (highlight)
- Point comment (context click/tap)
- Region selection (lasso/encircle target)
- Structural target (section/page level)

Capture sequence:
1. User taps/selects target location on artifact.
2. System captures line context from tap point.
3. User speaks (via tap-to-record) or types (via right-click text input).
4. Message sent with location context prefix; context cleared after send.

## Intent Classification for Reply Drafting

Current runtime behavior uses deterministic branch outcomes:
- `prompt`
- `dictation`

Ambiguous input falls back to prompt branch with explicit fallback policy metadata.

## Human Control Model

All generated outputs are proposals.

Required controls:
- Accept
- Edit
- Reject

Committed state is separated from draft state.

## E-Ink and Low-Refresh Constraints

UI behavior targets minimal redraw and low-motion interaction:
- avoid animations and sliding panels
- keep overlays compact and local
- use high-contrast text and structural markers for diffs
- preserve document continuity over chat-like modality switching

## Live Sessions

Tabura exposes exactly two live session types through the hidden top edge panel:

- `Dialogue`
- `Meeting`

Shared baseline:

- local-first
- Whisper-backed by default
- built-in `Alexa` hotword
- one shared browser audio/runtime owner
- no floating on-canvas launcher

`Dialogue` is for direct back-and-forth with Tabura:

- hands-free after entry
- lower turn-end latency
- interruption and follow-up listening
- full spoken requests without requiring the hotword

`Meeting` is for room context with selective replies:

- continuous transcription for context
- stronger directed-speech gating before speaking
- `Alexa` remains a direct trigger
- microphone only, with no separate meeting bot identity
- face states represent `idle`, `listening`, `thinking`, `talking`, and `error`
- black mode is the alternate idle surface
- if a canvas document is visible, the document takes precedence over the idle surface

Workspace model:

- meetings and long-running jobs should default to ephemeral workspaces
- each workspace keeps one active run in its main thread
- ad hoc requests and run monitoring stay workspace-independent; there is no hub entity

Noise filtering remains important:

- short filler-only transcripts should be rejected as noise
- known Whisper hallucinations on silent audio should be filtered
- background TV/radio patterns should be rejected

## Non-Goals for v0.0.1

- Fully autonomous end-to-end action chains.
- Unreviewed background modifications.
- Multi-user synchronization semantics.
