# Test runtime isolation

| Rule | Enforcer | Evidence | Status |
| --- | --- | --- | --- |
| PiG tests must not use inherited config/auth/session directories | `PiG(t)` in `pig.go`, used by runtime and real-provider server tests; `TestIsolation` in `pig_test.go` | PiG v0.4.1 honors `PIG_USE_PI_DIRS` before `PIG_HOME`; a temporary Polymux data directory alone does not isolate its global services | Verified 2026-10-09 with `go test -race -tags sqlite_fts5 github.com/CarlvinceTan/polymux/apps/...` |

The regression uses a dummy inherited directory, never a user's auth data.
Isolation belongs at the test runtime boundary rather than in agent reminders.
This guard covers the current real PiG consumers, not future tests that omit it.
