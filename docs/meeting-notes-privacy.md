# Meeting Notes Privacy Contract

This document formalizes the audio privacy guarantees for Tabura's speech-to-text pipeline.

**Key invariant: audio exists only in RAM during processing and is never persisted to disk or database.**

## Audio Lifecycle

1. **Capture**: browser MediaRecorder captures audio chunks.
2. **Transport**: chunks are sent over WebSocket as binary frames to the Go server.
3. **Buffer**: chunks accumulate in `chatWSConn.sttBuf` (RAM-only `[]byte`, bounded by `stt.MaxAudioBytes` = 10 MB).
4. **Transcribe**: on STT stop, the buffer is sent via HTTP POST (multipart) to the whisper.cpp sidecar. The sidecar processes audio in-memory and returns JSON text.
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

## Whisper Sidecar Boundary

The whisper.cpp sidecar (`tabura-stt.service`) receives audio via HTTP multipart POST to `/inference`. The sidecar:

- Processes audio entirely in RAM (whisper.cpp does not write temp files for inference).
- Returns only transcript text as JSON (`{"text": "..."}`).
- Does not persist audio between requests.

## Operational Notes

- **Swap/page-file**: on a local install, the OS may page RAM to swap. This is outside Tabura's control. Users who require confidentiality against swap forensics should use encrypted swap or disable swap.
- **Crash dumps**: a process crash dump may contain the in-flight audio buffer. Users who require confidentiality against crash-dump forensics should configure their OS to restrict core dumps.
- **Network**: audio transits the local WebSocket (browser to Go server) and local HTTP (Go server to whisper sidecar). Both default to loopback (`127.0.0.1`). No audio leaves the machine in the default configuration.

## Schema Invariant

No database table may contain a column whose name includes `audio`, `wav`, `pcm`, `recording`, or `sound_blob`. This is enforced by an automated test (`TestPrivacySchemaNoAudioColumns` in `internal/web/server_security_test.go`).

## Code References

- `internal/web/chat_stt.go`: STT WebSocket message handlers (start/stop/cancel/binary).
- `internal/web/chat_ws.go`: `chatWSConn` struct definition (RAM-only `sttBuf`).
- `internal/stt/transcribe.go`: `Transcribe` function (HTTP-only, no temp files).
