package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/server"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// Harness runs the real Polymux server against a model emulator.
type Harness struct {
	t            *testing.T
	id           string
	Dir          string
	Emulator     *Emulator
	API          *server.Server
	HTTP         *httptest.Server
	Store        *store.Store
	Runtime      *agentruntime.DurableRuntime
	closeStorage func() error
	defs         []agentruntime.Definition
	effects      map[string]*atomic.Int32
	extras       map[string][]agent.AgentTool

	backgroundTimeout time.Duration
}

// Options configures a harness start.
type Options struct {
	ID     string // scenario EVAL id (required)
	Steps  []Step
	Agents []agentruntime.Definition

	BackgroundTimeout time.Duration
	// Effects maps agent ID to a shared side-effect counter installed as side_effect.
	Effects map[string]*atomic.Int32
	// Extras maps agent ID to additional ExtraTools (typed_tool, error_tool, …).
	Extras map[string][]agent.AgentTool
}

// Start opens temp data, durable runtime, and HTTP server pointed at the emulator.
func Start(t *testing.T, opts Options) *Harness {
	t.Helper()
	if opts.ID == "" {
		t.Fatal("evals: Options.ID required")
	}
	testenv.PiG(t)
	if len(opts.Agents) == 0 {
		opts.Agents = []agentruntime.Definition{{
			Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: evalModel},
			Tools: []string{},
		}}
	}
	for i := range opts.Agents {
		if opts.Agents[i].Agent.Model == "" {
			opts.Agents[i].Agent.Model = evalModel
		}
		if opts.Agents[i].Tools == nil {
			opts.Agents[i].Tools = []string{}
		}
	}
	h := &Harness{
		t:       t,
		id:      opts.ID,
		Dir:     t.TempDir(),
		defs:    append([]agentruntime.Definition(nil), opts.Agents...),
		effects: opts.Effects,
		extras:  opts.Extras,

		backgroundTimeout: opts.BackgroundTimeout,
	}
	if h.effects == nil {
		h.effects = map[string]*atomic.Int32{}
	}
	if h.extras == nil {
		h.extras = map[string][]agent.AgentTool{}
	}
	h.Emulator = StartEmulator(opts.Steps)
	t.Cleanup(h.Close)
	t.Cleanup(h.ensureEmulatorEval)
	h.open()
	return h
}

// ensureEmulatorEval records FAIL for fixture failures or a missing EVAL result.
func (h *Harness) ensureEmulatorEval() {
	if h == nil || h.Emulator == nil {
		if h != nil && h.id != "" && !hasRecord(h.id) {
			fail(h.t, h.id, "scenario ended without EVAL result")
		}
		return
	}
	if fails := h.Emulator.Failures(); len(fails) > 0 {
		fail(h.t, h.id, "emulator: "+strings.Join(fails, "; "))
		return
	}
	if !hasRecord(h.id) {
		fail(h.t, h.id, "scenario ended without EVAL result")
	}
}

func (h *Harness) die(msg string) {
	h.t.Helper()
	fail(h.t, h.id, msg)
	h.t.FailNow()
}

func (h *Harness) open() {
	h.t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(h.Dir, "db"))
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			skip(h.t, h.id, "build with -tags sqlite_fts5: "+err.Error())
		}
		h.die(err.Error())
	}
	storage, closeStorage, err := agentruntime.NewDurableStorage(h.Dir)
	if err != nil {
		db.Close()
		h.die(err.Error())
	}
	runtime, err := agentruntime.NewDurable(context.Background(), h.Dir, h.Emulator.URL, evalModel, evalKey, agentruntime.Client{}, storage)
	if err != nil {
		closeStorage()
		db.Close()
		h.die(err.Error())
	}
	api, err := server.New(context.Background(), db, server.Config{
		DataDir:     h.Dir,
		Model:       evalModel,
		Definitions: h.definitions(),
		Factory:     runtime,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),

		BackgroundTimeout: h.backgroundTimeout,
	})
	if err != nil {
		runtime.Close()
		closeStorage()
		db.Close()
		h.die(err.Error())
	}
	h.Store = db
	h.Runtime = runtime
	h.closeStorage = closeStorage
	h.API = api
	h.HTTP = httptest.NewServer(api)
}

