# Lifecycle

## handoff.create

Input:
- `kind`: `file`
- `selector`: kind-specific source selection
- `policy` (optional): `ttl_seconds`, `expires_at`, `max_consumes`

Output:
- `spec_version`, `handoff_id`, `kind`, `meta`, `created_at`, `policy_summary`

## handoff.peek

Input:
- `handoff_id`

Output:
- Same as create metadata, no payload.

## handoff.consume

Input:
- `handoff_id`

Output:
- `spec_version`, `handoff_id`, `kind`, `created_at`, `meta`, `payload`, `policy`

## handoff.revoke

Input:
- `handoff_id`

Output:
- Revocation acknowledgement + policy summary.

## handoff.status

Input:
- `handoff_id`

Output:
- Metadata + policy counters + revocation state.
