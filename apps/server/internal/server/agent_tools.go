package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type agentTool struct {
	server *Server
	action string
	// models adds a model choice to create, once there are models to choose between.
	models bool
}

func (t *agentTool) Name() string                         { return t.action + "_agent" }
func (*agentTool) Label() string                          { return "Manage bots" }
func (*agentTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *agentTool) Description() string {
	switch t.action {
	case "create":
		return "Hire a persistent bot with its own direct chat. Set its role and instructions (prompt), which add to the default chat style; initial_task starts its first task after its introduction. Omit name for a random 4–5-letter name; only set name when the user requests a specific name. This is different from a temporary task."
	case "update":
		return "Update your own name, role, instructions (prompt), workspace, avatar or task delegation, or another bot by agent_id. Change task_delegation only when the user explicitly asks to enable or disable delegation. Omitted fields stay unchanged. A prompt replaces the whole of a bot's instructions, including the default chat style, so carry over what should stay. Changes persist; instructions and workspace take effect next turn. Avatar values use the application's shape, color and expression IDs."
	default:
		return "Remove a bot and its chat by stable agent_id. Deletion starts after your current reply finishes so active chats can close cleanly."
	}
}
func (t *agentTool) Schema() ai.ToolSchema {
	properties := map[string]any{"agent_id": map[string]any{"type": "string", "maxLength": 128}}
	required := []string{}
	if t.action == "delete" {
		required = append(required, "agent_id")
	} else {
		properties["name"] = map[string]any{"type": "string", "maxLength": 128}
		properties["role"] = map[string]any{"type": "string", "maxLength": 256, "description": "The bot's job title in Title Case, naming who it is, such as \"Polymux Maintainer\" or \"Travel Planner\"; never the duty or activity (not \"Polymux maintenance\")"}
		properties["prompt"] = map[string]any{"type": "string", "maxLength": 64000}
		if t.action == "create" {
			delete(properties, "agent_id")
			properties["initial_task"] = map[string]any{"type": "string", "maxLength": 64000}
			if t.models {
				properties["model"] = map[string]any{"type": "string", "maxLength": 256}
				properties["provider"] = map[string]any{"type": "string", "maxLength": 128}
			}
		} else {
			properties["task_delegation"] = map[string]any{"type": "boolean", "description": "Enable or disable this bot’s task delegation only at the user’s explicit request"}
			properties["workspace"] = map[string]any{"type": "string", "maxLength": 4096}
			for field, values := range map[string]map[string]bool{"shape": shapes, "color": colors, "expression": expressions} {
				options := make([]string, 0, len(values))
				for value := range values {
					options = append(options, value)
				}
				slices.Sort(options)
				properties[field] = map[string]any{"type": "string", "enum": options}
			}
		}
	}
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": properties, "required": required,
	}}
}
func (t *agentTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		agentPatch
		AgentID    string  `json:"agent_id"`
		Task       *string `json:"initial_task"`
		Shape      *string `json:"shape"`
		Color      *string `json:"color"`
		Expression *string `json:"expression"`
		Model      *string `json:"model"`
		Provider   *string `json:"provider"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if input.Role != nil {
		title := roleTitle(*input.Role)
		input.Role = &title
	}
	if t.action == "delete" && input.AgentID == "" {
		return agent.AgentToolResult{}, errors.New("provide the bot ID to delete")
	}
	if input.AgentID == "" {
		input.AgentID = state.run.AgentID
	}
	switch t.action {
	case "create":
		// A first task has no parent run, so it would start free of this chat's rules.
		if input.Task != nil && t.server.plugins.Gated(ctx, state.chats) {
			return agent.AgentToolResult{}, errors.New("this chat's rules don't let a new bot start work alone; create it without initial_task, then message it")
		}
		creator, err := t.server.Store.Profile(ctx, state.run.AgentID)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		created, err := t.server.addAgent(ctx, contract.CreateAgentRequest{Name: value(input.Name), Role: value(input.Role), Prompt: agentruntime.StartPrompt(value(input.Prompt)), InitialTask: input.Task, Model: input.Model, Provider: input.Provider}, creator.Agent)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(created)
	case "update":
		if input.Shape != nil || input.Color != nil || input.Expression != nil {
			profile, err := t.server.Store.Profile(ctx, input.AgentID)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			avatar := defaultAvatar()
			if profile.Agent.Avatar != nil {
				avatar = *profile.Agent.Avatar
			}
			if input.Shape != nil {
				avatar.Shape = *input.Shape
			}
			if input.Color != nil {
				avatar.Color = *input.Color
			}
			if input.Expression != nil {
				avatar.Expression = *input.Expression
			}
			input.Avatar = &avatar
		}
		profile, err := t.server.editAgent(ctx, input.AgentID, input.agentPatch)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		if input.TaskDelegation != nil {
			return toolResult(map[string]any{"profile": profile, "task_delegation": *input.TaskDelegation})
		}
		return toolResult(profile)
	case "delete":
		if _, err := t.server.Store.Profile(ctx, input.AgentID); err != nil {
			return agent.AgentToolResult{}, err
		}
		state.mu.Lock()
		if !slices.Contains(state.removals, input.AgentID) {
			state.removals = append(state.removals, input.AgentID)
		}
		state.mu.Unlock()
		return toolResult(map[string]any{"agent_id": input.AgentID, "status": "scheduled"})
	}
	return agent.AgentToolResult{}, errors.New("invalid bot action")
}

// roleTitle capitalises each word of a role, leaving small joining words and words with their own capitals (iOS) alone.
func roleTitle(role string) string {
	small := map[string]bool{"a": true, "an": true, "and": true, "at": true, "by": true, "for": true, "in": true, "of": true, "on": true, "or": true, "the": true, "to": true}
	words := strings.Fields(role)
	for i, word := range words {
		if i > 0 && small[word] {
			continue
		}
		runes := []rune(word)
		if unicode.IsLower(runes[0]) && !strings.ContainsFunc(word[len(string(runes[0])):], unicode.IsUpper) {
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}
