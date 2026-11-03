package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A turn whose every model is out waits instead of failing, and carries on when one is back.
func TestFallbackWaitsForProvider(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit string
		back  func(*DurableRuntime)
	}{
		// The provider says when it resets, so the turn tries again then by itself.
		{name: "at its reset", limit: `{"error":{"type":"insufficient_quota","resets_in_seconds":1}}`, back: func(*DurableRuntime) {}},
		// No reset time, as after a top-up: the user's next message rechecks straight away.
		{name: "on recheck", limit: `{"error":{"type":"insufficient_quota"}}`, back: (*DurableRuntime).Recheck},
	} {
		t.Run(test.name, func(t *testing.T) {
			var spent atomic.Bool
			spent.Store(true)
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if spent.Load() {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(429)
					_, _ = io.WriteString(w, test.limit)
					return
				}
				sse(w, "back", nil)
			}))
			t.Cleanup(provider.Close)
			runtime := openDurable(t, provider)
			runtime.SetProviders([]ProviderSpec{{ID: "own", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}}})
			session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own"}}, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			waiting := make(chan struct{}, 4)
			done := make(chan error, 1)
			var text string
			go func() {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				result, err := session.Send(ctx, turn("hello"), func(delta Delta) error {
					if delta.Kind == "waiting" {
						waiting <- struct{}{}
					}
					return nil
				})
				text = result.Text
				done <- err
			}()
			select {
			case <-waiting:
			case err := <-done:
				t.Fatalf("turn ended instead of waiting: %v", err)
			}
			spent.Store(false)
			test.back(runtime)
			if err := <-done; err != nil || text != "back" {
				t.Fatalf("text=%q err=%v, want the provider's reply once it's back", text, err)
			}
			if calls.Load() != 2 {
				t.Fatalf("%d calls, want the refused one and the one after it came back", calls.Load())
			}
		})
	}
}

// A bad key never clears by itself, so the turn fails rather than waiting forever.
func TestFallbackFailsOnBadKey(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	t.Cleanup(provider.Close)
	runtime := openDurable(t, provider)
	runtime.SetProviders([]ProviderSpec{{ID: "own", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}}})
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := session.Send(ctx, turn("hello"), nil); err == nil || ctx.Err() != nil {
		t.Fatalf("err=%v ctx=%v, want a prompt failure", err, ctx.Err())
	}
}

// A provider that accepts the request and then sends an error isn't waited on, since retrying may never help.
func TestFallbackFailsOnStreamError(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	t.Cleanup(provider.Close)
	runtime := openDurable(t, provider)
	runtime.SetProviders([]ProviderSpec{{ID: "own", BaseURL: provider.URL + "/v1", Models: []string{"chosen"}}})
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "assistant", Name: "Assistant", Model: "chosen", Provider: "own"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := session.Send(ctx, turn("hello"), nil); err == nil || ctx.Err() != nil {
		t.Fatalf("err=%v ctx=%v, want a prompt failure", err, ctx.Err())
	}
}
