# Experiments

Experimental work lives on `experimental/*` branches so `dev` and `main` stay free of it. Check out the branch to work on it; nothing here ships until it is deliberately merged.

| Branch | About |
| --- | --- |
| `experimental/plugins` | A plugin system: versioned packages from a JSON registry (`plugins/index.json`), a server-side plugin host, native plugin screens, and the Hub plugin that adopts a running Matrix Hub. Removed from `dev` in "Move plugins to experimental/plugins". |
| `experimental/tauri` | A work-in-progress Tauri port of the app. |
| `experimental/kmp` | A Kotlin Multiplatform client and shared Compose Multiplatform UI in `apps/kmp/` for Android, iOS, macOS, Windows and Linux, generated from `contracts/`. |

Add a row when you start a new experimental branch, and remove it when the branch is merged or deleted.
