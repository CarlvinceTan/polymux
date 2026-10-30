package evals

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestOUT2OutputBudget(t *testing.T) {
	var effects atomic.Int32
	reply := ToolReply("audit-effect", "side_effect", map[string]any{})
	reply.AssistantText = strings.Repeat("x", 70<<10)
	h := Start(t, Options{ID: "OUT2", Effects: map[string]*atomic.Int32{agentID: &effects}, Steps: []Step{Fixed(nil, reply), Fixed(nil, TextReply("Short final answer"))}})
	if code := h.Send(agentID, "audit-budget", "Synthetic output budget test"); code != 202 {
		t.Fatal(code)
	}
	run := h.WaitRun(agentID, "audit-budget", agentID, deadline)
	if run.Status == "completed" || effects.Load() != 0 {
		h.die(fmt.Sprintf("over-budget generation status=%s subsequent effects=%d", run.Status, effects.Load()))
	}
	pass(t, "OUT2", "over-budget work stopped")
}

func TestMCP2EffectJournal(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(405)
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(200)
			return
		}
		var request struct {
			ID     json.RawMessage
			Method string
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
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "audit", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Synthetic read", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "Synthetic MCP success"}}, "isError": false}
		default:
			t.Errorf("unexpected MCP method %s", request.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer endpoint.Close()
	h := Start(t, Options{ID: "MCP2", Agents: []agentruntime.Definition{{Agent: contract.Agent{ID: agentID, Name: "Assistant", Model: evalModel}, Tools: []string{}, MCP: []agentruntime.MCPServer{{Name: "audit", URL: endpoint.URL}}}}, Steps: []Step{
		Fixed(nil, ToolReply("audit-code", "codemode", map[string]any{"code": "text(await tools.audit__echo({}));"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "audit-code", "Synthetic MCP success") {
				return fmt.Errorf("MCP fixture failed")
			}
			return nil
		}, TextReply("MCP done")),
	}})
	if code := h.Send(agentID, "audit-mcp", "Call the synthetic MCP read"); code != 202 {
		t.Fatal(code)
	}
	run := h.WaitRun(agentID, "audit-mcp", agentID, deadline)
	if run.Status != "completed" {
		t.Fatal("MCP setup failed", run.Status)
	}
	pending, err := h.Store.PendingEffects(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		h.die(fmt.Sprintf("successful MCP read left %d unresolved effect intents", pending))
	}
	pass(t, "MCP2", "MCP effects settled")
}

func TestDEL1DurableTranscript(t *testing.T) {
	const marker = "AUDIT_DELETION_SENTINEL_20261023"
	const spoken = "VOICE_DELETION_SENTINEL_20261023"
	h := Start(t, Options{ID: "DEL1", Steps: []Step{Fixed(nil, TextReply("Synthetic reply")), Fixed(nil, TextReply("Spoken reply"))}})
	if code := h.Send(agentID, "audit-delete-user", marker); code != 202 {
		t.Fatal(code)
	}
	h.WaitRun(agentID, "audit-delete-user", agentID, deadline)
	privateVoice(t, h, "audit-delete-voice", spoken)
	count := func(text string) int {
		db, err := sql.Open("sqlite3", "file:"+filepath.Join(h.Dir, "durable.db")+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM entries WHERE instr(CAST(record AS TEXT),?)>0`, text).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A private voice consultation is ephemeral, so its transcript is gone once it ends.
	if n := count(spoken); n != 0 {
		h.die(fmt.Sprintf("ended voice consultation retains %d durable records", n))
	}
	request, _ := http.NewRequest("DELETE", h.HTTP.URL+"/agents/"+agentID, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if n := count(marker); n != 0 {
		h.die(fmt.Sprintf("permanently deleted bot retains %d live durable transcript records containing its user message", n))
	}
	pass(t, "DEL1", "durable transcripts removed with the bot and after voice")
}
