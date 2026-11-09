// Package acp runs a conversation on an external Agent Client Protocol process.
//
// Polymux stays the orchestrator: a bot delegates bounded work to an ACP
// agent (Claude Code, opencode, ...) by launching it over stdio and driving one
// session. The external agent owns its own tools and model; this package only
// relays the prompt, streams its activity back as runtime deltas, and returns its
// final text. It does not expose Polymux tools into the external agent.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	core "github.com/CarlvinceTan/polymux/packages/runtime"
)

// protocolVersion is the ACP revision this client speaks.
const protocolVersion = 1

// Config launches one external agent session.
type Config struct {
	Command string
	Args    []string
	// Env adds variables to the server's environment, as KEY=VALUE.
	Env []string
	// Cwd is the absolute working directory the agent runs in. It must be stable
	// across reopens so agents that persist sessions can load them again.
	Cwd string
	// SessionID resumes a prior session when the agent supports loading; empty
	// starts a new one.
	SessionID string
	Log       *slog.Logger
}

// Session is one opened conversation with an external ACP agent. It satisfies
// core.Session so the server can treat it like any other runtime session.
type Session struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	nextID  int64
	closed  bool
	exited  bool

	sessionID   string
	loadSession bool

	turnMu  sync.Mutex
	emit    func(core.Delta) error
	text    strings.Builder
	tracked map[string]string
	// broken is set by a tool call, so the text that follows starts a new paragraph.
	broken bool

	done     chan struct{}
	exitErr  error
	exitOnce sync.Once
}

type rpcResponse struct {
	Result json.RawMessage
	Error  *rpcError
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("acp: %s (code %d)", e.Message, e.Code) }

// Open launches the agent process and establishes a session. It performs the
// initialize handshake and either loads the given session or creates a new one.
func Open(ctx context.Context, cfg Config) (*Session, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, errors.New("acp: no command configured")
	}
	if cfg.Cwd == "" {
		return nil, errors.New("acp: session needs a working directory")
	}
	// The process lives for the session, not one turn; Close ends it. Its own process
	// group lets Close stop whatever it spawned, such as the agent behind an npx wrapper.
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Cwd
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = &cfgLog{cfg: cfg}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start %s: %w", cfg.Command, err)
	}
	s := &Session{
		cmd: cmd, stdin: stdin,
		pending: map[int64]chan rpcResponse{},
		tracked: map[string]string{},
		done:    make(chan struct{}),
	}
	go s.readLoop(stdout)
	go s.wait()

	initCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	var init initializeResponse
	if err := s.call(initCtx, "initialize", initializeRequest{
		ProtocolVersion: protocolVersion,
		ClientCapabilities: clientCapabilities{
			FS:       fsCapabilities{},
			Terminal: false,
		},
		ClientInfo: implementation{Name: "polymux", Version: "1"},
	}, &init); err != nil {
		s.Close()
		return nil, fmt.Errorf("acp: initialize: %w", err)
	}
	s.loadSession = init.AgentCapabilities.LoadSession

	if cfg.SessionID != "" && s.loadSession {
		if err := s.call(initCtx, "session/load", loadSessionRequest{
			Cwd: cfg.Cwd, SessionID: cfg.SessionID, MCPServers: []any{},
		}, nil); err == nil {
			s.sessionID = cfg.SessionID
			return s, nil
		}
		// A missing or unloadable session falls back to starting fresh.
		if cfg.Log != nil {
			cfg.Log.Warn("acp session could not be loaded; starting new", "error", err)
		}
	}
	var created newSessionResponse
	if err := s.call(initCtx, "session/new", newSessionRequest{Cwd: cfg.Cwd, MCPServers: []any{}}, &created); err != nil {
		s.Close()
		return nil, fmt.Errorf("acp: new session: %w", err)
	}
	s.sessionID = created.SessionID
	return s, nil
}

// Path returns the session identity Polymux persists to reopen this conversation.
func (s *Session) Path() string { return s.sessionID }

