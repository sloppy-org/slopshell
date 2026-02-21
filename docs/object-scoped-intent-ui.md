# Object-Scoped Intent UI

## Purpose

Define a calm, document-first AI interaction model optimized for high-latency and e-ink style environments.

Core constraints:
- No persistent chat panel.
- No hidden autonomous actions.
- Invocation is local to selected object.
- User approval is required before applied changes.

## Invocation Primitive

Long press is the primary invoke action on an object.
Desktop equivalents: long left-click hold at a target point, or hold `Ctrl` at the current cursor/anchor point for Push To Prompt.  
Right-click opens the text comment box mode.

Mode-dependent behavior:
- Voice mode (Push To Prompt): capture spoken intent.
- Silent mode: open compact inline prompt.
- Comment mode: capture annotation text/speech as draft mark.

No global assistant console is required for object-level operations.

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
1. User selects target object.
2. System captures intent/comment in current mode.
3. System creates draft mark attached to target.
4. Mark remains editable until commit/reject.

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

## Non-Goals for v0.0.1

- Fully autonomous end-to-end action chains.
- Unreviewed background modifications.
- Multi-user synchronization semantics.
