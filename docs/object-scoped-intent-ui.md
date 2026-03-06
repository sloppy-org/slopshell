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
Pen mode is a first-class input mode for direct ink capture on the canvas; dirty ink shows explicit submit/clear controls and saves artifacts under `.tabura/artifacts/ink/`.

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

## Companion Mode

This section describes the planned successor to the current split between
tap-to-record interaction and legacy conversation-mode behavior. It is a
product-direction spec, not a claim that the runtime has already converged.

Companion Mode is the single continuous assistant mode for:

- live in-person meetings
- one-on-one conversations
- solo workday assistant presence
- online calls as one additional context source

Companion Mode is:

- botless
- local-first
- Whisper-backed by default
- manually toggled
- always-transcribing for context

Default capture policy:

- microphone only
- no separate meeting attendee/bot
- no mandatory cloud recorder identity

Default flow:
1. User explicitly enters Companion Mode.
2. Tabura continuously transcribes microphone input for context.
3. Directed-speech / command detection decides when the assistant should answer.
4. Assistant responds via TTS playback unless silent mode is enabled.
5. Transcript context continues even when the assistant stays silent.

Idle surface:

- if no document is displayed, show a full-screen minimal humanoid face
- face states represent `idle`, `listening`, `thinking`, `talking`, and `error`
- black mode is the alternate idle surface
- if a canvas document is visible, the document takes precedence over the idle surface

Project model:

- meetings and long-running jobs should default to temporary projects
- each project keeps one active run in its main thread
- Hub remains for ad hoc requests and run monitoring only

Noise filtering remains important:

- short filler-only transcripts should be rejected as noise
- known Whisper hallucinations on silent audio should be filtered
- background TV/radio patterns should be rejected

## Non-Goals for v0.0.1

- Fully autonomous end-to-end action chains.
- Unreviewed background modifications.
- Multi-user synchronization semantics.
