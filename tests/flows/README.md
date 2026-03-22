# Flow Definitions

`tests/flows/` is the source of truth for cross-platform UI interaction flows.

Tabura now uses a layered UI source of truth:

- `internal/web/static/tabura-circle-contract.ts` is the component contract for the Tabura Circle across web, iOS, and Android.
- `tests/flows/` is the interaction contract shared across platforms.
- platform implementations must preserve the same semantic ids, states, icon meanings, and corner-placement model even when rendered with different native toolkits.

Each `.yaml` file defines one platform-agnostic flow:

```yaml
name: tabura_circle_select_ink_tool
description: Open the circle, select ink, and keep the circle open for more changes.
tags: [circle, tool]
preconditions:
  tool: pointer
  session: none
  silent: false
steps:
  - action: tap
    target: tabura_circle_dot
    expect:
      tabura_circle: expanded
  - action: tap
    target: tabura_circle_segment_ink
    expect:
      active_tool: ink
      tabura_circle: expanded
```

Supported schema:

- `name`: unique string
- `description`: human-readable string
- `tags`: non-empty string array
- Machine-readable schema: `tests/flows/schema.json`
- `preconditions`:
  - `tool`: `pointer|highlight|ink|text_note|prompt`
  - `session`: `none|dialogue|meeting`
  - `silent`: boolean
  - `indicator_state`: `idle|listening|paused|recording|working`
- `steps[]`:
  - `action`: `tap|tap_outside|verify|wait`
  - `target`: logical target id for `tap` and optional for `verify`
  - `duration_ms`: required for `wait`
  - `expect`: logical assertions
  - `platforms`: optional subset of `web|ios|android`

Logical targets live in `tests/flows/targets.cjs`. That contract is the shared map
from logical ids to platform-specific selectors or accessibility ids. Web-only test
hooks must declare `platforms: [web]` so the linter does not let harness-only steps
pretend to be portable.

The Tabura Circle UI contract currently defines:

- icon-only segment rendering with accessible labels
- persisted placement in `top_left|top_right|bottom_left|bottom_right`
- bug reporting as a top-panel action instead of a competing floating control

The shared navigation contract also defines:

- no canvas scrolling inside artifacts
- short horizontal swipe or flip = page navigation first, artifact navigation only at boundaries
- long-held horizontal swipe or flip = direct artifact navigation
- the same horizontal gesture semantics for canvas, left-panel edge swipe, and inbox swipe surfaces

Supported logical assertions:

- `active_tool`
- `session`
- `silent`
- `tabura_circle`
- `dot_inner_icon`
- `body_class_contains`
- `indicator_state`
- `cursor_class`

Coverage is enforced in two ways:

- every tool/session/silent combination must appear in the flow expectations
- every required Tabura Circle target and indicator state must be covered

The Playwright adapter executes these flows against the browser harness in
`tests/playwright/flow-harness.html` across this matrix:

- Chromium desktop at `1920x1080`
- Firefox desktop at `1920x1080`
- iPhone 14 touch emulation
- iPad Pro 11 touch emulation
- Pixel 7 touch emulation

Touch profiles use `page.tap()` while desktop profiles use mouse clicks. Assertions
such as `cursor_class` are skipped on touch profiles, matching the shared contract.

Native flow contract runners consume generated fixtures derived from the same YAML:

- `node ./scripts/sync-native-flow-fixtures.mjs` refreshes the checked-in native fixtures.
- `npm run test:flows:ios:contract` runs the Swift flow contract suite in `platforms/ios`.
- `npm run test:flows:android:contract` runs the Android model/contract suites in `platforms/android`.
- `npm run test:flows:ios` runs the iOS simulator UI harness on `faepmac1`.
- `npm run test:flows:android` runs the Android emulator UI harness locally.
- `npm run test:flows:native` runs the full validation path: fixture sync, shared web flows, both native contract suites, Android UI, and iOS UI.

The iOS contract runner is a Swift package. Run it on a machine with Swift
available; for this repo the documented macOS host is `faepmac1`. The full iOS
UI run is performed there through `./scripts/test-native-flows.sh`.

The generated fixture files live in the native test bundles and UI harness assets:

- `platforms/ios/Tests/TaburaFlowContractTests/Resources/flow-fixtures.json`
- `platforms/ios/TaburaIOSUITests/Resources/flow-fixtures.json`
- `platforms/android/flow-contracts/src/test/resources/flow-fixtures.json`
- `platforms/android/app/src/androidTest/assets/flow-fixtures.json`
