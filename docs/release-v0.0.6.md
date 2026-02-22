# Release v0.0.6

## Scope

`v0.0.6` removes daemon-backed server-side microphone capture from the VoxType MCP bridge. All speech-to-text now uses browser audio exclusively.

## Highlights

- Removed daemon capture mode from VoxType MCP server; browser-buffered audio is the sole capture path.
- Deleted `voxtype record start/stop/cancel` integration (server-side microphone capture was a security risk and did not work for remote browsers).
- Simplified `sessionState` struct and all tool handlers (`start`, `append`, `stop`, `cancel`, `health`).
- Removed `voxtype.service` dependency from `tabura-voxtype-mcp.service` systemd unit.
- Removed `TABURA_VOXTYPE_MCP_CAPTURE_MODE` environment variable.
- Removed daemon early-return branches from chat, mail, and review voice capture flows in the web UI.
- Bumped MCP server version to `0.0.6`.

## Migration

- The `capture_mode` parameter on `push_to_prompt_start` is no longer accepted.
- The `capture_backend` field is no longer present in tool responses.
- The `voxtype_daemon` dependency is no longer reported by `push_to_prompt_health`.
- The `voxtype.service` system daemon is no longer required for Push To Prompt.

## Traceability

For publication metadata, associate this release with:

- release label: `v0.0.6`
- repository: `https://github.com/krystophny/tabura`
- exact source revision: tag target commit hash
