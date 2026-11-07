package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type voiceKey struct{}
type voiceTask struct {
	target, text, trigger string
}
type voiceReply struct {
	AgentID string `json:"agentID"`
	Name    string `json:"name"`
	Text    string `json:"text"`
}
type voiceWork struct {
	server    *Server
	id        string
	thread    contract.Thread
	cancel    context.CancelFunc
	done      chan struct{}
	targets   map[string]bool // Guarded by actorsMu, including queued descendants.
	tasks     []voiceTask
	notes     []voiceReply
	chain     *chainState
	published map[string]string
	publishMu sync.Mutex
	device    string // The device that started the conversation, preferred for device tools.
}

// voice is a private consultation for a live spokesperson.
func (s *Server) voice(w http.ResponseWriter, r *http.Request) {
	thread, ok := s.thread(w, r)
	if !ok {
		return
	}
	// Speech is one to one: a direct chat, or one of its bot's task threads.
	if thread.ID != thread.AgentID && thread.Task == nil {
		fail(w, 400, "invalid_request", "Speech conversations are only available in direct chats.")
		return
	}
	var request contract.VoiceRequest
	if !decode(w, r, &request) {
		return
	}
	if request.ID == "" || len(request.ID) > 128 || strings.TrimSpace(request.Text) == "" || len(request.Text) > 65536 || !utf8.ValidString(request.Text) {
		fail(w, 400, "invalid_request", "Voice tasks require a bounded identity and text.")
		return
	}
	s.clients.touch(clientID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	work := &voiceWork{server: s, id: ID(), thread: thread, cancel: cancel, done: make(chan struct{}), device: clientID(r.Context()),
		targets: map[string]bool{}, chain: &chainState{}, published: map[string]string{}}
	s.actorsMu.Lock()
	if s.closing || s.voiceSeen[request.ID] || len(s.voiceSeen) >= 4096 || len(s.voices) >= 16 {
		s.actorsMu.Unlock()
		fail(w, 409, "voice_unavailable", "This voice task was already submitted, or the private voice channel is full. Tasks are never automatically replayed.")
		return
	}
	work.targets[thread.AgentID] = true
	work.tasks = append(work.tasks, voiceTask{target: thread.AgentID, text: request.Text, trigger: "voice"})
	s.voiceSeen[request.ID] = true
	s.voices[work.id] = work
	s.workers.Add(1)
	s.actorsMu.Unlock()
	defer func() {
		s.actorsMu.Lock()
		delete(s.voices, work.id)
		close(work.done)
		s.actorsMu.Unlock()
		s.workers.Done()
	}()
	ctx = context.WithValue(ctx, voiceKey{}, work)
	ctx = context.WithValue(ctx, chainKey{}, work.chain)
	var replies []voiceReply
	for index := 0; index < len(work.tasks); index++ {
		task := work.tasks[index]
		result, name, err := work.run(ctx, task)
		if err != nil {
			fail(w, 502, "voice_task", "The private consultation failed; its outcome may be uncertain. Do not retry automatically.")
			return
		}
		replies = append(replies, voiceReply{task.target, name, result.Text})
	}
	if len(replies) == 0 {
		fail(w, 409, "voice_unavailable", "No selected agent is available.")
		return
	}
	data, err := json.Marshal(append(replies, work.notes...))
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	write(w, 200, contract.VoiceResponse{Text: string(data)})
}

func (v *voiceWork) run(ctx context.Context, task voiceTask) (result agentruntime.Result, name string, err error) {
	var empty agentruntime.Result
	// The consultation has its own session, so it doesn't wait for the bot's chat turns.
	release, err := v.server.acquire(ctx, "voice-"+v.id+"/"+task.target)
	if err != nil {
		return empty, "", err
	}
	defer release()
	v.server.actorsMu.RLock()
	a := v.server.actors[task.target]
	_, err = v.server.Store.Thread(ctx, v.thread.ID)
	if a == nil || v.server.closing || v.server.deletes[task.target] != nil || err != nil {
		v.server.actorsMu.RUnlock()
		return empty, "", errors.New("voice target is unavailable")
	}
	a.mu.Lock()
	d := a.definition
	a.mu.Unlock()
	v.server.actorsMu.RUnlock()
	d.MCP, err = v.server.effectiveMCP(ctx, task.target, d.MCP)
	if err != nil {
		return empty, "", err
	}
	d.Ephemeral = true
	d.Scope = "voice-" + v.id
	d.BeforeTool = beforeTool
	d.AfterInner = afterInner
	d.ExtraTools = append(slices.Clone(d.ExtraTools), &htmlTool{store: v.server.Store, dir: filepath.Join(v.server.config.DataDir, "attachments")},
		&peerTool{server: v.server, private: true}, &voicePublish{work: v, agentID: task.target},
		&historyTool{store: v.server.Store}, &workTool{store: v.server.Store}, &routineTool{server: v.server})
	for _, action := range []string{"save", "recall", "forget"} {
		d.ExtraTools = append(d.ExtraTools, &memoryTool{store: v.server.Store, provider: v.server.memory, action: action})
	}
	d.ExtraTools = append(d.ExtraTools, v.server.sidebarTools()...)
	d.ExtraTools = append(d.ExtraTools, &attachTool{files: v.server.outputs}, &mediaTool{server: v.server, files: v.server.outputs, kind: "image"}, &mediaTool{server: v.server, files: v.server.outputs, kind: "video"}, &httpTool{server: v.server})
	for _, action := range []string{"request", "list", "forget", "write"} {
		d.ExtraTools = append(d.ExtraTools, &credentialTool{server: v.server, action: action})
	}
	for _, action := range []string{"list", "manage", "import", "request", "write", "fill", "passkey"} {
		d.ExtraTools = append(d.ExtraTools, &vaultTool{server: v.server, action: action})
	}
	for _, capability := range v.server.clients.allowed() {
		d.ExtraTools = append(d.ExtraTools, &clientTool{server: v.server, capability: capability})
	}
	d.Prompt = d.SystemPrompt() + "\n\n" + v.role()
	// Persist the private run without emitting chat events.
	jobID := "voice-job:" + v.id + ":" + task.target
	// A consulted peer keeps its own chat's scope; the thread's bot keeps this thread's.
	scope := task.target
	if task.target == v.thread.AgentID {
		scope = v.thread.ID
	}
	run := store.NewRun(jobID, scope, task.target, task.trigger)
	run.Status = contract.RunStatusRunning
	started := store.Now()
	run.StartedAt = &started
	if err := v.server.Store.PutPrivateRun(ctx, run); err != nil {
		return empty, "", err
	}
	state := &turnState{
		run: run, work: store.Work{Trigger: task.trigger, DeviceID: v.device}, store: v.server.Store,
		external: map[string]bool{},
	}
	defer func() {
		if err == nil {
			err = ctx.Err()
		}
		state.mu.Lock()
		err = errors.Join(err, state.hookErr)
		state.mu.Unlock()
		finished := store.Now()
		if err != nil {
			run.Status, run.Summary, run.FinishedAt = contract.RunStatusFailed, "Private consultation failed", &finished
			if errors.Is(err, context.Canceled) {
				run.Status = contract.RunStatusCancelled
			}
			run.Error = &contract.Error{Code: "voice_task", Message: "The private consultation failed; its outcome may be uncertain."}
		} else {
			run.Status, run.Summary, run.FinishedAt = contract.RunStatusCompleted, "Private consultation complete", &finished
			outcome := "reply"
			run.Outcome = &outcome
		}
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if e := v.server.Store.PutPrivateRun(writeCtx, run); e != nil && err == nil {
			err = e
		}
	}()
	for _, name := range d.Tools {
		state.external[name] = true
	}
	for _, name := range []string{"generate_image", "generate_video", "http_request", "control_home", "manage_alarms"} {
		state.external[name] = true
	}
	if len(d.Devices) > 0 {
		state.external["run_device"] = true
	}
	for _, connection := range d.MCP {
		state.prefixes = append(state.prefixes, connection.Name+"__")
	}
	ctx = context.WithValue(ctx, turnKey{}, state)
	ctx = context.WithValue(ctx, htmlKey{}, &htmlScope{messageID: state.run.ID, cwd: outputCWD(d, v.server.config.DataDir), publish: func(ctx context.Context, attachment contract.Attachment, path string) error {
		return v.publish(ctx, task.target, attachment.ID, "", &attachment, path)
	}})
	message := contract.Message{ID: state.run.ID, ThreadID: scope, Role: contract.MessageRoleUser, Parts: []contract.Part{{Kind: contract.PartKindText, Text: &task.text}}, CreatedAt: store.Now()}
	message, err = v.server.peerContext(ctx, store.Work{Message: message, Trigger: task.trigger})
	if err == nil {
		message, err = v.server.memoryContext(ctx, run, message, task.text)
	}
	if err == nil {
		message, err = v.server.workContext(ctx, run, message)
	}
	if err == nil {
		message, err = v.server.autoContext(ctx, run, state.work, message)
	}
	if err != nil {
		return empty, "", err
	}
	d.TurnContext = func() (context.Context, bool) { return ctx, ctx.Err() == nil }
	if d, err = v.server.withModel(d); err != nil {
		return empty, "", err
	}
	session, err := v.server.config.Factory.Open(ctx, d, "")
	if err != nil {
		return empty, "", err
	}
	defer func() {
		if err := session.Close(); err != nil {
			v.server.config.Log.Warn("close private voice session", "error", err)
		}
		v.server.discard(context.WithoutCancel(ctx), session)
	}()
	aborted := make(chan struct{})
	stopAbort := context.AfterFunc(ctx, func() {
		defer close(aborted)
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if err := session.Abort(abortCtx); err != nil && !errors.Is(err, context.Canceled) {
			v.server.config.Log.Warn("abort private voice session", "error", err)
		}
	})
	defer func() {
		if !stopAbort() {
			<-aborted
		}
	}()
	output := 0
	result, err = session.Send(ctx, message, func(delta agentruntime.Delta) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if delta.Kind == "tool.done" {
			state.mu.Lock()
			defer state.mu.Unlock()
			if externalTool(state, delta.Tool) {
				if err := v.server.Store.EndEffect(ctx, run.ID, delta.ToolID, delta.Failed); err != nil {
					state.hookErr = err
					return err
				}
			}
		}
		output += len(delta.Text)
		v.chain.mu.Lock()
		defer v.chain.mu.Unlock()
		v.chain.output += len(delta.Text)
		if output > 64<<10 || v.chain.output > 128<<10 {
			return errors.New("voice output budget exhausted")
		}
		return nil
	})
	if len(result.Text) > 64<<10 {
		return empty, "", errors.New("voice output budget exhausted")
	}
	if len(result.Text) > output {
		v.chain.mu.Lock()
		v.chain.output += len(result.Text) - output
		over := v.chain.output > 128<<10
		v.chain.mu.Unlock()
		if over {
			return empty, "", errors.New("voice output budget exhausted")
		}
	}
	return result, d.Agent.Name, err
}

