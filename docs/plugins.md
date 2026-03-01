# Tabura Plugin System

This document defines plugin scope, contracts, and runtime boundaries.

## Core vs Plugin

Core (non-plugin):

- Auth/session enforcement and API authorization.
- Chat queue/cancellation/ws/persistence guarantees.
- STT/TTS transport and media validation.
- Meeting-notes privacy invariants (RAM-only audio, no audio persistence).
- Canvas/file safety boundaries.

Plugin scope:

- Product decision logic and capability modules.
- Meeting-partner behavior (always-listen policy, directed speech gating, intelligent responses).

## Loading and Inventory

- Manifest directory: `TABURA_PLUGINS_DIR`
  - Default: `<data-dir>/plugins`
  - Disable: `TABURA_PLUGINS_DIR=off`
- Runtime inventory:
  - `GET /api/runtime` -> `plugins_dir`, `plugins_loaded`
  - `GET /api/plugins` -> loaded plugin metadata

## Manifest

```json
{
  "id": "meeting-partner",
  "kind": "webhook",
  "endpoint": "http://127.0.0.1:9901/hooks",
  "hooks": [
    "chat.pre_user_message",
    "chat.pre_assistant_prompt",
    "chat.post_assistant_response",
    "meeting_partner.session_state",
    "meeting_partner.segment_finalized",
    "meeting_partner.decide"
  ],
  "timeout_ms": 1200,
  "enabled": true,
  "secret_env": "TABURA_PLUGIN_SECRET"
}
```

Notes:

- Only `kind=webhook` is supported.
- Timeout is capped at `30000ms`.
- If `secret_env` resolves, Tabura sends `Authorization: Bearer <secret>`.

## Hook Request Contract

Tabura sends:

```json
{
  "hook": "meeting_partner.decide",
  "session_id": "chat-session-id",
  "project_key": "project-key",
  "output_mode": "voice",
  "text": "latest transcript chunk or prompt",
  "metadata": {
    "source": "meeting_notes",
    "speaker": "user"
  }
}
```

## Response Contract

Supported response shapes:

1. Text mutation/blocking for chat hooks:

```json
{
  "text": "rewritten text",
  "blocked": false,
  "reason": ""
}
```

2. Meeting-partner decision (nested or top-level):

```json
{
  "meeting_partner": {
    "decision": "respond",
    "response_text": "Here is the summary.",
    "channel": "voice",
    "urgency": "normal"
  }
}
```

or

```json
{
  "decision": "action",
  "action": {
    "type": "create_task",
    "title": "Follow up with legal"
  }
}
```

Allowed meeting-partner `decision` values:

- `noop`
- `respond`
- `action`

## Built-in Hook Points

Chat hooks:

- `chat.pre_user_message`
- `chat.pre_assistant_prompt`
- `chat.post_assistant_response`

Meeting-partner hooks:

- `meeting_partner.session_state`
- `meeting_partner.segment_finalized`
- `meeting_partner.decide`

## Debug Endpoint for Meeting Partner

`POST /api/plugins/meeting-partner/decide`

Request:

```json
{
  "session_id": "s1",
  "project_key": "p1",
  "text": "Could you summarize that?",
  "metadata": { "source": "meeting_notes" }
}
```

Response:

```json
{
  "ok": true,
  "matched": true,
  "decision": {
    "decision": "respond",
    "response_text": "Let me summarize.",
    "channel": "voice",
    "urgency": "normal",
    "plugin_id": "meeting-partner"
  }
}
```

If no plugin returns a decision, Tabura returns `decision=noop` with `matched=false`.

## Repository Split

- `tabura` keeps runtime substrate and invariants.
- `tabura-plugins` (private) contains premium/plugin implementations.
