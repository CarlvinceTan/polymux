package server

import (
	"context"
	"encoding/json"
	"errors"

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
	if state.work.Trigger != "message" || state.handoff != nil || state.checkpoint != nil {
		return agent.AgentToolResult{}, errors.New("spawning requires an active user turn")
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
