# Working on Polymux

If `AGENTS.local.md` exists, read it too. Where it conflicts with this file, it takes priority.

Do not edit this instruction file unless explicitly asked.
Do not modify `README.md` unless explicitly instructed. Suggest changes instead.
If instructed to make changes, preserve the existing structure where possible; if restructuring would clearly improve it, propose that instead of silently doing it.

## Working Principles

Apply these unless instructed otherwise.

- Think before coding. If in doubt, ask.
- If there are multiple ways to implement something, present them first before coding.
- Simplicity first: write the minimal code that solves the problem.
- Only touch what's necessary. Prefer not to touch what already works.
- Don't retain legacy code (old paths, compatibility shims, deprecated fallbacks) unless instructed; suggest keeping it if removal might be a concern.
- Define what success looks like up front, and don't stop until it is achieved.

## Verification

- After an implementation, test it end to end. For UI changes, build the app and exercise the real scenario.
- Scope tests to the change: skip flow steps already verified earlier in the conversation and unaffected since.
- Test enough to confirm the implementation works; no more.
- Run tests and manual verification in an isolated instance: a temporary server and app named "Polymux N" (N is a number, unique per run) with their own throwaway configuration and app data. Never touch the user's real configuration or app data, or the installed copy of the app.
- Use `scripts/run.sh` to start it, in the background. Stop it by killing that process, which also deletes its data. Always stop it when done. Several runs can go at once without affecting each other.
- Two kinds of E2E UI test, split by what they need:
  - cua-driver flows in `e2e/flows/*.js` cover behaviour against a real server (create a chat and send, search, relaunch persistence). Run them with `scripts/run-e2e.sh [flow.js ...]`; it starts a `run.sh` instance, runs the flows, fails if focus moves, and stops the instance. Flows target elements by role and name, never by pixel position, and `APP` holds the instance name.
  - XCUITest in `PolymuxUITests` covers states a real server can't produce on demand (failed, slow, approvals), using the fixture server. Run the relevant ones with `xcodebuild test`.
- Launch and operate the app in the background: never bring its windows to the front or take focus from the user's open windows. For manual checks, use the `cua-driver` skill. `PolymuxUITests` activates the app when it launches it, so run it only as a foreground exception and say so first.
- Ask permission before adding or changing an E2E test. Propose the scenario first.
- Tests cover non-trivial behaviour that other changes could easily break. Don't test trivial things, such as a single value like a width.
- After a change, check whether existing tests need updating or new coverage is warranted, scoped to that change.

## Reports and Summaries

Keep reports and summaries simple, concise and easy to understand. If there is a large amount to report, prefer clear categories or grouping so the change or work for that group can be mentioned effectively. Prefer image/videos to convey visual differences with a before and after.

## UI Proposals

Use codebase components to create a mock to propose a certain ui. Do not create some roughly similar visual as representation.

## UI

`DESIGN.md` is the design system of record: strict rules only, no rationale.
Read and follow it before any component, style, layout, colour, or motion work.
Never update it unless explicitly asked; report conflicts or suggest changes instead.

## Comments

- Prefer short, one-line comments.
- Can consider to comment when behaviour or intent isn't immediately clear from the code. If function name clearly states purpose, there's no need for a comment.
- When in doubt, leave it out; a clear name beats a redundant comment.

## Naming

- Prefer function, component, and variable names of two words or fewer. Use a third word only when two isn't enough to make the name clear. Short names keep call sites and narrow views (e.g. SwiftUI layouts) readable.

## File organisation

- When files are moved out of a folder and it's clear the folder is no longer needed, delete it. Don't leave empty folders behind.

## Repository safety

- Preserve unrelated worktree changes. Never run `git stash`.
- Don't push or make changes to `main` unless instructed
- Don't commit unless instructed
- Don't create worktrees unless instructed
- Don't make backups (copies, snapshots, `.bak` files, backup branches) unless explicitly instructed; suggesting one is fine
