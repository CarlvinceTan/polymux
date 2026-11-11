package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CarlvinceTan/polymux/apps/server/internal/skills"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// skillTool loads a skill for an agent that can't read files, the way one that can reads the skill's file, so skills
// arrive when a task needs them instead of with every request.
type skillTool struct{ configured []string }

func (*skillTool) Name() string                           { return "read_skill" }
func (*skillTool) Label() string                          { return "Read skill" }
func (*skillTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeParallel }

func (t *skillTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: "Load a listed skill's full instructions by name before acting on a task it covers.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}}}
}

func (t *skillTool) Execute(_ context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	text, err := t.read(input.Name)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: text}}}, nil
}

func (t *skillTool) read(name string) (string, error) {
	switch name {
	case skills.CardsName:
		return skills.Cards()
	case "polymux":
		return skills.Polymux()
	case "bot-creator":
		return skills.BotCreator()
	case "report":
		return skills.Report()
	}
	for _, path := range t.configured {
		data, err := os.ReadFile(path)
		if err == nil && configuredName(path, string(data)) == name {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("no skill named %q", name)
}

// configuredName is a configured skill's name: its front matter's, or its folder's.
func configuredName(path, body string) string {
	if name := skills.Describe(body).Name; name != "" {
		return name
	}
	return filepath.Base(filepath.Dir(path))
}
