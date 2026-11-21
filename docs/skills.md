# Project skills

Skills are task-specific instructions for coding agents working in this repository. Some also include helper scripts. They are development tools—not the skills users attach to their agents inside Polymux.

Each skill lives in [`.agents/skills/`](../.agents/skills/) and starts with a `SKILL.md` describing when to use it and what to do. Read the relevant skills alongside [`AGENTS.md`](../AGENTS.md); they don't replace repository rules.

## Audits and readiness

These workflows start when the user indicates the relevant intent; no exact skill name or slash command is required. Ordinary coding does not trigger them. They remain model-invokable, and preparation alone does not authorize Git mutations or publication.

| Skill | What it's for |
| --- | --- |
| [audit](../.agents/skills/audit/SKILL.md) | Shared, read-only audit: fast diff-scoped rule checks, existing verification tools, and contextual review. Includes a [check guide](../.agents/skills/audit/references/checks.md). |
| [pre-commit](../.agents/skills/pre-commit/SKILL.md) | Commit readiness for the intended changes, using `audit`; staged by default, without staging or committing. |
| [pre-release](../.agents/skills/pre-release/SKILL.md) | Full-release auditing, changelog/version preparation, and `dev → main` promotion and distribution readiness; no merging or publishing. |

## Design and UI tools

| Skill | What it's for |
| --- | --- |
| [cdesign](../.agents/skills/cdesign/SKILL.md) | General UI rules: responsiveness, layout balance, native platform fit, generic AI patterns to reject, text, accessibility, and contrast. Use with [`DESIGN.md`](../DESIGN.md), which holds Polymux-specific policies. |
| [avatar-preview](../.agents/skills/avatar-preview/SKILL.md) | Animated GIF previews using the real avatar renderer. Use to review avatars, emotions, motion and state effects without launching the app. |
| [cua-driver](../.agents/skills/cua-driver/SKILL.md) | Operating and verifying real native app UI. This is the current driver for manual checks and E2E flows. |

## Website

Use both when creating, editing, or reviewing Svelte components and modules in `apps/site/`.

| Skill | What it's for |
| --- | --- |
| [svelte-code-writer](../.agents/skills/svelte-code-writer/SKILL.md) | Looking up current Svelte documentation and checking code with the Svelte autofixer. |
| [svelte-core-bestpractices](../.agents/skills/svelte-core-bestpractices/SKILL.md) | Svelte reactivity, state, events, styling, and library integration. |

Use these when the task calls for them.

| Skill | What it's for |
| --- | --- |
| [mobile-native](../.agents/skills/mobile-native/SKILL.md) | Web fixes for phones, such as sticky hover and input zoom. For the website only; the app is native. |
| [agent-browser](../.agents/skills/agent-browser/SKILL.md) | Exploratory browser checks, using your own `--session`. It drives Chrome, so regression coverage stays in `apps/site/test/`, which also runs WebKit. Native app UI uses cua-driver. Needs the CLI; its `skills get core` command serves the guide for the installed version. |

## Go server

Start with **golang-how-to** for Go work. It identifies the relevant topic skills; use more than one when the task spans several areas.

| Skill | What it's for |
| --- | --- |
| [golang-how-to](../.agents/skills/golang-how-to/SKILL.md) | Choosing and combining Go skills for a task. |
| [golang-code-style](../.agents/skills/golang-code-style/SKILL.md) | Clear control flow, formatting, variable declarations, and comments. |
| [golang-naming](../.agents/skills/golang-naming/SKILL.md) | Naming packages, types, functions, variables, and tests. |
| [golang-data-structures](../.agents/skills/golang-data-structures/SKILL.md) | Choosing and using slices, maps, collections, pointers, and copy semantics. |
| [golang-database](../.agents/skills/golang-database/SKILL.md) | Database queries, transactions, connection pools, and database testing. |
| [golang-design-patterns](../.agents/skills/golang-design-patterns/SKILL.md) | API construction, architecture, resource lifecycles, shutdown, and retries. |
| [golang-error-handling](../.agents/skills/golang-error-handling/SKILL.md) | Creating, wrapping, inspecting, and logging errors. |
| [golang-safety](../.agents/skills/golang-safety/SKILL.md) | Preventing accidental bugs such as nil panics, aliasing, and unsafe conversions. |
| [golang-security](../.agents/skills/golang-security/SKILL.md) | Preventing exploitable bugs involving input, secrets, authentication, files, and networks. |
| [golang-testing](../.agents/skills/golang-testing/SKILL.md) | Tests, fixtures, mocks, fuzzing, and coverage. |
| [golang-troubleshooting](../.agents/skills/golang-troubleshooting/SKILL.md) | Finding the root cause of crashes, races, deadlocks, and unexpected behaviour. |
| [golang-refactoring](../.agents/skills/golang-refactoring/SKILL.md) | Restructuring code while preserving behaviour. |
| [golang-modernize](../.agents/skills/golang-modernize/SKILL.md) | Adopting newer Go features and replacing deprecated patterns. |
| [golang-documentation](../.agents/skills/golang-documentation/SKILL.md) | Go documentation, API comments, and runnable examples. |

## Where to make changes

- Edit the skill's own `SKILL.md` for its instructions; keep this page as a short index.
- [`.claude/skills`](../.claude/skills) links to `.agents/skills/`, so there is no second copy to maintain.
- [`skills-lock.json`](../skills-lock.json) records sources and hashes for imported skills. Repository-created skills such as `cdesign` and `avatar-preview` live directly in the checkout.

The **Skills up to date** CI check reports available updates on pull requests and pushes to `dev` or `main`, respecting pinned refs. Updates and check errors produce warnings while the check passes; details appear in its job summary. It does not update skills or the lock file.
