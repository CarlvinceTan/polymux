---
name: ui-control
description: Drive or inspect an owned Polymux development instance with the repository's lightweight native Control session. Use for Control development, exploratory Polymux UI checks, and its opt-in E2E driver; general apps and production instances are unsupported.
---

# UI Control

Read `docs/control.md` for the session protocol, examples and current replacement gates. Control performs native input but remains an opt-in candidate; Cua is still the default E2E driver.

- Start with `scripts/control.sh permissions`. It never prompts. Only run `request-accessibility` when the user has asked to grant access; the user approves the system prompt. Do not edit TCC or reuse another driver's identity.
- Start your own background `scripts/run.sh` instance, record its runner PID and use its exact printed instance directory. Connect through `scripts/control.sh session` or `connect(instance)` from `tools/control/client.mjs`. The session retains real native references; do not select another run by app name.
- Read a complete `snapshot`, choose an unambiguous element, act with its returned `token`, then verify the postcondition from fresh state. A new snapshot or action invalidates old tokens. For multi-window work, use a returned native `window` token; `captureWindow` is the separate WindowServer ID for screenshots.
- Use `type` for insertion and `set` for replacement. Secure values are never returned; secure entry requires `set`. Query absence is unproven when the snapshot is incomplete. Treat snapshots/captures as sensitive and clean up images after inspection.
- A receipt proves dispatch only. Unknown effects, cancellation, timeout, changed identity or transport failure require observation before any retry. Control has no automatic Cua or foreground fallback. Missing native routes do not authorize broader input.
- Run existing flows with `scripts/run-e2e.sh --driver control ...`. The adapter executes trusted repository JS, not sandboxed arbitrary code. It must preserve the same UI assertions and must not hide unsupported behavior behind a fallback.
- Close the session in `finally`; stop only the recorded runner PID and verify its app/data are gone. Preserve other instances and the shared Cua service.

Frontmost-app, pointer and clipboard samples are useful evidence, not continuous keyboard-focus or window-order protection. Keep the documented default-switch gates until verified; do not claim full Cua parity from successful ordinary flows.
