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
