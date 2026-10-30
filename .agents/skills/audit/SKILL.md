---
name: audit
description: Audit Polymux changes using fast programmatic checks followed by contextual review. Use only when the user indicates they want an audit or readiness review, or as the shared audit step of a user-requested pre-commit or pre-release workflow. Natural-language intent is enough; no exact skill name or slash command is required. Do not start this workflow merely because ordinary coding or testing has finished.
compatibility: Fast checks require Git and Python 3; deeper checks use the repository toolchains.
---

# Audit

Find actionable problems in the requested scope, with evidence. Fast checks reduce repetitive inspection; they do not establish correctness or replace review.

## User intent and safety

- Start only on the user intent described above. A request to create or edit this skill is not itself a request to audit the project.
- A user-requested `pre-commit` or `pre-release` workflow may delegate its audit here without another confirmation. Keep the parent's scope.
- Read the repository's `AGENTS.md`, `AGENTS.local.md` if present, and applicable nested instructions. Read `DESIGN.md` and the UI skills for UI review; use the relevant Go/Svelte skills for those areas.
- Audit without modifying source, staging files, committing, stashing, switching branches, making worktrees/backups, merging, tagging, pushing, deploying, or publishing. Findings do not authorize fixes. Explicitly requested fixes are a separate implementation step, followed by focused rechecks.
- Preserve concurrent work. List recent changes as required by `AGENTS.md`, re-read changing files, and report another agent's in-progress breakage instead of repairing it.
- Never print credentials, private-key contents, or raw sensitive source in reports. Use paths, line numbers, rule IDs, and redacted descriptions.

## 1. Fix the scope

State what is being audited before checks. Ask if user intent is ambiguous.

| Scope | Content |
| --- | --- |
| Staged | The index, including partially staged files; not their unstaged versions. |
| Worktree | Net tracked changes against `HEAD`, plus nonignored untracked files. Limit paths to the user's work when unrelated changes exist. |
| Range | Two pinned commits, `BASE..HEAD`; no local edits. Choose the baseline explicitly rather than silently calculating a merge base. |

Inspect the selected diff and surrounding code. Identify affected components, risky boundaries, and tests. Do not expand to a repository-wide audit unless requested. Record exact commit IDs for ranges; rerun if the selected content changes.

## 2. Run cheap checks first

Read [the check guide](references/checks.md). Resolve `scripts/check.py` relative to this skill directory and invoke it by absolute path, with the working directory inside the checkout:

```sh
AUDIT="$(git rev-parse --show-toplevel)/.agents/skills/audit"
python3 -B "$AUDIT/scripts/check.py" --staged
python3 -B "$AUDIT/scripts/check.py" --worktree -- apps/server
python3 -B "$AUDIT/scripts/check.py" --base BASE --head CANDIDATE
```

Choose **one** invocation matching the requested scope; replace placeholder refs. Optional paths are literal repository-relative files or directories, not glob patterns.

- The helper inspects selected added lines and snapshot-aware Go directives. It does not change files or run builds/tests.
- `FAIL` means a concrete fast-check problem. `REVIEW` is a candidate to inspect, not a proven violation. Exit 0 can still include unresolved review candidates; exit 1 means failures; exit 2 means the check is unverified.
- Run the matching `git diff --check` command too; it covers Git's configured patch-hygiene rules beyond the helper's trailing-whitespace check.
- Use targeted `rg` searches for other applicable rules, tracing each search back to the current project instructions. Review full matches in context. Search results in unchanged code are not newly introduced defects by default.

## 3. Verify affected behaviour

Use existing tools from the check guide, cheapest relevant checks first. Do not install or upgrade dependencies, contact paid providers, or run credentialed/deploying checks as hidden setup.

Builds and tests normally read the working tree, **not** the staged or historical snapshot. Only claim they validate the selected candidate when their relevant inputs match it. Otherwise report the mismatch and what remains unverified; do not stash, check out another revision, or create a worktree to hide it. Ask before arranging another isolated checkout.

Reuse earlier evidence only when its inputs and environment still match. Run relevant tests/builds/E2E, not every possible check for every change. Follow the repository's isolated-instance and background-focus rules for app checks; never use personal data or installations as fixtures. Shut down owned instances and remove owned temporary outputs afterward.

## 4. Review what checks cannot prove

- Correctness and regressions across callers, error/cancellation paths, retries, concurrency, and persistence.
- Authentication/authorization, untrusted input, secrets, and privacy boundaries.
- Data migrations, compatibility, retention, and recovery when storage or protocols change.
- Real UI behaviour, accessibility, offline state, responsiveness, and relevant design rules.
- Tests that exercise meaningful behaviour, plus documentation/contracts that must move with the change.
- For upgrades: before/after versions, required migrations/release notes, and behavioural parity.

Do not turn personal style preferences or speculative grep hits into blockers. Explain a plausible failure path for every correctness finding.

## Report

Keep it concise:

1. **Scope:** refs or selected paths; staged/worktree distinctions.
2. **Findings:** severity, file/line, evidence or source rule, impact, and suggested action. Separate confirmed findings from unresolved candidates and optional suggestions.
3. **Verification:** passed, failed, and not checked, with commands and snapshot limitations.
4. **Verdict:** no findings in the audited scope, blocked by findings, or incomplete because of verification gaps.

Never equate a clean grep, exit 0, or successful build with a complete audit. If invoked by another skill, return this evidence to its readiness report instead of duplicating the workflow.
