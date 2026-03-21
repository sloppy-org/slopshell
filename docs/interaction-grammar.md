# Interaction Grammar

This document is the canonical product reference for Tabura's interaction model.

If code, UI copy, storage, tests, or follow-on design notes disagree with this document, the implementation is wrong and should be changed. The document is not a compatibility promise for legacy shapes.

## Authoritative Ontology

Tabura has exactly five primary product nouns:

- **Workspace** — a real directory the user works in. Always filesystem-grounded. Composed via symlinks and environment variables, not virtual abstractions. Thin record: path, name, active flag, chat config. One workspace = one chat session (compatible with Codex CLI model). Archivable as a self-contained unit.
- **Artifact** — curated content shown on canvas. Not every file is an artifact; artifacts are created lazily when interacted with, synced from external systems, or explicitly captured. Has kind, file path or URL, and metadata. Non-file artifacts (email, issue) can be materialized as real files in a workspace.
- **Item** — an open loop requiring attention. Tracked in the inbox. Has state (inbox/waiting/someday/done), an optional artifact, an optional actor, and a workspace where it is tracked. Items do not always have artifacts; bare tasks like "call Bob" are items without artifacts.
- **Actor** — a human or agent responsible for progress.
- **Label** — a hierarchical organizing label. Attached to items, artifacts, and workspaces. The only cross-cutting grouping mechanism. Workspace labels cascade: querying a label includes items and artifacts in workspaces carrying that label. Querying a parent label includes everything under it.

Project is not a product concept. Session and message are transport or storage details, not user-facing ontology.

### Key invariants

1. The canvas shows artifacts, always. Bare items (no artifact) show in the sidebar only.
2. An item may have zero or more artifacts with roles (source, related, output). One artifact is designated primary and shown on canvas. Bare tasks (no artifact) show in sidebar only.
3. A workspace is a directory. No virtual workspaces. Composition via filesystem tools. The default fallback is today's persistent Daily Workspace.
4. Labels are the only cross-cutting grouping mechanism. Project is not a product concept.
5. Work and private are top-level labels, not a separate "sphere" field. They follow the same rules as all other labels.
6. Intent classification is workspace-independent. Execution is workspace-aware.

### Label hierarchy and filtering

Labels form a tree. Examples:

```
work/
  w7x/
  DEMO-2025/
    EURATOM
  tabura/
  plasma-codes/
private/
  health/
  family/
important
urgent
```

Filtering by a parent label includes everything under it. Filters combine: work/w7x + urgent = urgent W7X items. Filtering works globally across all nouns — items, artifacts, workspaces.

### Time tracking

Time accrues to ALL labels on whatever you interact with, including ancestor labels. Working on an item labeled work/w7x credits time to both work/w7x and work. If the item also carries urgent, time credits to that too. Filtering time by "work" gives total work time across all sub-labels. Filtering by "urgent" gives total time on urgent things regardless of topic. No explicit time-tracking activation needed — the system tracks what you touch.

### Triage and assignment

Labels auto-assigned from external container mappings (configured once per source). Workspace assignment is always manual. Most items float in inbox with labels only, no workspace.

Triage flow:

1. External sync → item (inbox) + artifact + auto-labels from container mappings
2. User sees item in inbox with labels already set
3. Triage options:
   - Reply and mark done (no workspace needed, labels sufficient)
   - Assign to workspace (explicit: "track this in ~/write/DEMO-2025")
   - Add more labels
   - Materialize artifact into workspace (explicit archival)
   - Delegate to actor

### Workspace composition

Workspaces are composed from multiple directories using filesystem tools:

- **Symlinks** compose workspaces from multiple directories. A paper workspace at `~/write/DEMO-2025/` symlinks to `~/data/aug-campaign/` and `~/code/analysis/`.
- **Path containment**: artifacts with ref_path inside the workspace directory are auto-associated.
- **Explicit links**: workspace_artifact_links for cross-directory references that cannot be expressed as symlinks.
- **Environment variables** ($CODE, $DATA) for scripts in composed workspaces, so paths remain portable across machines.

### Materialization

Non-file artifacts linked to a workspace can be materialized as real files: email to .eml, GitHub issue to .md, external task to .md, calendar event to .ics. This is an explicit archival action, not automatic. The user decides when to persist a non-file artifact as a real file in a workspace directory.

Use cases:

- **Scientific archival**: paper workspace → materialize all non-file artifacts → archive → upload to Zenodo → DOI. The workspace IS the research compendium.
- **Compliance**: admin workspace → materialize emails, decisions, financials → archive for audit.
- **Reproducibility**: the archived workspace is self-contained — data, code, narrative, correspondence.

### Concrete mapping

How real entities map to the five nouns:

