# Audit checks

Run from the repository root. Read the current rules and scripts before relying on this guide; they remain the source of truth. This is a small set of fast checks, not a replacement rulebook or general linter.

## Bundled helper

`scripts/check.py` is relative to the audit skill directory. It uses Git and Python's standard library, with no installation or network access. It scans added lines, including all lines of nonignored untracked text files in worktree mode. Deleted lines are not scanned; renames are treated as delete/add. Context-dependent matches can occur in comments and fixtures.

| Rule ID | Result | Source / interpretation |
| --- | --- | --- |
| `whitespace` | FAIL | Trailing spaces/tabs on added lines. Run `git diff --check` for the complete configured Git rules. |
| `conflict`, `unmerged` | FAIL | Apparent opening/closing conflict markers or unresolved index entries. |
| `go-version` | FAIL | When either Go version file is in scope, compare the selected snapshots of `go.work` and `apps/server/go.mod`; missing/invalid directives in present files also fail. `AGENTS.md`, Versions and Upgrades. |
| `go-toolchain` | FAIL or REVIEW | A lagging toolchain fails; other explicit toolchain pins need review against the one-version rule. `AGENTS.md`, Versions and Upgrades. |
| `lockfile` | REVIEW | A manifest changed without its paired lockfile in scope. Determine whether dependencies actually changed; metadata-only edits need not change the lock. `AGENTS.md`, Versions and Upgrades. |
| `permission` | REVIEW | Changes to root `AGENTS.md`, root `DESIGN.md`, or any `README.md` require checking the user's authorization. `AGENTS.md`, Working on Polymux. |
| `fixed-size` | REVIEW | Swift `.fixedSize(...)`: prohibited on text in flexible rows/panels, not everywhere. `DESIGN.md`, Text. |
| `alert`, `retry-button` | REVIEW | Swift alert APIs and simple `Button("Retry")` declarations. Check actual purpose and context. `DESIGN.md`, Status and errors. |
| `private-key` | REVIEW | Private-key headers may indicate exposed secrets or intentional examples/fixtures. No source contents are printed. Security review, not an exhaustive credential scanner. |

Exit codes: **0** no definite fast-check failures (review candidates may remain), **1** failures found, **2** scope/tool/read errors, so results are unverified. Binary content and symlink targets are not searched. Other spellings, multiline syntax, indirect APIs, and contextual violations still need review. Worktree mode requires an existing `HEAD`; staged mode also supports a first commit.

Verify helper changes with:

```sh
python3 -B .agents/skills/audit/scripts/check_test.py
```

Tests use disposable Git repositories, never the real index or app data.

## Existing tools

Select commands according to the scope and affected components. Do not run this whole table blindly. For ranges, replace `BASE` and `CANDIDATE` with the chosen pinned commits. For scoped checks, pass the same paths after `--` where supported.

| Area | Checks |
| --- | --- |
| Patch hygiene | Staged: `git diff --cached --check`; worktree: `git diff HEAD --check`; range: `git diff BASE CANDIDATE --check`. These do not include untracked files; the helper covers their trailing whitespace. |
| Go formatting | `gofmt -l` on affected Go files. For staged content, pipe `git show ':path/to/file.go'` to `gofmt -d`; for a range use `git show 'CANDIDATE:path/to/file.go'`. Inspect locally and report paths, not sensitive diff contents. Never use `gofmt -w` during an audit. |
| Go vet/tests | `(cd apps/server && go vet -tags sqlite_fts5 ./...)`; use `scripts/test.sh server` with the affected packages first. Broader release coverage: `scripts/test.sh server -race -count=1 -timeout=600s ./...`. These run worktree sources. |
| Server fixture | `(cd apps/server && go test -tags 'sqlite_fts5 verifyfixture' -count=1 ./internal/fixture)` when fixture behaviour changes. |
| Contracts | `python3 contracts/generate.py --check` when contracts or generated clients change; do not regenerate during a read-only audit. |
| Swift packages | `scripts/test.sh swift`, or the relevant Swift test filter. Check `Package.resolved` alongside manifest/dependency changes. |
| App compilation | `scripts/check.sh macos`, `ios`, or `all`, as affected. A build does not prove UI behaviour. |
| App behaviour | `scripts/run-e2e.sh` with affected `e2e/flows/*.js`; manual checks use an owned background `scripts/run.sh` instance and the `cua-driver` skill. Follow `AGENTS.md` isolation/cleanup rules. |
| Website | `npm run verify --prefix apps/site` for site/release content changes: tests, type checks, and build. Do not reinstall packages or run this concurrently with another agent's site build without coordinating shared outputs. |
| Delivery scripts | Relevant `python3 -B scripts/dev-version_test.py`, `scripts/deploy_test.py`, `scripts/testflight_test.py`, and `scripts/e2e_test.py`. Tests are not proof of actual delivery. |
| Workflow YAML | If installed, `actionlint` on affected workflows, respecting `.github/actionlint.yaml`. Compare with the current CI jobs. Do not dispatch workflows during an audit. |

The current CI definitions in `.github/workflows/tests.yml`, `site.yml`, and `release.yml` define broader gates. Read them for release preparation instead of freezing their contents into this guide. Actual notarization, TestFlight delivery, Sparkle updates, and production downloads require separate authorized verification.

## Targeted searches

Use `rg -n` over selected source files for additional checks that are relevant to the diff. Examples for Swift UI review:

```sh
rg -n '\.fixedSize\s*\(|\.alert\s*\(|\bNSAlert\s*\(' -- path/to/ChangedView.swift
rg -n 'Retry|Button\s*\(' -- path/to/ChangedView.swift
```

Those searches inspect **working-tree files**, including unchanged lines. Use `git show ':path'` or `git show 'CANDIDATE:path'` piped to `rg -n` when reviewing another snapshot. A no-match exit code of 1 is normal for `rg`; a tool error is not a pass.

Keep new checks narrow: identify the authoritative rule, choose the right snapshot and file types, distinguish definite failures from contextual candidates, and add a regression case for false positives and partial staging when extending the helper. Never add a broad ban merely because a pattern is easy to grep.
