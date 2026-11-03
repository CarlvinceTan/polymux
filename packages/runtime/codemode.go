package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/packages/runtime/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding/extension"
	builtin "github.com/MichaelKinsy/PiG/coding/extension/builtin/codemode"
)

// pauseAfter keeps quick scripts whole, even when a message is waiting.
var pauseAfter = shellYield

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
	// PiG gives this guideline through its extension's prompt, which this wrapper doesn't use.
	description = "Batch independent tool calls with Promise.allSettled so they run at once, rather than awaiting each in turn or across separate codemode calls; also chain calls or filter large output.\n\n" + description
	return ai.ToolSchema{Name: definition.Name, Description: description, Parameters: params}, nil
}

func (t *codeTool) Execute(ctx context.Context, id string, params json.RawMessage, update agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var calls sync.Mutex
	serial, stopped, paused := 0, false, false
	// A script that has run a while stops at its next tool call when the user is waiting, as a turn would between steps.
	host, _ := ctx.Value(shellKey{}).(*ShellHost)
	started := time.Now()
	var finished, background []string
	infos := make([]extension.ToolInfo, len(t.declared))
	for n, tool := range t.declared {
		infos[n] = extension.ToolInfo{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters}
	}
	base := extension.NewContext("", nil, ctx.Err, extension.ContextActions{GetAllTools: func() []extension.ToolInfo { return infos }})
	nested := extension.NewToolContext(base, id, ctx, extension.ToolActions{
		GetCallableTools: func() []extension.AgentTool { return t.declared },
		ExecuteTool: func(callCtx context.Context, caller, name string, args json.RawMessage, options extension.ExecuteToolOptions) (extension.AgentToolCallOutcome, error) {
			extension.CallInitiated(callCtx)
			// The lock guards the script's bookkeeping only, so calls a script makes together run at once.
			calls.Lock()
			if stopped {
				calls.Unlock()
				return extension.AgentToolCallOutcome{}, fmt.Errorf("codemode_blocked: turn stopped")
			}
			if host != nil && time.Since(started) >= pauseAfter && host.Waiting(ctx) {
				stopped, paused = true, true
				calls.Unlock()
				return extension.AgentToolCallOutcome{}, fmt.Errorf("codemode_paused: the user sent a message")
			}
			serial++
			seq := serial
			calls.Unlock()
			var arguments ai.JsonObject
			if err := json.Unmarshal(args, &arguments); err != nil {
				return extension.AgentToolCallOutcome{}, err
			}
			call := ai.ToolCall{ID: fmt.Sprintf("%s/%d", caller, seq), Name: name, Arguments: arguments}
			outcome, moved, err := runCall(callCtx, ctx, host, call, string(args), agent.RunToolCallOptions{
				Tools: t.tools, OnUpdate: options.OnUpdate,
				ToolCallHooks: agent.ToolCallHooks{
					BeforeToolCallHooks: []agent.BeforeToolCallHook{func(ctx context.Context, id, name string, args json.RawMessage) agent.ToolCallHookResult {
						if t.before == nil {
							return agent.ToolCallHookResult{}
						}
						decision := t.before(ctx, id, name, args)
						calls.Lock()
						stopped = stopped || decision.Terminate
						calls.Unlock()
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
			calls.Lock()
			defer calls.Unlock()
			if moved {
				stopped, paused = true, true
				background = append(background, name)
				return extension.AgentToolCallOutcome(outcome), nil
			}
			result := outcome.Result
			// The script's whole output is wrapped once, so a tool's own envelope would only stop the script parsing it.
			for n, content := range result.Content {
				if text, ok := content.(ai.TextContent); ok {
					text.Text = untrusted.Unwrap(text.Text)
					result.Content[n] = text
				}
			}
			if !result.Thrown {
				finished = append(finished, fmt.Sprintf("%s %s: %s", name, args, clip(contentText(result.Content), 2048)))
			}
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
	if paused {
		note := "The script paused at a tool call because the user sent a message. Answer it first, then continue the work only if it still makes sense, without repeating calls that already finished."
		if len(finished) > 0 {
			note += "\nCalls that finished, with their results:\n" + untrusted.Wrap("codemode", clip(strings.Join(finished, "\n"), 16<<10))
		}
		if len(background) > 0 {
			note += "\nStill running in the background, with results to come by themselves as new messages: " + strings.Join(background, ", ") + "."
		}
		result.Content = append(result.Content, ai.TextContent{Text: note})
	}
	return result, err
}

func contentText(content []ai.ToolResultMessageContent) string {
	var texts []string
	for _, part := range content {
		if text, ok := part.(ai.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// runCall runs one nested call. A call still running after pauseAfter while the user waits moves to the background,
// as a long command does: the script gets a notice and stops at its next call, and the result arrives as a new message.
func runCall(callCtx, turn context.Context, host *ShellHost, call ai.ToolCall, args string, options agent.RunToolCallOptions) (agent.AgentToolCallOutcome, bool, error) {
	if host == nil || host.Detach == nil {
		outcome, err := agent.RunToolCall(callCtx, call, options)
		return outcome, false, err
	}
	type ran struct {
		outcome agent.AgentToolCallOutcome
		err     error
	}
	// The call outlives the script once moved, so only this loop passes on a stop.
	work, stop := context.WithCancel(context.WithoutCancel(callCtx))
	done := make(chan ran, 1)
	go func() {
		outcome, err := agent.RunToolCall(work, call, options)
		done <- ran{outcome, err}
	}()
	ticker := time.NewTicker(shellPoll)
	defer ticker.Stop()
	begun := time.Now()
	for {
		select {
		case end := <-done:
			stop()
			return end.outcome, false, end.err
		case <-callCtx.Done():
			stop()
			end := <-done
			return end.outcome, false, end.err
		case <-ticker.C:
			if time.Since(begun) < pauseAfter || !host.Waiting(turn) {
				continue
			}
			finish := host.Detach(call.ID, stop)
			if finish == nil {
				continue
			}
			go func() {
				end := <-done
				stop()
				result := ShellResult{ID: call.ID, Command: call.Name + " " + args, Output: contentText(end.outcome.Result.Content)}
				result.Failed = end.err != nil || end.outcome.Result.IsError || end.outcome.Result.Thrown
				if end.err != nil {
					result.Output = end.err.Error()
				}
				finish(result)
			}()
			notice := "Still running in the background, because the user sent a message. Its result arrives by itself as a new message, so don't call it again."
			return agent.AgentToolCallOutcome{Result: agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: notice}}}}, true, nil
		}
	}
}