// Send runs one prompt turn, streaming the agent's text and tool activity into
// emit and returning its final reply.
func (s *Session) Send(ctx context.Context, m core.Message, emit func(core.Delta) error) (core.Result, error) {
	prompt := promptBlocks(m)
	if len(prompt) == 0 {
		return core.Result{}, errors.New("acp: empty prompt")
	}
	s.turnMu.Lock()
	s.emit = emit
	s.text.Reset()
	s.broken = false
	s.turnMu.Unlock()
	defer func() {
		s.turnMu.Lock()
		s.emit = nil
		s.turnMu.Unlock()
	}()

	var resp promptResponse
	err := s.call(ctx, "session/prompt", promptRequest{SessionID: s.sessionID, Prompt: prompt}, &resp)
	s.turnMu.Lock()
	text := s.text.String()
	s.turnMu.Unlock()
	if err != nil {
		return core.Result{Text: text}, err
	}
	switch resp.StopReason {
	case "end_turn", "max_tokens", "max_turn_requests":
		return core.Result{Text: text}, nil
	case "cancelled":
		return core.Result{Text: text}, context.Canceled
	case "refusal":
		return core.Result{Text: text}, errors.New("acp: the agent refused the task")
	default:
		return core.Result{Text: text}, nil
	}
}

// Abort asks the agent to cancel the in-flight turn.
func (s *Session) Abort(ctx context.Context) error {
	return s.notify("session/cancel", cancelNotification{SessionID: s.sessionID})
}

// Reusable reports whether the agent process is still running, so a session whose
// agent exited is reopened rather than failing every later turn.
func (s *Session) Reusable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && !s.exited
}

// Close ends the session and stops the agent process. Closing stdin asks the agent
// to exit; its process group is terminated only if it doesn't.
func (s *Session) Close() error {
	var err error
	s.exitOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		_ = s.stdin.Close()
		if !s.exitWithin(5 * time.Second) {
			s.signal(syscall.SIGTERM)
			if !s.exitWithin(2 * time.Second) {
				s.signal(syscall.SIGKILL)
				<-s.done
			}
		}
		err = s.exitErr
	})
	return err
}

func (s *Session) exitWithin(limit time.Duration) bool {
	select {
	case <-s.done:
		return true
	case <-time.After(limit):
		return false
	}
}

// signal reaches the agent's whole process group.
func (s *Session) signal(sig syscall.Signal) {
	_ = syscall.Kill(-s.cmd.Process.Pid, sig)
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.exited = true
	s.exitErr = err
	for _, ch := range s.pending {
		close(ch)
	}
	s.pending = map[int64]chan rpcResponse{}
	s.mu.Unlock()
	close(s.done)
}

// call sends a request and waits for its response.
func (s *Session) call(ctx context.Context, method string, params any, result any) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("acp: session closed")
	}
	s.nextID++
	id := s.nextID
	ch := make(chan rpcResponse, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	if err := s.write(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return fmt.Errorf("acp: agent exited during %s", method)
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return ctx.Err()
	case <-s.done:
		return fmt.Errorf("acp: agent exited during %s", method)
	}
}

// notify sends a notification that expects no response.
func (s *Session) notify(method string, params any) error {
	return s.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *Session) write(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("acp: write: %w", err)
	}
	return nil
}

func (s *Session) readLoop(stdout io.Reader) {
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s.dispatch(line)
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) dispatch(line []byte) {
	var msg struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return
	}
	switch {
	case msg.Method != "" && msg.ID != nil:
		s.handleRequest(*msg.ID, msg.Method, msg.Params)
	case msg.Method != "":
		s.handleNotification(msg.Method, msg.Params)
	case msg.ID != nil:
		s.mu.Lock()
		ch, ok := s.pending[*msg.ID]
		delete(s.pending, *msg.ID)
		s.mu.Unlock()
		if ok {
			ch <- rpcResponse{Result: msg.Result, Error: msg.Error}
		}
	}
}

func (s *Session) handleNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var note struct {
		Update sessionUpdate `json:"update"`
	}
	if json.Unmarshal(params, &note) != nil {
		return
	}
	s.applyUpdate(note.Update)
}

// handleRequest answers agent-to-client requests. Polymux advertises no filesystem
// or terminal capability, so the only expected request is a tool permission.
func (s *Session) handleRequest(id int64, method string, params json.RawMessage) {
	switch method {
	case "session/request_permission":
		var req permissionRequest
		if json.Unmarshal(params, &req) != nil {
			s.respondError(id, -32602, "invalid permission request")
			return
		}
		option, ok := selectPermission(req.Options)
		if !ok {
			s.respond(id, permissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}})
			return
		}
		s.respond(id, permissionResponse{Outcome: permissionOutcome{Outcome: "selected", OptionID: option}})
	default:
		s.respondError(id, -32601, "method not supported by Polymux")
	}
}