| Entity | Workspace | Artifact | Item | Labels |
|---|---|---|---|---|
| Scientific data (`~/data/`) | One workspace (whole git-lfs repo) | Files become artifacts when opened | Items when action needed | work/w7x, work/DEMO-2025 |
| Code workspace (`~/code/tabula/`) | One workspace per repo | GitHub issues/PRs synced as artifacts | Issues/PRs are items tracked here | work/tabura, work/plasma-codes |
| Paper (`~/write/DEMO-2025/`) | Composed workspace (symlinks to data+code) | Paper draft, figures, linked data | "Redo figure 3", "address reviewer 2" | work/DEMO-2025, work/DEMO-2025/EURATOM |
| Documentation (`~/Nextcloud/plasma/DOCUMENTS/`) | One workspace | Reports, slides when opened | Rarely — mostly reference | work, per-topic |
| Management (`~/Nextcloud/plasma_orga/`) | One workspace | Budgets, contracts, receipts | "Process invoice", "submit claim" | work, work/budget, work/personnel |
| Email (Exchange work) | No workspace by default. Assigned manually during triage. | Email body+metadata. Materializable as .eml. | Inbox item. | work + auto from folder mappings |
| Email (personal Gmail) | No workspace by default. | Email body+metadata. Materializable as .eml. | Inbox item. | private + auto from folder mappings |
| Tasks (Todoist) | Assigned manually or left floating | Often bare (no artifact) | The item IS the task | Auto from Todoist workspace mappings |
| Calendar (Google) | Assigned manually | Meeting agenda/notes. Transcript after meeting. | Meeting event. Transitions to Meeting live session. | Auto from calendar mappings |
| GitHub issues/PRs | Tracked in code workspace | Issue/PR content as artifact | Item in code workspace | work/tabura, topic labels |

### Inbox

Inbox is a global view accessible from any workspace sidebar. It shows all items in inbox state. The inbox is filterable by label: selecting a label narrows the view to items carrying that label (or any child label). Unfiltered inbox shows everything.

### Daily workspaces

Starting without an explicit workspace activates today's Daily Workspace. It is a persistent directory under `<data-dir>/daily/YYYY/MM/DD/`, and it is reused across restarts for the same day.

Daily Workspaces also carry stable date labels in the same hierarchy: `YYYY`, `YYYY/MM`, and `YYYY/MM/DD`. Those date labels combine with topic labels, for example `2026/03/11 + work/plasma`.

- **Day rollover**: the next interaction after midnight creates and activates a new Daily Workspace for the new date.
- **Promote**: renaming the Daily Workspace turns it into a normal explicit workspace while retaining its recorded date.

Landing state: last explicitly active workspace, otherwise today's Daily Workspace.

### Intent architecture

Intent classification is workspace-independent. The classifier takes user utterance and outputs: system command, canonical action, or dialogue continuation. It does not need workspace context, artifact state, or canvas state.

Execution is workspace-aware. System commands operate at system level (switch workspace, sync, settings). Canonical actions operate on the current workspace (artifacts, items, canvas state). Dialogue goes to the workspace chat session with full workspace context.

There is no hub entity. System commands are available from any workspace.

### Deterministic fast paths

Deterministic fast paths run before semantic routing and must not invoke the local intent model.

The registered fast-path families include:

- source sync and external sync commands
- calendar and briefing requests
- cursor, titled-item, item, and workspace commands
- GitHub issue and artifact-linking commands
- runtime control commands such as `toggle_silent`, `toggle_live_dialogue`, `cancel_work`, and `show_status`
- direct UI loopback controls for `system_action` events and push-to-talk hold/release

The backend registry in `internal/web/chat_intent_fast.go` is authoritative for these boundaries.

## Authoritative Live Model

Tabura exposes exactly two live runtime modes:

- Dialogue
- Meeting

Anything else is either a temporary implementation detail or a bug in naming, UX, or state modeling.

## Control Surface Contract

The canvas runtime exposes one persistent control surface: the Tabura Circle.

- The collapsed circle shows the active tool in its center and the active live session on the outer ring.
- Expanding the circle exposes the five interaction tools (`pointer`, `highlight`, `ink`, `text_note`, `prompt`) plus the live-session controls (`dialogue`, `meeting`) and the independent `silent` toggle.
- Dialogue and Meeting are zero-or-one runtime sessions. Tapping the active session again returns the runtime to manual mode.
- Stop is not a dedicated top-panel button. The stop affordance is the active indicator frame, the active circle session segment, or a voice stop intent.
- Management surfaces such as hotword/model/voice configuration live under `/manage`, not in the canvas runtime shell.

## Canonical Action Semantics

All shipped actions must reduce to this grammar:

- Open / Show
- Annotate / Capture
- Compose
- Bundle / Review
- Dispatch / Execute
- Track as Item
- Delegate to Actor

These actions may appear in different UI contexts, but they must not change meaning across surfaces.

## Allowed Tool Modalities

Tabura may accept input through these modalities:

- tap-to-voice
- right-click and type
- keyboard direct input
- ink or stylus
- live Dialogue
- live Meeting
- narrow intake surfaces

These are input paths into the same system, not alternate product models.

## Rules for Auxiliary Surfaces

Auxiliary surfaces are allowed only when all of the following are true:

- The surface is narrower than the main workspace.
- The surface exists to make one job materially faster.
- The surface writes back into the same Workspace / Artifact / Item / Actor / Label ontology.
- The surface does not create its own action grammar.
- The surface does not create a parallel runtime shell, inbox, review system, or workspace universe.

If a surface cannot satisfy all of those constraints, it does not belong in the product.

## Rules for New Artifact Kinds

New artifact kinds are allowed only when all of the following are true:

- The artifact still participates in the same ontology.
- The artifact can be opened, annotated, composed around, reviewed, dispatched, tracked, or delegated without inventing a separate grammar.
- Any artifact-specific affordance remains a narrow specialization of the canonical actions above.
- The artifact does not require its own naming scheme for live modes, state, or workflow concepts.

Artifact kinds may add presentation or extraction details. They must not define a second product.

## Design Lineage

The intellectual foundations for this interaction model are documented in `design-lineage.md`. The core claim, supported by six decades of computing research and cognitive science, is that the workspace (not the app) should be the unit of design, and that action is situated (not pre-planned), which is why annotation-accumulation beats immediate-dispatch.
