package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/harness"
	"github.com/MichaelKinsy/PiG/durable/storage"
)

// stateTool stands in for a Polymux tool: it needs the turn's context values to act.
type stateTool struct {
	ran    atomic.Bool
	marker atomic.Value
}

func (*stateTool) Name() string                           { return "state_probe" }
func (*stateTool) Label() string                          { return "State probe" }
func (*stateTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*stateTool) Description() string                    { return "probe" }
func (*stateTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "state_probe", Description: "probe", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}}
}

func (t *stateTool) Execute(ctx context.Context, _ string, _ json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.ran.Store(true)
	if value, ok := ctx.Value(turnMarker{}).(string); ok {
		t.marker.Store(value)
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "probed"}}}, nil
}

// durableTurn runs one harness turn against a scripted provider with the given definition.
func durableTurn(t *testing.T, definition Definition) {
	t.Helper()
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "state_probe", "arguments": "{}"}}})
			return
		}
		sse(w, "READY", nil)
	}))
	t.Cleanup(provider.Close)
	registry := harness.CreateRegistry()
	opened, err := harness.OpenHarness(t.Context(), storage.NewMemoryStorage(), harness.HarnessOptions{
		Models: durableModels(provider.URL+"/v1", "test-key", "test-model", Client{}), Registry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close(context.WithoutCancel(t.Context())) })
	conversation, err := opened.Root(t.Context(), &harness.RootOptions{Agent: &harness.AgentChange{
		Model: harness.SetTo(durable.ModelRef{Provider: DurableProvider, ModelId: "test-model"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Tools bind to the conversation they run in, so install once it exists.
	if err := registry.Install(&durable.Extension{Name: "polymux", Tools: Tools(definition, conversation.Id())}); err != nil {
		t.Fatal(err)
	}
	submission, err := conversation.Submit(t.Context(), durable.SubmissionDraft{Type: durable.SubmissionTypeInput, Content: ai.UserText("probe then answer")})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := submission.Wait(waitCtx); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestDurableToolRestoresTurnState(t *testing.T) {
	tool := &stateTool{}
	definition := Definition{
		ExtraTools: []agent.AgentTool{tool},
		TurnContext: func() (context.Context, bool) {
			return context.WithValue(context.Background(), turnMarker{}, "polymux-run"), true
		},
	}
	durableTurn(t, definition)
	if !tool.ran.Load() {
		t.Fatal("tool did not run")
	}
	if value, _ := tool.marker.Load().(string); value != "polymux-run" {
		t.Fatalf("tool did not see its turn state: %q", value)
	}
}

func TestDurableToolWithoutTurnStateIsRefused(t *testing.T) {
	tool := &stateTool{}
	// TurnContext reports the state is gone, as it is after a restart resumes a task.
	definition := Definition{
		ExtraTools:  []agent.AgentTool{tool},
		TurnContext: func() (context.Context, bool) { return nil, false },
	}
	durableTurn(t, definition)
	if tool.ran.Load() {
		t.Fatal("tool ran without its turn state")
	}
	if err := ErrTurnState; !strings.Contains(err.Error(), "review this stage") {
		t.Fatalf("unhelpful refusal: %v", err)
	}
}

type resultTool struct {
	stateTool
	result agent.AgentToolResult
}

func (t *resultTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	return t.result, nil
}

func TestDurableToolResults(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		failed    bool
		thrown    bool
		terminate bool
	}{
		{name: "failure", failed: true},
		{name: "thrown", thrown: true},
		{name: "terminate", terminate: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var hits atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if hits.Add(1) == 1 {
					sse(w, "", []map[string]any{{"index": 0, "id": "call", "type": "function", "function": map[string]string{"name": "state_probe", "arguments": "{}"}}})
					return
				}
				sse(w, "RECOVERED", nil)
			}))
			defer provider.Close()
			runtime := openDurable(t, provider)
			tool := &resultTool{result: agent.AgentToolResult{
				Content:   []ai.ToolResultMessageContent{ai.TextContent{Text: "tool result"}},
				IsError:   scenario.failed,
				Thrown:    scenario.thrown,
				Terminate: scenario.terminate,
				Details:   map[string]any{"reference": "persisted"},
				Usage:     &ai.Usage{Input: 3, Output: 5},
			}}
			session, err := runtime.Open(t.Context(), Definition{
				Agent:      contract.Agent{ID: "result", Name: "Result"},
				ExtraTools: []agent.AgentTool{tool},
				BeforeTool: func(context.Context, string, string, json.RawMessage) agent.ToolCallHookResult {
					return agent.ToolCallHookResult{}
				},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			var done atomic.Bool
			var failed atomic.Bool
			result, err := session.Send(t.Context(), turn("run the tool"), func(delta Delta) error {
				if delta.Kind == "tool.done" {
					done.Store(true)
					failed.Store(delta.Failed)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			wantFailure := scenario.failed || scenario.thrown
			if !done.Load() || failed.Load() != wantFailure {
				t.Fatalf("reported completion=%v failure=%v, want failure=%v", done.Load(), failed.Load(), wantFailure)
			}
			if scenario.terminate {
				if hits.Load() != 1 {
					t.Fatalf("terminating tool made %d model requests, want 1", hits.Load())
				}
			} else if result.Text != "RECOVERED" {
				t.Fatalf("agent did not recover from the tool failure: %+v", result)
			}
			view, err := session.(*durableSession).conversation.Context(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range view.Messages {
				stored, ok := message.(ai.ToolResultMessage)
				if !ok || stored.ToolCallID != "call" {
					continue
				}
				if stored.IsError != wantFailure || toolExcerpt(stored.Content) != "tool result" {
					t.Fatalf("stored result: %+v", stored)
				}
				details, err := json.Marshal(stored.Details)
				if err != nil || string(details) != `{"reference":"persisted"}` {
					t.Fatalf("stored details: %s, %v", details, err)
				}
				if stored.Usage == nil || stored.Usage.Input != 3 || stored.Usage.Output != 5 {
					t.Fatalf("stored tool usage: %+v", stored.Usage)
				}
				return
			}
			t.Fatal("tool result was not persisted")
		})
	}
}
