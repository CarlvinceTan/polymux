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

func TestACPSessionID(t *testing.T) {
	for path, want := range map[string]string{
		"":          "",
		"acp:abc":   "abc",
		"durable:7": "",
		"other":     "",
	} {
		if got := acpSessionID(path); got != want {
			t.Fatalf("acpSessionID(%q) = %q, want %q", path, got, want)
		}
	}
}
