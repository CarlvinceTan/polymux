package agentruntime

import (
	"strings"
	"testing"
)

func TestVaultDiscoveryPolicy(t *testing.T) {
	prompt, err := buildPrompt(Definition{}, []string{"tool_search", "bash", "read"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{"find vault_list", "private cards", "KDBX", "Never extract vault values", "never inspect filled secret fields"} {
		if !strings.Contains(prompt, instruction) {
			t.Errorf("policy absent before deferred vault tools are loaded: %s", instruction)
		}
	}
}
