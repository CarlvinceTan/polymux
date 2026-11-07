package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

func (s *Server) react(w http.ResponseWriter, r *http.Request) {
	thread, ok := s.thread(w, r)
	if !ok {
		return
	}
	var request contract.ReactionRequest
	if !decode(w, r, &request) {
		return
	}
	message, err := s.Store.React(r.Context(), thread.ID, r.PathValue("messageID"), request, nil)
	if errors.Is(err, store.ErrReaction) {
		fail(w, http.StatusBadRequest, "reaction", "This reaction or message is unavailable.")
		return
	}
	s.respond(w, message, err)
}

type reactionTool struct{ store *store.Store }

func (*reactionTool) Name() string                           { return "react_to_message" }
func (*reactionTool) Label() string                          { return "React to message" }
func (*reactionTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*reactionTool) ToolClass() string                      { return "reversible" }
func (t *reactionTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: "Add or remove your emoji reaction on a message in this chat. Omit message_id to react to the user's current message; use an ID from recall_history for an older message. Repeating the same request does not toggle it. A reaction can acknowledge a message without a text reply.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"emoji":      map[string]any{"type": "string", "minLength": 1, "maxLength": 32, "description": "One emoji, including skin tones and joined emoji"},
			"message_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"remove":     map[string]any{"type": "boolean", "description": "Remove your reaction instead of adding it; defaults to false"},
		},
		"required": []string{"emoji"},
	}}
}

func (t *reactionTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		Emoji     string `json:"emoji"`
		MessageID string `json:"message_id"`
		Remove    bool   `json:"remove"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if input.MessageID == "" {
		input.MessageID = state.work.Message.ID
	}
	if len(input.MessageID) > 128 {
		return agent.AgentToolResult{}, store.ErrReaction
	}
	message, err := t.store.React(ctx, state.run.ConversationID, input.MessageID, contract.ReactionRequest{Emoji: input.Emoji, Remove: input.Remove}, &state.run.AgentID)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	state.mu.Lock()
	state.posts++
	state.mu.Unlock()
	return toolResult(map[string]any{"message_id": message.ID, "reactions": message.Reactions})
}
