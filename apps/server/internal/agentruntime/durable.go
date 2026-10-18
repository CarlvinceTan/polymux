package agentruntime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/durable"
)

// ErrTurnState reports a durable tool call that arrived without its turn state. Durable
// executes tools on its own task line, so a resumed call after a restart has no caller
// context; failing is safer than running without the run, scope and effect records.
var ErrTurnState = errors.New("turn state is unavailable; review this stage before retrying")

// toolReplay derives durable replay safety from the tool's approval class.
// Consequential (and undeclared ExtraTools) are unsafe so crash recovery does not re-execute them.
func toolReplay(tool agent.AgentTool) durable.ToolReplay {
	type classed interface{ ToolClass() string }
	if c, ok := tool.(classed); ok {
		switch c.ToolClass() {
		case "read", "reversible":
			return durable.ReplaySafe
		default:
			return durable.ReplayUnsafe
		}
	}
	switch tool.Name() {
	case "read", "grep", "find", "ls", "recall_history", "recall_memory", "show_html",
		"track_work", "save_memory", "forget_memory", "write", "edit",
		"message_agent", "handoff_agent", "manage_tasks", "finish_review", "send_to_user":
		return durable.ReplaySafe
	default:
		return durable.ReplayUnsafe
	}
}

// ToolRegistration adapts a Polymux tool to the durable harness. The definition's TurnContext
// restores the context values a tool expects, because the caller's own context does not
// reach a durable tool call.
func ToolRegistration(d Definition, tool agent.AgentTool, conversation durable.ConversationId) *durable.ToolRegistration {
	mode := durable.ToolExecutionParallel
	if tool.ExecutionMode() == agent.ToolModeSequential {
		// A sequential tool (a checkpoint, a remote command) must see earlier calls settle first.
		mode = durable.ToolExecutionSequential
	}
	return &durable.ToolRegistration{
		ToolSchema:    tool.Schema(),
		Replay:        toolReplay(tool),
		ExecutionMode: mode,
		Execute: func(ctx context.Context, args any, api durable.ToolExecutionApi) (durable.ToolExecutionResult, error) {
			if d.TurnContext != nil {
				// Only this session's conversation may use its turn state: a call from
				// anywhere else, such as a child conversation, must not inherit it.
				if api.ConversationId() != conversation {
					return durable.ToolExecutionResult{}, ErrTurnState
				}
				restored, ok := d.TurnContext()
				if !ok {
					return durable.ToolExecutionResult{}, ErrTurnState
				}
				ctx = restored
			}
			raw, err := json.Marshal(args)
			if err != nil {
				return durable.ToolExecutionResult{}, err
			}
			result, err := tool.Execute(ctx, api.CallId(), raw, nil)
			if err != nil {
				return durable.ToolExecutionResult{}, err
			}
			return durable.ToolExecutionResult{Content: result.Content}, nil
		},
	}
}

// Tools adapts every extra tool a definition carries, in order.
func Tools(d Definition, conversation durable.ConversationId) []*durable.ToolRegistration {
	registrations := []*durable.ToolRegistration{}
	for _, tool := range d.ExtraTools {
		registrations = append(registrations, ToolRegistration(d, tool, conversation))
	}
	return registrations
}
