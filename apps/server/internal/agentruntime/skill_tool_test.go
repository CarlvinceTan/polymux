package agentruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
)

func TestReadSkillServesBundledAndConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crm", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: crm\ndescription: Log calls\n---\nUse the CRM."), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := &skillTool{configured: []string{path}}
	for name, marker := range map[string]string{"report": skills.IssueRepo, skills.CardsName: "Chart", "polymux": "Polymux", "crm": "Use the CRM."} {
		result, err := tool.Execute(t.Context(), "call", json.RawMessage(`{"name":"`+name+`"}`), nil)
		if err != nil || !strings.Contains(result.Text(), marker) {
			t.Fatalf("read_skill %s: %v, missing %q", name, err, marker)
		}
	}
	if _, err := tool.Execute(t.Context(), "call", json.RawMessage(`{"name":"nope"}`), nil); err == nil {
		t.Fatal("an unknown skill should fail")
	}
}
