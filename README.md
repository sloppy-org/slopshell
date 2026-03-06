# タブラ tabura

Core paradigm:
- Full-viewport zen canvas: blank screen (tabula rasa) or artifact fills the view.
- Tap to talk, right-click to type, keyboard auto-activates. No visible chrome.
- Responses stream as ephemeral overlays; document edits update in place with diff highlighting.
- Edge panels (hover/swipe to reveal) for project switching and chat panel access.
- Companion Mode is the active unified assistant surface for live meetings, 1:1 conversations, and workday assistance.

License: MIT (`LICENSE`)
Legal notice: Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](DISCLAIMER.md).

## Start Here

- **Spec hub**: [`docs/spec-index.md`](docs/spec-index.md)
- **System architecture**: [`docs/architecture.md`](docs/architecture.md)
- **Companion Mode whitepaper**: [`docs/companion-mode-whitepaper.md`](docs/companion-mode-whitepaper.md)
- **Codex app-server integration**: [`docs/codex-app-server-pivot.md`](docs/codex-app-server-pivot.md)
- **HTTP/MCP interface inventory**: [`docs/interfaces.md`](docs/interfaces.md)
- **UI paradigm**: [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- **Model download policy**: [`docs/model-download-policy.md`](docs/model-download-policy.md)
- **Meeting notes privacy**: [`docs/meeting-notes-privacy.md`](docs/meeting-notes-privacy.md)
- **Third-party licenses**: [`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md)
- **Current release notes (v0.1.8)**: [`docs/release-v0.1.8.md`](docs/release-v0.1.8.md)

## Install

Universal installers:

```bash
curl -fsSL https://github.com/krystophny/tabura/releases/latest/download/install.sh | bash
```

```powershell
irm https://github.com/krystophny/tabura/releases/latest/download/install.ps1 | iex
```

Package managers:

```bash
brew install krystophny/tap/tabura
```

```bash
paru -S tabura-bin
# or
yay -S tabura-bin
```

```powershell
winget install krystophny.tabura
```

Package-manager installs provide the `tabura` binary only. For full local setup, run `tabura server` or the installer scripts above.

Uninstall:

```bash
./scripts/install.sh --uninstall
```

```powershell
./scripts/install.ps1 -Uninstall
```

Manual build:

```bash
go build ./cmd/tabura
go install ./cmd/tabura
```

Requirements:
- Go 1.24+

## Core Commands

```bash
tabura bootstrap --project-dir .
tabura mcp-server --project-dir .
tabura server --project-dir . --data-dir ~/.tabura-web --web-host 0.0.0.0 --web-port 8420 --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
tabura server --project-dir . --data-dir ~/.tabura-web --web-host 0.0.0.0 --web-port 8443 --web-cert-file ~/.config/tabura/certs/tabura.pem --web-key-file ~/.config/tabura/certs/tabura-key.pem --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
```

## Runtime Stack (Canonical)

Tabura runs as one Go runtime plus six local services:

1. `tabura-web.service` (`tabura server`)
2. `tabura-codex-app-server.service` (`codex app-server`)
3. `tabura-piper-tts.service` (Piper `/v1/audio/speech`)
4. `tabura-stt.service` (voxtype `/v1/audio/transcriptions`)
5. `tabura-intent.service` (local intent classifier at `127.0.0.1:8425/classify`)
6. `tabura-llm.service` (Qwen3.5 9B local coordinator at `127.0.0.1:8426/v1/chat/completions`)

Voice commit still uses built-in browser VAD auto-stop, then sends audio to the local STT sidecar.

Why Piper remains an HTTP sidecar:
- Piper `libpiper` linking is GPL-governed; direct linking would change distribution obligations.
- A local loopback HTTP sidecar keeps integration simple and license boundaries clear.

## Local Integration Defaults

- Web UI/API listener: `http://localhost:8420` (public-facing)
- Optional HTTPS listener: add `--web-cert-file <cert.pem> --web-key-file <key.pem>` (for example on `8443`)
- MCP listener: `http://127.0.0.1:9420/mcp` (loopback-only)
- Canvas websocket relay source: `ws://127.0.0.1:9420/ws/canvas`
- Codex app-server websocket: `ws://127.0.0.1:8787`
- Piper TTS endpoint: `http://127.0.0.1:8424/v1/audio/speech`
- Voxtype STT endpoint: `http://127.0.0.1:8427/v1/audio/transcriptions`
- Intent classifier endpoint: `http://127.0.0.1:8425/classify` (`TABURA_INTENT_CLASSIFIER_URL`, set `off` to disable)
- Intent LLM fallback endpoint: `http://127.0.0.1:8426/v1/chat/completions` (`TABURA_INTENT_LLM_URL`, set `off` to disable)
- Intent/delegator request model id: `TABURA_INTENT_LLM_MODEL` (default `local`)
- Intent/delegator profile selection: `TABURA_INTENT_LLM_PROFILE` (default `qwen3.5-9b`)
- Intent/delegator profile options: `TABURA_INTENT_LLM_PROFILE_OPTIONS` (default `qwen3.5-9b,qwen3.5-4b`)
- Local canvas session id: `local`
- Spark thinking budget for Spark model (fast path): `TABURA_APP_SERVER_SPARK_REASONING_EFFORT=low` (`low`/`medium`/`high`/`xhigh`)

Security model:
- MCP routes are intentionally not exposed on the web listener.
- By default, non-loopback MCP bind is rejected unless `--unsafe-public-mcp` is explicitly set.

## Temporary Voxtype Branch Pin

Until upstream release catches up, Tabura docs and service integration assume:

- Repo: `https://github.com/peteonrails/voxtype`
- Branch: `feature/single-daemon-openai-stt-api`

If you build voxtype from source for Tabura STT, use that branch.

## LAN HTTPS For Voice Capture

Some browsers (especially on macOS/iOS) block microphone features on insecure LAN origins.
Run the web listener with TLS and open `https://<your-lan-ip>:8443`.

Example with `mkcert`:

```bash
mkdir -p ~/.config/tabura/certs
mkcert -install
mkcert -cert-file ~/.config/tabura/certs/tabura.pem -key-file ~/.config/tabura/certs/tabura-key.pem localhost 127.0.0.1 ::1 192.168.1.50
tabura server --project-dir . --data-dir ~/.tabura-web --web-host 0.0.0.0 --web-port 8443 --web-cert-file ~/.config/tabura/certs/tabura.pem --web-key-file ~/.config/tabura/certs/tabura-key.pem --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
```

If a second device (for example a Mac) connects to this server, trust the same local CA on that device too.

Zen canvas behavior:
- Browser opens to tabula rasa (blank white screen) or last artifact.
- Tap anywhere to start/stop voice recording. Right-click to type. Keyboard auto-activates.
- Built-in VAD auto-stop detects utterance end and commits speech.
- Companion Mode is botless, local-first, and Whisper-backed by default.
- If no document is displayed, Companion Mode shows a full-screen minimal humanoid idle surface or optional black mode.
- Meetings and long-running jobs default to temporary projects with persisted text artifacts only.
- Assistant output follows one path only:
  - chat-only (spoken), or
  - file-backed canvas (`:::file`) with canvas content rendered only on canvas.
- Multi-paragraph assistant output is auto-promoted to a temp canvas file and not shown/spoken in chat.
- Responses stream as ephemeral overlays. Click outside to dismiss.
- Edge panels: hover near top edge for projects, right edge for chat log.
- Slash commands: `/plan`, `/plan on`, `/plan off`, `/pr [selector]`, `/status`, `/stop`, `/clear`, `/compact`.
- Artifacts render Markdown + LaTeX.

## Markdown LaTeX Rendering

Markdown text artifacts support TeX math rendering via MathJax.
- Inline math: `$...$` or `\(...\)`
- Display math: `$$...$$` or `\[...\]`

## Novel UI Focus (What To Evaluate First)

1. Zen canvas: invisible chrome, full-viewport document surface.
2. Tap-to-talk voice capture with recording dot indicator.
3. Ephemeral response overlays with hidden chat history in the edge panel.
4. Edge-reveal panels for hidden project/diagnostics chrome.
5. E-ink-friendly: no animations for functional elements, static indicators.

See:
- [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- [`docs/interfaces.md`](docs/interfaces.md)

## Integration Example (Optional)

```bash
PRODUCER=http://127.0.0.1:8090/mcp
CONSUMER=http://127.0.0.1:9420/mcp

handoff_id=$(
  curl -sS -X POST "$PRODUCER" -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"handoff.create","arguments":{"kind":"file","selector":{"path":"README.md"}}}}' \
  | jq -r '.result.structuredContent.handoff_id'
)

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"canvas_session_open","arguments":{"session_id":"local"}}}'

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"canvas_import_handoff\",\"arguments\":{\"session_id\":\"local\",\"handoff_id\":\"$handoff_id\",\"producer_mcp_url\":\"$PRODUCER\",\"title\":\"Imported File\"}}}"
```

## Tests

```bash
./scripts/sync-surface.sh --check
go test ./...
npm run test:reports
```

Test report artifacts are written under `.tabura/artifacts/test-reports/`.

## Citation and Archival Metadata

- Citation metadata: `CITATION.cff`
- Zenodo metadata: `.zenodo.json`