func (v *voiceWork) peer(ctx context.Context, tool *peerTool, target, text, intent string) (agent.AgentToolResult, error) {
	state, _ := ctx.Value(turnKey{}).(*turnState)
	if state == nil {
		return agent.AgentToolResult{}, errors.New("no active private turn")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if target == state.run.AgentID {
		return agent.AgentToolResult{}, errors.New("invalid private dispatch")
	}
	if tool.handoff {
		return agent.AgentToolResult{}, errors.New("invalid private coordination mode")
	}
	if state.work.Trigger == contract.RunTriggerPeer && (intent == "request" || intent == "question") {
		return agent.AgentToolResult{}, errors.New("peer requests cannot recursively wake peers")
	}
	if !slices.Contains([]string{"request", "question", "result", "status"}, intent) {
		return agent.AgentToolResult{}, errors.New("invalid private delivery intent")
	}
	if err := ctx.Err(); err != nil {
		return agent.AgentToolResult{}, err
	}
	v.server.actorsMu.Lock()
	defer v.server.actorsMu.Unlock()
	if _, err := v.server.Store.Thread(ctx, v.thread.ID); err != nil {
		return agent.AgentToolResult{}, err
	}
	if v.server.closing || v.server.actors[target] == nil || v.server.deletes[target] != nil {
		return agent.AgentToolResult{}, errors.New("peer is unavailable")
	}
	v.chain.mu.Lock()
	defer v.chain.mu.Unlock()
	if len(v.tasks) >= 64 {
		return agent.AgentToolResult{}, errors.New("voice delegation budget exhausted")
	}
	if intent == "request" || intent == "question" {
		v.targets[target] = true
		v.tasks = append(v.tasks, voiceTask{target: target, text: text, trigger: "peer"})
	} else {
		v.chain.output += len(text)
		if v.chain.output > 128<<10 || len(v.notes) >= 64 {
			return agent.AgentToolResult{}, errors.New("voice message budget exhausted")
		}
		v.notes = append(v.notes, voiceReply{AgentID: state.run.AgentID, Text: "Private " + intent + " to " + target + ": " + text})
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "Private message accepted; results return to the spokesperson, not the user's chat."}}}, nil
}

