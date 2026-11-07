package testenv

import (
	"path/filepath"
	"testing"
)

func PiG(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PIG_HOME", root)
	t.Setenv("PIG_USE_PI_DIRS", "")
	t.Setenv("PIG_CODING_AGENT_DIR", filepath.Join(root, "agent"))
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PIG_CODING_AGENT_SESSION_DIR", "")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	t.Setenv("PIG_TEST_FAUX", "")
}
