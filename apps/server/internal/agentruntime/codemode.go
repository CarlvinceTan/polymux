package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	sandbox "github.com/MichaelKinsy/PiG/codemode"
	"github.com/MichaelKinsy/PiG/coding/extension"
	builtin "github.com/MichaelKinsy/PiG/coding/extension/builtin/codemode"
)

type codeTool struct {
	tools  []agent.AgentTool
	before agent.BeforeToolCallHook
}

func wrapMCP(tools []agent.AgentTool, before agent.BeforeToolCallHook) []agent.AgentTool {
	if len(tools) == 0 {
		return nil
	}
	return []agent.AgentTool{&codeTool{tools: tools, before: before}}
}
func (t *codeTool) Name() string                           { return "codemode" }
func (t *codeTool) Label() string                          { return "codemode" }
func (t *codeTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *codeTool) Schema() ai.ToolSchema {
	declared := make([]extension.AgentTool, 0, len(t.tools))
	for _, tool := range t.tools {
		schema := tool.Schema()
		params, _ := json.Marshal(schema.Parameters)
		declared = append(declared, extension.AgentTool{Name: schema.Name, Description: schema.Description, Parameters: params})
	}
	budget := builtin.DefaultInlineBudget
	return ai.ToolSchema{Name: t.Name(), Description: builtin.CreateDescription(declared, builtin.DescriptionOptions{InlineBudget: &budget}), Parameters: map[string]any{"type": "object", "properties": map[string]any{"code": map[string]any{"type": "string"}}, "required": []string{"code"}}}
}
func (t *codeTool) Execute(ctx context.Context, id string, params json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var input struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("codemode_input: %w", err)
	}
	parsed, err := sandbox.ParseCodemodeSource(input.Code)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var serial atomic.Uint64
	var calls sync.Mutex
	var stopped atomic.Bool
	tools := make([]sandbox.Tool, 0, len(t.tools))
	for _, tool := range t.tools {
		schema := tool.Schema()
		raw, _ := json.Marshal(schema.Parameters)
		tools = append(tools, sandbox.Tool{Name: tool.Name(), Description: schema.Description, InputSchema: raw, Execute: func(callCtx context.Context, args json.RawMessage) (json.RawMessage, error) {
			calls.Lock()
			defer calls.Unlock()
			if stopped.Load() {
				return nil, fmt.Errorf("codemode_blocked: turn stopped")
			}
			callID := fmt.Sprintf("%s/%d", id, serial.Add(1))
			if t.before != nil {
				decision := t.before(callCtx, callID, tool.Name(), args)
				if decision.Terminate {
					stopped.Store(true)
				}
				if decision.Block {
					return nil, fmt.Errorf("codemode_blocked: %s", decision.Reason)
				}
				if len(decision.Args) > 0 {
					args = decision.Args
				}
			}
			result, err := tool.Execute(callCtx, callID, args, nil)
			if err != nil {
				return nil, err
			}
			// Preserve the MCP envelope rather than reducing inner results to text.
			return json.Marshal(map[string]any{"content": result.Content, "structuredContent": result.StructuredContent, "isError": result.IsError})
		}})
	}
	globals := []sandbox.Tool{
		{Name: "searchTools", Signature: "(query: string) => string", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			var query string
			if err := json.Unmarshal(args, &query); err != nil {
				return nil, err
			}
			matches := []sandbox.Tool{}
			for _, tool := range tools {
				if strings.Contains(strings.ToLower(tool.Name+" "+tool.Description), strings.ToLower(query)) {
					matches = append(matches, tool)
				}
			}
			return json.Marshal(sandbox.RenderDeclarations(sandbox.RenderDeclarationsOptions{Tools: matches}))
		}},
		{Name: "describeTool", Signature: "(name: string) => string", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			var name string
			if err := json.Unmarshal(args, &name); err != nil {
				return nil, err
			}
			for _, tool := range tools {
				if name == tool.Name || name == sandbox.ToCodemodeIdentifier(tool.Name) {
					return json.Marshal(sandbox.RenderDeclarations(sandbox.RenderDeclarationsOptions{Tools: []sandbox.Tool{tool}}))
				}
			}
			return nil, fmt.Errorf("codemode_unknown_tool: %s", name)
		}},
	}
	box, err := sandbox.NewSandbox(sandbox.SandboxOptions{Tools: tools, Globals: globals, MemoryLimitBytes: 128 << 20})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	defer box.Close()
	opts := sandbox.ExecuteOptions{}
	if parsed.Options.TimeoutMs != nil {
		opts.TimeoutMs = float64(*parsed.Options.TimeoutMs)
	}
	result, err := box.Execute(ctx, parsed.Code, opts)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	out := agent.AgentToolResult{IsError: !result.OK}
	for _, item := range result.Output {
		if item.Type == "image" {
			out.Content = append(out.Content, ai.ImageContent{Data: item.Data, MimeType: item.MimeType})
		} else {
			out.Content = append(out.Content, ai.TextContent{Text: item.Text})
		}
	}
	if result.Value != nil {
		out.Content = append(out.Content, ai.TextContent{Text: string(result.Value)})
	}
	if result.Error != nil {
		out.Content = append(out.Content, ai.TextContent{Text: fmt.Sprintf("codemode_%s: %s", result.Error.Kind, result.Error.Message)})
	}
	return out, nil
}
