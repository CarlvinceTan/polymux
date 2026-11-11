package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

// AgentTool is a plugin's tool as one agent sees it.
type AgentTool struct {
	Plugin string
	Title  string
	Tool
}

// ToolCall is what POST /tools/{name} on a service receives.
type ToolCall struct {
	Chat  string          `json:"chat"`
	Agent string          `json:"agent"`
	Args  json.RawMessage `json:"args"`
}

// ToolResult is a service's answer, which goes back to the model.
type ToolResult struct {
	Text string `json:"text"`
}

// Tools lists what a chat's agents may call: the tools of ready plugins that apply to the chat.
func (m *Manager) Tools(ctx context.Context, chat string) []AgentTool {
	records, err := m.config.Store.Plugins(ctx)
	if err != nil {
		return nil
	}
	out := []AgentTool{}
	for _, saved := range records {
		var value record
		if json.Unmarshal(saved.Data, &value) != nil || value.State != contract.PluginStateReady {
			continue
		}
		if value.Kind == KindScoped && !slices.Contains(saved.Chats, chat) {
			continue
		}
		for _, tool := range value.Manifest.Tools {
			out = append(out, AgentTool{Plugin: saved.Name, Title: value.Title, Tool: tool})
		}
	}
	return out
}

// CallTool asks a plugin's service to run one of its tools.
func (m *Manager) CallTool(ctx context.Context, plugin, tool string, call ToolCall) (string, error) {
	m.mu.Lock()
	unit := m.services[plugin]
	m.mu.Unlock()
	if unit == nil {
		return "", errors.New("this plugin isn't running")
	}
	var result ToolResult
	if err := unit.post(ctx, "/tools/"+tool, call, &result); err != nil {
		return "", err
	}
	return result.Text, nil
}
