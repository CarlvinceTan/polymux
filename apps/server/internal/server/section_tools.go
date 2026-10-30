package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// sidebarActions are what organize_chats can do, one tool rather than eight.
var sidebarActions = []string{"list_sections", "create_section", "rename_section", "delete_section", "pin_chat", "unpin_chat", "move_chat", "reorder"}

func (s *Server) sidebarTools() []agent.AgentTool {
	return []agent.AgentTool{&sectionTool{server: s}}
}

type sectionTool struct {
	server *Server
}

func (*sectionTool) Name() string                           { return "organize_chats" }
func (*sectionTool) Label() string                          { return "Organize chats" }
func (*sectionTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*sectionTool) ToolClass() string                      { return "reversible" }
func (*sectionTool) Description() string {
	return "Organize the user's chat list: sections, pins and placement. list_sections first to get stable IDs. Unassigned is built in and cannot be renamed or deleted. " +
		"create_section needs name (after_section_id optional); rename_section needs section_id and name; delete_section needs section_id and returns its chats to the main list. " +
		"pin_chat/unpin_chat need chat_id; move_chat needs chat_id and section_id (null for the main list), and a pinned chat must be unpinned first. " +
		"reorder needs kind (sections or chats), id, and before_id or after_id in the same list. Repeating a state is a no-op; at most thirty changes a minute."
}
func (t *sectionTool) Schema() ai.ToolSchema {
	text := map[string]any{"type": "string"}
	properties := map[string]any{
		"action":           map[string]any{"type": "string", "enum": sidebarActions},
		"name":             text,
		"section_id":       map[string]any{"type": []string{"string", "null"}},
		"after_section_id": text,
		"chat_id":          text,
		"kind":             map[string]any{"type": "string", "enum": []string{"sections", "chats"}},
		"id":               text,
		"before_id":        text,
		"after_id":         text,
	}
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": []string{"action"}}}
}

func (t *sectionTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil || state.work.Trigger == contract.RunTriggerCreated || state.work.Trigger == contract.RunTriggerReview {
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
		Action         string          `json:"action"`
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
	switch input.Action {
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
		if input.Name == "" {
			return sidebarFailure("invalid_name", "Give the section a name.")
		}
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
		if input.ChatID == "" {
			return sidebarFailure("not_found", "Choose a chat ID.")
		}
		pinned := input.Action == "pin_chat"
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
		return sidebarFailure("invalid_action", "Choose one of: "+strings.Join(sidebarActions, ", ")+".")
	}
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return sidebarFailure("not_found", "The sidebar item could not be found.")
		case errors.Is(err, store.ErrSectionMissing):
			return sidebarFailure("section_not_found", "The section could not be found.")
		case errors.Is(err, store.ErrChatPinned):
			return sidebarFailure("chat_pinned", "Unpin the chat before moving it.")
		case errors.Is(err, store.ErrSectionFixed):
			return sidebarFailure("fixed_section", "Unassigned cannot be renamed or removed.")
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
