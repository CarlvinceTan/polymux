package agentruntime

import (
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
)

func TestRoutineModeGuidance(t *testing.T) {
	prompt, err := buildPrompt(Definition{}, []string{"manage_routines"}, false)
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := skills.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	policy := strings.ReplaceAll(prompt, bundled, "")
	for _, mode := range []string{"crons", "watch", "timezone", "pause", "resume"} {
		if !strings.Contains(policy, mode) {
			t.Errorf("routine guidance omits supported mode/action %q", mode)
		}
	}
	if strings.Contains(policy, "with an exact UTC start, fixed elapsed-time interval, and prompt") {
		t.Error("routine guidance requires interval arguments for every mode")
	}
}

func TestWorkspaceWithoutCheckout(t *testing.T) {
	prompt, err := buildPrompt(Definition{Workspace: t.TempDir(), Ephemeral: true}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range []string{"detached conversation checkout", "This conversation's checkout"} {
		if strings.Contains(prompt, claim) {
			t.Errorf("prompt claims an unprepared workspace: %q", claim)
		}
	}
}
