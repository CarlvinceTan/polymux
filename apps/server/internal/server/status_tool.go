package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// statusTool lets the model name the step it is on, as Grok Bot's SetStatus does.
type statusTool struct{}

func (*statusTool) Name() string                           { return "set_status" }
func (*statusTool) Label() string                          { return "Set status" }
func (*statusTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*statusTool) ToolClass() string                      { return "reversible" }
func (*statusTool) Description() string {
	return "Set the short line shown to the user while you work, for example \"Searching for launch dates\". Call it when you start a distinct step, in a few plain present-tense words. Don't repeat a status, narrate every tool call, or use it for quick work."
}
func (*statusTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "set_status", Description: (&statusTool{}).Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
		}, "required": []string{"status"},
	}}
}

func (t *statusTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil || state.status == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	text := statusLine(input.Status)
	if text == "" {
		return agent.AgentToolResult{}, errors.New("status is empty")
	}
	if err := state.status(text); err != nil {
		return agent.AgentToolResult{}, err
	}
	return toolResult(map[string]string{"status": text})
}

// statusLine matches Grok Bot: one trimmed line, no leading "is", capitalised.
func statusLine(text string) string {
	fields := strings.Fields(text)
	if len(fields) > 0 && strings.EqualFold(fields[0], "is") {
		fields = fields[1:]
	}
	text = strings.Join(fields, " ")
	runes := []rune(text)
	if len(runes) > 60 {
		runes = []rune(strings.TrimSpace(string(runes[:59])) + "…")
	}
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
