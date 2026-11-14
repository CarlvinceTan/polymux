package server

import (
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
)

func TestPluginToolNameIsSafeForModels(t *testing.T) {
	tool := pluginTool{tool: plugins.AgentTool{Plugin: "notes", Tool: plugins.Tool{Name: "add_note"}}}
	if name := tool.Name(); name != "notes__add_note" {
		t.Fatal(name)
	}
}
