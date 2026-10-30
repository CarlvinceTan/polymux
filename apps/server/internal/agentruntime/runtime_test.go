package agentruntime

import (
	"net/http/httptest"
	"testing"

	"github.com/MichaelKinsy/PiG/durable/storage"
)

func openDurable(t *testing.T, provider *httptest.Server) *DurableRuntime {
	t.Helper()
	runtime, err := NewDurable(t.Context(), t.TempDir(), provider.URL+"/v1", "test-model", "test-key", Client{}, storage.NewMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}
