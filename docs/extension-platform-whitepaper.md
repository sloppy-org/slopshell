# Tabura Extension Platform Whitepaper

Public disclosure date: March 1, 2026 (UTC)

Repository: `https://github.com/krystophny/tabura`

## Abstract

This document discloses a local-first extension platform for Tabura with
out-of-process execution, capability permissions, typed event hooks, and
command routing. It is published as technical prior art for extension-based
meeting-partner and operational assistant systems.

## Design Goals

1. Keep security-critical runtime guarantees in core.
2. Move product behavior and vertical workflows into extension bundles.
3. Support deterministic, auditable automation with bounded latency.
4. Enable private premium bundles without forking core runtime.

## Runtime Model

- Core process: Tabura web/runtime and extension host.
- Extension process: webhook service per extension bundle.
- Contract transport: authenticated HTTP webhook calls from core to extension.
- Execution scope: hook mutation/blocking, meeting decisions, command dispatch.

## Extension Contracts

Manifest capabilities include:

- Hook subscriptions (`chat.*`, `meeting_partner.*`).
- Permission declarations (`hook.*`, `meeting_partner.*`, `command.execute`, `ui.contribute`).
- Command registrations (`/api/extensions/commands/{command_id}`).
- Engine constraints (`engine.tabura`, exact or minimum).
- Signing metadata for publisher identity.

## Security And Isolation

1. Browser clients do not call extension endpoints directly.
2. Core remains auth/session authority.
3. Extension calls are bounded by timeouts and response size limits.
4. Optional bearer forwarding through `secret_env`.
5. Permission checks are enforced before hook/command execution.

## Meeting Partner As Extension Bundle

Meeting-partner logic is implemented as one extension bundle with:

- Directed-speech gating.
- Cooldown/intervention budget policy.
- Structured decisions (`noop`, `respond`, `action`).
- Command surface for explicit user-triggered interactions.

## Helpy Capability Bundles

Future Tabura extension bundles can expose Helpy features as policy modules:

- Email triage/actions (archive/delete/defer/mark/read/unread).
- Calendar synchronization and scheduling workflows.
- ICS ingestion and event-normalization actions.
- Handoff protocol for deterministic external actions.

These capabilities are extension-level business logic. Core Tabura remains
transport/auth/session/event substrate.

## Prior Art Statement

This publication discloses:

1. An out-of-process extension host integrated into a local-first AI runtime.
2. Permissioned hook and command contracts for conversational systems.
3. Meeting-partner decisioning as an extension bundle over typed events.
4. Integration of productivity connectors (email/calendar/ICS/handoff) via
   extension bundles without modifying core safety substrate.

## Revision Policy

- Keep dated revisions in git history.
- Preserve API/manifest examples in docs/plugins.md.
- Track extension SDK compatibility by Tabura release version.
