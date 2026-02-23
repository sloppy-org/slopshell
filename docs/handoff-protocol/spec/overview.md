# Handoff Protocol v1 Overview

## Goals

- Decouple producer/consumer from shared filesystem assumptions.
- Keep payload bytes out of LLM prompt/tool argument context.
- Provide a typed and versioned envelope for interoperability.

## Roles

- Producer: creates and serves handoff payloads.
- Consumer: imports payload and renders/uses it.

## Required producer tools

- `handoff.create`
- `handoff.peek`
- `handoff.consume`
- `handoff.revoke`
- `handoff.status`

## Required envelope fields

- `spec_version` (example: `handoff.v1`)
- `handoff_id`
- `kind`
- `created_at` (RFC3339)
- `meta` (kind metadata)
- `payload` (kind payload)

## Policy model

- TTL (`expires_at` or `ttl_seconds` at create-time)
- Consume limit (`max_consumes`)
- Counters (`consumed_count`, `remaining_consumes`)
- Optional revocation (`revoked`)
