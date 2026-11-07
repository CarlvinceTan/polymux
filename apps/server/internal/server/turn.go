package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
)

var errGroupBudget = errors.New("group output budget exhausted")

// spend adds n bytes to the group's output and reports whether it is over budget.
func (c *chainState) spend(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.output += n
	return c.output > 128<<10
}

// turnInput adds the group, peer, work, memory and result context a turn needs.
func (s *Server) turnInput(ctx context.Context, a *actor, run contract.Run, work store.Work, user contract.Message) (contract.Message, error) {
	var err error
	if a.threadID != "" {
		user, err = s.groupContext(ctx, a, user)
	} else if work.Trigger != contract.RunTriggerCreated {
		user, err = s.peerContext(ctx, store.Work{Trigger: work.Trigger, Message: user, DeliveryID: work.DeliveryID, ParentRunID: work.ParentRunID, Hops: work.Hops, FromAgentID: work.FromAgentID})
	}
	if err != nil {
		return user, err
	}
	if work.Trigger != contract.RunTriggerChild {
		user = s.clientContext(ctx, work, user)
	}
	if work.Trigger != contract.RunTriggerCreated && work.Trigger != contract.RunTriggerChild {
		if user, err = s.workContext(ctx, run, user); err != nil {
			return user, err
		}
	}
	// Memory joins each model request separately (freshMemory); a store that can't be read fails the turn here,
	// before the model could answer as if nothing were remembered.
	if work.Trigger != contract.RunTriggerChild {
		if _, err = s.memoryContext(ctx, run, contract.Message{}, store.Text(work.Message)); err != nil {
			return user, err
		}
	}
	if automatic(work.Trigger) {
		if user, err = s.autoContext(ctx, run, work, user); err != nil {
			return user, err
		}
	}
	if work.Trigger == contract.RunTriggerMessage && a.threadID == "" {
		return s.resultContext(ctx, run, user)
	}
	return user, nil
}

// turnTools lists the server tools a turn adds to its definition.
func (s *Server) turnTools(a *actor, work store.Work, state *turnState) []agent.AgentTool {
	var tools []agent.AgentTool
	if work.Trigger != contract.RunTriggerChild && work.Trigger != contract.RunTriggerReview {
		tools = append(tools, &statusTool{})
	}
	if work.Trigger != contract.RunTriggerReview {
		tools = append(tools, s.sidebarTools()...)
	}
	if work.Trigger != contract.RunTriggerChild {
		tools = append(tools,
			&peerTool{server: s, handoff: a.threadID != ""}, &groupTool{server: s}, &routineTool{server: s}, &triggerTool{server: s},
			&agentTool{server: s, action: "create", models: s.hasModels()}, &agentTool{server: s, action: "update"}, &agentTool{server: s, action: "delete"}, &groupEdit{server: s}, &chatTool{store: s.Store}, &chatTool{store: s.Store, question: true}, &reactionTool{store: s.Store})
		tools = append(tools, &delegateTool{server: s, agentID: a.id})
		// Choosing a model only means something once a provider offers models.
		if s.hasModels() {
			tools = append(tools, &modelTool{server: s})
		}
	}
	if work.Trigger != contract.RunTriggerChild && a.threadID == "" {
		tools = append(tools, &integrationTool{server: s}, &taskThreadTool{server: s, action: "create"})
		state.external["manage_integrations"] = true
	}
	if work.Trigger != contract.RunTriggerChild {
		tools = append(tools, &workTool{store: s.Store}, &taskTool{server: s}, &historyTool{store: s.Store},
			&taskThreadTool{server: s, action: "read"}, &taskThreadTool{server: s, action: "update"})
		for _, action := range []string{"save", "recall", "forget"} {
			tools = append(tools, &memoryTool{store: s.Store, provider: s.memory, action: action})
		}
		tools = append(tools, &projectTool{store: s.Store})
	}
	if work.Trigger == contract.RunTriggerReview {
		tools = append(tools, &reviewTool{})
	}
	tools = append(tools,
		&htmlTool{store: s.Store, dir: filepath.Join(s.config.DataDir, "attachments")},
		&attachTool{files: s.outputs}, &mediaTool{server: s, files: s.outputs, kind: "image"}, &mediaTool{server: s, files: s.outputs, kind: "video"}, &httpTool{server: s})
	// Saved secrets work in every chat, so asking for, listing, forgetting and writing them does too.
	for _, action := range []string{"request", "list", "forget", "write"} {
		tools = append(tools, &credentialTool{server: s, action: action})
	}
	for _, action := range []string{"list", "manage", "import", "reveal", "request", "write", "fill", "passkey"} {
		tools = append(tools, &vaultTool{server: s, action: action})
	}
	// Device tools only appear once a device allows them, so they cost nothing otherwise.
	for _, capability := range s.clients.allowed() {
		tools = append(tools, &clientTool{server: s, capability: capability})
	}
	return tools
}

// externalTools names the tools whose effects must be recorded before they run.
func externalTools(d agentruntime.Definition) map[string]bool {
	external := map[string]bool{"generate_image": true, "generate_video": true, "http_request": true, "control_home": true, "manage_alarms": true}
	if len(d.Devices) > 0 {
		external["run_device"] = true
	}
	for _, name := range d.Tools {
		external[name] = true
	}
	for _, tool := range d.ExtraTools {
		external[tool.Name()] = true
	}
	return external
}

