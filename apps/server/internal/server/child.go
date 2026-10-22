package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type spawnTool struct{ server *Server }

func (*spawnTool) Name() string                           { return "spawn_task" }
func (*spawnTool) Label() string                          { return "Spawn child task" }
func (*spawnTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*spawnTool) Description() string {
	return "Queue a bounded child task in a separate session and continue your turn without waiting. Supply only the explicit task, never private conversation history or secrets. agent_id optionally selects an available agent; omit it to use your own agent in a fresh session. Children cannot delegate further."
}
func (t *spawnTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"task":     map[string]any{"type": "string", "minLength": 1, "maxLength": 8192},
			"agent_id": map[string]any{"type": "string", "maxLength": 128},
		},
		"required": []string{"task"},
	}}
}
func (t *spawnTool) Execute(ctx context.Context, call string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		Task    string `json:"task"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.work.Trigger == "created" || state.work.Trigger == "child" || state.handoff != nil || state.checkpoint != nil {
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

type subagentTool struct{ server *Server }

func (*subagentTool) Name() string                           { return "manage_subagents" }
func (*subagentTool) Label() string                          { return "Manage child tasks" }
func (*subagentTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*subagentTool) Description() string {
	return "List the child tasks you spawned with spawn_task (newest first, with status and the latest output), or stop one that is queued, wedged, looping or no longer needed. Stopping also stops anything it started; a stopped task's effects may be incomplete."
}
func (t *subagentTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"list", "stop"}},
			"id":     map[string]any{"type": "string", "maxLength": 256, "description": "Child run ID from list or spawn_task"},
		},
		"required": []string{"action"},
	}}
}
func (t *subagentTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil || state.work.Trigger == "created" || state.work.Trigger == "child" {
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
