# Working on Polymux

If `AGENTS.local.md` exists, read it too. Where it conflicts with this file, it takes priority.
When asked to save an instruction here that is specific to a development environment (a machine, path, device or account), save it to `AGENTS.local.md` instead.

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
- Other agents may edit this tree at the same time. Before editing an area, list recent changes (`find . -mmin -30 -type f -not -path './.build/*' -not -path '*/node_modules/*'`) and re-read changed files. Report another agent's in-progress breakage rather than reworking it.

## Reference Matching

When asked to make something like another app or implementation, follow this order for the requested scope:

1. Find the reference. Inspect and exercise the relevant feature in the app, or inspect its source code when available. Establish the appearance, behaviour, states, and timing to match.
2. Match it exactly first. Reproduce the reference from its code or by direct visual and interaction comparison, before introducing project-specific customisations. When the source uses another language or framework, start with a faithful direct port, preserving its logic, structure, and behaviour even if the result is not yet idiomatic in this project.
3. Verify parity. Compare the running implementation against the reference, including relevant interactions, states, and transitions. A successful build alone does not establish a match. If the reference cannot be accessed or a difference cannot be verified, state the gap rather than claiming parity.
4. Adapt only after parity is verified. Apply the necessary project-specific differences, such as styling, and make intentional differences explicit. Repeat the comparisons to confirm that only those intended differences changed.
5. Refactor into idiomatic project code as a separate pass after the matching baseline works. Repeat the same comparisons to ensure the refactor preserves appearance and behaviour, including any intentional project adaptations.

## Verification

- After an implementation, test it end to end. For UI changes, build the app and exercise the real scenario.
- Scope tests to the change: skip flow steps already verified earlier in the conversation and unaffected since.
- Test in both light and dark mode only when the change could be affected by appearance mode, such as colours, contrast, materials, or mode-specific styling. For changes unaffected by appearance mode, testing in the current mode is enough.
- Test enough to confirm the implementation works; no more.
- Run tests and manual verification in an isolated instance: a temporary server and app named "Polymux N" (N is a number, unique per run) with their own throwaway configuration and app data. Never touch the user's real configuration or app data, or the installed copy of the app.
- Use `scripts/run.sh` to start it, in the background. Stop it by killing that process (`kill -TERM <pid>`), which also deletes its data. Kill by PID, never by pattern: another agent's instance can match. Always stop it when done. Several runs can go at once without affecting each other.
- To check the app compiles without launching it, run `scripts/check.sh [macos|ios|all]`; it shares `run.sh`'s build lock. For an iPhone screenshot, run `scripts/sim.sh out.png [app flags]` in a headless simulator, then `scripts/sim.sh --stop`.
- E2E UI tests are cua-driver flows in `e2e/flows/*.js`. Run them with `scripts/run-e2e.sh [flow.js ...]`; it starts a `run.sh` instance, runs the flows, fails if the test app takes focus, and stops the instance. Flows target elements by role and name, never by pixel position, and `APP` holds the app name.
  - Most flows cover behaviour against a real server (create a chat and send, search, relaunch persistence).
  - Flows whose first line is `// @fixture <flags>` cover states a real server can't produce on demand (failed, slow, approvals). They run against a copy of the instance's app on the DEBUG fixture server, started with those flags. `--verify-fixture` alone shows no window; add a view flag such as `--verify-tools`.
  - A `// @relaunch` line quits and reopens the app midway, and `// @crash` kills it; the next part gets the previous result as `PREVIOUS`.
- Follow the user's GUI control preference in `AGENTS.local.md`, which may allow background control, foreground control, or both. If no preference is saved, use background control. For manual checks, use the `cua-driver` skill.
- Clean up after manual testing: close every browser window, tab, app, and server you opened, including Chrome windows used to check a site or docs. Verify they are closed; nothing should still be running or cluttering the dock when the task is done.
- Add or change an E2E test without asking when it is genuinely useful: it covers behaviour that is easy to break, or keeps a flow true to an intended change. Mention it in the report.
- Tests cover non-trivial behaviour that other changes could easily break. Don't test trivial things, such as a single value like a width.
- After a change, check whether existing tests need updating or new coverage is warranted, scoped to that change.

## Versions and Upgrades

Keep toolchains and dependencies on the latest stable release unless that breaks something; prefer upgrading in one scoped change over mixing it with feature work.

- Keep one Go version. `go.work` and `apps/server/go.mod` carry the same `go` directive, and CI reads `apps/server/go.mod`; don't leave a lagging `toolchain` line. Update any doc that states the Go version (`apps/site/src/content/docs/`).
- Swift packages are locked by `Package.resolved` (`Package.swift` at the root and under `apps/Polymux`); the site uses `apps/site/package-lock.json`. Refresh the lock file with the upgrade, not separately.
- Read the release notes for each major or minor bump, then apply only the migration it requires. Drop deprecated APIs the upgrade obsoletes instead of leaving compatibility shims.
- Preserve behaviour. An upgrade should not change what the app does; a visible or behavioural difference is a bug to fix, or an intentional change to state explicitly and verify against the reference.
- Verify with the build plus existing tests and the E2E flows that cover the upgraded area (`scripts/check.sh`, `scripts/run-e2e.sh`). Say which checks you ran and which you could not.
- If latest can't be adopted, keep the reason next to the pin and report it rather than silently staying behind.
- In the report: versions before and after, breaking changes handled, and any behaviour left unverified.

## Documentation

Keep documents in `docs/` human- and developer-focused: simple, concise, and easy to scan. Prioritise helping readers quickly understand the repository, its structure, and how the code fits together. Include only the detail needed for that understanding; link to relevant code or focused references instead of duplicating implementation details or exhaustive specifications.

- Organise `docs/*.md` around key repository topics, not individual features, tasks, or implementation logs. Extend an existing topic guide before adding a new document.
- Keep `docs/README.md` as a short introduction, directory map, and table of contents. Keep its links current when documents are added, renamed, or removed; leave topic details in the linked guides.
- Keep the root `README.md` focused on the product overview, requirements, and getting started; link to `docs/` for developer topics.
- For authorised updates to either README, preserve its existing structure and scope. Make small, targeted changes; propose substantial restructuring rather than doing it silently.

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
- UI surfaces: a modal is presented over the window; a side panel is docked left (the sidebar) or right (profile, group chat) and becomes a page on iPhone. Use these terms for new views.

## File organisation

- When files are moved out of a folder and it's clear the folder is no longer needed, delete it. Don't leave empty folders behind.

## Build outputs

- Use existing scripts and output paths: `.build/` for caches, intermediate builds, and local staging; root `dist/` for app/server distribution artifacts; `apps/site/dist/` for the built website.
- Reuse stable build directories. Don't create per-run or per-commit build caches or extra artifact copies unless isolation requires them; clean up those temporary outputs when finished.
- Remove temporary logs, captures, and staging files created for the task once they are no longer needed for verification or the report. Preserve reusable caches and required distribution artifacts.
- Before deleting existing outputs, confirm they are generated, no longer needed, and not in use by another build, process, or agent. If ownership or purpose is unclear, report them rather than deleting them. Don't wipe `.build/` or `dist/` wholesale as routine cleanup.

## Repository safety

- Preserve unrelated worktree changes. Never run `git stash`.
- Don't push or make changes to `main` unless instructed
- Don't commit unless instructed
- Don't create worktrees unless instructed
- Don't make backups (copies, snapshots, `.bak` files, backup branches) unless explicitly instructed; suggesting one is fine
