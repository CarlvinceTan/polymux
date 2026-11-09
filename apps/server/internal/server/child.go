package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/acp"
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
	return "Delegate a bounded task to a subagent in a fresh session. Run returns a task ID immediately; the result returns automatically when finished. Give a self-contained prompt with relevant context and the expected result, never unrelated private history or secrets. Omit agent_id to use your own agent; set it to select an available agent. Children cannot delegate. Use list to inspect tasks and their latest output, or stop with id to cancel one.\n\n" + t.externalHint()
}

// externalHint tells the bot which external agents it can hand work to, and when.
func (t *delegateTool) externalHint() string {
	hint := "External agents: set external to run the task on an Agent Client Protocol agent instead of a Polymux bot. Polymux is for operational automation, so do that work yourself. Hand specialised work, above all coding (writing, changing, reviewing or debugging code in a repository), to an external agent built for it when one is installed, unless the user or your instructions say otherwise. Use one the user or your instructions name even if it isn't installed: external.agent takes any ACP registry ID (action agents lists them), or external.command and external.args launch any ACP command. The task's working directory is your workspace when you have one; otherwise name the absolute path to work in."
	detected := t.server.config.Registry.Detected()
	if len(detected) == 0 {
		return hint + " None are installed on this server."
	}
	names := make([]string, len(detected))
	for i, agent := range detected {
		names[i] = agent.ID + " (" + agent.Name + ")"
	}
	return hint + " Installed on this server: " + strings.Join(names, ", ") + "."
}
func (t *delegateTool) Schema() ai.ToolSchema {
	actions := []string{"list", "stop", "agents"}
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
			"external": map[string]any{"type": "object", "additionalProperties": false, "description": "Run on an external agent: agent, or command with args",
				"properties": map[string]any{
					"agent":   map[string]any{"type": "string", "maxLength": 128, "description": "ACP registry ID, such as claude-acp"},
					"command": map[string]any{"type": "string", "maxLength": 4096, "description": "ACP executable, when no registry agent fits"},
					"args":    map[string]any{"type": "array", "maxItems": 64, "items": map[string]any{"type": "string", "maxLength": 4096}},
				}},
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
		Action   string                  `json:"action"`
		Task     string                  `json:"prompt"`
		AgentID  string                  `json:"agent_id"`
		External *contract.ExternalAgent `json:"external"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if input.Action == "list" || input.Action == "stop" {
		return t.manage(ctx, args)
	}
	if input.Action == "agents" {
		return t.agents()
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
	external, err := t.external(ctx, input.External)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	t.server.actorsMu.RLock()
	defer t.server.actorsMu.RUnlock()
	if t.server.closing || t.server.deletes[state.run.AgentID] != nil || t.server.deletes[input.AgentID] != nil {
		return agent.AgentToolResult{}, errors.New("agent is being removed or the server is stopping")
	}
	run, err := t.server.Store.Spawn(ctx, state.run.ID, call, input.AgentID, input.Task, external)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return toolResult(run)
}

// external checks the agent a task should run on and names it for display.
func (t *delegateTool) external(ctx context.Context, in *contract.ExternalAgent) (*contract.ExternalAgent, error) {
	if in == nil || in.Agent == "" && strings.TrimSpace(in.Command) == "" {
		return nil, nil
	}
	if in.Agent != "" {
		if in.Command != "" || len(in.Args) > 0 {
			return nil, errors.New("set external.agent or external.command, not both")
		}
		registered, ok := t.server.config.Registry.Find(ctx, in.Agent)
		if !ok {
			return nil, fmt.Errorf("%q is not in the ACP registry; use action agents to list it", in.Agent)
		}
		return &contract.ExternalAgent{Agent: registered.ID, Name: registered.Name}, nil
	}
	command := strings.TrimSpace(in.Command)
	if strings.ContainsAny(command, "\x00\r\n") || slices.ContainsFunc(in.Args, func(arg string) bool { return strings.Contains(arg, "\x00") }) {
		return nil, errors.New("invalid external command")
	}
	return &contract.ExternalAgent{Name: filepath.Base(command), Command: command, Args: in.Args}, nil
}

// agents lists the ACP registry, marking what is installed.
func (t *delegateTool) agents() (agent.AgentToolResult, error) {
	type entry struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Installed   bool   `json:"installed,omitempty"`
	}
	detected := t.server.config.Registry.Detected()
	out := []entry{}
	for _, a := range t.server.config.Registry.Agents() {
		installed := slices.ContainsFunc(detected, func(d acp.Agent) bool { return d.ID == a.ID })
		out = append(out, entry{a.ID, a.Name, a.Description, installed})
	}
	if len(out) == 0 {
		return agent.AgentToolResult{}, errors.New("the ACP registry is unavailable; use external.command instead")
	}
	return toolResult(out)
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
			On      string `json:"external,omitempty"`
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
			on := ""
			if run.External != nil {
				on = run.External.Name
			}
			out = append(out, child{run.ID, run.AgentID, run.Status, run.Summary, run.CreatedAt, output, on})
		}
		return toolResult(out)
	default:
		return agent.AgentToolResult{}, errors.New("invalid child task action")
	}
}