func (h *Harness) definitions() []agentruntime.Definition {
	out := make([]agentruntime.Definition, len(h.defs))
	for i, d := range h.defs {
		d.ExtraTools = append([]agent.AgentTool{}, d.ExtraTools...)
		if tools := h.extras[d.Agent.ID]; len(tools) > 0 {
			d.ExtraTools = append(d.ExtraTools, tools...)
		}
		if counter, ok := h.effects[d.Agent.ID]; ok {
			d.ExtraTools = append(d.ExtraTools, &effectTool{counter: counter})
		}
		out[i] = d
	}
	return out
}

// Restart closes and reopens the server on the same data directory.
func (h *Harness) Restart() {
	h.t.Helper()
	h.HTTP.Close()
	_ = h.API.Close()
	_ = h.Runtime.Close()
	_ = h.closeStorage()
	_ = h.Store.Close()
	h.open()
}

// Close tears down HTTP, server, runtime, store, and emulator.
func (h *Harness) Close() {
	if h.HTTP != nil {
		h.HTTP.Close()
		h.HTTP = nil
	}
	if h.API != nil {
		_ = h.API.Close()
		h.API = nil
	}
	if h.Runtime != nil {
		_ = h.Runtime.Close()
		h.Runtime = nil
	}
	if h.closeStorage != nil {
		_ = h.closeStorage()
		h.closeStorage = nil
	}
	if h.Store != nil {
		_ = h.Store.Close()
		h.Store = nil
	}
	if h.Emulator != nil {
		h.Emulator.Close()
		// Leave pointer set: Failures() stay readable if cleanup order varies.
	}
}

// Send posts a user message and returns the HTTP status.
func (h *Harness) Send(thread, id, text string) int {
	h.t.Helper()
	body, _ := json.Marshal(contract.SendRequest{ID: id, Text: text, AttachmentIDs: []string{}})
	resp, err := http.Post(h.HTTP.URL+"/threads/"+thread+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		h.die(err.Error())
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// Stop posts a stop request for the thread.
func (h *Harness) Stop(thread string) int {
	h.t.Helper()
	resp, err := http.Post(h.HTTP.URL+"/threads/"+thread+"/stop", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		h.die(err.Error())
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// WaitRun polls until the run for jobID reaches a terminal status or deadline.
func (h *Harness) WaitRun(thread, job, agent string, deadline time.Duration) contract.Run {
	h.t.Helper()
	if agent == "" {
		agent = thread
	}
	return h.WaitRunID(store.NewRun(job, thread, agent, "message").ID, deadline)
}

// WaitRunID polls a run id until terminal.
func (h *Harness) WaitRunID(id string, deadline time.Duration) contract.Run {
	h.t.Helper()
	end := time.Now().Add(deadline)
	var last contract.Run
	var err error
	for time.Now().Before(end) {
		last, err = h.Store.Run(context.Background(), id)
		if err == nil && store.Terminal(last.Status) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.die(fmt.Sprintf("run %s not terminal: %+v %v", id, last, err))
	return last
}

// WaitStatus polls until the run reaches one of the named statuses.
func (h *Harness) WaitStatus(id string, deadline time.Duration, statuses ...string) contract.Run {
	h.t.Helper()
	want := map[string]bool{}
	for _, s := range statuses {
		want[s] = true
	}
	end := time.Now().Add(deadline)
	var last contract.Run
	var err error
	for time.Now().Before(end) {
		last, err = h.Store.Run(context.Background(), id)
		if err == nil && want[last.Status] {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.die(fmt.Sprintf("run %s not in %v: %+v %v", id, statuses, last, err))
	return last
}

// WaitHold blocks until ch is closed or the deadline elapses.
func WaitHold(t *testing.T, id string, ch <-chan struct{}, deadline time.Duration) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(deadline):
		fail(t, id, "hold did not open in time")
		t.FailNow()
	}
}

// RunID is the deterministic run id for a message job on an agent thread.
func RunID(job, agent string) string {
	return store.NewRun(job, agent, agent, "message").ID
}

// Messages returns thread messages.
func (h *Harness) Messages(thread string) []contract.Message {
	h.t.Helper()
	messages, err := h.Store.Messages(context.Background(), thread, 0, 200)
	if err != nil {
		h.die(err.Error())
	}
	return messages
}

// AssistantDone returns done assistant messages in the thread.
func (h *Harness) AssistantDone(thread string) []contract.Message {
	var out []contract.Message
	for _, m := range h.Messages(thread) {
		if m.Role == "assistant" && m.Status == "done" {
			out = append(out, m)
		}
	}
	return out
}

type effectTool struct{ counter *atomic.Int32 }

func (*effectTool) Name() string                           { return "side_effect" }
func (*effectTool) Label() string                          { return "Side effect" }
func (*effectTool) Description() string                    { return "Counting side effect for evals" }
func (*effectTool) ToolClass() string                      { return "reversible" }
func (*effectTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*effectTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "side_effect", Description: "Counting side effect for evals", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}}
}
func (t *effectTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.counter.Add(1)
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "effect-ok"}}}, nil
}

