package agentruntime

import (
	"strings"
	"testing"
)

func TestWorkspaceWithoutCheckout(t *testing.T) {
	prompt, err := buildPrompt(Definition{Workspace: t.TempDir(), Ephemeral: true}, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range []string{"detached conversation checkout", "This conversation's checkout"} {
		if strings.Contains(prompt, claim) {
			t.Errorf("prompt claims an unprepared workspace: %q", claim)
		}
	}
}
