# Interfaces

This document summarizes externally relevant interfaces in `v0.0.5`.

## MCP HTTP Daemon

Routes in `internal/serve/app.go`:
- `POST /mcp`
- `GET /mcp`
- `DELETE /mcp`
- `GET /ws/canvas`

## Web Runtime HTTP APIs

Auth and setup:
- `GET /api/setup`
- `POST /api/setup`
- `POST /api/login`
- `POST /api/logout`

Runtime and chat session management:
- `GET /api/runtime`
- `POST /api/chat/sessions`
- `GET /api/chat/sessions/{session_id}/history`
- `POST /api/chat/sessions/{session_id}/messages`
- `POST /api/chat/sessions/{session_id}/commands`

Canvas/files:
- `GET /api/canvas/{session_id}/snapshot`
- `POST /api/canvas/{session_id}/commit`
- `GET /api/files/{session_id}/*`

Mail interaction endpoints:
- `POST /api/mail/action-capabilities`
- `POST /api/mail/read`
- `POST /api/mail/mark-read`
- `POST /api/mail/action`
- `POST /api/mail/draft-reply`
- `POST /api/mail/draft-intent`
- `POST /api/mail/stt`
- `POST /api/stt/push-to-prompt`

Websocket routes:
- `GET /ws/chat/{session_id}`
- `GET /ws/canvas/{session_id}`

## MCP Tool Surface

Defined in `internal/mcp/server.go`:
- `canvas_session_open`
- `canvas_artifact_show`
- `canvas_mark_set`
- `canvas_mark_delete`
- `canvas_marks_list`
- `canvas_mark_focus`
- `canvas_commit`
- `canvas_status`
- `canvas_import_handoff`

## Reply Intent Contract

`POST /api/mail/draft-intent` returns classification metadata including:
- `intent` (`prompt` or `dictation`)
- `reason`
- `fallback_applied`
- `fallback_policy`

`POST /api/mail/draft-reply` returns generated or prepared draft text for explicit user review.

## Stability Statement

`v0.0.5` is pre-stable; interfaces may evolve. Breaking changes are documented in release notes.
