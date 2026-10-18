package agentruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeMCP(t *testing.T, count int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "test", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			tools := []any{}
			for i := range count {
				tools = append(tools, map[string]any{"name": fmt.Sprintf("echo%d", i), "description": "Echo text", "inputSchema": map[string]any{"type": "object"}})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "tool reply"}}, "isError": false}
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestMCPComposition(t *testing.T) {
	server := fakeMCP(t, 1)
	tools, clients, err := connectMCP(t.Context(), []MCPServer{{Name: "notes", URL: server.URL}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeMCP(clients)
	if len(tools) != 1 || tools[0].Name() != "notes__echo0" {
		t.Fatalf("tools %+v", tools)
	}
	params, _ := json.Marshal(map[string]string{"code": `text(await searchTools("echo")); text(await describeTool("notes__echo0")); text(await tools.notes__echo0({text:"hello"})); text(await tools.notes__echo0({text:"again"}));`})
	result, err := wrapMCP(tools, nil)[0].Execute(t.Context(), "call", params, nil)
	if err != nil || result.IsError || strings.Count(result.Text(), "tool reply") != 2 {
		t.Fatalf("MCP result %+v %v", result, err)
	}
}
