package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type delegateTool struct {
	server  *Server
	agentID string
}

func (t *delegateTool) enabled() bool {
	t.server.actorsMu.RLock()
	defer t.server.actorsMu.RUnlock()
	a := t.server.actors[t.agentID]
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.definition.DelegationDisabled
}

func (*delegateTool) Name() string                           { return "task" }
func (*delegateTool) Label() string                          { return "Delegate task" }
func (*delegateTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *delegateTool) Description() string {
	if !t.enabled() {
		return "List or stop previously delegated tasks. New task delegation is disabled by the user."
	}
	return "Delegate a bounded task to a subagent in a fresh session. Run returns a task ID immediately; the result returns automatically when finished. Give a self-contained prompt with relevant context and the expected result, never unrelated private history or secrets. Omit agent_id to use your own agent; set it to select an available agent. Children cannot delegate. Use list to inspect tasks and their latest output, or stop with id to cancel one."
}
func (t *delegateTool) Schema() ai.ToolSchema {
	actions := []string{"list", "stop"}
	if t.enabled() {
		actions = append([]string{"run"}, actions...)
	}
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"action":   map[string]any{"type": "string", "enum": actions},
			"prompt":   map[string]any{"type": "string", "minLength": 1, "maxLength": 8192, "description": "Self-contained instructions for run"},
			"agent_id": map[string]any{"type": "string", "maxLength": 128},
			"id":       map[string]any{"type": "string", "maxLength": 256, "description": "Task ID for stop"},
		},
		"required": []string{"action"},
	}}
}
func (t *delegateTool) Execute(ctx context.Context, call string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		Action  string `json:"action"`
		Task    string `json:"prompt"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if input.Action == "list" || input.Action == "stop" {
		return t.manage(ctx, args)
	}
	if input.Action != "run" {
		return agent.AgentToolResult{}, errors.New("invalid task action")
	}
	if !t.enabled() {
		return agent.AgentToolResult{}, errors.New("task delegation is disabled")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.work.Trigger == contract.RunTriggerCreated || state.work.Trigger == contract.RunTriggerChild || state.handoff != nil || state.checkpoint != nil {
		return agent.AgentToolResult{}, errors.New("spawning requires an active bot turn")
	}
	if input.AgentID == "" {
		input.AgentID = state.run.AgentID
	}
	t.server.actorsMu.RLock()
	defer t.server.actorsMu.RUnlock()
	if t.server.closing || t.server.deletes[state.run.AgentID] != nil || t.server.deletes[input.AgentID] != nil {
		return agent.AgentToolResult{}, errors.New("agent is being removed or the server is stopping")
	}
	run, err := t.server.Store.Spawn(ctx, state.run.ID, call, input.AgentID, input.Task)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return toolResult(run)
}

func (t *delegateTool) manage(ctx context.Context, args json.RawMessage) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil || state.work.Trigger == contract.RunTriggerCreated || state.work.Trigger == contract.RunTriggerChild {
		return agent.AgentToolResult{}, errors.New("managing child tasks requires an active bot turn")
	}
	var input struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	switch input.Action {
	case "stop":
		ids, err := t.server.Store.StopChild(ctx, state.run.AgentID, input.ID)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		for _, id := range ids {
			if active, ok := t.server.turns.Load(id); ok {
				active.(*execution).cancel()
			}
		}
		return toolResult(map[string]bool{"stopped": true})
	case "list":
		runs, err := t.server.Store.Children(ctx, state.run.AgentID)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		type child struct {
			ID      string `json:"id"`
			AgentID string `json:"agentID"`
			Status  string `json:"status"`
			Summary string `json:"summary"`
			Created string `json:"createdAt"`
			Output  string `json:"output,omitempty"`
		}
		out := []child{}
		for _, run := range runs {
			messages, err := t.server.Store.ChildMessages(ctx, run.ID)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			output := ""
			if len(messages) > 0 {
				output = bounded(store.Text(messages[len(messages)-1]), 2048)
			}
			out = append(out, child{run.ID, run.AgentID, run.Status, run.Summary, run.CreatedAt, output})
		}
		return toolResult(out)
	default:
		return agent.AgentToolResult{}, errors.New("invalid child task action")
	}
}
