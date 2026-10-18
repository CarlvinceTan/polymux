package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

func useClock(t *testing.T, c Clock) {
	t.Helper()
	previous := retryClock()
	setRetryClock(c)
	t.Cleanup(func() { setRetryClock(previous) })
}

type recordClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *recordClock) Wait(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	return ctx.Err()
}

func (c *recordClock) durations() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

type gateClock struct {
	started chan struct{}
	once    sync.Once
}

func (c *gateClock) Wait(ctx context.Context, _ time.Duration) error {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return ctx.Err()
}

func TestCutMarker(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"hello\npolymux-conversation-id:42"}]}`)
	id, cleaned := cutMarker(body)
	if id != "42" {
		t.Fatalf("id %q", id)
	}
	if strings.Contains(string(cleaned), "polymux-conversation-id") || !json.Valid(cleaned) || !strings.Contains(string(cleaned), "hello") {
		t.Fatalf("cleaned %s", cleaned)
	}
}

// TestPEHardStatus fails when a durable turn retries a 400 or 401 whose body says overloaded.
func TestPEHardStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			clock := &recordClock{}
			useClock(t, clock)
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "30")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"error":{"message":"overloaded","type":"fixture_error"}}`)
					return
				}
				sse(w, "should not run", nil)
			}))
			defer provider.Close()
			runtime := openDurable(t, provider)
			session, err := runtime.Open(t.Context(), Definition{
				Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Intro: true, Ephemeral: true,
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			text := "fail overloaded"
			_, err = session.Send(ctx, contract.Message{Parts: []contract.Part{{Kind: "text", Text: &text}}}, func(Delta) error { return nil })
			if requests.Load() != 1 {
				t.Fatalf("%d retried: requests=%d", status, requests.Load())
			}
			if err == nil {
				t.Fatal("hard status succeeded")
			}
			if got := clock.durations(); len(got) != 0 {
				t.Fatalf("hard status waited %v", got)
			}
		})
	}
}

