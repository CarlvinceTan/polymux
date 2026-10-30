package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/CarlvinceTan/polymux/packages/runtime/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding/extension"
	builtin "github.com/MichaelKinsy/PiG/coding/extension/builtin/codemode"
)

type codeTool struct {
	tools    []agent.AgentTool
	declared []extension.AgentTool
	before   agent.BeforeToolCallHook
	after    func(ctx context.Context, call, name string, failed bool)
	schema   ai.ToolSchema
}

func wrapMCP(tools []agent.AgentTool, before agent.BeforeToolCallHook, after func(context.Context, string, string, bool)) ([]agent.AgentTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	tool := &codeTool{tools: tools, before: before, after: after}
	schema, err := tool.makeSchema()
	if err != nil {
		return nil, err
	}
	tool.schema = schema
	return []agent.AgentTool{tool}, nil
}
func (t *codeTool) Name() string                           { return builtin.ToolName }
func (t *codeTool) Label() string                          { return builtin.ToolName }
func (t *codeTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *codeTool) Schema() ai.ToolSchema                  { return t.schema }

func (t *codeTool) makeSchema() (ai.ToolSchema, error) {
	t.declared = make([]extension.AgentTool, 0, len(t.tools))
	for _, tool := range t.tools {
		schema := tool.Schema()
		params, err := json.Marshal(schema.Parameters)
		if err != nil {
			return ai.ToolSchema{}, fmt.Errorf("MCP tool schema: %w", err)
		}
		// Nested MCP calls retain their content, structured content and error flag.
		t.declared = append(t.declared, extension.AgentTool{Name: schema.Name, Description: schema.Description, Parameters: params,
			OutputSchema: json.RawMessage(`{"type":"object","properties":{"content":{},"structuredContent":{},"isError":{"type":"boolean"}}}`)})
	}
	budget := builtin.DefaultInlineBudget
	description, err := builtin.CreateDescription(t.declared, builtin.DescriptionOptions{InlineBudget: &budget})
	if err != nil {
		return ai.ToolSchema{}, fmt.Errorf("MCP tool declarations: %w", err)
	}
	definition := builtin.Definition(builtin.Options{})
	var params map[string]any
	if err := json.Unmarshal(definition.Parameters, &params); err != nil {
		return ai.ToolSchema{}, err
	}
	return ai.ToolSchema{Name: definition.Name, Description: description, Parameters: params}, nil
}

func (t *codeTool) Execute(ctx context.Context, id string, params json.RawMessage, update agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var calls sync.Mutex
	serial, stopped := 0, false
	infos := make([]extension.ToolInfo, len(t.declared))
	for n, tool := range t.declared {
		infos[n] = extension.ToolInfo{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters}
	}
	base := extension.NewContext("", nil, ctx.Err, extension.ContextActions{GetAllTools: func() []extension.ToolInfo { return infos }})
	nested := extension.NewToolContext(base, id, ctx, extension.ToolActions{
		GetCallableTools: func() []extension.AgentTool { return t.declared },
		ExecuteTool: func(callCtx context.Context, caller, name string, args json.RawMessage, options extension.ExecuteToolOptions) (extension.AgentToolCallOutcome, error) {
			calls.Lock()
			defer calls.Unlock()
			extension.CallInitiated(callCtx)
			if stopped {
				return extension.AgentToolCallOutcome{}, fmt.Errorf("codemode_blocked: turn stopped")
			}
			serial++
			var arguments ai.JsonObject
			if err := json.Unmarshal(args, &arguments); err != nil {
				return extension.AgentToolCallOutcome{}, err
			}
			call := ai.ToolCall{ID: fmt.Sprintf("%s/%d", caller, serial), Name: name, Arguments: arguments}
			outcome, err := agent.RunToolCall(callCtx, call, agent.RunToolCallOptions{
				Tools: t.tools, OnUpdate: options.OnUpdate,
				ToolCallHooks: agent.ToolCallHooks{
					BeforeToolCallHooks: []agent.BeforeToolCallHook{func(ctx context.Context, id, name string, args json.RawMessage) agent.ToolCallHookResult {
						if t.before == nil {
							return agent.ToolCallHookResult{}
						}
						decision := t.before(ctx, id, name, args)
						stopped = stopped || decision.Terminate
						return decision
					}},
					AfterToolCallHooks: []agent.AfterToolCallHook{func(ctx context.Context, id, name string, _ json.RawMessage, result agent.AgentToolResult) agent.AfterToolCallResult {
						if t.after != nil {
							t.after(ctx, id, name, result.Thrown)
						}
						return agent.AfterToolCallResult{}
					}},
				},
			})
			if err != nil {
				return extension.AgentToolCallOutcome{}, err
			}
			result := outcome.Result
			if outcome.DurationMs != nil && !result.Thrown {
				envelope, err := json.Marshal(map[string]any{"content": result.Content, "structuredContent": result.StructuredContent, "isError": result.IsError})
				if err != nil {
					return extension.AgentToolCallOutcome{}, err
				}
				outcome.Result.StructuredContent = envelope
			}
			return extension.AgentToolCallOutcome(outcome), nil
		},
	})
	result, err := builtin.Execute(extension.WithToolContext(ctx, nested), id, params, update, builtin.Options{})
	for n, content := range result.Content {
		if text, ok := content.(ai.TextContent); ok {
			text.Text = untrusted.Wrap("codemode", text.Text)
			result.Content[n] = text
		}
	}
	return result, err
}
