package agentruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestWorkspacePrompt(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	d := Definition{Agent: contract.Agent{ID: "coding", Name: "Coding"}, Workspace: filepath.Join(home, "code", "Polymux"), Worktree: t.TempDir(), Scope: "chat", Intro: true}
	prompt, err := buildPrompt(d, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Start your first reply") {
		t.Fatal("prompt scripts the first reply")
	}
	if !strings.Contains(prompt, "working in ~/code/Polymux in a detached conversation checkout") || !strings.Contains(prompt, "Do not create or push another branch") {
		t.Fatal("prompt does not identify the repository and detached workflow")
	}
}
