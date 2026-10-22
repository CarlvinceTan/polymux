package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type projectTool struct{ store *store.Store }

func (*projectTool) Name() string                           { return "manage_projects" }
func (*projectTool) Label() string                          { return "Manage projects" }
func (*projectTool) ToolClass() string                      { return "reversible" }
func (*projectTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*projectTool) Description() string {
	return "List projects, create one (creating an existing slug joins it), join, or leave. A project's memory is shared by the bots that joined it; save project facts with save_memory and project=<slug>. Record only facts about the project that are useful to every member."
}
func (t *projectTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"action":      map[string]any{"type": "string", "enum": []string{"list", "create", "join", "leave"}},
		"slug":        map[string]any{"type": "string", "maxLength": 64, "description": "Short kebab-case id"},
		"name":        map[string]any{"type": "string", "maxLength": 128},
		"description": map[string]any{"type": "string", "maxLength": 1024},
	}, "required": []string{"action"}}}
}
func (t *projectTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, t.store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var input struct {
		Action      string `json:"action"`
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	self := state.run.AgentID
	switch input.Action {
	case "create":
		name := strings.TrimSpace(input.Name)
		if name == "" || len(name) > 128 || len(input.Description) > 1024 {
			return agent.AgentToolResult{}, errors.New("a project needs a name of at most 128 bytes")
		}
		created, err := t.store.CreateProject(ctx, self, input.Slug, name, strings.TrimSpace(input.Description))
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(map[string]any{"slug": input.Slug, "created": created, "joined": true})
	case "join":
		if err := t.store.JoinProject(ctx, self, input.Slug); err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(map[string]any{"slug": input.Slug, "joined": true})
	case "leave":
		if err := t.store.LeaveProject(ctx, self, input.Slug); err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(map[string]any{"slug": input.Slug, "joined": false})
	case "list":
		projects, err := t.store.Projects(ctx)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		agents, err := t.store.Agents(ctx)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		names := map[string]string{}
		for _, a := range agents {
			names[a.ID] = cmp.Or(a.Name, a.ID)
		}
		type listed struct {
			store.Project
			Joined bool `json:"joined"`
		}
		out := []listed{}
		for _, p := range projects {
			joined := slices.Contains(p.Members, self)
			for i, id := range p.Members {
				p.Members[i] = names[id]
			}
			out = append(out, listed{p, joined})
		}
		return toolResult(out)
	default:
		return agent.AgentToolResult{}, errors.New("invalid project action")
	}
}