type typedTool struct{ ran *atomic.Int32 }

func (*typedTool) Name() string                           { return "typed_tool" }
func (*typedTool) Label() string                          { return "Typed tool" }
func (*typedTool) Description() string                    { return "Requires a string value" }
func (*typedTool) ToolClass() string                      { return "reversible" }
func (*typedTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*typedTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "typed_tool", Description: "Requires a string value", Parameters: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"value": map[string]any{"type": "string"}},
		"required":             []string{"value"},
	}}
}
func (t *typedTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.ran.Add(1)
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "typed-ok"}}}, nil
}

type errorTool struct{ ran *atomic.Int32 }

func (*errorTool) Name() string                           { return "error_tool" }
func (*errorTool) Label() string                          { return "Error tool" }
func (*errorTool) Description() string                    { return "Always errors" }
func (*errorTool) ToolClass() string                      { return "reversible" }
func (*errorTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*errorTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "error_tool", Description: "Always errors", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}}
}
func (t *errorTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.ran.Add(1)
	return agent.AgentToolResult{}, fmt.Errorf("synthetic tool failure")
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []map[string]any
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if t, ok := p["text"].(string); ok {
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return string(raw)
}

func requestText(req *ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(contentText(m.Content))
		b.WriteByte(' ')
	}
	return b.String()
}

func toolMessage(req *ChatRequest, callID string) (ChatMessage, bool) {
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m, true
		}
	}
	return ChatMessage{}, false
}

func hasToolResult(req *ChatRequest, callID, want string) bool {
	m, ok := toolMessage(req, callID)
	if !ok {
		return false
	}
	return strings.Contains(contentText(m.Content), want)
}

func hasToolError(req *ChatRequest, callID string, needles ...string) bool {
	m, ok := toolMessage(req, callID)
	if !ok {
		return false
	}
	text := strings.ToLower(contentText(m.Content))
	for _, n := range needles {
		if !strings.Contains(text, strings.ToLower(n)) {
			return false
		}
	}
	return true
}

func messageText(m contract.Message) string { return store.Text(m) }

func assistantCount(messages []contract.Message) int {
	n := 0
	for _, m := range messages {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

func hasMessageError(events []contract.Event) bool {
	for _, e := range events {
		if e.Type == "message.error" {
			return true
		}
	}
	return false
}
