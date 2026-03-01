# Release v0.1.6

## Scope

`v0.1.6` ships the first extension-host redesign in core Tabura, preserves
legacy plugin compatibility, and publishes extension-focused prior-art
whitepapers for meeting-partner and productivity capability bundles.

## Highlights

### Extension Host v1 (Webhook Runtime)

- Added new extension runtime package:
  - `internal/extensions/host.go`
  - `internal/extensions/types.go`
- Added extension manifest format (`*.extension.json`) with:
  - `hooks`
  - `permissions`
  - `commands`
  - `ui_contributions`
  - `engine.tabura` compatibility constraints
  - signing metadata fields
- Added hook permission model and command execution path.

### Runtime and API Surface

- Added runtime metadata fields:
  - `extensions_dir`
  - `extensions_loaded`
- Added extension APIs:
  - `GET /api/extensions`
  - `POST /api/extensions/commands/{command_id}`
- Preserved compatibility APIs:
  - `GET /api/plugins`
  - `POST /api/plugins/meeting-partner/decide`
- Meeting-partner decision flow now resolves extensions first, then legacy plugins.

### Meeting-Partner Contract

- Continued support for meeting-partner hooks:
  - `meeting_partner.session_state`
  - `meeting_partner.segment_finalized`
  - `meeting_partner.decide`
- Continued typed decision contract:
  - `noop`
  - `respond`
  - `action`

### Tests and Validation

- Added extension host unit tests:
  - `internal/extensions/host_test.go`
- Added web API tests for extension inventory and command execution:
  - `internal/web/plugins_test.go`
- Verified with:
  - `go test ./internal/extensions ./internal/web`
  - `go test ./internal/plugins`
  - `go test ./...`

### Public Prior-Art Documentation

- Updated extension/runtime contract spec:
  - `docs/plugins.md`
- Added extension platform whitepaper:
  - `docs/extension-platform-whitepaper.md`
- Updated meeting-partner whitepaper with extension-platform and Helpy bundle alignment:
  - `docs/meeting-partner-whitepaper.md`

## Traceability

For publication metadata, associate this release with:

- release label: `v0.1.6`
- repository: `https://github.com/krystophny/tabura`
- exact source revision: tag target commit hash
