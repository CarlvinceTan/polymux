// Ideas adapted from Rakazo (github.com/elie222/rakazo, packages/testkit/src/model-emulator.ts), Copyright Rakazo contributors, Apache License 2.0.
package evals

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

const evalModel = "eval-model"
const evalKey = "test-key"

// ChatRequest is one OpenAI-compatible chat completion body.
type ChatRequest struct {
	Bytes    int             `json:"-"`
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []ChatMessage   `json:"messages"`
	Tools    json.RawMessage `json:"tools"`
}

// ChatMessage is one transcript message from the provider request.
type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  json.RawMessage `json:"tool_calls"`
}

// Response is one scripted provider outcome.
type Response struct {
	Kind string // text, tool, error, disconnect, hold

	Text string
	// Chunks optionally splits text into separate SSE content deltas.
	Chunks []string

	ToolID   string
	ToolName string
	ToolArgs map[string]any
	// ArgChunks must concatenate to JSON(ToolArgs) when set.
	ArgChunks     []string
	AssistantText string

	Status  int
	Message string
	// RetryAfter is the Retry-After value in seconds. Empty pins a 429 to 0.
	RetryAfter string

	OnOpen  func()
	OnClose func()
	// Release is closed by the test to end a hold; request cancel also ends it.
	Release <-chan struct{}
}

// Step is one expected provider request and its scripted reply.
type Step struct {
	Expect   func(*ChatRequest) error
	Response func(*ChatRequest) Response
}

// Emulator is a loopback OpenAI-compatible /v1/chat/completions SSE fixture.
type Emulator struct {
	URL      string
	server   *httptest.Server
	model    string
	key      string
	mu       sync.Mutex
	steps    []Step
	index    int
	requests []*ChatRequest
	times    []time.Time
	failures []string
}

// StartEmulator serves scripted SSE responses on a loopback listener.
func StartEmulator(steps []Step) *Emulator {
	e := &Emulator{model: evalModel, key: evalKey, steps: append([]Step(nil), steps...)}
	e.server = httptest.NewServer(http.HandlerFunc(e.serve))
	e.URL = e.server.URL + "/v1"
	return e
}

// Requests returns a snapshot of observed provider bodies.
func (e *Emulator) Requests() []*ChatRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*ChatRequest, len(e.requests))
	copy(out, e.requests)
	return out
}

// RequestTimes returns arrival times for each observed provider request.
func (e *Emulator) RequestTimes() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]time.Time, len(e.times))
	copy(out, e.times)
	return out
}

// Count returns how many provider requests were observed.
func (e *Emulator) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}

// Close shuts the fixture listener.
func (e *Emulator) Close() {
	e.server.Close()
}

// Failures returns recorded fixture failures (unexpected requests, auth, expect, …).
func (e *Emulator) Failures() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.failures))
	copy(out, e.failures)
	return out
}

// Done returns an error if fixture failures exist or steps were not all consumed.
func (e *Emulator) Done() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var parts []string
	if len(e.failures) > 0 {
		parts = append(parts, e.failures...)
	}
	if e.index != len(e.steps) {
		parts = append(parts, fmt.Sprintf("emulator steps consumed %d of %d", e.index, len(e.steps)))
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// DoneAllowExtra returns an error only for fixture failures; unused steps are OK.
func (e *Emulator) DoneAllowExtra() error {
	fails := e.Failures()
	if len(fails) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(fails, "; "))
}

func (e *Emulator) fail(msg string) {
	e.failures = append(e.failures, msg)
}

func (e *Emulator) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		e.mu.Lock()
		e.fail(fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path))
		e.mu.Unlock()
		http.Error(w, `{"error":{"message":"Model fixture request mismatch","type":"fixture_error"}}`, http.StatusBadRequest)
		return
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+e.key {
		e.mu.Lock()
		e.fail("authorization mismatch: " + got)
		e.mu.Unlock()
		http.Error(w, `{"error":{"message":"Model fixture request mismatch","type":"fixture_error"}}`, http.StatusBadRequest)
		return
	}
	limited := io.LimitReader(r.Body, 2<<20+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > 2<<20 {
		e.mu.Lock()
		e.fail("request body read failed or exceeded 2 MiB")
		e.mu.Unlock()
		http.Error(w, `{"error":{"message":"Model fixture request mismatch","type":"fixture_error"}}`, http.StatusBadRequest)
		return
	}
	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		e.mu.Lock()
		e.fail("invalid json: " + err.Error())
		e.mu.Unlock()
		http.Error(w, `{"error":{"message":"Model fixture request mismatch","type":"fixture_error"}}`, http.StatusBadRequest)
		return
	}
	e.mu.Lock()
	req.Bytes = len(body)
	e.requests = append(e.requests, &req)
	e.times = append(e.times, time.Now())
	if req.Model != e.model {
		e.fail("model routing changed: " + req.Model)
		e.mu.Unlock()
		writeFixtureError(w, http.StatusInternalServerError)
		return
	}
	if !req.Stream {
		e.fail("expected streaming model request")
		e.mu.Unlock()
		writeFixtureError(w, http.StatusInternalServerError)
		return
	}
	if e.index >= len(e.steps) {
		e.fail(fmt.Sprintf("unexpected model request %d", e.index+1))
		e.mu.Unlock()
		writeFixtureError(w, http.StatusInternalServerError)
		return
	}
	step := e.steps[e.index]
	e.index++
	stepIndex := e.index
	e.mu.Unlock()

	if step.Expect != nil {
		if err := step.Expect(&req); err != nil {
			e.mu.Lock()
			e.fail(fmt.Sprintf("step %d expect: %v", stepIndex, err))
			e.mu.Unlock()
			writeFixtureError(w, http.StatusInternalServerError)
			return
		}
	}
	reply := Response{Kind: "text", Text: "ok"}
	if step.Response != nil {
		reply = step.Response(&req)
	}
	e.writeReply(w, r, stepIndex, reply)
}

