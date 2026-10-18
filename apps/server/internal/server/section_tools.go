package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

var sidebarNames = []string{"list_sections", "create_section", "rename_section", "delete_section", "pin_chat", "unpin_chat", "move_chat", "reorder"}

func sidebarToolName(name string) bool { return slices.Contains(sidebarNames, name) }

func (s *Server) sidebarTools() []agent.AgentTool {
	tools := make([]agent.AgentTool, 0, len(sidebarNames))
	for _, name := range sidebarNames {
		tools = append(tools, &sectionTool{server: s, name: name})
	}
	return tools
}

type sectionTool struct {
	server *Server
	name   string
}

func (t *sectionTool) Name() string                         { return t.name }
func (*sectionTool) Label() string                          { return "Organize sidebar" }
func (*sectionTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *sectionTool) ToolClass() string {
	if t.name == "list_sections" {
		return string(ToolRead)
	}
	return string(ToolReversible)
}
func (*sectionTool) Description() string {
	return "Manage this user's saved sidebar sections, chat pins, and placement. List sections first to reuse stable IDs. Pinning removes a chat from its section; unpin a chat before moving it. Deleting a section returns its chats to the main list and keeps every conversation. Repeating an existing state is a no-op. Ordering uses a before/after ID in the same list, never a raw order number. Changes sync across devices and are limited to thirty actual mutations per minute per agent."
}
func (t *sectionTool) Schema() ai.ToolSchema {
	text := func() map[string]any { return map[string]any{"type": "string"} }
	properties := map[string]any{}
	required := []string{}
	add := func(name string, needed bool) {
		properties[name] = text()
		if needed {
			required = append(required, name)
		}
	}
	switch t.name {
	case "create_section":
		add("name", true)
		add("after_section_id", false)
	case "rename_section":
		add("section_id", true)
		add("name", true)
	case "delete_section":
		add("section_id", true)
	case "pin_chat", "unpin_chat":
		add("chat_id", true)
	case "move_chat":
		add("chat_id", true)
		properties["section_id"] = map[string]any{"type": []string{"string", "null"}}
		required = append(required, "section_id")
	case "reorder":
		properties["kind"] = map[string]any{"type": "string", "enum": []string{"sections", "chats"}}
		required = append(required, "kind")
		add("id", true)
		add("section_id", false)
		add("before_id", false)
		add("after_id", false)
	}
	return ai.ToolSchema{Name: t.name, Description: t.Description(), Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}}
}

func (t *sectionTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil || state.work.Trigger == "created" || state.work.Trigger == "review" {
		return agent.AgentToolResult{}, errors.New("no sidebar scope")
	}
	member, err := t.server.Store.Member(ctx, state.run.AgentID, state.run.ConversationID)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	if !member {
		return agent.AgentToolResult{}, store.ErrMembers
	}
	var input struct {
		Name           string          `json:"name"`
		SectionID      json.RawMessage `json:"section_id"`
		AfterSectionID *string         `json:"after_section_id"`
		ChatID         string          `json:"chat_id"`
		Kind           string          `json:"kind"`
		ID             string          `json:"id"`
		BeforeID       *string         `json:"before_id"`
		AfterID        *string         `json:"after_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	var sectionID *string
	if input.SectionID != nil {
		if err := json.Unmarshal(input.SectionID, &sectionID); err != nil {
			return sidebarFailure("section_not_found", "Choose a section ID or null for the main list.")
		}
	}
	ctx, change := store.SidebarActor(ctx, state.run.AgentID, state.run.ID)
	var result map[string]any
	s := t.server.Store
	switch t.name {
	case "list_sections":
		sections, err := s.Sections(ctx)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		out := []map[string]any{}
		for _, section := range sections {
			out = append(out, map[string]any{"section_id": section.ID, "name": section.Name, "chat_count": section.ChatCount, "collapsed": section.Collapsed})
		}
		return toolResult(out)
	case "create_section":
		var section contract.Section
		var created bool
		section, created, err = s.CreateSection(ctx, ID(), input.Name, input.AfterSectionID)
		result = map[string]any{"section_id": section.ID, "name": section.Name, "created": created}
	case "rename_section":
		if sectionID == nil {
			return sidebarFailure("not_found", "Choose a section ID.")
		}
		var section contract.Section
		section, err = s.UpdateSection(ctx, *sectionID, &input.Name, nil)
		result = map[string]any{"section_id": section.ID, "name": section.Name}
	case "delete_section":
		if sectionID == nil {
			return sidebarFailure("not_found", "Choose a section ID.")
		}
		var ids []string
		ids, err = s.DeleteSection(ctx, *sectionID)
		if errors.Is(err, sql.ErrNoRows) {
			return toolResult(map[string]any{"deleted": *sectionID, "already_deleted": true, "changed": false})
		}
		result = map[string]any{"deleted": *sectionID, "returned_chat_ids": ids}
	case "pin_chat", "unpin_chat":
		pinned := t.name == "pin_chat"
		err = s.PlaceThread(ctx, input.ChatID, store.Placement{Pinned: &pinned})
		result = map[string]any{"chat_id": input.ChatID, "pinned": pinned}
	case "move_chat":
		if input.SectionID == nil {
			return sidebarFailure("section_not_found", "Provide a section ID or explicit null for the main list.")
		}
		err = s.PlaceThread(ctx, input.ChatID, store.Placement{SectionSet: true, SectionID: sectionID})
		result = map[string]any{"chat_id": input.ChatID, "section_id": sectionID}
	case "reorder":
		var order float64
		order, err = s.ReorderSidebar(ctx, store.SidebarOrder{Kind: input.Kind, ID: input.ID, SectionID: sectionID, BeforeID: input.BeforeID, AfterID: input.AfterID})
		result = map[string]any{"id": input.ID, "order": order}
	default:
		return agent.AgentToolResult{}, errors.New("unknown sidebar tool")
	}
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return sidebarFailure("not_found", "The sidebar item could not be found.")
		case errors.Is(err, store.ErrSectionMissing):
			return sidebarFailure("section_not_found", "The section could not be found.")
		case errors.Is(err, store.ErrChatPinned):
			return sidebarFailure("chat_pinned", "Unpin the chat before moving it.")
		case errors.Is(err, store.ErrSectionName):
			return sidebarFailure("invalid_name", "Use a section name within forty characters.")
		case errors.Is(err, store.ErrSectionTaken):
			return sidebarFailure("name_taken", "A section already uses this name.")
		case errors.Is(err, store.ErrSectionLimit):
			return sidebarFailure("limit_reached", "You can create up to thirty sections.")
		case errors.Is(err, store.ErrPinLimit):
			return sidebarFailure("pin_limit", "You can pin up to twelve chats.")
		case errors.Is(err, store.ErrSidebarRate):
			return sidebarFailure("rate_limit", "Try again in a minute.")
		case errors.Is(err, store.ErrAnchor), errors.Is(err, store.ErrPlacement):
			return sidebarFailure("bad_anchor", "Choose an item in the same sidebar list.")
		default:
			return agent.AgentToolResult{}, err
		}
	}
	result["changed"] = change.Changed
	return toolResult(result)
}

func sidebarFailure(code, message string) (agent.AgentToolResult, error) {
	result, err := toolResult(map[string]any{"error": code, "message": message, "changed": false})
	result.IsError = true
	return result, err
}
