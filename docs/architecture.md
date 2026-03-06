# Tabura Architecture

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

Tabura is a Go monolithic web runtime with a split listener model:
- public web/UI listener
- local-only MCP listener

Runtime stack:
- `tabura-web.service` runs the Go monolith (`tabura server`)
- `tabura-codex-app-server.service` runs Codex app-server
- `tabura-piper-tts.service` runs Piper TTS API on loopback
- `tabura-intent.service` runs local intent classification on loopback (`/classify`)
- `tabura-llm.service` runs Qwen3.5 9B local coordinator on loopback (`/v1/chat/completions`)

## Components

- `cmd/tabura/main.go`
  - CLI entrypoint and subcommand dispatch.
- `internal/mcp/server.go`
  - MCP JSON-RPC methods and tool dispatch.
- `internal/canvas/adapter.go`
  - Canvas sessions, artifact state, and event log.
- `internal/serve/app.go`
  - MCP HTTP daemon (`/mcp`) and canvas websocket (`/ws/canvas`) mounted on the MCP listener.
- `internal/web/server.go`
  - Browser APIs for chat sessions, canvas APIs, and chat/canvas websocket routes on the web listener.
- `internal/extensions/host.go`
  - Legacy manifest-driven compatibility runtime pending contraction,
    replacement, or removal. Loads only `*.extension.json` manifests.
- `internal/plugins/manager.go`
  - Legacy webhook compatibility runtime pending contraction, replacement, or
    removal. Loads only legacy plugin `*.json` manifests and ignores
    `*.extension.json` files so the two retained compatibility paths stay
    distinct.
- `internal/store/store.go`
  - SQLite persistence for auth and chat session/message history.
- `internal/protocol/bootstrap.go`
  - Bootstrap behavior for project-local integration files.

## Runtime Modes

- `tabura mcp-server`: stdio MCP runtime
- `tabura server`: monolithic runtime (web + local MCP listeners)

## Local Sidecars

- Codex app-server remains a separate local service and is consumed over `ws://127.0.0.1:8787`.
- Piper TTS remains a separate local HTTP service on `http://127.0.0.1:8424`.
- Intent classifier remains a separate local HTTP service on `http://127.0.0.1:8425/classify`.
- Intent LLM fallback remains a separate local HTTP service on `http://127.0.0.1:8426/v1/chat/completions`.
- Voxtype STT remains a separate local HTTP service on `http://127.0.0.1:8427/v1/audio/transcriptions`.
- Current Tabura integration tracks voxtype branch `feature/single-daemon-openai-stt-api` from `https://github.com/peteonrails/voxtype`.
- Piper is intentionally not linked into the Go binary (`libpiper`) to avoid GPL-linked distribution coupling.

## UI Layout (Zen Canvas)

The browser UI is a full-viewport canvas with no visible chrome:

- **Tabula rasa**: blank white screen when no artifact is loaded.
- **Artifact mode**: document (text, image, PDF) fills the viewport.
- No toolbar, no prompt bar, no chat column. All interaction is invisible.
- **Edge panels** (hidden): top edge = project switcher, right edge = chat log / diagnostics. Revealed by hovering near screen edge (desktop) or swiping inward (mobile).

## Primary Data Flows

1. MCP client calls tool on `tabura mcp-server` or the local MCP listener from `tabura server`.
2. Tool dispatch in `internal/mcp/server.go` resolves into adapter operations.
3. Adapter updates session/artifact state in memory and emits events.
4. Browser consumes websocket events: responses stream into ephemeral overlay, artifacts update the canvas in place.

Chat hook flow:
1. Current code may route through legacy extension/plugin compatibility hooks.
2. New product behavior should stay in ordinary public core packages, not a new
   bundle ecosystem.
3. If any hook/API survives, it should be narrowed to explicit local
   capability-provider interop and deterministic compatibility needs.
4. Meeting-notes follow-up planning lives in public `krystophny/tabura` issues only.

## Interaction Model

