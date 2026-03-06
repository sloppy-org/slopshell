# Legacy Extension Runtime And Future Capability Boundary

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

This document used to define Tabura's extension/plugin runtime and the split
between public core and private capability bundles.

That bundle-oriented product direction is no longer active, but the remaining
runtime surfaces are not all automatically mistakes. Some narrow compatibility
or interop APIs may remain if they support the public-core product and local
capability providers such as Helpy.

## Current Direction

- No private `tabura-plugins` repo as a product dependency
- No extension/plugin bundle system as the preferred way to add behavior
- New product work belongs in the public `krystophny/tabura` repo under normal
  modular packages in `internal/`
- Any retained integration boundary should be narrow, local-first, and
  capability-oriented rather than a general marketplace or bundle SDK

## What This Means

- Core UI stays in core UI code
- Meeting-notes behavior stays in core meeting-notes code
- Privacy and safety invariants stay in core
- Plugin manifests stay on the legacy `*.json` path; extension manifests stay on
  `*.extension.json`, and the plugin loader ignores extension manifests so the
  compatibility boundary stays explicit
- Any remaining `internal/extensions` and `internal/plugins` code should be
  treated as transitional compatibility or interop code, not an expanding
  platform surface
- If a runtime API remains, it should justify itself in terms of one of:
  - backwards compatibility during cleanup
  - local capability-provider interop
  - deterministic testing of external adapters

## Preferred External Integration Shape

When Tabura integrates optional external capabilities in the future, prefer:

- local MCP servers
- explicit HTTP sidecars on loopback
- handoff-protocol based artifact exchange
- small purpose-built contracts for deterministic actions

Avoid:

- private bundle ecosystems
- manifest marketplaces
- hidden feature ownership split across repos
- wide generic plugin hooks for core behavior

## Helpy Implication

Helpy is a plausible future local capability provider for:

- email and calendar actions
- ICS feeds
- spreadsheet reads
- handoff-based artifact exchange
- operator status/assistant orchestration helpers

Tabura should therefore preserve or revise only the minimum API needed to call
such local capabilities cleanly. That is different from preserving an
extension-platform product.

## Planning References

- Architecture simplification tracker: `#128`
- Repo cleanup and compatibility contraction: `#139`
- Meeting-notes post-MVP public follow-up: `#129`, `#130`, `#131`, `#132`

## Historical Note

Old release notes and historical documents may still mention extension/plugin
runtime details because they recorded what existed at that time.
