# Experiments

Experimental work lives on `experimental/*` branches so `dev` and `main` stay free of it. The commit guard only allows commits on `dev` or a detached HEAD, so work on a branch in a detached worktree and move the branch to your commit with `git branch -f experimental/<name> HEAD`. Nothing here ships until it is deliberately merged.

| Branch | About |
| --- | --- |
| `experimental/tauri` | A work-in-progress Tauri port of the app. |
| `experimental/kmp` | A Kotlin Multiplatform client and shared Compose Multiplatform UI in `apps/kmp/` for Android, iOS, macOS, Windows and Linux, generated from `contracts/`. |
| `experimental/dreaming` | Ambient ingestion through an attention filter, short-lived impressions, and dreaming that turns goals and references into ideas, replayed over past repository days. See [Proactivity](proactivity.md). |
| `experimental/canvas` | The work map reworked as one canvas with two app-drawn views of the same work: a graph and a board. |

Add a row when you start a new experimental branch, and remove it when the branch is merged or deleted.
