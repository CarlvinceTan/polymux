package server

import (
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

// durableRuntime opens the production runtime over in-memory storage for one test.
func durableRuntime(t *testing.T, dir, baseURL string) *agentruntime.DurableRuntime {
	t.Helper()
	runtime, err := agentruntime.NewDurable(t.Context(), dir, baseURL, "local/test", "test-key", agentruntime.Client{}, storage.NewMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}