// fileActivity reports a write or edit that saves a skill or instructions file.
func fileActivity(delta agentruntime.Delta) (contract.Activity, bool, error) {
	if delta.Tool != "write" && delta.Tool != "edit" {
		return contract.Activity{}, false, nil
	}
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(delta.Args), &input) != nil {
		return contract.Activity{}, false, nil
	}
	kind, label := "", filepath.Base(input.Path)
	switch label {
	case "SKILL.md":
		kind, label = "skill.saved", filepath.Base(filepath.Dir(input.Path))
	case "AGENTS.md", "CLAUDE.md", "GEMINI.md":
		kind = "instructions.updated"
	}
	if kind == "" {
		return contract.Activity{}, false, nil
	}
	payload, err := store.NewActivity(kind, input.Path, label)
	return payload, err == nil, err
}

// isolatedTurn runs apart from the agent's chat session.
func isolatedTurn(trigger string) bool {
	return trigger == contract.RunTriggerChild || automatic(trigger) && trigger != contract.RunTriggerFollowUp
}

// splitBeats gives each paragraph of a reply its own bubble, leaving code blocks whole.
func splitBeats(parts []contract.Part) []contract.Part {
	var beats []contract.Part
	for _, part := range parts {
		if part.Kind != contract.PartKindText || part.Text == nil || strings.Contains(*part.Text, "```") {
			beats = append(beats, part)
			continue
		}
		for _, beat := range strings.Split(*part.Text, "\n\n") {
			if beat = strings.TrimSpace(beat); beat != "" {
				beats = append(beats, contract.Part{Kind: contract.PartKindText, Text: &beat})
			}
		}
	}
	return beats
}

// setText replaces the message's first text part, or adds one.
func setText(m *contract.Message, text *string) {
	for n := range m.Parts {
		if m.Parts[n].Kind == contract.PartKindText {
			m.Parts[n].Text = text
			return
		}
	}
	m.Parts = append(m.Parts, contract.Part{Kind: contract.PartKindText, Text: text})
}

// replyOutcome names how a successful turn ended and summarises it.
func replyOutcome(trigger string, parts []contract.Part, silent, handed, checkpoint, waiting bool) (outcome, summary string) {
	outcome, summary = "reply", "Reply complete"
	if silent {
		outcome = "silent"
	}
	if trigger == contract.RunTriggerCreated {
		outcome = "introduced"
	}
	if handed {
		outcome = "handoff"
	}
	if checkpoint {
		outcome, summary = "checkpoint", "Task checkpoint saved"
	}
	if waiting && !handed {
		outcome, summary = "waiting_input", "Waiting for your input; independent work can continue"
	}
	for _, part := range parts {
		if part.Attachment != nil && part.Attachment.Kind == contract.AttachmentKindHTML {
			outcome = "artifact"
		}
	}
	return outcome, summary
}

// failure maps a failed turn to its client error and run outcome.
func failure(err error, interrupted bool, attempt int64, background bool) (code, message, status, summary string) {
	code, message = "provider", "The agent could not complete this reply."
	status, summary = contract.RunStatusFailed, "Reply failed"
	if strings.Contains(err.Error(), "no providers are available right now") {
		code, message, summary = "providers_unavailable", "No providers are available right now.", "Providers unavailable"
	}
	if strings.Contains(err.Error(), "this bot's model is unavailable right now") {
		code, message, summary = "model_unavailable", "This bot’s model is unavailable right now.", "Model unavailable"
	}
	if errors.Is(err, agentruntime.ErrProviderMissing) {
		code, message, summary = "provider_missing", "This bot’s provider is no longer configured. Choose a model in its profile.", "Provider unavailable"
	}
	if errors.Is(err, errNoProvider) {
		code, message, summary = "no_provider", "No provider is set up. Add one in Settings.", "No provider"
	}
	if errors.Is(err, errEmptyReply) {
		code, message, summary = "empty", "The agent returned an empty reply.", "Empty reply"
	}
	if errors.Is(err, context.Canceled) {
		code, message = "stopped", "Reply stopped."
		status, summary = contract.RunStatusCancelled, "Reply stopped"
	}
	if errors.Is(err, store.ErrMembers) {
		code, message = "membership", "Agent is no longer a member of this chat."
		status, summary = contract.RunStatusCancelled, "Member removed"
	}
	var repoErr *agentruntime.RepoError
	if errors.As(err, &repoErr) {
		code, message = "workspace", repoErr.Error()
		status, summary = contract.RunStatusFailed, "Repository unavailable"
	}
	if interrupted {
		code, message = "interrupted", "The server stopped during this reply. Review before retrying."
		status, summary = contract.RunStatusInterrupted, "Execution interrupted"
		if attempt > 1 {
			message = "The server restarted again during resume. Review before retrying."
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code, message = "timeout", "The model took too long to reply."
		if background {
			// Agent context only. The parent follow-up speaks to the user.
			message = "The background task timed out and was stopped."
			summary = "Background work timed out"
		}
	}
	return code, message, status, summary
}