func (v *voiceWork) publish(ctx context.Context, source, callID, text string, attachment *contract.Attachment, path string) error {
	v.publishMu.Lock()
	defer v.publishMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(v.id + "\x00" + source + "\x00" + callID))
	id := "voice-publish-" + hex.EncodeToString(sum[:16])
	payload := text
	if attachment != nil {
		payload = attachment.ID
	}
	digest := sha256.Sum256([]byte(payload))
	fingerprint := hex.EncodeToString(digest[:])
	if previous := v.published[id]; previous != "" {
		if previous != fingerprint {
			return errors.New("publication identity reused with different content")
		}
		return nil
	}
	v.server.actorsMu.RLock()
	defer v.server.actorsMu.RUnlock()
	thread, err := v.server.Store.Thread(ctx, v.thread.ID)
	if err != nil {
		return err
	}
	if v.server.closing || v.server.deletes[source] != nil || !slices.Contains(thread.AgentIDs, source) {
		return errors.New("publication target is unavailable")
	}
	message := contract.Message{ID: id, ThreadID: thread.ID, Role: contract.MessageRoleAssistant, AgentID: &source, Status: contract.MessageStatusDone, CreatedAt: store.Now(), Parts: []contract.Part{}}
	if attachment != nil {
		message.Parts = append(message.Parts, contract.Part{Kind: contract.PartKindAttachment, Attachment: attachment})
	} else {
		message.Parts = append(message.Parts, contract.Part{Kind: contract.PartKindText, Text: &text})
	}
	event := contract.Event{ConversationID: thread.ID, Type: contract.EventTypeMessageEnd, MessageID: &id, AgentID: &source}
	if attachment != nil {
		_, err = v.server.Store.PublishAttachment(ctx, *attachment, path, event, &message)
	} else {
		_, err = v.server.Store.Append(ctx, event, &message)
	}
	if err == nil {
		v.published[id] = fingerprint
	}
	return err
}

