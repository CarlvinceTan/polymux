package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
)

// offeredTools records the tool list each request offered, so isolation between
// conversations is observable rather than assumed.
func offeredTools(t *testing.T) (*httptest.Server, func() []string, *atomic.Int32) {
	t.Helper()
	var mu sync.Mutex
	offered := []string{}
	hits := &atomic.Int32{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encoded, _ := json.Marshal(body["tools"])
		mu.Lock()
		offered = append(offered, string(encoded))
		mu.Unlock()
		if hits.Add(1) == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "state_probe", "arguments": "{}"}}})
			return
		}
		sse(w, "Noted.", nil)
	}))
	t.Cleanup(provider.Close)
	return provider, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), offered...)
	}, hits
}

func TestDurableToolsStayWithTheirConversation(t *testing.T) {
	provider, offered, _ := offeredTools(t)
	runtime := openDurable(t, provider)
	tool := &stateTool{}

	withTool := Definition{Agent: contract.Agent{ID: "alpha", Name: "Alpha"}, ExtraTools: []agent.AgentTool{tool}}
	session, err := runtime.Open(t.Context(), withTool, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Send(t.Context(), turn("use the probe"), nil); err != nil {
		t.Fatal(err)
	}
	if !tool.ran.Load() {
		t.Fatal("a tool set on the conversation did not run")
	}

	plain := Definition{Agent: contract.Agent{ID: "beta", Name: "Beta"}}
	other, err := runtime.Open(t.Context(), plain, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Send(t.Context(), turn("no tools here"), nil); err != nil {
		t.Fatal(err)
	}

	seen := offered()
	if len(seen) != 3 {
		t.Fatalf("requests %d", len(seen))
	}
	// The first two requests are the same conversation: the tool call and its follow-up.
	for i := range 2 {
		if !strings.Contains(seen[i], "state_probe") {
			t.Fatalf("conversation did not offer its tool at request %d: %s", i, seen[i])
		}
	}
	if strings.Contains(seen[2], "state_probe") {
		t.Fatalf("tool leaked into another conversation: %s", seen[2])
	}
}

// Isolates how the harness stores an exact-but-empty tool selection.
func TestHarnessExactEmptySelection(t *testing.T) {
	provider, _, _ := offeredTools(t)
	runtime := openDurable(t, provider)
	tool := &stateTool{}
	definition := Definition{Agent: contract.Agent{ID: "gamma", Name: "Gamma"}, ExtraTools: []agent.AgentTool{tool}}
	session, err := runtime.Open(t.Context(), definition, "")
	if err != nil {
		t.Fatal(err)
	}
	durable, ok := session.(*durableSession)
	if !ok {
		t.Fatal("not a durable session")
	}
	installed, err := durable.conversation.Agent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("with a tool: %d offered", len(installed.Tools))
	empty := Definition{Agent: contract.Agent{ID: "delta", Name: "Delta"}}
	second, err := runtime.Open(t.Context(), empty, "")
	if err != nil {
		t.Fatal(err)
	}
	none, err := second.(*durableSession).conversation.Agent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("without tools: %d offered", len(none.Tools))
	if len(none.Tools) != 0 {
		t.Fatalf("an exact empty selection offered %d tools", len(none.Tools))
	}
}

// The durable tool task must still honour Polymux's gate: an unattended turn's tool budget,
// the automatic-turn tool gate and effect receipts all live in that hook.
func TestDurableGateBlocksAndReportsToolActivity(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "allowed", true: "blocked"}[blocked], func(t *testing.T) {
			provider, _, _ := offeredTools(t)
			runtime := openDurable(t, provider)
			tool := &stateTool{}
			definition := Definition{
				Agent:      contract.Agent{ID: "gate", Name: "Gate"},
				ExtraTools: []agent.AgentTool{tool},
				BeforeTool: func(_ context.Context, _, name string, _ json.RawMessage) agent.ToolCallHookResult {
					if blocked && name == "state_probe" {
						return agent.ToolCallHookResult{Block: true, Reason: "Automatic turns cannot use external or scheduling tools"}
					}
					return agent.ToolCallHookResult{}
				},
			}
			session, err := runtime.Open(t.Context(), definition, "")
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			deltas := []Delta{}
			emit := func(delta Delta) error {
				mu.Lock()
				deltas = append(deltas, delta)
				mu.Unlock()
				return nil
			}
			if _, err := session.Send(t.Context(), turn("use the probe"), emit); err != nil {
				t.Fatal(err)
			}
			if tool.ran.Load() == blocked {
				t.Fatalf("blocked=%v but the tool ran=%v", blocked, tool.ran.Load())
			}
			mu.Lock()
			defer mu.Unlock()
			kinds := []string{}
			for _, delta := range deltas {
				if delta.Kind != "" {
					kinds = append(kinds, delta.Kind)
				}
			}
			has := func(kind string) bool {
				for _, seen := range kinds {
					if seen == kind {
						return true
					}
				}
				return false
			}
			if blocked {
				if has("tool.start") || has("tool.done") {
					t.Fatalf("a blocked call reported activity: %v", kinds)
				}
				return
			}
			if !has("tool.start") || !has("tool.done") {
				t.Fatalf("allowed call did not report tool activity: %v", kinds)
			}
		})
	}
}
