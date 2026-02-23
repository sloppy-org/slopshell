# Helpy Recovery Issue Pack

This document tracks functionality intentionally removed from Tabura during monolith simplification and meant to be reintroduced in the private Helpy repo.

## Reference Commits in Tabura

- `d7c3ef7`: import Helpy email-header handoffs into canvas via MCP
- `15e03c2`: deterministic mail action UI/action flow foundation
- `7218a10`: Helpy STT integration path for voice draft reply

## Issues to Create in Helpy

1. Rebuild email header handoff producer for file-first Tabura
- Goal: expose Helpy-side `handoff.create/peek/consume` for email header collections as producer capability.
- Include: provider/folder selector, bounded payload policy, envelope validation.
- Reference commits: `d7c3ef7`, `15e03c2`.

2. Rebuild deterministic message action API profile in Helpy
- Goal: provide action capabilities and deterministic operations (`open/archive/delete/defer`) entirely in Helpy.
- Include: explicit provider mode (`native`/`stub`), action audit trail, error taxonomy.
- Reference commits: `15e03c2`.

3. Rebuild draft intent + draft reply pipeline in Helpy
- Goal: implement transcript intent classification and draft reply generation in Helpy-side services.
- Include: explicit fallback policy behavior and deterministic contracts.
- Reference commits: `15e03c2`, `7218a10`.

4. Rebuild speech-to-text ingestion for mail drafting in Helpy
- Goal: move voice draft STT ownership to Helpy with clear interface contracts.
- Include: audio payload limits, MIME validation, transcript quality checks, error handling.
- Reference commits: `7218a10`.
