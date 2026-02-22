#!/usr/bin/env bash
# Usage: scripts/bump-version.sh v0.0.7
set -euo pipefail

version="${1:-}"
if [ -z "$version" ]; then
  echo "Usage: $0 <version>" >&2
  echo "Example: $0 v0.0.7" >&2
  exit 1
fi

# Strip leading v for bare version (0.0.7)
bare="${version#v}"
# Ensure prefixed version (v0.0.7)
prefixed="v${bare}"
today="$(date +%Y-%m-%d)"

cd "$(git rev-parse --show-toplevel)"

# Metadata files
sed -i "s/\"version\": \"v[^\"]*\"/\"version\": \"${prefixed}\"/" .zenodo.json
sed -i "s/^version: v.*/version: ${prefixed}/" CITATION.cff
sed -i "s/^date-released: .*/date-released: ${today}/" CITATION.cff

# Go source (bare version without v prefix)
sed -i "s/ServerVersion         = \"[^\"]*\"/ServerVersion         = \"${bare}\"/" internal/mcp/server.go
sed -i "s/\"version\":          \"[^\"]*\"/\"version\":          \"${bare}\"/" internal/web/server.go
sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"${bare}\"/" internal/appserver/client.go
sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"${bare}\"/" internal/voxtypemcp/server.go

echo "Bumped to ${prefixed} (${today})"
echo ""
echo "Files updated:"
echo "  .zenodo.json"
echo "  CITATION.cff"
echo "  internal/mcp/server.go"
echo "  internal/web/server.go"
echo "  internal/appserver/client.go"
echo "  internal/voxtypemcp/server.go"
echo ""
echo "Still manual:"
echo "  - Create docs/release-${prefixed}.md"
echo "  - Update README.md and docs/spec-index.md release links"
