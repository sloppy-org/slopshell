# Security Model

## Defaults

- `max_consumes = 1`
- TTL should be short (recommended: 10 minutes)
- Producer returns typed payloads only to authenticated consumers

## Guidance

- Use TLS for producer endpoints.
- Bind tokens or credentials to consumer identity when possible.
- Audit create/peek/consume/revoke operations.
- For `file` handoffs, validate `sha256` and `size_bytes` on consume.

## Out of scope for v1

- Brokered object storage fallback
- End-to-end encrypted multi-hop payload wrapping