type voicePublish struct {
	work    *voiceWork
	agentID string
}

func (*voicePublish) Name() string                           { return "send_to_user" }
func (*voicePublish) Label() string                          { return "Send message to user" }
func (*voicePublish) ToolClass() string                      { return "reversible" }
func (*voicePublish) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *voicePublish) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: "Deliberately send a useful message to the user's typed conversation. Do not publish private voice transcripts or internal agent exchanges.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{"text": map[string]any{"type": "string", "maxLength": 16384}}, "required": []string{"text"},
	}}
}
func (t *voicePublish) Execute(ctx context.Context, id string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var input struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	if id == "" || len(id) > 256 || strings.TrimSpace(input.Text) == "" || len(input.Text) > 16384 || !utf8.ValidString(input.Text) {
		return agent.AgentToolResult{}, errors.New("invalid user-facing message")
	}
	if err := t.work.publish(ctx, t.agentID, id, input.Text, nil, ""); err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("publish user-facing message: %w", err)
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "Message sent to the user's typed conversation."}}}, nil
}

func (s *Server) stopVoices(ctx context.Context, agentID string) error {
	s.actorsMu.Lock()
	var pending []*voiceWork
	for _, work := range s.voices {
		if work.targets[agentID] {
			work.cancel()
			pending = append(pending, work)
		}
	}
	s.actorsMu.Unlock()
	for _, work := range pending {
		select {
		case <-work.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// role tells the agent that a spokesperson turns its findings into speech for the user.
func (v *voiceWork) role() string {
	return "You are consulting privately with the user's spokesperson agent. Return your findings to the spokesperson, who will speak to the user. These exchanges do not appear in the user's typed chat. Use send_to_user or show_html only to deliberately send a user-facing message or artifact to that chat. Do not reproduce the private exchange there. Retain your existing identity and configured tools. Use your scoped memory and typed history to answer references to earlier conversations. Save a durable fact or create a reminder when requested; do not save the private transcript as memory. Use only tools in the available tool list; unavailable management or continuation tools cannot run in this private consultation."
}