func writeFixtureError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"message":"Model fixture request mismatch","type":"fixture_error"}}`))
}

func (e *Emulator) writeReply(w http.ResponseWriter, r *http.Request, stepIndex int, reply Response) {
	switch reply.Kind {
	case "error":
		status := reply.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		w.Header().Set("Content-Type", "application/json")
		if status == http.StatusTooManyRequests || reply.RetryAfter != "" {
			after := reply.RetryAfter
			if after == "" {
				after = "0"
			}
			w.Header().Set("Retry-After", after)
			// Retry-After-Ms wins over Retry-After. Only the zero pin sets it,
			// so a non-zero Retry-After is the delay the retry layer waits.
			if after == "0" {
				w.Header().Set("Retry-After-Ms", "0")
			}
		}
		w.WriteHeader(status)
		msg := reply.Message
		if msg == "" {
			msg = "fixture_error"
		}
		_, _ = fmt.Fprintf(w, `{"error":{"message":%q,"type":"fixture_error"}}`, msg)
		return
	case "tool":
		if len(reply.ArgChunks) > 0 {
			encoded, _ := json.Marshal(reply.ToolArgs)
			if strings.Join(reply.ArgChunks, "") != string(encoded) {
				e.mu.Lock()
				e.fail("argumentChunks must concatenate to JSON arguments")
				e.mu.Unlock()
				writeFixtureError(w, http.StatusInternalServerError)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	emit := func(delta any, finish any) {
		payload := map[string]any{
			"id":      fmt.Sprintf("fixture-%d", stepIndex),
			"object":  "chat.completion.chunk",
			"created": 0,
			"model":   e.model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		data, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	emit(map[string]any{"role": "assistant"}, nil)

	switch reply.Kind {
	case "tool":
		if reply.AssistantText != "" {
			emit(map[string]any{"content": reply.AssistantText}, nil)
		}
		emit(map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": reply.ToolID, "type": "function",
			"function": map[string]any{"name": reply.ToolName, "arguments": ""},
		}}}, nil)
		chunks := reply.ArgChunks
		if len(chunks) == 0 {
			encoded, _ := json.Marshal(reply.ToolArgs)
			chunks = []string{string(encoded)}
		}
		for _, fragment := range chunks {
			emit(map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "function": map[string]any{"arguments": fragment},
			}}}, nil)
		}
		emit(map[string]any{}, "tool_calls")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	case "hold":
		if reply.Text != "" {
			emit(map[string]any{"content": reply.Text}, nil)
		}
		if reply.OnOpen != nil {
			reply.OnOpen()
		}
		if reply.OnClose != nil {
			defer reply.OnClose()
		}
		select {
		case <-reply.Release:
			emit(map[string]any{}, "stop")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case <-r.Context().Done():
		}
		return
	case "disconnect":
		if reply.Text != "" {
			emit(map[string]any{"content": reply.Text}, nil)
		}
		_, _ = io.WriteString(w, ": disconnect\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		return
	default: // text
		chunks := reply.Chunks
		if len(chunks) == 0 && reply.Text != "" {
			chunks = []string{reply.Text}
		}
		for _, chunk := range chunks {
			emit(map[string]any{"content": chunk}, nil)
		}
		emit(map[string]any{}, "stop")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
}

// TextReply is a finished assistant text response.
func TextReply(text string) Response {
	return Response{Kind: "text", Text: text}
}

// ToolReply is a single tool call response.
func ToolReply(id, name string, args map[string]any) Response {
	return Response{Kind: "tool", ToolID: id, ToolName: name, ToolArgs: args}
}

// ErrorReply is a non-SSE HTTP error.
func ErrorReply(status int, message string) Response {
	return Response{Kind: "error", Status: status, Message: message}
}

// DisconnectReply emits optional partial text then closes the connection.
func DisconnectReply(text string) Response {
	return Response{Kind: "disconnect", Text: text}
}

// HoldReply blocks the stream until release is closed or the request ends.
func HoldReply(text string, release <-chan struct{}, onOpen, onClose func()) Response {
	return Response{Kind: "hold", Text: text, Release: release, OnOpen: onOpen, OnClose: onClose}
}

// Fixed returns a step with a constant response.
func Fixed(expect func(*ChatRequest) error, reply Response) Step {
	return Step{Expect: expect, Response: func(*ChatRequest) Response { return reply }}
}
