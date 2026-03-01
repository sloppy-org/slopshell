# Tabura Extension System

This document defines the extension runtime, contracts, and migration boundary
from legacy plugins.

## Core vs Extension

Core (public Tabura):

- Auth/session enforcement and API authorization.
- Chat queueing/cancellation/websocket/persistence guarantees.
- STT/TTS transport and media validation.
- Meeting-notes privacy invariants (RAM-only audio, no audio persistence).
- Canvas/file safety boundaries.
- Extension host lifecycle, permission enforcement, and command routing.

Extension scope (private/public bundles):

- Product decision logic and capability modules.
- Meeting-partner behavior (always-listen policy, directed speech gating, intelligent responses).
- Vertical integrations (email/calendar/task systems, Helpy connectors, organization workflows).

## Runtime Directories

Extension manifests:

- `TABURA_EXTENSIONS_DIR`
  - Default: `<data-dir>/extensions`
  - Disable: `TABURA_EXTENSIONS_DIR=off`

Legacy plugin manifests (compatibility):

- `TABURA_PLUGINS_DIR`
  - Default: `<data-dir>/plugins`
  - Disable: `TABURA_PLUGINS_DIR=off`

## Runtime Inventory APIs

- `GET /api/runtime` -> `extensions_dir`, `extensions_loaded`, `plugins_dir`, `plugins_loaded`
- `GET /api/extensions` -> loaded extension metadata
- `GET /api/plugins` -> loaded legacy plugin metadata

## Extension Manifest (`*.extension.json`)

```json
{
  "id": "meeting-partner",
  "display_name": "Meeting Partner",
  "version": "1.0.0",
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
  "permissions": [
    "hook.chat.pre_user_message",
    "hook.chat.pre_assistant_prompt",
    "hook.chat.post_assistant_response",
    "meeting_partner.decide"
  ],
  "commands": [
    {
      "id": "meeting_partner.respond",
      "title": "Respond",
      "description": "Emit a guided response",
      "hook": "extension.command",
      "permission": "command.execute"
    }
  ],
  "ui_contributions": [
    {
      "id": "meeting_panel",
      "slot": "right.sidebar",
      "title": "Meeting Partner"
    }
  ],
  "engine": {
    "tabura": ">=0.1.6"
  },
  "signing": {
    "publisher": "acme-labs"
  },
  "timeout_ms": 1200,
  "enabled": true,
  "secret_env": "TABURA_EXTENSION_SECRET"
}
```

Notes:

- Only `kind=webhook` is currently supported.
- Timeout is capped at `30000ms`.
- If `secret_env` resolves, Tabura sends `Authorization: Bearer <secret>`.
- `engine.tabura` supports exact (`0.1.6`) or minimum (`>=0.1.0`) constraints.

## Hook Contract

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

Webhook response supports:

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

Allowed meeting-partner decision values:

- `noop`
- `respond`
- `action`

## Command Execution Contract

Execute extension command:

- `POST /api/extensions/commands/{command_id}`

Request:

```json
{
  "session_id": "s1",
  "project_key": "p1",
  "output_mode": "voice",
  "text": "optional command context",
  "args": {
    "urgency": "normal"
  },
  "metadata": {
    "source": "ui"
  }
}
```

Response:

```json
{
  "ok": true,
  "result": {
    "command_id": "meeting_partner.respond",
    "extension_id": "meeting-partner",
    "success": true,
    "message": "executed"
  }
}
```

## Built-in Hooks

Chat hooks:

- `chat.pre_user_message`
- `chat.pre_assistant_prompt`
- `chat.post_assistant_response`

Meeting-partner hooks:

- `meeting_partner.session_state`
- `meeting_partner.segment_finalized`
- `meeting_partner.decide`

Extension command hook:

- `extension.command`

## Meeting Partner Debug Endpoint

`POST /api/plugins/meeting-partner/decide`

This endpoint checks extension hooks first, then legacy plugins.

## Repository Split

- `tabura` keeps runtime substrate and extension host contracts.
- `tabura-plugins` (private) contains premium extension bundles (meeting partner, always-on workflows, automations).
- `helpy` provides external capability services that can be exposed as Tabura extension bundles.
