package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
)

func probeProvider(t *testing.T) *httptest.Server {
	t.Helper()
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1)%2 == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "state_probe", "arguments": "{}"}}})
			return
		}
		sse(w, "READY", nil)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func marker(value string) func() (context.Context, bool) {
	return func() (context.Context, bool) {
		return context.WithValue(context.Background(), turnMarker{}, value), true
	}
}

func TestDurableToolUsesReboundTurn(t *testing.T) {
	runtime := openDurable(t, probeProvider(t))
	tool := &stateTool{}
	session, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "rebound"}, ExtraTools: []agent.AgentTool{tool}, TurnContext: marker("first")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Send(t.Context(), turn("first"), nil); err != nil {
		t.Fatal(err)
	}
	session.(TurnBinder).BindTurn(nil, marker("second"))
	if _, err := session.Send(t.Context(), turn("second"), nil); err != nil {
		t.Fatal(err)
	}
	if value, _ := tool.marker.Load().(string); value != "second" {
		t.Fatalf("tool retained previous turn: %q", value)
	}
}

func TestDurableSameToolNamesStayScoped(t *testing.T) {
	runtime := openDurable(t, probeProvider(t))
	alpha, beta := &stateTool{}, &stateTool{}
	first, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "alpha"}, ExtraTools: []agent.AgentTool{alpha}, TurnContext: marker("alpha")}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "beta"}, ExtraTools: []agent.AgentTool{beta}, TurnContext: marker("beta")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Send(t.Context(), turn("alpha"), nil); err != nil {
		t.Fatal(err)
	}
	if !alpha.ran.Load() || beta.ran.Load() {
		t.Fatal("a later session replaced another session's tool")
	}
	if _, err := second.Send(t.Context(), turn("beta"), nil); err != nil {
		t.Fatal(err)
	}
	if value, _ := beta.marker.Load().(string); value != "beta" {
		t.Fatalf("wrong context %q", value)
	}
}

func TestDurableConnectsMCP(t *testing.T) {
	var called atomic.Bool
	connector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		var request struct {
			ID     json.RawMessage
			Method string
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "fixture", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Read fixture", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			called.Store(true)
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "fixture reply"}}}
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer connector.Close()
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools    []struct{ Function struct{ Name string } }
			Messages json.RawMessage
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) != 1 || body.Tools[0].Function.Name != "codemode" {
			t.Errorf("expected one codemode schema, got %+v", body.Tools)
		}
		if hits.Add(1) == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "mcp-call", "type": "function", "function": map[string]string{"name": "codemode", "arguments": `{"code":"text(await tools.notes__echo({}))"}`}}})
			return
		}
		if !strings.Contains(string(body.Messages), "fixture reply") {
			t.Error("codemode result did not reach the model")
		}
		sse(w, "READY", nil)
	}))
	defer provider.Close()
	runtime := openDurable(t, provider)
	session, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "mcp"}, MCP: []MCPServer{{Name: "notes", URL: connector.URL}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Send(t.Context(), turn("read fixture"), nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("provider requests = %d, want 2", hits.Load())
	}
	if !called.Load() {
		t.Fatal("MCP call did not reach connector")
	}
}

func TestDurableStreamDoesNotReplayOldAnswer(t *testing.T) {
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text := "OLD_ANSWER"
		if hits.Add(1) > 1 {
			text = "NEW_ANSWER"
		}
		sse(w, text, nil)
	}))
	defer provider.Close()
	runtime := openDurable(t, provider)
	session, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "stream"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Send(t.Context(), turn("first"), nil); err != nil {
		t.Fatal(err)
	}
	var streamed strings.Builder
	result, err := session.Send(t.Context(), turn("second"), func(d Delta) error { streamed.WriteString(d.Text); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(streamed.String(), "OLD_ANSWER") {
		t.Fatal("previous answer streamed as this turn's reply")
	}
	if result.Text != "NEW_ANSWER" {
		t.Fatalf("result %q", result.Text)
	}
}
