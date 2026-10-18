package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"context"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type probeTool struct{ calls *atomic.Int32 }

func (*probeTool) Name() string                           { return "probe_effect" }
func (*probeTool) Label() string                          { return "Probe" }
func (*probeTool) Description() string                    { return "Synthetic test effect" }
func (*probeTool) ToolClass() string                      { return string(ToolReversible) }
func (*probeTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*probeTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "probe_effect", Description: "Synthetic test", Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}}
}
func (t *probeTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.calls.Add(1)
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "Synthetic effect returned"}}}, nil
}

func TestPiGCheckpointBlocksMixedBatchEffects(t *testing.T) {
	testenv.PiG(t)
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			var requests, effects atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if requests.Add(1) == 1 {
					calls := []map[string]any{}
					add := func(name, id, args string) {
						calls = append(calls, map[string]any{"index": len(calls), "id": id, "type": "function", "function": map[string]string{"name": name, "arguments": args}})
					}
					if mixed {
						add("probe_effect", "before", `{}`)
					}
					add("manage_tasks", "checkpoint", `{"action":"checkpoint","goal":"Synthetic goal","next":"Read the synthetic receipt","evidence":"Fixture only","after_seconds":60}`)
					if mixed {
						add("probe_effect", "after", `{}`)
					}
					data, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": calls}, "finish_reason": nil}}})
					fmt.Fprintf(w, "data: %s\n\n", data)
					fmt.Fprint(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Stage finished\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer provider.Close()
			runtime := durableRuntime(t, t.TempDir(), provider.URL+"/v1")
			api, endpoint := startTest(t, runtime)
			actor := api.actors["assistant"]
			actor.mu.Lock()
			actor.definition.ExtraTools = []agent.AgentTool{&probeTool{calls: &effects}}
			actor.mu.Unlock()
			response, err := http.Post(endpoint.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"synthetic","text":"Continue this established goal in stages","attachmentIDs":[]}`))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				items, err := api.Store.Tasks(t.Context(), "assistant", "assistant")
				if err != nil {
					t.Fatal(err)
				}
				if len(items) > 0 {
					if items[0].Status != "sleeping" {
						t.Fatalf("task %+v", items[0])
					}
					want := int32(0)
					if mixed {
						want = 1
					}
					if effects.Load() != want {
						t.Fatalf("post-checkpoint effect executed: %d", effects.Load())
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("PiG did not seal checkpoint")
		})
	}
}
