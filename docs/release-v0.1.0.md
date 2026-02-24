# Release v0.1.0

> Historical note: This file documents a past release. For the current runtime stack, use README.md and docs/architecture.md.

## Scope

`v0.1.0` is the first minor pre-release milestone for Tabura. It consolidates the web+codex+piper runtime baseline and improves voice turn-end reliability with frontend VAD auto-stop behavior.

## Highlights

### Runtime Baseline Consolidation

- Simplified and hardened startup behavior for the web runtime and user service setup.
- Clarified canonical runtime model around:
  - `tabura-web.service`
  - `tabura-codex-app-server.service`
  - `tabura-piper-tts.service`
- Updated top-level docs and architecture references to reflect the consolidated baseline.

### Voice EOU and VAD Improvements

- Added frontend VAD-driven auto-stop path for push-to-talk voice capture.
- Improved no-speech and ambient-noise handling to reduce false turn endings.
- Tightened speech gating and cancellation paths so voice turns stop more predictably.
- Added mobile close control for pinned right-edge panel behavior.

### UX and Interaction Stability

- Improved stop/record lifecycle handling across voice interactions.
- Refined canvas/chat behavior consistency during active voice and assistant turns.

## Traceability

For publication metadata, associate this release with:

- release label: `v0.1.0`
- repository: `https://github.com/krystophny/tabura`
- exact source revision: tag target commit hash
