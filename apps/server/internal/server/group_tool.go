package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type groupTool struct{ server *Server }

func (*groupTool) Name() string                           { return "create_group_chat" }
func (*groupTool) Label() string                          { return "Create group chat" }
func (*groupTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*groupTool) Description() string {
	return "Create a group chat with you and other agents when the user asks for one. agent_ids are the other members' stable IDs from the peer roster; you join automatically. title is optional and defaults to the member names."
}

type groupEdit struct{ server *Server }

func (*groupEdit) Name() string                           { return "update_group_chat" }
func (*groupEdit) Label() string                          { return "Update group chat" }
func (*groupEdit) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*groupEdit) Description() string {
	return "Update a group chat's title or member list. chat_id defaults to the current conversation. agent_ids replaces the members; use existing stable bot IDs."
}
func (t *groupEdit) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{
			"chat_id":   map[string]any{"type": "string", "maxLength": 128},
			"title":     map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"agent_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": map[string]any{"type": "string"}},
		},
	}}
}
func (t *groupEdit) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		ChatID   string   `json:"chat_id"`
		Title    *string  `json:"title"`
		AgentIDs []string `json:"agent_ids"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if input.ChatID == "" {
		input.ChatID = state.run.ConversationID
	}
	if input.Title == nil && input.AgentIDs == nil {
		return agent.AgentToolResult{}, errors.New("choose a group field to update")
	}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" || len(title) > 128 {
			return agent.AgentToolResult{}, errors.New("invalid group title")
		}
		input.Title = &title
	}
	t.server.actorsMu.Lock()
	defer t.server.actorsMu.Unlock()
	if t.server.closing {
		return agent.AgentToolResult{}, errClosing
	}
	if t.server.groups[input.ChatID] == nil {
		return agent.AgentToolResult{}, errors.New("choose a group chat")
	}
	for _, id := range input.AgentIDs {
		if t.server.actors[id] == nil {
			return agent.AgentToolResult{}, errUnknownMember
		}
	}
	if err := t.server.Store.UpdateThread(ctx, input.ChatID, contract.UpdateThreadRequest{Title: input.Title, AgentIDs: input.AgentIDs}); err != nil {
		return agent.AgentToolResult{}, err
	}
	thread, err := t.server.Store.Thread(ctx, input.ChatID)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return toolResult(thread)
}
func (t *groupTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"agent_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": 15,
				"items": map[string]any{"type": "string", "maxLength": 128}},
			"title": map[string]any{"type": "string", "maxLength": 128},
		},
		"required": []string{"agent_ids"},
	}}
}
func (t *groupTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		AgentIDs []string `json:"agent_ids"`
		Title    string   `json:"title"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	state.mu.Lock()
	allowed := state.work.Trigger != contract.RunTriggerCreated && state.handoff == nil && state.checkpoint == nil
	owner := state.run.AgentID
	state.mu.Unlock()
	if !allowed {
		return agent.AgentToolResult{}, errors.New("creating a group requires an active bot turn")
	}
	members := []string{owner}
	for _, id := range input.AgentIDs {
		if !slices.Contains(members, id) {
			members = append(members, id)
		}
	}
	request := contract.CreateThreadRequest{AgentIDs: members}
	if title := strings.TrimSpace(input.Title); title != "" {
		if len(title) > 128 {
			return agent.AgentToolResult{}, errors.New("keep the chat name within 128 characters")
		}
		request.Title = &title
	}
	thread, err := t.server.newGroup(ctx, request)
	if errors.Is(err, errUnknownMember) {
		return agent.AgentToolResult{}, errors.New("choose existing agents from the peer roster")
	}
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return toolResult(map[string]any{"chat_id": thread.ID, "title": thread.Title, "agent_ids": thread.AgentIDs})
}
