package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
)

func TestCM1SchemaCount(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Function.Name != "codemode" {
			t.Errorf("MCP schemas: %+v", body.Tools)
		}
		sse(w, "Done", nil)
	}))
	defer provider.Close()
	runtime := openDurable(t, provider)
	servers := []MCPServer{}
	for i := range 3 {
		servers = append(servers, MCPServer{Name: fmt.Sprintf("server%d", i), URL: fakeMCP(t, 20).URL})
	}

	session, err := runtime.Open(t.Context(), Definition{Agent: contract.Agent{ID: "cm1", Name: "CM1"}, MCP: servers}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Send(t.Context(), turn("discover tools"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestCM1ApprovalStillGates(t *testing.T) {
	probe := &stateTool{}
	session := &durableSession{}
	session.BindTurn(func(context.Context, string, string, json.RawMessage) agent.ToolCallHookResult {
		return agent.ToolCallHookResult{Block: true, Terminate: true, Reason: "Waiting for approval"}
	}, nil)
	tool := wrapMCP([]agent.AgentTool{probe}, session.beforeMCP)[0]
	params := json.RawMessage(`{"code":"text(await tools.state_probe({}))"}`)
	result, err := tool.Execute(t.Context(), "call", params, nil)
	if err != nil || !result.IsError || probe.ran.Load() || !session.park.Load() || !strings.Contains(result.Text(), "Waiting for approval") {
		t.Fatalf("gate: %+v %v", result, err)
	}
	session.BindTurn(func(context.Context, string, string, json.RawMessage) agent.ToolCallHookResult {
		return agent.ToolCallHookResult{}
	}, nil)
	result, err = tool.Execute(t.Context(), "approved", params, nil)
	if err != nil || result.IsError || !probe.ran.Load() {
		t.Fatalf("approved: %+v %v", result, err)
	}
}

func TestCM1InfiniteLoopHalts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	tool := &codeTool{}
	result, err := tool.Execute(ctx, "loop", json.RawMessage(`{"code":"// @options: {\"timeout_ms\": 20}\nwhile(true){}"}`), nil)
	if err != nil || !result.IsError || !strings.Contains(result.Text(), "codemode_timeout") {
		t.Fatalf("loop: %+v %v", result, err)
	}
}
