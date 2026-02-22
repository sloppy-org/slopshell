# Integrated Handoff Protocol Spec

This directory carries the handoff protocol specification and conformance assets as part of the Tabura publication set.

Primary goals:
- keep the object-scoped UI paradigm as the top-level product framing
- keep transport/protocol contracts versioned and citable in the same public repository
- avoid split publication timing across multiple repos for core interoperability specs

Read in this order:
1. `spec/overview.md`
2. `spec/lifecycle.md`
3. `spec/message-actions-v1.md`
4. `spec/security.md`
5. `security/threat-model.md`

Schemas:
- `schemas/envelope-v1.json`
- `schemas/kind-file-v1.json`
- `schemas/kind-mail-headers-v1.json`
- `schemas/message-action-capabilities-v1.json`
- `schemas/message-action-request-v1.json`
- `schemas/message-action-response-v1.json`
- `schemas/error-v1.json`

Conformance examples:
- `conformance/examples/*`
- `conformance/negative/*`
- `conformance/runner-spec.md`

Upstream snapshot note:
- `README.upstream.md` preserves the imported upstream overview text.

License:
- This integrated spec is distributed under repository MIT license (`LICENSE`).
