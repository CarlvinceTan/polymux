# Control

Control drives owned Polymux development instances through a small native macOS helper. It supports accessibility snapshots, buttons, text, selections, popup and context menus, scrolling, and window capture. It is an **opt-in E2E driver** while the remaining coverage and desktop-interference checks are completed. Cua remains the default.

## Operation

Requires macOS 15+, Swift 6.2+, and Node.js 22+ for the optional JS client and flow adapter. The Swift package in `tools/control/` has no third-party dependencies and does not change the production app.

```sh
scripts/control.sh permissions
scripts/run-e2e.sh --driver control e2e/flows/fixture-draft.js
scripts/run-e2e.sh --driver cua e2e/flows/fixture-draft.js
```

For exploratory work, start `scripts/run.sh --fixture --verify-tools` in the background. Record its runner PID and the exact instance directory printed above `data`. Connect only to that owned instance; do not discover another agent's or a personal app by name. Stop the recorded runner PID with `kill -TERM PID` when finished, including after failures.

```js
import { connect } from './tools/control/client.mjs';
const session = await connect(instance);
try {
  const state = await session.request({ op: 'snapshot', compact: true });
  if (!state.complete) throw new Error(state.incompleteReasons.join(', '));
  const buttons = state.elements.filter(row => row.role === 'AXButton' && row.label === 'Settings');
  if (buttons.length !== 1) throw new Error('Settings is missing or ambiguous');
  const receipt = await session.request({ op: 'press', token: buttons[0].token });
  // Read fresh state and verify that Settings appeared; the receipt is delivery evidence only.
  const after = await session.request({ op: 'snapshot', compact: true });
} finally {
  await session.close();
}
```

An agent can also start `scripts/control.sh session '{"instance":"ABSOLUTE_INSTANCE_PATH"}'` and keep its stdin open. Each subsequent line is a JSON request; each stdout line is one JSON response. The CLI and JS client use the same executable. Pass `fixture: true` only for the fixture copy owned by the E2E runner, not for a base `run.sh --fixture` app.

The wrapper builds into `.build/control/`, reusing a content/toolchain fingerprint and a shared build lock. A native process lives only for its connection. There is no daemon, TCP listener, MCP server, app-local test endpoint or Cua fallback. `flow.mjs` executes trusted repository test scripts against this client; it is not an untrusted-code sandbox.

## Native session

| Operation | Fields | Meaning |
| --- | --- | --- |
| `windows` | — | Retained native window identities, main-window state, frame, and a `captureWindow` ID when exact association is available. |
| `snapshot` | Optional `window`, `compact`, `maxElements`, `maxDepth`, `budgetMS` | Read the selected native window, or all owned native windows. Return elements, fresh tokens and completeness. `compact` collapses layout groups and scroll containers. |
| `press` / `menu` | `token` | Native press or context menu. Menu input uses an advertised AX action or an exact window-targeted event when no AX menu action exists. |
| `set` | `token`, `text` | Replace a writable text value, including secure fields, or set a finite slider value. |
| `type` | `token`, `text` | Insert at the current native text selection and update the caret. Secure fields require `set`. |
| `key` | `token`, `key` | `cmd+a`, Return, Escape, Delete, arrows, Tab or Space. Selection/confirmation use AX where available; other keys require confirmed focus in the observed native window. |
| `scroll` | `token`, `direction`, optional `amount` | Send window-targeted wheel events at the visible element. Directions: up/down/left/right; amount: 1–50 notches, two lines each. |
| `close` | — | Release handles and the target lock, then exit. EOF also closes the session. |

A complete snapshot retains real AX objects, not just numeric tree indices. Tokens expire after ten seconds, a new snapshot, or an action attempt. Closed windows, changed element identities, detached elements, app relaunches, and changed runner records are refused. Never carry a token into a new session. A failed or bounded snapshot cannot authorize actions.

Reads are bounded to 5,000 visited nodes, depth 100 and five seconds, with per-element AX timeouts. Ordinary values and optional metadata are returned; unreadable optional attributes are listed in `unavailable`. Secure values/subtrees are omitted. Unknown role or secure-field classification makes the read incomplete. `complete` describes traversal within these rules, not a guarantee that every optional attribute exists. Snapshots and images can contain chat or credential-card text: do not publish them as routine test artifacts.

Responses contain `ok` and `summary`; refusals include `code`. Action receipts report the chosen `route`, invalidate tokens and require observation. `effect: "unknown"`, timeout, cancellation or a transport failure can mean input already happened. Do not automatically replay it. The helper has a ten-second request deadline and a 120-second idle deadline. The JS client serializes requests and supports `terminate()`; interruption releases its OS lock but cannot undo input already delivered.

Only one Control session may own a particular base app or fixture at a time. Runner/app start times, exact paths, bundle identifiers and current PID records are checked before actions and after operations. This is cooperative isolation for same-user development, not a security boundary against other local code or another controller.

### Desktop behavior and native dependencies

Control does not activate an app, warp the physical pointer, or write the clipboard. Actions sample frontmost app, pointer location and clipboard change count; the E2E runner separately samples the frontmost app throughout each flow and fails closed if observation fails or the test app takes focus. These samples do **not** establish continuous keyboard-focus or window-order isolation, particularly during native menu tracking. A dedicated interference test remains a default-switch gate.

