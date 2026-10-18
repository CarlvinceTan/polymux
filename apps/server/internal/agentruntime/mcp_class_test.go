package agentruntime

import "testing"

func TestMCPClassRequiresOperatorReview(t *testing.T) {
	if (&mcpTool{}).ToolClass() != "consequential" {
		t.Fatal("unknown MCP tools must require approval")
	}
	if (&mcpTool{readOnly: true}).ToolClass() != "read" {
		t.Fatal("reviewed read-only MCP tools must be replay-safe reads")
	}
}
