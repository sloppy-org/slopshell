# Meeting Notes Privacy Contract

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

This document formalizes the audio privacy guarantees for Tabura's speech-to-text pipeline, including meeting capture, tap-to-talk transcription, and capture-mode voice memos.

**Key invariant: audio exists only in RAM during processing and is never persisted to disk or database.**

## Capture Retention Policy

- Captured voice memos follow the same RAM-only policy as meeting capture.
- Uploaded audio is normalized and transcribed in memory, then discarded immediately.
- Offline or retryable voice memos may stay in browser RAM for the current page session only; they must not be persisted to IndexedDB, disk, SQLite, or other durable browser storage.
- Only transcript text may persist as an artifact or chat message. The original recording must not be stored for replay.
- Oversized or invalid uploads must be rejected without creating multipart temp files on disk.

## Meeting Consent Boundary

- Meeting capture is explicit opt-in. Capture must not start until meeting live mode is enabled. The current config API expresses that as `companion_enabled=true`.
- Disabling Meeting live mode is an exit action: any active participant capture session must stop immediately.
- Meeting capture defaults to microphone-only input. Alternate capture sources are not accepted through the config surface.

## Audio Lifecycle

1. **Capture**: browser MediaRecorder captures audio chunks.
2. **Transport**: chunks are sent over WebSocket as binary frames to the Go server.
3. **Buffer**: chunks accumulate in `chatWSConn.sttBuf` (RAM-only `[]byte`, bounded by `stt.MaxAudioBytes` = 10 MB).
4. **Transcribe**: on STT stop, the buffer is sent via HTTP POST (multipart) to the voxtype STT sidecar. The sidecar processes audio in-memory and returns JSON text.
5. **Discard**: the buffer reference is set to nil immediately after the HTTP call returns (or on cancel/error/disconnect). The Go garbage collector reclaims the memory.

Only the transcript text is stored. Audio data never reaches the database or filesystem.

## Banned Operations

The following are prohibited in all Tabura code paths:

- Writing audio data to any database table or column.
- Writing audio data to temporary files (`os.CreateTemp`, `os.WriteFile`, etc.).
- Logging audio bytes or base64-encoded audio to application logs.
- Including audio data in telemetry, metrics, or export payloads.
- Passing audio buffers to any function that persists to disk.

## RAM Buffer Policy

- Maximum size: `stt.MaxAudioBytes` (10 MB). Exceeding this discards the buffer and sends an error.
- The buffer is zeroed (set to nil) on:
  - Successful transcription (`handleSTTStop`)
  - Cancellation (`handleSTTCancel`)
  - Size overflow (`handleSTTBinaryChunk`)
  - WebSocket disconnect (connection teardown releases all fields)
- No audio data is copied to secondary buffers or caches within the Go process.

## Voxtype Sidecar Boundary

The voxtype sidecar (`tabura-stt.service`) receives audio via HTTP multipart POST to `/v1/audio/transcriptions`. The sidecar:

- Processes audio entirely in RAM for request handling.
- Returns only transcript text as JSON (`{"text": "..."}`).
- Does not persist audio between requests.

## Operational Notes

- **Swap/page-file**: on a local install, the OS may page RAM to swap. This is outside Tabura's control. Users who require confidentiality against swap forensics should use encrypted swap or disable swap.
- **Crash dumps**: a process crash dump may contain the in-flight audio buffer. Users who require confidentiality against crash-dump forensics should configure their OS to restrict core dumps.
- **Network**: audio transits the local WebSocket (browser to Go server) and local HTTP (Go server to voxtype sidecar). Both default to loopback (`127.0.0.1`). No audio leaves the machine in the default configuration.

## Schema Invariant

No database table may contain a column whose name includes `audio`, `wav`, `pcm`, `recording`, or `sound_blob`. This is enforced by an automated test (`TestPrivacySchemaNoAudioColumns` in `internal/web/server_security_test.go`).

## Code References

- `internal/web/chat_stt.go`: STT WebSocket message handlers (start/stop/cancel/binary).
- `internal/web/chat_ws.go`: `chatWSConn` struct definition (RAM-only `sttBuf`).
- `internal/stt/transcribe.go`: `Transcribe` function (HTTP-only, no temp files).
