---
name: pre-commit
description: Check and prepare Polymux changes for a commit when the user indicates commit-readiness intent, such as checking changes before committing. Reuse the audit skill over the intended commit scope. No exact skill name or slash command is required. Do not start merely because coding finished, and do not treat this workflow as permission to stage, commit, or push.
---

# Pre-commit

Establish whether the intended changes are ready to commit. This is an agent workflow, not a Git hook.

## Start from user intent

Use only when the user asks for pre-commit preparation or a readiness check before committing. Ordinary implementation, tests, or discussion of a future commit do not trigger this workflow. A request to actually commit may use this preparation, but any Git mutation still needs its own authorization and scope.

Read repository and local instructions. Preserve unrelated work and concurrent edits; do not stage everything, stash, reset, switch branches, create worktrees/backups, or repair another agent's changes.

## Prepare

1. Inspect branch and index state:
   ```sh
   git branch --show-current
   git status --short
   git diff --cached --stat
   git diff --cached --name-status
   ```
2. Default to the **staged diff**. If nothing is staged, or staged changes include unrelated work, ask which files/hunks belong to the intended commit. Do not silently switch to auditing the whole worktree or stage files yourself. If the user explicitly requests an unstaged pre-commit review, agree on its paths and label the result as a worktree review, not validation of the index.
3. Note partially staged files, deletions, generated files, and lockfiles. Confirm the proposed change is coherent and excludes accidental credentials, local data, captures, logs, and build outputs. Check permission-protected files against the user's request.
4. Read and follow [the audit skill](../audit/SKILL.md), using the scope agreed above. For the default staged scope, run its helper with `--staged` and run `git diff --cached --check`. Do not copy the audit checklist here or broaden its scope.
5. Review test evidence carefully: builds/tests use worktree sources. Check all relevant inputs, including unstaged/untracked dependency files, before attributing results to the staged candidate. Record any mismatch as a verification gap rather than passing the index on the strength of different source content.
6. If requested, suggest a concise commit title or a split into coherent commits. Do not stage, commit, push, or amend anything as part of preparation alone.

## Report

- **Scope:** staged candidate or explicitly agreed worktree paths; unrelated work excluded.
- **Audit:** confirmed blockers and unresolved review candidates, with evidence.
- **Checks:** passed, failed, and not checked; identify source/index mismatches.
- **Readiness:** ready in the reviewed scope, blocked, or incomplete.
- **Next step:** fixes or authorizations still needed; optional suggested commit title.

Recheck affected content after requested fixes or staging changes. Do not claim a commit exists unless the user separately authorized it and it actually succeeded.
