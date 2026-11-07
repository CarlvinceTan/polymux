package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
)

func stream(t *testing.T, url string) bool {
	t.Helper()
	models := durableModels(url+"/v1", "test-key", "test-model", Client{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	failed := false
	for event := range models.StreamSimple(ctx, models.GetModel(DurableProvider, "test-model"), ai.Context{Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("hi")}}}).Events(ctx) {
		failed = failed || event.EventType() == ai.EventError
	}
	return !failed
}

// A provider that's busy for a while is waited out with growing backoff instead of failing after three tries.
func TestBusyProviderIsWaitedOut(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 5 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"Service is too busy"}}`)
			return
		}
		sse(w, "made it", nil)
	}))
	defer provider.Close()
	if !stream(t, provider.URL) || requests.Load() != 6 {
		t.Fatalf("busy provider not waited out: requests=%d", requests.Load())
	}
	waits := clock.durations()
	if len(waits) != 5 || waits[4] < waits[0] {
		t.Fatalf("waits %v, want five growing ones", waits)
	}
}

// A provider that never recovers is given up on once the busy budget is spent, not retried forever.
func TestBusyProviderBudget(t *testing.T) {
	clock := &recordClock{}
	useClock(t, clock)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer provider.Close()
	if stream(t, provider.URL) {
		t.Fatal("a provider that never recovered succeeded")
	}
	var total time.Duration
	for _, d := range clock.durations() {
		total += d
	}
	if total < busyBudget || total > busyBudget+retryCap {
		t.Fatalf("waited %s in all, want about %s", total, busyBudget)
	}
}

// Calls to a provider are unlimited until it's busy, then held to half and grown back by successes.
func TestProviderLoadAdapts(t *testing.T) {
	load := &providerLoad{changed: make(chan struct{})}
	for range 8 {
		if err := load.enter(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	load.leave(-1)
	if load.limit != 4 || load.inflight != 7 {
		t.Fatalf("limit %d inflight %d after busy, want 4 and 7", load.limit, load.inflight)
	}
	blocked, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := load.enter(blocked); err == nil {
		t.Fatal("a call went past the limit")
	}
	for range 7 {
		load.leave(1)
	}
	if load.limit != 11 || load.inflight != 0 {
		t.Fatalf("limit %d inflight %d after successes, want 11 and 0", load.limit, load.inflight)
	}
	for load.limit != 0 {
		_ = load.enter(t.Context())
		load.leave(1)
	}
}