// selectPermission picks the least-privilege allow option. The attached agent is a
// worker the user explicitly configured, so its tool use within the session cwd is
// permitted; reject-only prompts are declined.
func selectPermission(options []permissionOption) (string, bool) {
	for _, kind := range []string{"allow_once", "allow_always"} {
		for _, o := range options {
			if o.Kind == kind {
				return o.OptionID, true
			}
		}
	}
	return "", false
}

func (s *Session) respond(id int64, result any) {
	_ = s.write(rpcResponseEnvelope{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Session) respondError(id int64, code int, message string) {
	_ = s.write(rpcResponseEnvelope{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

// applyUpdate turns one session/update into runtime deltas and accumulates text.
func (s *Session) applyUpdate(u sessionUpdate) {
	s.turnMu.Lock()
	emit := s.emit
	s.turnMu.Unlock()
	if emit == nil {
		return
	}
	switch u.SessionUpdate {
	case "agent_message_chunk":
		var block contentBlock
		if json.Unmarshal(u.Content, &block) == nil && block.Type == "text" && block.Text != "" {
			text := block.Text
			s.turnMu.Lock()
			if s.broken && s.text.Len() > 0 && !strings.HasSuffix(s.text.String(), "\n\n") {
				text = "\n\n" + strings.TrimLeft(text, "\n")
			}
			s.broken = false
			s.text.WriteString(text)
			s.turnMu.Unlock()
			_ = emit(core.Delta{Text: text})
		}
	case "tool_call":
		name := u.Title
		if u.Name != nil && *u.Name != "" {
			name = *u.Name
		}
		if name == "" {
			name = "tool"
		}
		s.turnMu.Lock()
		s.tracked[u.ToolCallID] = name
		s.broken = true
		s.turnMu.Unlock()
		_ = emit(core.Delta{Kind: "tool.start", Tool: name, ToolID: u.ToolCallID, Brief: brief(u.RawInput)})
	case "tool_call_update":
		s.turnMu.Lock()
		name := s.tracked[u.ToolCallID]
		finished := u.Status == "completed" || u.Status == "failed"
		if finished {
			delete(s.tracked, u.ToolCallID)
		}
		s.turnMu.Unlock()
		if name == "" {
			name = "tool"
		}
		if finished {
			_ = emit(core.Delta{Kind: "tool.done", Tool: name, ToolID: u.ToolCallID, Failed: u.Status == "failed", Output: outputText(u.Content)})
		}
	}
}

// brief summarizes a tool call's input for the chat row.
func brief(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	for _, key := range []string{"command", "path", "file_path", "query", "pattern", "url"} {
		if v, ok := fields[key].(string); ok && v != "" {
			if len(v) > 200 {
				v = v[:197] + "…"
			}
			return v
		}
	}
	return ""
}

// outputText excerpts a finished tool call's content for the chat row.
func outputText(raw json.RawMessage) string {
	var content []toolCallContent
	if len(raw) == 0 || json.Unmarshal(raw, &content) != nil {
		return ""
	}
	var parts []string
	size := 0
	for _, c := range content {
		if c.Type == "content" && c.Content.Type == "text" {
			parts = append(parts, c.Content.Text)
			if size += len(c.Content.Text); size >= 2048 {
				break
			}
		}
	}
	text := strings.Join(parts, "\n")
	if len(text) > 2048 {
		text = text[:2045] + "…"
	}
	return text
}

// promptBlocks converts a runtime message into ACP content blocks. Attachments
// ride along as their extracted text or a named reference.
func promptBlocks(m core.Message) []contentBlock {
	var blocks []contentBlock
	for _, p := range m.Parts {
		if p.Text != nil && *p.Text != "" {
			blocks = append(blocks, contentBlock{Type: "text", Text: *p.Text})
		}
		if p.Attachment != nil {
			a := p.Attachment
			if a.ExtractedText != nil && *a.ExtractedText != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: "Attachment " + a.Name + ":\n" + *a.ExtractedText})
			} else {
				blocks = append(blocks, contentBlock{Type: "text", Text: "Attachment: " + a.Name})
			}
		}
	}
	return blocks
}

// cfgLog drains agent stderr into the server log so a failing process is diagnosable.
type cfgLog struct{ cfg Config }

func (w *cfgLog) Write(p []byte) (int, error) {
	if w.cfg.Log != nil {
		w.cfg.Log.Warn("acp agent stderr", "line", strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}