// TestPERetryingEvent fails when a durable retry does not tell the turn it is retrying.
func TestPERetryingEvent(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	var requests atomic.Int32
	var bodies []string
	var mu sync.Mutex
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"overloaded","type":"rate_limit"}}`)
			return
		}
		sse(w, "after retry", nil)
	}))
	defer provider.Close()
	runtime := openDurable(t, provider)
	session, err := runtime.Open(t.Context(), Definition{
		Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Intro: true, Ephemeral: true,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var deltas []Delta
	text := "retry me"
	started := time.Now()
	_, err = session.Send(ctx, contract.Message{Parts: []contract.Part{{Kind: "text", Text: &text}}}, func(d Delta) error {
		deltas = append(deltas, d)
		return nil
	})
	if time.Since(started) > time.Second {
		t.Fatal("retry slept instead of using the test clock")
	}
	if err != nil {
		t.Fatal(err)
	}
	retries := 0
	for _, d := range deltas {
		if d.Kind != "run.retrying" {
			continue
		}
		retries++
		if d.Attempt != retries {
			t.Fatalf("run.retrying attempt %d, want %d", d.Attempt, retries)
		}
	}
	if retries != 1 {
		t.Fatalf("run.retrying events=%d, want 1", retries)
	}
	if got := clock.durations(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("retry wait %v, want 2s", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, body := range bodies {
		if strings.Contains(body, "polymux-conversation-id:") {
			t.Fatalf("provider saw the conversation marker: %s", body)
		}
	}
}

type onceTool struct{ n atomic.Int32 }

func (*onceTool) Name() string                           { return "once_tool" }
func (*onceTool) Label() string                          { return "Once" }
func (*onceTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*onceTool) Description() string                    { return "once" }
func (*onceTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "once_tool", Description: "once", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}}
}
func (t *onceTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.n.Add(1)
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "once"}}}, nil
}

func dropAfterText(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	payload, _ := json.Marshal(map[string]any{
		"id": "fixture", "object": "chat.completion.chunk",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
	})
	fmt.Fprintf(w, "data: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

// TestPENoReplayAfterOutput fails when a dropped stream is retried after text or a tool call,
// which would duplicate the text or run the tool twice.
func TestPENoReplayAfterOutput(t *testing.T) {
	tool := &onceTool{}
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			sse(w, "", []map[string]any{{"index": 0, "id": "call-1", "type": "function", "function": map[string]string{"name": "once_tool", "arguments": "{}"}}})
		case 2:
			dropAfterText(w, "ONCE")
		default:
			sse(w, "ONCE", []map[string]any{{"index": 0, "id": "call-2", "type": "function", "function": map[string]string{"name": "once_tool", "arguments": "{}"}}})
		}
	}))
	defer provider.Close()
	runtime := openDurable(t, provider)
	session, err := runtime.Open(t.Context(), Definition{
		Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Intro: true, Ephemeral: true,
		ExtraTools: []agent.AgentTool{tool},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var text string
	prompt := "call once"
	_, _ = session.Send(ctx, contract.Message{Parts: []contract.Part{{Kind: "text", Text: &prompt}}}, func(d Delta) error {
		text += d.Text
		return nil
	})
	if requests.Load() != 2 {
		t.Fatalf("requests=%d, want 2", requests.Load())
	}
	if tool.n.Load() != 1 {
		t.Fatalf("tool ran %d times", tool.n.Load())
	}
	if strings.Count(text, "ONCE") != 1 {
		t.Fatalf("partial text %q, want ONCE exactly once", text)
	}
}

func TestRetrySinkDeduplicatesRetainedText(t *testing.T) {
	tests := []struct {
		name string
		run  func(*retrySink)
		want string
	}{
		{name: "retained before stream", run: func(s *retrySink) {
			_ = s.partial("ONCE")
			_ = s.send(Delta{Text: "ON"})
			_ = s.send(Delta{Text: "CE"})
		}, want: "ONCE"},
		{name: "stream before retained", run: func(s *retrySink) {
			_ = s.send(Delta{Text: "ON"})
			_ = s.partial("ONCE")
			_ = s.send(Delta{Text: "CE"})
		}, want: "ONCE"},
		{name: "new text after retained", run: func(s *retrySink) {
			_ = s.partial("ONCE")
			_ = s.send(Delta{Text: "ONCE"})
			_ = s.send(Delta{Text: " MORE"})
		}, want: "ONCE MORE"},
		{name: "repeated stream text", run: func(s *retrySink) {
			_ = s.send(Delta{Text: "ONCE"})
			_ = s.send(Delta{Text: "ONCE"})
		}, want: "ONCEONCE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got string
			sink := &retrySink{emit: func(delta Delta) error {
				got += delta.Text
				return nil
			}}
			test.run(sink)
			if got != test.want {
				t.Fatalf("text = %q, want %q", got, test.want)
			}
		})
	}
}

// TestPENoRetryAfterStream fails when response bytes are retried as a new request.
func TestPENoRetryAfterStream(t *testing.T) {
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			dropAfterText(w, "ONCE")
			return
		}
		sse(w, "ONCE", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
	for range stream.Events(ctx) {
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want 1", requests.Load())
	}
}

// TestPECancelDuringRetry fails when a cancel during Retry-After waits out the delay or sends again.
func TestPECancelDuringRetry(t *testing.T) {
	clock := &gateClock{started: make(chan struct{})}
	useClock(t, clock)
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit"}}`)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-clock.started:
			cancel()
		case <-time.After(2 * time.Second):
			cancel()
		}
	}()
	started := time.Now()
	stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
	for range stream.Events(ctx) {
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancel during Retry-After did not stop at once")
	}
	select {
	case <-clock.started:
	default:
		t.Fatal("retry did not enter the Retry-After wait")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want 1", requests.Load())
	}
}

// TestPERetryCap fails when a Retry-After above 30s is waited out in full.
func TestPERetryCap(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit"}}`)
			return
		}
		sse(w, "after cap", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
	for event := range stream.Events(ctx) {
		if event.EventType() == ai.EventError {
			t.Fatalf("provider error: %+v", event)
		}
	}
	if time.Since(started) > time.Second {
		t.Fatal("cap slept instead of using the test clock")
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d, want 2", requests.Load())
	}
	if got := clock.durations(); len(got) != 1 || got[0] != retryCap {
		t.Fatalf("retry wait %v, want %s", got, retryCap)
	}
}

func TestPEEnableFailureContinues(t *testing.T) {
	previous := configureRetry
	configureRetry = func() error { return fmt.Errorf("retry settings unavailable") }
	t.Cleanup(func() { configureRetry = previous })
	provider := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(provider.Close)
	runtime := openDurable(t, provider)
	if runtime == nil {
		t.Fatal("enabling retry stopped the runtime")
	}
}
