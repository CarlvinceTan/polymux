package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
	"github.com/CarlvinceTan/polymux/apps/server/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// pluginTool hands an agent's call to the plugin service that offers it.
type pluginTool struct {
	server *Server
	tool   plugins.AgentTool
	chat   string
	agent  string
}

func (t *pluginTool) Name() string {
	return strings.ReplaceAll(t.tool.Plugin, "-", "_") + "__" + t.tool.Name
}
func (t *pluginTool) Label() string                        { return t.tool.Title }
func (*pluginTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *pluginTool) Description() string                  { return t.tool.Description }
func (t *pluginTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: t.tool.Parameters}
}
func (t *pluginTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	text, err := t.server.plugins.CallTool(ctx, t.tool.Plugin, t.tool.Name, plugins.ToolCall{Chat: t.chat, Agent: t.agent, Args: args})
	if err != nil {
		t.server.config.Log.Warn("plugin tool", "plugin", t.tool.Plugin, "tool", t.tool.Name, "error", err)
		return agent.AgentToolResult{}, errors.New(t.tool.Title + " couldn't answer.")
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: untrusted.Wrap(t.tool.Title, text)}}}, nil
}

func (s *Server) pluginTools(ctx context.Context, chat, agentID string, state *turnState) []agent.AgentTool {
	listed := s.plugins.Tools(ctx, chat)
	tools := make([]agent.AgentTool, 0, len(listed))
	for _, item := range listed {
		tool := &pluginTool{server: s, tool: item, chat: chat, agent: agentID}
		state.external[tool.Name()] = true
		tools = append(tools, tool)
	}
	return tools
}
