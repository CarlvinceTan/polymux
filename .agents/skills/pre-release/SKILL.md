---
name: pre-release
description: Prepare Polymux for dev-to-main promotion and release when the user indicates release-readiness intent, such as getting a version ready to ship. Reuse audit over the full release diff, prepare changelog/version metadata, and check merge and distribution prerequisites. No exact skill name or slash command is required. Do not start during ordinary coding or a passing mention of releases; preparation does not authorize merging, tagging, pushing, deploying, or publishing.
---

# Pre-release

Prepare a verifiable release candidate, not just release notes. Keep preparation, merge readiness, and actual publication separate.

## 1. Establish the candidate

Read repository/local instructions, [development delivery](../../../docs/development.md), and the current workflows: `.github/workflows/branch-topology.yml`, `tests.yml`, `site.yml`, `dev.yml`, and `release.yml` at the repository root. They are the source of truth; do not assume local workflow edits have been committed or activated remotely.

Confirm with the user where not already specified:

- Target version and channel: native public release, development delivery, or another explicitly requested target.
- Candidate branch/commit and intended components/platforms.
- Last applicable released baseline. Do not mix historical Electron `v*` releases, native `app-v*` releases, and the moving `dev-updates` prerelease. If this is the first native release, agree on a meaningful baseline rather than picking an unrelated tag.
- Whether to edit release notes/version metadata or only assess readiness. A request to prepare a named release can authorize those scoped preparation edits, but not unrelated code fixes or protected-file changes. Ask if intent is ambiguous.

Inspect the current status and refs without switching branches. Pin commit IDs. Separate the **promotion diff** (`main` to the candidate, using the relevant merge base) from the **release diff** (previous release baseline to the candidate); they can differ. Explain stale/missing remote refs and ask before a fetch if current remote state is needed. Do not claim current CI/remote readiness from local refs alone.

Uncommitted changes are not part of a commit-range audit. If they belong in the planned release, identify their exact paths and audit them additionally as worktree changes; report that the candidate is not yet fully committed. Never hide unrelated work by staging, stashing, resetting, making a worktree/backup, or switching revisions.

## 2. Audit the whole release

Read and follow [the audit skill](../audit/SKILL.md). Use its range helper with the agreed release baseline and candidate, not just the most recent commit or staged diff. Include the agreed uncommitted preparation changes separately.

Cover cross-component compatibility, schema/data migrations, retention and recovery, authentication/security, public contracts, dependency/toolchain upgrades, and meaningful tests. A binary rollback is not a data rollback; identify any recovery plan needed for incompatible changes.

Select the broader relevant checks from the current CI and release workflows, including server race tests, contract freshness, Tauri checks, platform builds, site verification, and E2E flows for changed behaviour. Apply the audit skill's snapshot rules: working-tree tests do not prove a different historical/staged candidate. Reuse valid earlier evidence rather than rerunning unaffected checks.

## 3. Prepare release content

Use `apps/site/src/content/releases/<version>.md` as the changelog source; GitHub notes and the native update feed derive from it. Read the current content parser (`apps/site/lib/content/releases.ts`) and examples before editing. Do not create a second changelog or rewrite historical releases.

- Derive user-facing changes from the actual release diff, history, issues, and verified behaviour—not commit titles alone. Group meaningful changes; omit internal noise and unsupported feature claims.
- State breaking changes, migration/action requirements, and known limitations when relevant. Do not invent a release date or present pending delivery as completed.
- Check filename, frontmatter version, title, summary, date, publication/download status, historical status, and native tag together. Drafts may remain unpublished while blocked; call out the metadata changes still required before publication.
- For a public native release, check every applicable `MARKETING_VERSION`, the expected `app-v<version>` tag, build numbering, and release-note metadata. The current workflow requires `downloadable: true`, a matching native tag, and published notes. Historical entries must not become downloadable.
- Read existing versioning/build scripts before changing numbers. Development-channel build numbering and feeds are not stable-release numbering or feeds.
- Run the site's existing verification for changed release content. Re-audit the resulting notes/version diff; only authorized preparation files should change.

## 4. Check promotion and distribution readiness

- Confirm the current branch policy permits the intended promotion; this repository currently permits only same-repository `dev → main` pull requests. Check conflicts and PR/CI status against the exact candidate. Do not open or merge a PR without instruction. If requested, draft its body using `.github/pull_request_template.md` and respect title checks.
- Native public `app-v<version>` tags must eventually point at a commit on `main` whose version/release content matches. Before merge, report this as a required next step, not a passed condition. Do not create the tag.
- Check runner/SDK availability, signing/notarization requirements, Sparkle key/feed configuration, release assets, checksums, website/download routing, and declared platform support against the current release workflow. Inspect credential availability only through safe metadata; never dump secret values or assume named secrets are valid.
- Keep paused TestFlight/development delivery separate. Read current blockers in `docs/development.md`; do not resolve account agreements, upload builds, change delivery controls, or install personal builds as hidden release setup.
- A workflow called a dry run may still import signing credentials, contact Apple, notarize, or upload artifacts. Do not dispatch it without authorization. Local script/unit success does not prove notarization, installation, live downloads, or an installed update path.
- Distinguish pre-release gates from post-publication verification. List how the eventual authorized release will verify the published notes, downloadable assets/checksums, update feed, and real installation/update path. Do not mark those checks passed before they occur.

## Report

Give a concise handoff:

1. **Candidate:** version/channel, exact baseline/candidate IDs, promotion target, and any uncommitted preparation changes.
2. **Prepared:** notes/version files changed, or proposed edits in report-only mode.
3. **Audit and checks:** confirmed blockers, unresolved candidates, and passed/failed/not-checked evidence.
4. **Readiness:** separately state ready/blocked/incomplete for promotion and for publication.
5. **Next authorized steps:** remaining fixes, commit/PR/merge/tag actions, external checks, and post-release verification.

Stop after preparation. This skill does not grant permission to commit, push, merge, tag, upload, deploy, publish, change production configuration, or touch installed app/server data.
