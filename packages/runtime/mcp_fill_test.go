package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A remote MCP tool receives saved-secret values for references in its arguments, and the agent never sees them.
func TestMCPFillsSecretReferences(t *testing.T) {
	var mu sync.Mutex
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Arguments map[string]string `json:"arguments"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "test", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "login", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			mu.Lock()
			received = request.Params.Arguments["password"]
			mu.Unlock()
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "logged in with " + received}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer server.Close()
	const secret = `pa"ss-2026`
	fill := func(_ context.Context, args string) (string, []string) {
		quoted, _ := json.Marshal(secret)
		return strings.ReplaceAll(args, "{{vault:pulse:password}}", string(quoted[1:len(quoted)-1])), []string{secret}
	}
	tools, clients, err := connectMCP(t.Context(), []MCPServer{{Name: "site", URL: server.URL, Fill: fill}}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeMCP(clients)
	got := func() string {
		mu.Lock()
		defer mu.Unlock()
		return received
	}
	result, err := tools[0].Execute(t.Context(), "call", json.RawMessage(`{"password":"{{vault:pulse:password}}"}`), nil)
	if err != nil || got() != secret {
		t.Fatalf("server received %q: %v", got(), err)
	}
	if strings.Contains(result.Text(), "pa\"ss") || !strings.Contains(result.Text(), "[REDACTED]") {
		t.Fatalf("result showed the secret: %q", result.Text())
	}
	if _, err := tools[0].Execute(t.Context(), "call", json.RawMessage(`{"password":"plain"}`), nil); err != nil || got() != "plain" {
		t.Fatalf("a call without references changed: %q %v", got(), err)
	}
}
