package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

const (
	// Bots can't post either from ask_user, so only a person's answer to this tool's card installs.
	installMessage = "install_plugin:"
	installChoice  = "_install"
)

type pluginInstallTool struct{ server *Server }

func (*pluginInstallTool) Name() string                           { return "install_plugin" }
func (*pluginInstallTool) Label() string                          { return "Install plugin" }
func (*pluginInstallTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*pluginInstallTool) ToolClass() string                      { return "reversible" }
func (*pluginInstallTool) Description() string {
	return "Check a plugin you built in a folder on this server and ask the user to install it. It stays out of the Marketplace. The user's answer starts a new turn; once installed it appears in this chat. Read the plugin-creator skill first."
}
func (t *pluginInstallTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"path"},
		"properties": map[string]any{"path": map[string]any{"type": "string", "maxLength": 2048, "description": "Absolute path to the folder holding plugin.json"}},
	}}
}

func (t *pluginInstallTool) Execute(ctx context.Context, call string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	s := t.server
	state, err := scopedTurn(ctx, s.Store)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	if state.run.AgentID != state.run.ConversationID {
		return agent.AgentToolResult{}, errors.New("plugins are installed from a direct chat")
	}
	// A plugin runs outside its chat's rules, so it can't come from a chat that has any.
	if s.plugins.Gated(ctx, state.chats) {
		return agent.AgentToolResult{}, errors.New("this chat's rules don't allow installing plugins")
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if !filepath.IsAbs(input.Path) {
		return agent.AgentToolResult{}, errors.New("use the absolute path of the plugin's folder")
	}
	manifest, err := s.plugins.Stage(ctx, input.Path)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	question := strings.TrimSpace("Install " + manifest.Title + "? " + manifest.Description)
	if manifest.Service != "" {
		question += " It runs a program on this server with the server's access."
	}
	allow := false
	state.mu.Lock()
	defer state.mu.Unlock()
	message, err := s.Store.PostMessage(ctx, contract.Message{ID: installMessage + manifest.Name + ":" + state.run.ID + ":" + call,
		ThreadID: state.run.ConversationID, Role: contract.MessageRoleAssistant, Status: contract.MessageStatusDone, CreatedAt: store.Now(),
		AgentID: &state.run.AgentID, RunID: &state.run.ID, Parts: []contract.Part{{Kind: contract.PartKindChoice, Question: &question, AllowText: &allow,
			Options: []contract.ChoiceOption{{ID: installChoice, Label: "Install"}, {ID: "_skip", Label: "Not now"}}}}})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	state.posts++
	state.spoke = true
	return toolResult(map[string]any{"message_id": message.ID, "waiting": "The user decides; their answer starts a new turn."})
}

// confirmInstall reports false when someone chose to install a plugin and it couldn't be.
func (s *Server) confirmInstall(ctx context.Context, thread string, card contract.Message, answer contract.ChoiceAnswer) bool {
	rest, ok := strings.CutPrefix(card.ID, installMessage)
	if !ok || answer.OptionID != installChoice {
		return true
	}
	name, _, _ := strings.Cut(rest, ":")
	if _, err := s.plugins.Adopt(ctx, name, thread, s.agentTitle(ctx, card.AgentID)); err != nil {
		s.config.Log.Warn("install local plugin", "plugin", name, "error", err)
		return false
	}
	return true
}

func (s *Server) agentTitle(ctx context.Context, id *string) string {
	if agents, err := s.Store.Agents(ctx); err == nil && id != nil {
		if position := slices.IndexFunc(agents, func(item contract.Agent) bool { return item.ID == *id }); position >= 0 {
			return agents[position].Name
		}
	}
	return "a bot"
}
