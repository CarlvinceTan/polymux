# Development and delivery

## Local checks

Use the shared entrypoints rather than duplicating build commands:

| Command | Purpose |
| --- | --- |
| `scripts/run.sh` | A Tauri app and server on throwaway data. |
| `scripts/run.sh --server` | A server only, on throwaway data. |
| `scripts/run.sh --verify-server` | A Tauri app on the isolated fixture server. |
| `scripts/check.sh` | Compile the frontend and Tauri backend without launching. |
| `scripts/test.sh server [args...]` | Go tests with the required SQLite tag. |
| `scripts/test.sh packages [args...]` | Standalone Go module tests with the workspace disabled. |
| `scripts/test.sh tauri` | Tauri API tests. |
| `scripts/test.sh tools` | Script regression tests, without contacting deployment services. |
| `scripts/test.sh all` | All suites; select an explicit suite to pass arguments. |
| `scripts/run-e2e.sh [--verify-server] [flows...]` | Tauri UI flows on an isolated app; fixture flows use `--verify-server`. |

Browser tests live in `apps/tauri/tests` and run with `npm run test:ui --prefix apps/tauri -- --browser=chromium` (or `webkit`). Build locks are held by the OS and survive until the owning build processes exit; an abandoned lock file does not block later builds. Keep the shared `.build/` caches. Stop owned runs by their recorded runner PID so their processes and data are cleaned up.

## CI

- `.github/workflows/tauri.yml` checks the frontend, API tests, both browser engines, and the Rust backend.
- `.github/workflows/tests.yml` checks the Go server, packages, contracts, scripts, and shell/workflow syntax.
- `.github/workflows/site.yml` checks the website.
- `scripts/ci.py` selects affected jobs from changed paths. Shared scripts, contracts, and workflows run both the server and Tauri checks.

## Releases

`app-v*` tags run `.github/workflows/release.yml`. It reuses the test and Tauri workflows, builds `apps/tauri` with Tauri, signs and notarizes the DMG, uploads the artifact, and publishes the GitHub release. Workflow dispatch is a dry run that uploads the artifact without publishing.

The release version comes from `apps/tauri/src-tauri/tauri.conf.json`; the tag must be `app-v<version>`. A tag release requires these repository secrets:

- `MACOS_CERT_P12`, `MACOS_CERT_PASSWORD`
- `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`
- `TAURI_SIGNING_PRIVATE_KEY` and, if the key is encrypted, `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`

Signing imports the Developer ID identity into a temporary keychain; Tauri signs the app and DMG. The app and DMG are notarized with `notarytool` and stapled before upload. The workflow then creates a signed updater tarball, writes `latest.json`, and updates the `updates` release. The app checks `https://github.com/CarlvinceTan/polymux/releases/download/updates/latest.json`, verifies the signature, installs the update, and relaunches. The public key lives in `apps/tauri/src-tauri/tauri.conf.json`; keep the private key out of the repository and in the GitHub secret.

## Server installation

`scripts/install.sh [server|app|both]` installs the Go server and the Tauri app. `scripts/uninstall.sh` removes them and their data. `scripts/update.sh` hands a server update to an independent process. These commands are separate from local development runs and never touch an isolated `scripts/run.sh` instance.