- **Tap/left-click** toggles voice recording. A red dot appears at the tap position.
- Pure VAD detects end-of-utterance and commits speech input.
- **Right-click** opens a floating text input at the cursor position.
- **Keyboard typing** (when nothing is focused) auto-activates text input.
- **Enter** sends the message; input is cleared.
- **Ctrl long-press** (300ms) starts push-to-talk; release stops and sends.
- **Escape** dismisses overlay/input. If nothing is open and an artifact is showing, clears to tabula rasa.
- On artifact: tap/right-click captures line context (`[Line N of "title"]`) prepended to the message.
- Responses stream as ephemeral overlays; click outside to dismiss. Document edits update the canvas in place with diff highlighting.

## Handoff Import Flow

1. Producer creates handoff payload (outside Tabura).
2. Tabura receives `canvas_import_handoff` with `handoff_id`.
3. Tabura peeks/consumes producer handoff payload and renders artifact.

## Current Voice Runtime and Planned Companion Convergence

Current runtime behavior still exposes a legacy conversation-mode path with a
wake word. The active product direction is to converge that behavior into the
planned Companion Mode documented in [`companion-mode-whitepaper.md`](companion-mode-whitepaper.md).

Legacy conversation mode enables hands-free voice interaction via a wake word
("alexa" using openWakeWord's `alexa_v0.1.onnx` model).

Wake-word detection runs entirely in the browser using ONNX Runtime Web:
- `melspectrogram.onnx` extracts mel features from raw audio.
- `embedding_model.onnx` produces frame-level embeddings.
- `alexa.onnx` is the keyword classifier (16-frame input, ~1.28s detection latency).

All three models live in `internal/web/static/vendor/openwakeword/`.

Audio pipeline in `hotword.js`:
- Mic audio is downsampled to 16 kHz mono via a ScriptProcessorNode.
- Each audio frame is written to a 2-second ring buffer (32,000 samples) for pre-roll capture.
- Mel and embedding stages feed into the keyword classifier per frame.
- On wake-word detection, the app begins voice recording immediately (no intermediate listen window).

State transitions:
- **Paused** (black border + pause bars): legacy conversation mode on, waiting for wake word.
- **Recording** (red border + red dot): wake word detected or user tapped, capturing speech.
- **Listening** (blue border + pulse): follow-up window after TTS response (6s).
- Follow-up timeout returns to **Paused** and restarts hotword monitoring.

Planned Companion Mode differs from this legacy path in several ways:
- it is project-scoped instead of a free-floating toggle
- it always transcribes for context while active
- it targets live meetings, 1:1 conversations, and workday presence with one model
- it uses a humanoid idle surface or optional black mode when no document is shown
- meetings and long-running tasks are intended to become temporary projects

Utterance filtering (server-side in `internal/stt/transcribe.go`):
- Whisper hallucination blocklist (13 phrases).
- Noise rejection: filler-only transcripts (<3 words), TV/radio background patterns.
- Minimum audio buffer size (1024 bytes).

## STT Sidecar

- `tabura-stt.service` runs voxtype on loopback (`http://127.0.0.1:8427/v1/audio/transcriptions`).
- For source builds, use voxtype branch `feature/single-daemon-openai-stt-api` until this lands in an upstream release.
- Audio flows: browser WebSocket -> RAM buffer -> HTTP POST to sidecar -> transcript text returned.
- No audio is persisted to disk or database. See `docs/meeting-notes-privacy.md`.

## Trust and Access Boundaries

- Tabura does not require direct credentials to producer systems.
- Producer endpoint authority remains outside Tabura.
- Tabura stores local auth/session state in SQLite under web data dir.
- MCP routes are not mounted on the web listener and default to loopback-only bind.

## Modular Core Direction

Tabura's active direction is a single public repo with ordinary modular
packages under `internal/`. Product behavior should live in public core code,
not a private repo and not an extension/plugin bundle system.

Auth/session, media transport, queueing, persistence, privacy invariants, and
meeting-notes behavior stay in core. The legacy `internal/extensions` and
`internal/plugins` packages should be treated as transitional compatibility or
interop code rather than an expanding product surface.

If a compatibility surface remains during cleanup, it should be justified as a
small local capability boundary for deterministic external integrations such as
Helpy, not as a general SDK.