AX window references avoid title/geometry matching. Window-targeted pointer input additionally resolves `_AXUIElementGetWindow` and a small SkyLight event bridge. These are macOS SPI, so a missing symbol or unresolved identity refuses the route. Screenshots and pointer targets are associated by exact native identity, never by matching rectangles. Geometry is used only after identity is established, and offscreen targets are refused.

The background event implementation was checked against [Cua's native mouse routes](https://github.com/trycua/cua/blob/main/libs/cua-driver/rust/crates/platform-macos/src/input/mouse.rs). Control intentionally sends each event through one transport, restricts targets to owned Polymux instances, and omits Cua's broader app/browser and focus-management machinery. This is not a full parity port. Drag, browser automation, arbitrary apps, file-dialog workflows and stable distribution signing remain outside the verified scope.

## One-shot observation

```sh
scripts/control.sh windows '{"instance":"ABSOLUTE_INSTANCE_PATH"}'
scripts/control.sh inspect '{"instance":"ABSOLUTE_INSTANCE_PATH","window":123}'
scripts/control.sh capture '{"instance":"ABSOLUTE_INSTANCE_PATH","window":123,"output":"/tmp/control-window.png"}'
```

Use a real current window ID from `windows`, or a session window's `captureWindow`, in place of `123`. `inspect` reuses the session reader and returns no action tokens. `capture` uses ScreenCaptureKit and a new absolute PNG path with mode 0600; inspect and delete the image after use. Frame coordinates are desktop points with a top-left origin; capture reports the actual pixel scale. `desktop` returns a non-prompting frontmost-process sample used by both E2E drivers.

`permissions` reports the actual helper's Accessibility and Screen Recording access without prompting. Input requires Accessibility; screenshots additionally require Screen Recording. Local SwiftPM signing may attribute approval to the responsible terminal/agent host. Stable signed distribution identity has not been established.

Only when the user explicitly requests Accessibility setup:

```sh
scripts/control.sh request-accessibility
open 'x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility'
```

The user approves the system prompt; verify afterward with `permissions`. Do not change TCC, reuse another driver's identity, or prompt during unattended tests.

## Verification and replacement gates

```sh
swift test --package-path tools/control --scratch-path .build/control
node --test tools/control/client.test.mjs
python3 scripts/e2e_test.py
POLYMUX_CONTROL_INSTANCE="$INSTANCE" node --test tools/control/live.test.mjs
scripts/run-e2e.sh --driver control e2e/flows/fixture-*.js
```

The live tests need a fresh owned app with Settings available. They exercise stale tokens, incomplete reads, exclusive ownership, observed Settings transitions, termination and a blocked output pipe. Contract and runner tests run without GUI permission changes; GUI coverage requires an interactive host.

Validation on October 28, 2026:

- The full sweep passed 38 of 39 fixture flows, plus Settings and keyboard model selection against a temporary Go server. It exposed a media-transition flake and a secure-field focus issue.
- After the scoped fixes, media passed three consecutive runs and the final comparison. Secure provider-key entry/removal passed, including an assertion that the unsaved secret is absent from observations. All 39 fixture flows and three real-server configuration flows therefore have passing evidence; the entire set has not been rerun as one clean batch after those fixes.
- Five Swift contract tests, four JS transport tests, three live session tests and 34 script tests passed. Shell checks passed. The history flow now uses a complete traversal budget instead of depending on Cua's compressed tree depth.

A final paired sample used the same three flows on fresh instances:

| Flow | Control | Cua |
| --- | --- | --- |
| Draft and crash restoration | Passed, 7.4 s UI time | Passed, 12.5 s UI time |
| Media viewing and dismissal | Passed, 10.1 s UI time | Failed while revealing Landscape 6.png |
| Provider keys | Passed, 4.7 s UI time | Failed on a temporarily non-addressable removal button |

Cua also passed the media and provider-key flows in earlier clean runs. The shared media-reveal flake remains worth investigating in the app/test; this small sample is not a controlled benchmark or evidence of superiority over other controllers. The warm CLI fingerprint optimization previously reduced median `permissions` overhead from about 870 ms to 52 ms on this machine; that measures wrapper cost, not UI performance.

Keep Cua as the default until continuous keyboard-focus/window-order interference, cancellation during actual input, denied permissions on a fresh host and the remaining real-server workflows have dedicated evidence. Lifecycle interruption is tested; undoing partially delivered input is not promised. General apps, drag, file dialogs and production-agent integration are not covered by this development validation.

## Reusable component boundary

The useful long-term shape is a Swift native library, a thin CLI, and optional JS/Go clients. Keep Polymux instance discovery in an adapter outside the public core. The app should consume the same component external builders use, like [Pi's separate packages](https://github.com/earendil-works/pi/tree/main/packages). The current package is still a development executable; its interface is not a published compatibility promise. [Vault](vault.md#reusable-component-boundary) has a separate Go extraction boundary.

The archived [Control skill](https://github.com/CarlvinceTan/polymux-archive/blob/e582d67f818f58143f34f423ed7fbc9f9d7a9084/skills/control/SKILL.md) and [older Swift helper](https://github.com/CarlvinceTan/polymux-archive/blob/306a76d6f661aae0bfd389d08a8f560d6baf3d7f/old-old-midas/skills/official/window-control/scripts/exact-window-controller.swift) informed the owned-target observation contract. This implementation is not a claim of parity with the archived system.
