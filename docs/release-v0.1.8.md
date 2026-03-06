# Release v0.1.8

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

## Scope

`v0.1.8` publishes Companion Mode as the active unified assistant surface, closes the remaining roadmap and cleanup work, and aligns runtime/version metadata with the shipped reality.

## Highlights

### Companion Mode Now Defines the Core Runtime

- Companion Mode is now the active public surface for live meetings, 1:1 conversations, and workday assistance.
- Temporary projects, transcript context, room memory, runtime policy control, and single-run orchestration are part of the shipped core path.
- The runtime state machine, websocket protocol, and visible-state safeguards now match the documented product direction.

### Privacy, Consent, and Interaction Hardening

- Directed-speech gating, interruption policy handling, and consent/privacy safeguards were completed as part of the Companion Mode cut.
- Meeting-notes privacy constraints remain RAM-only for audio payloads, with enforcement tests kept in the main runtime.
- Public docs now describe retained local boundaries as shipped contracts rather than roadmap intent.

### Release Tooling and Documentation Alignment

- Version bump tooling now updates every live runtime version surface, including the CLI binary version and extension runtime version.
- Version consistency checks now fail if any shipped runtime surface drifts from the published release metadata.
- The spec hub and README now point at the current release note for `v0.1.8`.

## Verification Scope

This release is intended to be verified against:

- `scripts/check-version-consistency.sh`
- `./scripts/sync-surface.sh --check`
- `go test ./...`
- `./scripts/playwright.sh`
- `./scripts/e2e-local.sh`

## Traceability

For publication metadata, associate this release with:

- release label: `v0.1.8`
- repository: `https://github.com/krystophny/tabura`
- exact source revision: tag target commit hash
