# Native Clients Plan

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

## Architecture Decision

Tabura's mobile direction is server-driven thin native clients.

Business logic lives in the Go server. Native clients stay focused on platform
I/O, low-latency capture, native rendering, and background/runtime integration.
That keeps behavior fixes centralized in `internal/web/` and `internal/mcp/`
instead of splitting product logic across multiple frontends.

Each native client owns three responsibilities:

- **Capture**: audio PCM, ink strokes, taps, and gestures.
- **Render**: structured chat/canvas output rendered with native surfaces.
- **Platform services**: background audio, push notifications, wake locks, and
  e-ink refresh hooks where applicable.

## Current Server Anchors

The current server already exposes the foundations required by thin native
clients:

- `internal/web/chat_ws.go` for chat websocket turns.
- `internal/web/server_relay.go` for canvas relay and file-backed canvas
  transport.
- `internal/web/mdns.go` for loopback-safe mDNS advertisement of the runtime.
- `internal/web/push.go` for push registration and relay plumbing.
- `tests/playwright/canvas.spec.ts` for render-protocol coverage.

## Platform Surfaces

The shipped native surfaces match the thin-client split.

### iOS

- `platforms/ios/TaburaIOS/TaburaInkCaptureView.swift` uses `PencilKit` for ink
  capture.
- `platforms/ios/TaburaIOS/TaburaAudioCapture.swift` owns microphone capture.
- `platforms/ios/TaburaIOS/TaburaCanvasTransport.swift` and
  `platforms/ios/TaburaIOS/TaburaChatTransport.swift` connect to the server.
- `platforms/ios/TaburaIOS/TaburaServerDiscovery.swift` handles `_tabura._tcp`
  discovery.
- `platforms/ios/TaburaIOS/ContentView.swift` now exposes an explicit native
  dialogue surface selector and a full-screen black dialogue mode.
- `platforms/ios/TaburaIOS/TaburaAppModel.swift` wires dialogue entry and exit
  to `/api/live-policy`, `/api/workspaces/{id}/companion/config`, and incoming
  `toggle_live_dialogue` / `companion_state` chat events.

### Android and Boox

- `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaInkSurfaceView.kt`
  uses `MotionEventPredictor` for low-latency stylus capture.
- `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaBooxInkSurfaceView.kt`
  uses `TouchHelper.create` and raw drawing for Onyx Boox devices.
- `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaAudioCaptureService.kt`
  owns background microphone capture.
- `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaCanvasTransport.kt`
  and
  `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaChatTransport.kt`
  connect to the server.
- `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaServerDiscovery.kt`
  handles `_tabura._tcp` discovery.
- `platforms/android/app/src/main/kotlin/com/tabura/android/MainActivity.kt`
  now exposes an explicit native dialogue surface selector and a full-screen
  black dialogue mode.
- `platforms/android/app/src/main/kotlin/com/tabura/android/TaburaAppModel.kt`
  wires dialogue entry and exit to `/api/live-policy`,
  `/api/workspaces/{id}/companion/config`, and incoming
  `toggle_live_dialogue` / `companion_state` chat events.

### Web

- `internal/web/static/app-runtime-ui.ts` toggles `black-screen` dialogue mode.
- `internal/web/static/companion.css` defines the black-screen runtime surface.
- `tests/playwright/live-dialogue-companion.spec.ts` covers black-screen
  dialogue behavior.

## Ink Latency Targets

The current runtime keeps the original latency targets as design guidance:

| Platform | Target | Technique |
| --- | --- | --- |
| iOS + Apple Pencil | ~9ms | `PencilKit` with native prediction |
| Android + stylus | ~4ms | `MotionEventPredictor` with the Ink stack |
| Onyx Boox e-ink | ~100ms | raw drawing via `TouchHelper` |
| Web + stylus | ~10ms | browser ink path with delegated presentation where available |

These are target envelopes, not CI-enforced benchmarks. The concrete shipped
techniques are anchored in the platform files above.

## Delivery Status

Dialogue black-screen mode is now intentionally implemented across the shipped
clients:

- `#632` server-side render protocol
- `#633` native iOS client
- `#634` native Android client
- `#635` Onyx Boox support
- `#636` web ink rewrite
- `#637` black-screen dialogue mode on web, iOS, and Android
- `#638` mDNS advertisement and push relay

Issue `#639` remains the broader umbrella for the rest of the native-client
push. This document only claims the black-screen dialogue slice that is
implemented in the current repo state.

## Native Dialogue Mode Operation

Native dialogue mode is now explicit instead of implied:

- Choose `Robot` or `Black` in the native dialogue surface control.
- Tap `Start Dialogue` to enter live dialogue locally. The client posts
  `/api/live-policy` with `dialogue` and ensures
  `/api/workspaces/{id}/companion/config` has `companion_enabled=true`.
- When the selected idle surface is `black`, the client swaps into a full-screen
  black tap target and keeps the screen awake while dialogue mode stays active.
- Tap the full-screen surface to start recording and tap again to stop. Android
  continues to use the foreground microphone service for the active recording
  path.
- Tap `Exit Dialogue` or trigger `toggle_live_dialogue` from the server to
  leave the mode.

## Manual Verification

Use these pass/fail checks when real devices are available:

1. iOS black-screen dialogue surface
   Pass: set the surface to `Black`, tap `Start Dialogue`, confirm the app
   enters a full-screen black surface, the screen does not dim, and a tap starts
   then stops microphone capture.
   Fail: the app stays in the standard shell, the screen sleeps, or taps do not
   control recording.
2. Android black-screen dialogue surface
   Pass: set the surface to `Black`, tap `Start Dialogue`, confirm the app
   enters a full-screen black surface, the display stays awake, and a tap starts
   then stops the foreground microphone service path.
   Fail: the app stays in the standard shell, the display sleeps, or recording
   state diverges from the foreground-service state.
3. Server/client wiring
   Pass: switching the surface updates `/api/workspaces/{id}/companion/config`,
   entering dialogue posts `/api/live-policy`, and a server
   `toggle_live_dialogue` action toggles the native mode.
   Fail: native dialogue mode only works as a local visual toggle with no server
   state integration.
