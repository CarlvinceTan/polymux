package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type taskTool struct{ server *Server }

func (*taskTool) Name() string                           { return "manage_tasks" }
func (*taskTool) Label() string                          { return "Manage resumable tasks" }
func (*taskTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*taskTool) Description() string {
	return "List/cancel scoped multi-step tasks, or checkpoint this stage for a fresh automatic continuation after_seconds later. Call checkpoint alone at the end of a successful stage; no further tools can run. Save the established goal, next step and concrete evidence, never tool calls to replay or secrets. Set wait_for_user only when you need the user's answer; a dependency that may clear on its own is better served by after_seconds, which re-checks automatically. Only a direct user turn can resume parked/interrupted tasks after reviewing any ambiguous effects. Continuations are bounded to 8 stages/128 tools/256 KiB output; use this for real multi-step work, not an endless polling loop."
}
func (t *taskTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"list", "checkpoint", "cancel", "resume"}},
		"id":     map[string]any{"type": "string"}, "goal": map[string]any{"type": "string", "maxLength": 4096},
		"next": map[string]any{"type": "string", "maxLength": 8192}, "evidence": map[string]any{"type": "string", "maxLength": 8192},
		"after_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 604800}, "wait_for_user": map[string]any{"type": "boolean"},
	}, "required": []string{"action"}}}
}
func (t *taskTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil || state.work.Trigger == "created" || state.work.Trigger == "peer" || state.work.Trigger == "review" {
		return agent.AgentToolResult{}, errors.New("no task scope")
	}
	var input struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		store.Checkpoint
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	owner, thread := state.run.AgentID, state.run.ConversationID
	switch input.Action {
	case "list":
		tasks, err := t.server.Store.Tasks(ctx, owner, thread)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(tasks)
	case "cancel":
		id, err := t.server.Store.CancelTask(ctx, owner, thread, input.ID)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		if active, ok := t.server.turns.Load(id); ok {
			active.(*execution).cancel()
		}
		return toolResult(map[string]bool{"cancelled": true})
	case "resume":
		if state.work.Trigger != "message" {
			return agent.AgentToolResult{}, errors.New("resume requires a new user turn")
		}
		if err := t.server.Store.ResumeTask(ctx, owner, thread, input.ID, input.Checkpoint); err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(map[string]bool{"resumed": true})
	case "checkpoint":
		pending, err := t.server.Store.PendingEffects(ctx, state.run.ID)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		if pending > 0 {
			return agent.AgentToolResult{}, errors.New("a tool effect is ambiguous; review it before creating a continuation")
		}
		if !input.Valid() {
			return agent.AgentToolResult{}, store.ErrTask
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.checkpoint != nil || state.handoff != nil {
			return agent.AgentToolResult{}, store.ErrTask
		}
		state.checkpoint = &input.Checkpoint
		return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "Checkpoint staged; it becomes durable only when this stage finishes successfully."}}, Terminate: true}, nil
	default:
		return agent.AgentToolResult{}, errors.New("invalid task action")
	}
}
