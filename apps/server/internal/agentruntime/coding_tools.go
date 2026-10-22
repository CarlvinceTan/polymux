package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
)

type directoryTool struct {
	definition extension.ToolDefinition
	schema     ai.ToolSchema
}

func directoryTools(cwd string, names []string) ([]agent.AgentTool, error) {
	tools := []agent.AgentTool{}
	for _, name := range names {
		var definition extension.ToolDefinition
		switch name {
		case "find":
			// Nil options use PATH only; no tool download manager is installed.
			definition = coding.CreateFindToolDefinition(cwd, nil)
		case "ls":
			definition = coding.CreateLsToolDefinition(cwd, nil)
		default:
			continue
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			return nil, fmt.Errorf("encode %s definition: %w", name, err)
		}
		var schema ai.ToolSchema
		if err := json.Unmarshal(encoded, &schema); err != nil {
			return nil, fmt.Errorf("decode %s schema: %w", name, err)
		}
		tools = append(tools, &directoryTool{definition: definition, schema: schema})
	}
	return tools, nil
}

func (t *directoryTool) Name() string          { return t.definition.Name }
func (t *directoryTool) Label() string         { return t.definition.Label }
func (t *directoryTool) Schema() ai.ToolSchema { return t.schema }
func (*directoryTool) ToolClass() string       { return "read" }
func (t *directoryTool) ExecutionMode() agent.ToolExecutionMode {
	if t.definition.ExecutionMode == string(agent.ToolModeSequential) {
		return agent.ToolModeSequential
	}
	return agent.ToolModeParallel
}
func (t *directoryTool) Execute(
	ctx context.Context,
	id string,
	params json.RawMessage,
	update agent.ToolUpdateCallback,
) (agent.AgentToolResult, error) {
	return t.definition.Execute(ctx, id, params, update)
}
