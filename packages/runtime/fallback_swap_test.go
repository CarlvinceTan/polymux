package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
)

// A brief limit is waited out on the bot's model, since switching costs the other model an uncached request; a long
// one switches.
func TestFallbackHiccup(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryAfter string
		reply      string
		own, spare int32
	}{
		{name: "short wait stays", retryAfter: "2", reply: "own", own: 2},
		{name: "long wait switches", retryAfter: "120", reply: "spare", own: 1, spare: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			useClock(t, &recordClock{})
			var own, spare atomic.Int32
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if own.Add(1) == 1 {
					w.Header().Set("Retry-After", test.retryAfter)
					w.WriteHeader(429)
					return
				}
				sse(w, "own", nil)
			}))
			t.Cleanup(first.Close)
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				spare.Add(1)
				sse(w, "spare", nil)
			}))
			t.Cleanup(second.Close)
			runtime := openDurable(t, first)
			runtime.SetProviders([]ProviderSpec{
				{ID: "own", BaseURL: first.URL + "/v1", Models: []string{"chosen"}},
				{ID: "spare", BaseURL: second.URL + "/v1", Models: []string{"backup"}},
			})
			session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own"}}, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			result, err := session.Send(ctx, turn("hello"), nil)
			if err != nil || result.Text != test.reply || own.Load() != test.own || spare.Load() != test.spare {
				t.Fatalf("reply=%q err=%v own=%d spare=%d, want %q own=%d spare=%d", result.Text, err, own.Load(), spare.Load(), test.reply, test.own, test.spare)
			}
		})
	}
}

// A turn that had to switch stays on the model it switched to, even once the bot's own model is back, and the next
// turn starts from the top again.
func TestFallbackStaysForTurn(t *testing.T) {
	var spent atomic.Bool
	spent.Store(true)
	var own, spare atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		own.Add(1)
		if spent.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"type":"insufficient_quota","resets_in_seconds":10}}`)
			return
		}
		sse(w, "own", nil)
	}))
	t.Cleanup(first.Close)
	runtime := openDurable(t, first)
	var elapsed atomic.Int64
	start := time.Now()
	runtime.fallback.now = func() time.Time { return start.Add(time.Duration(elapsed.Load()) * time.Second) }
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if spare.Add(1) == 1 {
			// The bot's own model comes back while this turn is mid-task.
			spent.Store(false)
			elapsed.Store(20)
			sse(w, "", []map[string]any{{"index": 0, "id": "call-1", "type": "function", "function": map[string]string{"name": "once_tool", "arguments": "{}"}}})
			return
		}
		sse(w, fmt.Sprint("spare ", spare.Load()), nil)
	}))
	t.Cleanup(second.Close)
	runtime.SetProviders([]ProviderSpec{
		{ID: "own", BaseURL: first.URL + "/v1", Models: []string{"chosen"}},
		{ID: "spare", BaseURL: second.URL + "/v1", Models: []string{"backup"}},
	})
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own"}, Intro: true, ExtraTools: []agent.AgentTool{&onceTool{}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	send := func(want string, ownCalls int32) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		result, err := session.Send(ctx, turn("go"), nil)
		if err != nil || result.Text != want || own.Load() != ownCalls {
			t.Fatalf("reply=%q err=%v own calls=%d, want %q and %d", result.Text, err, own.Load(), want, ownCalls)
		}
	}
	send("spare 2", 1)
	send("own", 2)
}
