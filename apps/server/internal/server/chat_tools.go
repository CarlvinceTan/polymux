package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type chatTool struct {
	store    *store.Store
	question bool
}

func (t *chatTool) Name() string {
	if t.question {
		return "ask_user"
	}
	return "send_to_user"
}
func (*chatTool) Label() string                          { return "Message user" }
func (*chatTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*chatTool) ToolClass() string                      { return "reversible" }
func (t *chatTool) Description() string {
	if t.question {
		return "Post a question card with choices and optional free text. Continue independent work; the user's answer starts a new turn in this chat. Use stable unique option IDs. Only ask when the answer is needed."
	}
	return "Send one short message bubble to the user's chat immediately, then continue. Use it for each beat of a reply, an acknowledgement before real work, a real update or a result."
}
func (t *chatTool) Schema() ai.ToolSchema {
	properties := map[string]any{"text": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}}
	required := []string{"text"}
	if t.question {
		properties = map[string]any{
			"question":   map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
			"options":    map[string]any{"type": "array", "minItems": 2, "maxItems": 8, "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"id": map[string]any{"type": "string", "maxLength": 64}, "label": map[string]any{"type": "string", "maxLength": 256}}, "required": []string{"id", "label"}}},
			"allow_text": map[string]any{"type": "boolean", "description": "Allow an alternative answer; defaults to true"},
		}
		required = []string{"question", "options"}
	}
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}}
}
func (t *chatTool) Execute(ctx context.Context, call string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active turn")
	}
	var input struct {
		Text      string                  `json:"text"`
		Question  string                  `json:"question"`
		Options   []contract.ChoiceOption `json:"options"`
		AllowText *bool                   `json:"allow_text"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	part := contract.Part{Kind: contract.PartKindText, Text: &input.Text}
	if t.question {
		input.Question = strings.TrimSpace(input.Question)
		if input.Question == "" || len(input.Question) > 4096 || len(input.Options) < 2 || len(input.Options) > 8 {
			return agent.AgentToolResult{}, errors.New("provide a question and two to eight choices")
		}
		seen := map[string]bool{}
		for _, option := range input.Options {
			if strings.TrimSpace(option.ID) == "" || strings.HasPrefix(option.ID, "_") || len(option.ID) > 64 || strings.TrimSpace(option.Label) == "" || len(option.Label) > 256 || seen[option.ID] {
				return agent.AgentToolResult{}, errors.New("provide unique option ids and nonempty labels")
			}
			seen[option.ID] = true
		}
		allow := input.AllowText == nil || *input.AllowText
		part = contract.Part{Kind: contract.PartKindChoice, Question: &input.Question, Options: input.Options, AllowText: &allow}
	} else if strings.TrimSpace(input.Text) == "" || len(input.Text) > 8192 {
		return agent.AgentToolResult{}, errors.New("provide a short nonempty message")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	chain, _ := ctx.Value(chainKey{}).(*chainState)
	base := t.Name() + ":" + state.run.ID + ":" + call
	var source *string
	if chain != nil {
		source = state.run.SourceMessageID
	}
	beats := splitBeats([]contract.Part{part})
	ids := make([]string, 0, len(beats))
	posted := 0
	for n, beat := range beats {
		id := base
		if len(beats) > 1 {
			id = fmt.Sprintf("%s:%d", base, n)
		}
		if chain != nil {
			old, err := t.store.Message(ctx, id)
			if err == nil {
				if old.ThreadID != state.run.ConversationID || value(old.AgentID) != state.run.AgentID {
					return agent.AgentToolResult{}, store.ErrConflict
				}
				ids = append(ids, old.ID)
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return agent.AgentToolResult{}, err
			}
			encoded, err := json.Marshal(beat)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			if chain.spend(len(encoded)) {
				state.hookErr = errGroupBudget
				return agent.AgentToolResult{}, errGroupBudget
			}
		}
		message, err := t.store.PostMessage(ctx, contract.Message{ID: id, ThreadID: state.run.ConversationID, Role: contract.MessageRoleAssistant, Status: contract.MessageStatusDone, CreatedAt: store.Now(), AgentID: &state.run.AgentID, RunID: &state.run.ID, ReplyToID: source, Parts: []contract.Part{beat}})
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		ids = append(ids, message.ID)
		posted++
	}
	state.posts += posted
	state.spoke = true
	if len(ids) == 1 {
		return toolResult(map[string]any{"message_id": ids[0], "posted": true})
	}
	return toolResult(map[string]any{"message_ids": ids, "posted": true})
}

func (s *Server) answerChoice(w http.ResponseWriter, r *http.Request) {
	thread, ok := s.thread(w, r)
	if !ok {
		return
	}
	var answer contract.ChoiceAnswer
	if !decode(w, r, &answer) {
		return
	}
	card, err := s.Store.AnswerChoice(r.Context(), thread.ID, r.PathValue("messageID"), answer)
	if errors.Is(err, store.ErrChoice) {
		fail(w, http.StatusConflict, "choice", "This question is unavailable or has already been answered.")
		return
	}
	if err == nil && !s.confirmInstall(r.Context(), thread.ID, card, answer) {
		fail(w, http.StatusConflict, "plugin", "This plugin can't be installed.")
		return
	}
	s.respond(w, card, err)
}
