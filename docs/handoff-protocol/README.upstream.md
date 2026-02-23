# handoff-protocol

Versioned, producer-neutral handoff protocol for transferring typed payloads (for example files) between MCP services without routing payload bytes through model context.

## v1 Scope

- Generic lifecycle: `handoff.create`, `handoff.peek`, `handoff.consume`, `handoff.revoke`, `handoff.status`
- Kind contracts: `file`
- One-time or bounded-consume handoffs with TTL
- Integrity metadata for file handoffs

## Profiles

- Handoff envelope and kinds: `spec/overview.md` + `schemas/*kind*.json`
