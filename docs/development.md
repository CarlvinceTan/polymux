# Development delivery

The development channel follows `dev`; it is separate from public `app-v*` releases.
Client delivery is push-based but switched off until you opt in. Pushing `dev` always runs validation. With the repository variable `DEV_DELIVERY` set to `true`, the same push also builds the Mac Nightly and publishes it to the `dev-updates` feed. Setting `IOS_RUNNER` as well uploads the iPhone build to internal TestFlight from that self-hosted Mac. Without the variable, delivery stays manual (dispatch `dev.yml`).

To turn it on:

1. Register a self-hosted runner on the Mac signed in to Xcode with the FlareHQ account (Settings → Actions → Runners → New self-hosted runner), and give it a label such as `polymux-ios`. This replaces the App Store Connect API key until the Account Holder enables one.
2. In Settings → Secrets and variables → Actions → Variables, set `IOS_RUNNER` to that label and `DEV_DELIVERY` to `true`.
3. Keep `scripts/local/testflight-job.sh` stopped (`launchctl bootout gui/$(id -u)/com.flarehq.polymux.testflight`) so the poller and the runner don't upload the same commit twice.

The server follows `dev` through `scripts/deploy.sh`, which checks every 30 seconds and applies a passing commit through the safe drain below. Clients check for updates when the app opens, in addition to their scheduled checks.

Progress — November 1, 2026: build 1.0.1 was uploaded to internal TestFlight from the local Mac using the Xcode account (no API key). CI still needs the Account Holder to enable App Store Connect API access.

## Progress — October 25, 2026

TestFlight/CD setup is paused pending information about the Account Holder's Apple account and the updated Developer Program License Agreement. The signed-in Admin session offers “Contact Carlvince,” not an acceptance button. Apple says acceptance is required by October 29, 2026. No agreement was accepted by the agent.

Completed:

- Created the [Polymux app record](https://appstoreconnect.apple.com/apps/6826817136/distribution) for `com.flarehq.polymux.app`, with Limited Access. Organization-wide Admin/Finance/report access still applies; this is not hidden from those users.
- Created an internal TestFlight group, subsequently renamed **Internal**. Verified exactly one tester: the intended Carlvince Tan account. Automatic distribution for Xcode builds is enabled. There are no external testers or uploaded builds.
- Prepared `dev.yml`, `testflight.sh`, development build numbering, and a separate Mac development feed. These changes remain uncommitted/unpushed; no client update feed has been published.
- Created GitHub's `development` environment with a `dev`-only deployment branch policy. The new iOS/API signing secrets are not configured.
- Installed the revised server deploy script on the server Mac. The later personal installation updated/restarted the server and disabled the branch-polling launch job so it cannot replace personal builds. Re-enable that job deliberately before resuming CI-based deployment; the workflows still need to be pushed.
- Passed macOS/iOS simulator compilation, Developer ID signature verification for a local Mac development build, workflow lint, and 14 isolated deployment/version/TestFlight regression tests. Actual TestFlight delivery, notarization, and an installed Sparkle update remain unverified.
- Created the reusable `dev-delivery` skill in the separate cstack repository; it has not been installed into additional live agent configurations.

Next when resuming: resolve the agreement/account-holder step; configure the API key, Apple Distribution identity and App Store profile; upload the first internal-only build; verify installation and pairing; authorize the relevant commit/push before activating CI; then verify a second build updates each installed client. Encryption compliance and privacy-manifest auditing are still outstanding. The development agent/provider configuration is unfinished. The server drain protocol described below has since been added; first installation onto a server without that API still requires a separately arranged quiet bootstrap.

Direct personal installation is now available separately in `scripts/local/install.sh`, with local usage instructions in `scripts/local/usage.md`. The entire machine-specific `scripts/local/` directory, including its tests, is excluded with `.git/info/exclude`, not a shared `.gitignore` rule. It is not part of the public installation or TestFlight/CD workflow.

Verified personal installations: cvmac and wired iPhone clients **0.4.0 (1.0.3)**, and cvmini server **fa47bfe-local-20261025192053**. Mac signatures and installed binary identity passed; the phone's installed bundle version matched; authenticated server health returned the expected build identity and its process remained stable. Updates replace binaries without uninstalling/resetting data. Mac configuration, server configuration files, and the local authentication token had unchanged fingerprints across the update. Server chat/configuration table rows were unchanged, but those tables were empty before and after; the phone also had no existing Library user files, so nonempty phone-data retention has not been exercised. SQLite pairing database/WAL byte hashes changed during restart and do not establish logical record retention. Nine isolated personal-installer tests and the server CLI/store tests also passed. A transient launchd bootstrap failure was recovered; server installation now retries bootstrap a bounded number of times. Clients were not automatically launched, and pairing/provider configuration and a real cross-device chat are not yet verified. No public release, TestFlight upload, or commit/push was performed by this setup.

Direct development signing still depends on a valid Apple development profile and may require Developer Mode or a trust confirmation on the phone. The current phone profile includes HomeKit and expires October 25, 2027. The local Mac build is Developer ID signed, not notarized. Updates remain manual through the personal script while TestFlight/CD is paused.

## Pipeline

Manually dispatch `.github/workflows/dev.yml` on `dev` to run the reusable Tests workflow, then independently:

- archives and uploads an **internal-only** iOS build to TestFlight;
- signs and notarizes the Mac app, signs its Sparkle feed, and publishes it to the `dev-updates` GitHub prerelease.

Both clients use the existing `com.flarehq.polymux.app` identifier. The Mac development feed is:

```
https://github.com/CarlvinceTan/polymux/releases/download/dev-updates/appcast.xml
```

**Nightly:** `.github/workflows/nightly.yml` runs at 03:00 UTC and dispatches `dev.yml` on `dev` with `ios=false` only when `dev` has moved past the commit in the `dev-updates` release notes. Scheduled runs start from the default branch, so it takes effect once on `main`. The site's home page links the newest `Polymux-dev-*.zip` through `/api/nightly`, labelled unstable.

The repository and Mac prerelease downloads are public. TestFlight access is controlled separately by the internal tester group. Internal-only iOS builds cannot later be submitted to external TestFlight or the App Store.

All development jobs are serialized. `scripts/dev-version.py` sets the iOS build number to the `dev` commit count (shown as `0.4.0 (190)`), so local and CI uploads never collide; the script defaults to the same number. The Mac build number stays monotonic across workflow runs and reruns. Do not rewrite `dev` history or upload an arbitrary larger iOS build. Build `1.0.1` was the first manual upload; every later iOS build must exceed it.

Native validation and delivery use GitHub's [`xcode-27` preview runner](https://github.com/actions/runner-images/issues/14404) and select its latest stable Xcode for the required macOS/iOS 27 SDKs. The `macos-26` image only provides Xcode 26. Do not lower deployment targets or execute public PR code on the server to work around SDK availability.

## Crash logs

Release apps ship without local symbols. `scripts/build.sh macos` writes the matching dSYM to `dist/symbols/`, and CI publishes it beside each release and `dev-updates` build as `Polymux-<version>-<build>-arm64.dSYM.zip`. iOS symbols upload with each TestFlight build, so its crashes appear in Xcode's Organizer.

To read a Mac crash report, unzip the dSYM for the report's version and build, then run:

```bash
python3 "$(xcode-select -p)/../SharedFrameworks/CoreSymbolicationDT.framework/Versions/A/Resources/CrashSymbolicator.py" -d Polymux.app.dSYM -o symbolicated.ips crash.ips
```

## Credentials

Create a GitHub environment named `development`, restricted to the `dev` branch. The new iOS credentials belong in that environment, not in source or chat:

| Secret | Value |
| --- | --- |
| `ASC_PRIVATE_KEY` | Contents of the App Store Connect team API `.p8` key |
| `ASC_KEY_ID` | API key ID |
| `ASC_ISSUER_ID` | Team issuer ID |

Existing repository secrets supply `APPLE_TEAM_ID`, `MACOS_CERT_P12`, `MACOS_CERT_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, and `SPARKLE_PRIVATE_KEY`.

The iOS build uses automatic signing: with the API key, Xcode creates and manages the distribution certificate and profiles in the cloud. The key needs App Manager access, and only the Account Holder can enable API access for the team. Cloud signing in CI has not been exercised yet. Do not revoke a shared certificate or rotate the existing Sparkle key as part of setup.

## First iPhone installation

1. In App Store Connect, select the FlareHQ team and find/create the iOS app for `com.flarehq.polymux.app`.
2. Create a dedicated internal TestFlight group with only the intended tester. Enable automatic distribution for this group. Do not add all organization users or create a public link.
3. Sign in to Xcode (Settings → Accounts) with an Apple ID on the FlareHQ team. Signing is automatic, so no certificate or profile needs to be set up by hand.
4. Archive and upload. Add `ASC_KEY_PATH`, `ASC_KEY_ID` and `ASC_ISSUER_ID` to use an API key instead of the Xcode account:

   ```sh
      scripts/testflight.sh archive
   scripts/testflight.sh upload
   ```

5. Wait for processing. The owner must resolve encryption compliance based on the app's actual cryptography; no exemption is assumed. Audit privacy-manifest requirements before treating the build as upload-ready.
6. Install **from TestFlight**, enable Automatic Updates, pair with the server, and verify a real request/reply and relaunch. A USB development install is not proof of the TestFlight path. iOS applies TestFlight updates opportunistically, so a new build can wait hours on a phone nobody is using; opening TestFlight picks it up at once.

## First Mac installation

A stable-feed app does not automatically switch to the development feed. Install a signed, notarized development-channel build from `dev-updates` once the first workflow succeeds. Thereafter Sparkle uses the dev feed. Development builds default to hourly checks and automatic downloads; saved user preferences and Sparkle's installation rules still apply.

Verify a second build updates the same installed app through Sparkle. Switching back to stable may require an explicit reinstall because development build numbers can exceed stable ones. The stable feed is unchanged.

## Server deployment

### Remote connections

Publish the server through an HTTPS tunnel with a stable address. Clients use that
address directly; the tunnel runs on the server machine. Pairing still requires a
single-use invitation and every subsequent API request requires a device token.

```sh
polymux serve --public-url https://YOUR-HOST \
  --public-listen 127.0.0.1:7341
```

Route the tunnel to `http://127.0.0.1:7341`. This dedicated listener excludes local
proof endpoints. It must stay on loopback. Without `--public-url` it still runs,
preferring `127.0.0.1:7341` so a fixed tunnel route keeps working across restarts and
falling back to a free port if it is taken, for
[transport plugins](plugins.md#transports) such as Tailscale. `POLYMUX_PUBLIC_URL` and
`POLYMUX_PUBLIC_LISTEN` provide the same settings for a service manager.
New pairing links advertise the HTTPS address without an Iroh ticket. Existing
Iroh pairings remain usable until migrated; `--no-tunnel` disables that transport
once no paired clients need it.

- [OpenTunnel](https://github.com/anomalyco/opentunnel): use its native service
  with a dedicated profile and a persistent route. TLS terminates on the server
  machine, so its relay cannot read requests. The CLI manages reconnection and
  certificate renewal.
- [Cloudflare Tunnel](https://developers.cloudflare.com/tunnel/): use a named
  tunnel and a proxied DNS record. HTTPS terminates at Cloudflare. Keep Polymux's
  device authentication enabled; the hostname is not a credential.

The live streams send a heartbeat every 15 seconds. Clients time out after 45
seconds without data and reconnect using their saved event cursor.
Run the opt-in native pairing and stream checks against a throwaway `run.sh`
instance published through the tunnel:

```sh
POLYMUX_TEST_REMOTE=https://YOUR-TEST-HOST \
POLYMUX_TEST_TOKEN_FILE=/path/to/test/data/devices/local-token \
  scripts/test.sh swift --filter RemoteTests
```

Several comma-separated test addresses exercise the same server through multiple
providers. Never point these checks at a personal installation.

On the server Mac, run `scripts/deploy.sh setup` from a checkout once. The launch agent polls `dev` every 30 seconds from its own `~/.polymux/src` checkout. The **Server channel** in Settings → Updates picks what it follows: Nightly is `dev`, and Stable is the latest published release. Stable never moves a server backwards, so switching to it waits for the next release newer than the running build. Nightly requires a successful `server` check on the candidate commit and does not run pull-request code on the server.

- `~/.polymux/deploy/sha` records the last successfully applied commit, not failed CI candidates.
- If there are no server/build-script changes, it advances that marker without restarting.
- Build first, then acquire the server's owner-authenticated drain protocol. New messages stay queued, while admitted work finishes. `seal` closes mutations atomically before restart; a busy server waits rather than being force-stopped. Unsupported or cancelled drains defer installation without quarantining the commit.
- The candidate starts with admission still paused. Health requires an authenticated maintenance response with the expected build identity and the same PID across five samples; only then does the installer release queued work.
- A failed candidate is quarantined in `~/.polymux/deploy/failed-sha`; it is not retried on every pass. Binary rollback requires another successful safe-drain handshake, compatible data, and verified rollback health. If the candidate cannot answer or safety cannot be established, admission remains paused and recovery is manual.
- To retry a quarantined commit after diagnosing it, remove that marker deliberately and run `~/.local/bin/polymux-deploy`. Prefer pushing a fixed commit.
- Logs: `~/.polymux/logs/deploy.log` and `~/.polymux/logs/launchd.log`.

The existing installer takes data backups; the deployer retains the newest ten. Rollback restores **only the binary**, not data. Schema changes must remain backward compatible with the previous binary or have a separately approved recovery plan.

## Safe update handoff

After tests pass, an agent can hand a server update to an independent process:

```sh
scripts/update.sh
```

It builds and installs through the existing installer, with output in `.build/update.log`. The command returns immediately; the requesting agent must finish its turn rather than wait for the update that is waiting for it. Personal-installation routing is documented in `scripts/local/usage.md`.

`polymux drain --status` reports the state. To postpone an update, stop its updater first, then use `polymux drain --id ID --cancel` with the identity printed in its log. An abruptly killed updater can leave the server paused rather than silently reopening admission; a graceful cancellation before activation resumes the old server. The identity is also stored in the data directory's `maintenance` file; it is not a credential.

Draft text, mentions and reply context sync privately through `/threads/{id}/draft`.
Typing updates local state immediately; a 400 ms pause schedules the background
save. Visible chats check for changes once a second and wait until typing has
paused before applying another device's draft. Revision checks preserve both
copies when devices edit concurrently. Pending saves survive relaunch and reuse
their write identity after a lost response. Attachments remain local until sent.

### Maintenance protocol

Maintenance uses the normal authenticated API. All paired clients may read status; only the server owner credential may change it. Clients do not install or manage the server.

- `GET /maintenance` reads state without changing admission.
- `POST /maintenance` accepts `{"id":"<unique update identity>","action":"begin|seal|cancel"}`. The identity must be 16–128 bytes. A conflicting update identity receives HTTP 409; an unprivileged device receives HTTP 403.

Responses contain `state`, `active`, `blocked`, `buildSHA`, and `instance`. `instance` changes on every server start. `active` counts admitted operations, not agents; `blocked` is the persisted-work safety check populated by `seal`.

| State | Behaviour |
| --- | --- |
| `running` | Normal admission. |
| `draining` | No new queued jobs or background cycles start. Existing jobs and their asynchronous descendants finish. Messages persist as separate queued jobs, not steers. Approval answers and other writes remain available. New voice sessions, provider sign-ins, and MCP connection probes receive HTTP 503. |
| `ready` | No admitted operations or unsafe persisted runs remain. Mutations receive HTTP 503 and `Retry-After: 2`; reads and event streams remain available. The updater may stop this process. |

`begin` persists the update identity and closes job admission without cancelling or time-limiting existing work. `seal` atomically checks admitted operations and persisted running work before entering `ready`; otherwise it returns `draining`. `cancel` removes the marker and resumes admission, including queued messages. Actions may be repeated with the same identity until cancellation.

The protocol covers server-admitted jobs, voice, tools within those executions, background indexing/polling/delivery, provider sign-in, passkey work, and tracked asynchronous follow-up effects. MCP connection-status reads that perform a handshake are admitted operations, including implicit HEAD requests. It does not hot-swap executing code or supervise arbitrary detached processes. Existing execution deadlines still apply. Durable queued peer/child work resumes after activation; it need not start during draining.

Status is also included as `maintenance` in `/health`. Agents receive a pending-update notice on their next fresh-context model request; app update banners are not yet implemented.

### Activation

1. Build and verify the candidate before `begin`.
2. Poll `seal` with the same update identity and require the same `instance`. HTTP failure, a missing endpoint, or an empty queue snapshot is not permission to restart.
3. Stop the old process only after `ready` with zero `active` and `blocked` work.
4. Start the candidate on the same data. The persisted marker starts it in `ready`, holding queued work during health checks.
5. Check authenticated health/build identity and process stability, then `cancel` with the same update identity to release the queue.

An abandoned update stays paused rather than expiring. Before activation, the updater can cancel safely. After stopping the old server, failed health leaves admission paused for recovery. Binary rollback requires compatible data and its own safe handshake; an unreachable server is not assumed safe to replace.

The first update of an older server without `/maintenance` is deliberately refused. Arrange a quiet bootstrap separately; do not restore the old idle-snapshot fallback.

## Agent handoff

Keep a development agent's working checkout separate from `~/.polymux/src`, which the deployer checks out forcibly. Preserve the report skill's issue-confirmation step. Configure providers and select the coding agent deliberately; do not copy account credentials or grant automatic pushes/merges as a side effect of installing delivery scripts.

A turn runs until it finishes, is stopped, or the model goes silent for ten minutes. A running tool never counts as silence, so long builds are fine. If a message arrives once a command has run for 5 seconds, the command moves to the background so the bot can answer, and its result returns as a follow-up turn. Stop ends it. Background reviews stay limited to two minutes.

Keep SSH or another independent coding route available if the app/server fails. Commit/push and production activation are separate authorized steps, not implied by running local tests.

## Local checks

Enable the repository's Git hooks once per clone with `git config core.hooksPath .githooks`. The pre-commit hook allows commits only on `dev` (or a detached worktree) and runs `scripts/docs_check.py`, which fails when `README.md`, `AGENTS.md`, `DESIGN.md` or `docs/` link to or name files that no longer exist, or when `docs/README.md` omits a guide. The pre-push hook allows pushing only `dev`. Hooks are local, so CI repeats the docs check on every push and pull request.

Use the shared entrypoints rather than duplicating build commands:

| Command | Purpose |
| --- | --- |
| `scripts/test.sh server [args...]` | Go tests with the required SQLite tag. |
| `scripts/test.sh packages [args...]` | Standalone Go module tests with the workspace disabled. |
| `scripts/test.sh swift [args...]` | Swift package tests; filters and other arguments are forwarded. |
| `scripts/test.sh tools` | Script regression tests, without contacting deployment services. |
| `scripts/test.sh all` | All four suites; select an explicit suite to pass arguments. |
| `scripts/check.sh [macos\|ios\|all]` | Compile without launching. Shares the build lock with app runners and packaging. |
| `scripts/run.sh --server` | A server with throwaway data; also used by `make run` in `apps/server`. |
| `scripts/run.sh --fixture <flags>` | An isolated fixture app without building or launching the Go server. |
| `scripts/run.sh --fresh` | The app as a new install: no server is started, so it runs its own under a launchd job of that instance's. |
| `scripts/run-e2e.sh [flows...]` | Shared UI flows, driven by Cua. Fixture-only selections skip the Go server. A `// @fresh` line runs the flow on `run.sh --fresh`, which opens on the first-run welcome once its server is up. A `// @second-server` line starts another live server and gives the flow `SECOND_LINK`, a pairing link to it; `// @linked-job` links that server with the first and runs it under launchd. Every flow gets `ADDRESS`, the instance's own server, which works as a provider that refuses every model request. |
| `scripts/e2e.py [--changed [base]] [--jobs n] [--dry-run] [flows...]` | The fast way to run flows: builds once, starts the Cua daemon if it's down, gives each real-server flow a fresh server, and retries failures alone. It paces itself for throughput: it starts from the last run's pace, adds jobs while flows stay close to their best times, holds when they slow, drops after a failure and waits while the machine is busy. If nothing passes it stops early. `--jobs n` holds it at n. `--changed` picks flows from `e2e/areas.json`. |
| `scripts/sim.sh out.png [flags...]` | A fresh simulator and renamed app, deleted after capture. |

`scripts/run.sh --fixture --verify-load --verify-renders` seeds 72 chats and writes view update counts to `<data>/renders.txt`, to check that a gesture stays local to what it touches.

`e2e/intents.json` records behaviour that looks like a defect to Argus but is meant. Each entry names the sources behind it and goes stale when they change, at which point Argus reports the finding again. Update it in the same commit as the behaviour, with `argus intent` from the Argus repository.

`e2e/baselines/` holds the approved state of each screen in each Argus journey, one line per element, so a changed screen reads as a plain diff in review. Argus compares against it and fails on anything that appeared, vanished or moved. Runs never write it: after a wanted UI change, `argus baseline approve` records it with a reason, in the same commit as the change.

Build locks are held by the OS and survive until the owning build processes exit; an abandoned lock file does not block later builds. Keep the shared `.build/` caches. Stop owned runs by their recorded runner PID so their processes and data are cleaned up.

Simulator capture creates a device from an available iPhone hardware/runtime pair. `POLYMUX_SIM_DEVICE` selects that pair from an existing device without reusing its data; `POLYMUX_SIM_APP` selects a source bundle to copy. `SIM_WAIT` controls the post-launch delay (25 seconds by default); this is a settling delay, not a UI-readiness assertion. Delete captures after inspection. `--stop` is no longer needed because each capture removes its own device.

Outstanding manual checks include live theme-switch colour refresh, the scroll-to-latest pill's final appearance, and Quick Look file contents. Simulator pairing is not physical-iPhone coverage; cross-machine ticket pairing and restart recovery require separate native checks outside normal CI.

CI selects native jobs from changed inputs, including shared scripts/contracts. Site-only changes skip native compilation, while the server job still reports a successful check for the deployer's exact-commit requirement. Manual and release validation runs the full selection. Releases reuse the test and site workflows, and Sparkle signing uses the package resolved by the app build. Title validation has one workflow.

The remaining tooling work is targeted: add client-facing maintenance status; generator inputs should be pinned before regenerating reference assets; and the polling watcher should be replaced only if measured overhead warrants another dependency.

```sh
python3 -B scripts/dev-version_test.py
python3 -B scripts/deploy_test.py
python3 -B scripts/testflight_test.py  # macOS
scripts/check.sh all
```

The deployment tests use temporary HOME directories, fake CI/launchd, and disposable databases. They never install a crashing binary on the real server. TestFlight tests check the signing guards and internal-only export options without contacting Apple. Neither test suite proves real store delivery, notarization, or an installed update.
