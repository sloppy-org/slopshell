# Object-Scoped Intent UI

## Purpose

Define a calm, document-first AI interaction model optimized for high-latency and e-ink style environments.

Core constraints:
- No persistent chat panel.
- No hidden autonomous actions.
- Invocation is local to selected object.
- User approval is required before applied changes.

## Invocation Primitive

Tap (left-click) anywhere on the canvas toggles voice recording. A recording dot appears at the tap position.
Desktop equivalents: tap to toggle voice capture, or hold `Ctrl` (300ms) for push-to-talk.
Right-click opens a floating text input at the cursor position.
Keyboard typing (when nothing is focused) auto-activates text input.

On artifact: tap or right-click captures line context prepended to the message.

No prompt bar, no chat panel, no visible chrome. Responses stream as ephemeral overlays.

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

## Conversation Mode

Conversation mode enables continuous hands-free interaction via wake word detection.

Activation: toggle the conversation button in the top edge panel.

Flow:
1. **Passive listening**: hotword monitor runs in-browser (openWakeWord "alexa" model). Indicator shows pause bars (black border).
2. **Wake word detected**: recording starts immediately. Indicator switches to recording (red border + dot).
3. **Speech captured**: VAD detects end-of-utterance, audio is sent for STT transcription, then processed as a chat message.
4. **Response**: assistant responds via TTS playback (if not in silent mode).
5. **Follow-up window**: after TTS completes, a 6-second listening window opens for follow-up speech. Indicator shows listening (blue border + pulse).
6. **Follow-up timeout**: if no speech is detected within 6s, returns to passive wake-word listening with pause indicator.

During the follow-up window, tap or push-to-talk overrides the VAD-based listen and starts manual recording.

Nonsense/background filtering prevents spurious LLM calls during the follow-up window:
- Short filler-only transcripts are rejected as noise.
- Known Whisper hallucinations on silent audio are filtered.
- TV/radio background patterns are rejected.

## Non-Goals for v0.0.1

- Fully autonomous end-to-end action chains.
- Unreviewed background modifications.
- Multi-user synchronization semantics.
