package agentruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestWorkspaceFirstReply(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	d := Definition{Agent: contract.Agent{ID: "coding", Name: "Coding"}, Workspace: filepath.Join(home, "code", "Polymux"), Worktree: t.TempDir(), Scope: "chat", Intro: true}
	prompt, err := buildPrompt(d, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Working in ~/code/Polymux in a detached conversation checkout") || !strings.Contains(prompt, "Do not create or push another branch") {
		t.Fatal("first reply does not identify the repository and detached workflow")
	}
}
