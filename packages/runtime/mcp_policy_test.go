package runtime

import (
	"net/http/httptest"
	"testing"
)

func TestUnavailableMCPReturnsNonReusableFallback(t *testing.T) {
	isolatePiG(t)
	runtime := openDurable(t, &httptest.Server{URL: "http://127.0.0.1:1"})
	definition := Definition{Agent: Agent{ID: "a"}, Tools: []string{"read"}, MCP: []MCPServer{{Name: "unavailable"}}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal("CLI fallback failed", err)
	}
	defer session.Close()
	if session.(*durableSession).Reusable() {
		t.Fatal("missing MCP will never be retried")
	}
	definition.MCP = nil
	healthy, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	defer healthy.Close()
	if !healthy.(*durableSession).Reusable() {
		t.Fatal("healthy session not reusable")
	}
}
