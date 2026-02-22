# Release v0.0.1

## Version Scope

`v0.0.1` defines the first public baseline of Tabura documentation and release metadata.

## Included in v0.0.1

- MIT licensing in repository root.
- Canonical documentation index and architecture references.
- Interaction model specification for object-scoped intent workflows.
- Review workflow specification for draft/commit semantics.
- Interface inventory for web and MCP surfaces.
- Zenodo and citation metadata files for archival/publication.

## API and Behavior Notes

- Existing MCP tools and web endpoints are documented as currently implemented.
- Reply drafting remains explicitly user-controlled; no auto-send behavior.
- Draft marks remain non-persistent until commit.

## Out of Scope in v0.0.1

- Full multi-item batch execution orchestration UI.
- Multi-user conflict resolution semantics.
- Cross-instance distributed consistency protocol.

## Traceability

For archival publication records, pair this version with:
- release label: `v0.0.1`
- source revision: exact git commit hash in release metadata
- repository URL: `https://github.com/krystophny/tabura`
