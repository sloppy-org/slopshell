# Meeting-Partner Mode Whitepaper

Public disclosure date: March 1, 2026 (UTC)

Repository: `https://github.com/krystophny/tabura`

License context: repository-level MIT license.

## Abstract

This whitepaper describes a local-first "meeting-partner mode" for continuous,
meeting-context assistance: always-listen behavior, transcript-first reasoning,
directed-speech detection, selective interventions, and explicit privacy
constraints. The architecture is designed so that critical runtime guarantees
remain in core, while behavioral policy is plugin-controlled.

This publication is intended as technical prior art for the described
architecture, interfaces, and operational patterns.

## Problem Statement

Meeting assistance systems tend to fail in one of three ways:

1. They respond too often (interruptive, noisy).
2. They respond too late (context missed).
3. They compromise privacy by persisting raw audio.

Meeting-partner mode addresses these with:

- Transcript/event-driven decisioning.
- Directed-speech and interaction gating.
- RAM-only audio handling with text-only persistence.

## Scope and Non-Goals

In scope:

- Always-listen loop in meeting mode.
- Detection of "assistant-addressed" utterances from transcript context.
- Selective response/action decisions.
- Barge-in, cooldown, and anti-overtrigger policy.
- Room memory summary/entity timeline from text metadata.

Out of scope:

- Persistent audio storage.
- Always-on autonomous execution without explicit policy gates.
- Replacement of core transport/security invariants by plugins.

## Conceptual Model

Meeting-partner mode is modeled as three layers:

1. Capture and transcript substrate (core runtime).
2. Policy and intelligence layer (plugin).
3. Response/action actuation layer (core + plugin outputs).

Key principle:

- Core owns reliability and safety guarantees.
- Plugin owns product behavior decisions.

## Terminology

- Segment: finalized transcript chunk over a bounded time window.
- Session: meeting lifecycle interval (`start -> active -> stop`).
- DDSD: directed-speech/directed-dialogue gate determining if the assistant was addressed.
- Intervention: assistant response or structured action emitted by policy.

## Reference Architecture

### Core Runtime Responsibilities

- Audio ingress, buffering, VAD, STT transport.
- Session lifecycle and websocket state.
- Text/event persistence and query APIs.
- Auth/session boundaries and path safety.
- Privacy invariants (no persisted audio bytes).

### Plugin Responsibilities (`meeting-partner`)

- Evaluate segment stream and session state.
- Maintain policy state (cooldowns, confidence thresholds).
- Produce one of:
  - `noop`
  - `respond`
  - `action`

## Event Pipeline

Input events:

1. `meeting_partner.session_state`
2. `meeting_partner.segment_finalized`
3. `meeting_partner.decide`

Recommended event envelope:

```json
{
  "session_id": "s1",
  "project_key": "p1",
  "timestamp": "2026-03-01T00:00:00Z",
  "hook": "meeting_partner.segment_finalized",
  "text": "can you summarize the risks so far",
  "metadata": {
    "speaker": "participant_a",
    "lang": "en",
    "segment_start_ms": 15000,
    "segment_end_ms": 19800
  }
}
```

## Decision Contract

Allowed decisions:

1. `noop`
2. `respond`
3. `action`

Example response decision:

```json
{
  "meeting_partner": {
    "decision": "respond",
    "response_text": "Current top risks are timeline slip and procurement delay.",
    "channel": "voice",
    "urgency": "normal"
  }
}
```

Example action decision:

```json
{
  "decision": "action",
  "action": {
    "type": "create_task",
    "title": "Validate procurement lead times by Friday"
  }
}
```

## Directed-Speech Gating

A non-exclusive gating strategy:

1. Lexical cues:
  - "Tabura", "assistant", wake keywords.
2. Intent cues:
  - imperative request patterns.
3. Context cues:
  - follow-up within active dialogue window.
4. Confidence threshold:
  - only intervene if confidence >= threshold.

Pseudo-code:

```text
if explicit_addressed(segment):
  score += w_explicit
if imperative_request(segment):
  score += w_intent
if followup_window_active():
  score += w_context
if score < threshold:
  return noop
return respond_or_action
```

## Interaction Policy

Recommended controls:

1. Cooldown window after each intervention.
2. Max interventions per time window.
3. Barge-in suppression while user actively speaking.
4. Confidence hysteresis to reduce oscillation.

Pseudo-code:

```text
if within_cooldown(last_intervention_at):
  return noop
if speaking_now():
  return noop
if interventions_last_5m >= max_budget:
  return noop
return candidate_decision
```

## Room Memory and Entity Timeline

Room memory can be maintained from transcript/event metadata only:

- active topics
- open questions
- commitments/actions
- decision log

No audio-derived artifacts are required for this layer.

## Privacy and Safety Constraints

Required constraints:

1. Audio in RAM only.
2. No persisted raw/encoded audio in DB, disk, logs, exports.
3. No audio fingerprints enabling reconstruction.
4. Explicit cleanup on stop and error paths.

Meeting-partner policy consumes transcript/event data, not persisted audio.

## Performance Considerations

Targets for responsive behavior:

- Segment-to-decision latency: sub-200ms budget for local policy path.
- Deterministic fallback if plugin unavailable: `noop`.
- Bounded plugin timeout with non-fatal failure handling.

## Failure Semantics

When plugin call fails:

1. Log plugin error.
2. Preserve core session/transcript flow.
3. Emit safe default (`noop`) unless explicit fail-closed policy is configured.

## Security Model

- Plugins are server-side integrations.
- Browser clients do not call plugin endpoints directly.
- Existing API auth/session checks remain authoritative.

## Deployment Topology

Reference topology:

1. `tabura` core runtime on loopback or trusted host.
2. `meeting-partner` plugin service on loopback/internal network.
3. Manifest-based registration in `TABURA_PLUGINS_DIR`.

## Implementation Notes in Tabura

Public runtime disclosure includes:

- plugin manager with webhook manifests
- chat hook extension points
- meeting-partner decision hook and debug endpoint

Private implementation packages can live in `tabura-plugins` repository while
targeting the public contracts documented here.

## Prior Art Positioning

This document publicly discloses the following technical elements:

1. Transcript-first always-listen meeting assistant architecture.
2. Separation of core safety substrate and plugin policy layer.
3. Directed-speech gating with cooldown/budgeted interventions.
4. Structured decision contract (`noop/respond/action`) for meeting assistant behavior.
5. RAM-only audio + text-only persistence constraints in the same system design.

## Revision Policy

Future revisions should:

- Append dated change notes.
- Preserve prior versions in git history.
- Keep contracts explicit and machine-testable.
