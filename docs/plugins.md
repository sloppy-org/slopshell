# Tabura Plugin Boundaries

This document defines what belongs in the Tabura core runtime vs plugin space.

## Core Runtime (Non-Plugin)

These concerns stay in this repository and are not delegated to plugins:

- Auth/session/cookie enforcement and API access control.
- Chat turn queueing, cancellation, websocket transport, and persistence.
- STT/TTS transport primitives and media validation.
- Privacy invariants for meeting notes (RAM-only audio, no audio persistence).
- Canvas/file safety boundaries and path constraints.

Reason: these are correctness, security, and reliability guarantees.

## Plugin Space

Plugins should own product-specific decision logic and capability modules.

## Primary Plugin Target: `meeting-partner`

`meeting-partner` is the intended plugin domain for:

- Always-listen behavior policy in meeting mode.
- Directed speech detection and response gating.
- Intelligent response strategy from transcript/event context.
- Optional room memory/entity timeline behavior.

This aligns with meeting-notes assistant-intelligence scope and keeps transcript
pipeline/privacy guarantees in core.

## Issue Mapping

Plugin-oriented scope:

- `#106` DDSD gate from transcript context
- `#108` assistant response execution
- `#109` interaction policies
- `#111` room memory/entity timeline

Core runtime scope:

- `#102` transcript/event schema
- `#103` in-memory capture buffers
- `#105` meeting-notes core pipeline
- `#110` UI state sync for command-driven sessions
- `#113` config API and invariants
- `#114` transcript API/viewer
- `#116` intent command entrypoints (protocol wiring)
- `#117`, `#118` privacy contract and enforcement
- `#119` launch tracker
- `#121` local PTT daemon runtime

## Repository Split

- `tabura` keeps runtime substrate and guarantees.
- `tabura-plugins` (private) owns premium/product plugin implementations.

This split allows rapid feature evolution without weakening core guarantees.
