# Flow Definitions

`tests/flows/` is the source of truth for cross-platform UI interaction flows.

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
