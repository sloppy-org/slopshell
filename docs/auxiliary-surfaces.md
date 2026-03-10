# Auxiliary Surfaces

The main Tabura environment is `/`. Auxiliary surfaces are allowed only when they are narrower than the main workspace, materially faster for one job, and write back into the same Workspace / Artifact / Item / Actor model.

## Allowed exceptions

### `/capture`

- Purpose: fast mobile or transient capture when opening the full workspace would add friction.
- Boundary: it is capture-only. It does not expose chat state, workspace switching, or a second workflow shell.
- Canonical write-back: it creates an `idea_note` artifact through `POST /api/artifacts` and then creates an inbox item through `POST /api/items`.
- Canonical semantics: capture is still "make an artifact, create an item", not a separate product universe.

### `GET /api/items/{item_id}/print`

- Purpose: render a read-only print packet for an existing item and its linked artifact.
- Boundary: it does not create or mutate alternate state and does not introduce a separate editor.
- Canonical read-back: the page is derived from the current item, workspace, actor, and artifact records.
- Canonical semantics: printing is a presentation of the existing ontology, not a second item system.

## Non-exceptions

- `/canvas` is not a second surface. It redirects into the main workspace shell.
- No additional standalone page, panel, or sheet should introduce a parallel ontology or custom action grammar.
