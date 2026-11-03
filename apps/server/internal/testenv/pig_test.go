package testenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MichaelKinsy/PiG/coding"
)

func TestIsolation(t *testing.T) {
	foreign := filepath.Join(t.TempDir(), "foreign-config")
	t.Setenv("PIG_USE_PI_DIRS", "1")
	t.Setenv("PI_CODING_AGENT_DIR", foreign)
	if coding.DefaultAgentDir() != foreign {
		t.Fatal("fixture did not reproduce inherited override")
	}
	PiG(t)
	expected := filepath.Join(os.Getenv("PIG_HOME"), "agent")
	if actual := coding.DefaultAgentDir(); actual != expected || actual == foreign {
		t.Fatalf("runtime escaped isolated config: %s", actual)
	}
}
