package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

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
	Text   string      `json:"text"`
	Images []ToolImage `json:"images,omitempty"`
	// Structured is machine-readable output a codemode script reads as structuredContent (plugin API 7).
	Structured json.RawMessage `json:"structured,omitempty"`
}

// ToolImage is a base64 picture a tool hands the model, such as a screenshot (plugin API 7).
type ToolImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// maxToolImages caps how many pictures one call can add to the context, and maxStructured its structured output.
const (
	maxToolImages = 4
	maxStructured = 1 << 20
)

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
func (m *Manager) CallTool(ctx context.Context, plugin, tool string, call ToolCall) (ToolResult, error) {
	m.mu.Lock()
	unit := m.services[plugin]
	m.mu.Unlock()
	if unit == nil {
		return ToolResult{}, errors.New("this plugin isn't running")
	}
	var result ToolResult
	if err := unit.post(ctx, "/tools/"+tool, call, &result, toolLimits); err != nil {
		return ToolResult{}, err
	}
	result.Images = slices.DeleteFunc(result.Images, func(image ToolImage) bool {
		return image.Data == "" || !strings.HasPrefix(image.MimeType, "image/")
	})
	if len(result.Images) > maxToolImages {
		result.Images = result.Images[:maxToolImages]
	}
	if len(result.Structured) > maxStructured || !json.Valid(result.Structured) || string(result.Structured) == "null" {
		result.Structured = nil
	}
	return result, nil
}
