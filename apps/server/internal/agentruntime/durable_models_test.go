package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

// Isolates the models layer: does Polymux's OpenAI-compatible endpoint stream through PiG?
func TestDurableModelsStream(t *testing.T) {
	var requests int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Logf("request %d to %s", requests, r.URL.Path)
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	if model == nil {
		t.Fatal("model not registered")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
	for event := range stream.Events(ctx) {
		t.Logf("event %s", event.EventType())
		if event.EventType() == ai.EventError {
			t.Fatalf("provider error: %+v", event)
		}
	}
	result := stream.Result()
	if result == nil {
		t.Fatal("no terminal result")
	}
	if requests == 0 {
		t.Fatal("no provider request was made")
	}
}

// TestPERetryAfter fails when a 429 is returned at once or the wait is not Retry-After.
// The test clock records the wait, so a real sleep is a stall.
func TestPERetryAfter(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	var requests int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit"}}`))
			return
		}
		sse(w, "after retry", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
	model := models.GetModel(DurableProvider, "test-model")
	if model == nil {
		t.Fatal("model not registered")
	}
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
		t.Fatal("retry slept instead of using the test clock")
	}
	if requests != 2 {
		t.Fatalf("requests=%d, want 2", requests)
	}
	if got := clock.durations(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("retry wait %v, want 2s", got)
	}
	result := stream.Result()
	if result == nil || result.StopReason == ai.StopReasonError {
		t.Fatalf("retry did not recover: %+v", result)
	}
}

// TestPENoRetryOnHardStatus fails when a 400 or 401 is retried, including one whose body says overloaded.
// The 429 probe fails when no retry policy is configured, so a build that never retries cannot pass.
func TestPENoRetryOnHardStatus(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	attempts := func(status int, retryAfter, body string) (int, *ai.AssistantMessage) {
		t.Helper()
		var requests int
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests == 1 {
				w.Header().Set("Content-Type", "application/json")
				if retryAfter != "" {
					w.Header().Set("Retry-After", retryAfter)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"` + body + `","type":"fixture_error"}}`))
				return
			}
			sse(w, "after retry", nil)
		}))
		defer provider.Close()
		models := durableModels(provider.URL+"/v1", "test-key", "test-model", Client{})
		model := models.GetModel(DurableProvider, "test-model")
		if model == nil {
			t.Fatal("model not registered")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
		for range stream.Events(ctx) {
		}
		return requests, stream.Result()
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		requests, result := attempts(status, "30", "overloaded")
		if requests != 1 || result == nil || result.StopReason != ai.StopReasonError {
			t.Fatalf("%d retried or succeeded: requests=%d result=%+v", status, requests, result)
		}
	}
	if got := clock.durations(); len(got) != 0 {
		t.Fatalf("hard status waited %v", got)
	}
	requests, result := attempts(http.StatusTooManyRequests, "0", "overloaded")
	if requests != 2 || result == nil || result.StopReason == ai.StopReasonError {
		t.Fatalf("retry policy inactive: requests=%d result=%+v", requests, result)
	}
}
func TestDurableModelsRegistersEveryModel(t *testing.T) {
	models := durableModels("http://localhost/v1", "", "local/opencode", Client{}, "local/second", "local/cursor-agent", "local/second")
	for _, id := range []string{"local/opencode", "local/second", "local/cursor-agent"} {
		if models.GetModel(DurableProvider, id) == nil {
			t.Errorf("model %s not registered", id)
		}
	}
	if models.GetModel(DurableProvider, "missing") != nil {
		t.Fatal("unknown model registered")
	}
}

func TestDurableModelsSendsOwnModelID(t *testing.T) {
	requests := make(chan string, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Header.Get("X-Bridge") != "shared" {
			t.Error("missing shared header")
		}
		requests <- body.Model
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	models := durableModels(provider.URL+"/v1", "test-key", "local/opencode", Client{Headers: map[string]string{"X-Bridge": "shared"}}, "local/second", "local/cursor-agent")
	for _, id := range []string{"local/opencode", "local/second", "local/cursor-agent"} {
		t.Run(id, func(t *testing.T) {
			model := models.GetModel(DurableProvider, id)
			if model == nil {
				t.Fatal("model not registered")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			stream := models.StreamSimple(ctx, model, ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}})
			for event := range stream.Events(ctx) {
				if event.EventType() == ai.EventError {
					t.Fatalf("provider error: %+v", event)
				}
			}
			select {
			case got := <-requests:
				if got != id {
					t.Fatalf("request model = %q, want %q", got, id)
				}
			default:
				t.Fatal("no request")
			}
		})
	}
}

func TestDurableAgentOnNonDefaultModelReplies(t *testing.T) {
	for _, id := range []string{"local/second", "local/cursor-agent"} {
		t.Run(id, func(t *testing.T) {
			requests := make(chan string, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests <- body.Model
				sse(w, "READY", nil)
			}))
			defer provider.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			// The runtime starts with only the default model; Open must make the agent's model resolvable.
			runtime, err := NewDurable(ctx, t.TempDir(), provider.URL+"/v1", "local/opencode", "test-key", Client{}, storage.NewMemoryStorage())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			session, err := runtime.Open(ctx, Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: id}}, "")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			result, err := session.Send(ctx, turn("hi"), nil)
			if err != nil || result.Text != "READY" {
				t.Fatalf("turn = %+v, error = %v", result, err)
			}
			select {
			case got := <-requests:
				if got != id {
					t.Fatalf("request model = %q, want %q", got, id)
				}
			default:
				t.Fatal("no request")
			}
		})
	}
}
