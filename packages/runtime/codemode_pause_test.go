package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/packages/runtime/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// countTool answers with a wrapped JSON value, as an MCP tool does.
type countTool struct{ calls atomic.Int32 }

func (*countTool) Name() string                           { return "count_probe" }
func (*countTool) Label() string                          { return "Count probe" }
func (*countTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*countTool) Description() string                    { return "probe" }
func (*countTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "count_probe", Description: "probe", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}
}
func (t *countTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	n := t.calls.Add(1)
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: untrusted.Wrap("count_probe", `{"n":`+string(rune('0'+n))+`}`)}}}, nil
}

func TestCodemodeParsesToolResults(t *testing.T) {
	probe := &countTool{}
	wrapped, err := wrapMCP([]agent.AgentTool{probe}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"code":"const r = await tools.count_probe({}); text('n=' + JSON.parse(r.content[0].text).n);"}`)
	result, err := wrapped[0].Execute(t.Context(), "parse", params, nil)
	if err != nil || result.IsError || !strings.Contains(result.Text(), "n=1") {
		t.Fatalf("script could not parse the result: %+v %v", result, err)
	}
}

func TestCodemodePausesForWaitingMessage(t *testing.T) {
	saved := pauseAfter
	pauseAfter = 0
	t.Cleanup(func() { pauseAfter = saved })
	probe := &countTool{}
	wrapped, err := wrapMCP([]agent.AgentTool{probe}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A message arrives after the first call.
	ctx := WithShell(t.Context(), &ShellHost{Waiting: func(context.Context) bool { return probe.calls.Load() >= 1 }})
	params := json.RawMessage(`{"code":"for (let i = 0; i < 3; i++) { await tools.count_probe({}); } text('all done');"}`)
	result, err := wrapped[0].Execute(ctx, "pause", params, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Text()
	if probe.calls.Load() != 1 || strings.Contains(text, "all done") || !strings.Contains(text, "paused") || !strings.Contains(text, `{"n":1}`) {
		t.Fatalf("calls %d, result %q", probe.calls.Load(), text)
	}
}

// slowTool holds until released, like a long export.
type slowTool struct {
	release chan struct{}
	started atomic.Bool
}

func (*slowTool) Name() string                           { return "slow_probe" }
func (*slowTool) Label() string                          { return "Slow probe" }
func (*slowTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*slowTool) Description() string                    { return "probe" }
func (*slowTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "slow_probe", Description: "probe", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}
}
func (t *slowTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	t.started.Store(true)
	<-t.release
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "exported 4182"}}}, nil
}

func TestCodemodeMovesLongCallToBackground(t *testing.T) {
	saved := pauseAfter
	pauseAfter = 0
	t.Cleanup(func() { pauseAfter = saved })
	slow, count := &slowTool{release: make(chan struct{})}, &countTool{}
	wrapped, err := wrapMCP([]agent.AgentTool{slow, count}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan ShellResult, 1)
	// The message arrives once the long call is under way.
	ctx := WithShell(t.Context(), &ShellHost{
		Waiting: func(context.Context) bool { return slow.started.Load() },
		Detach: func(string, context.CancelFunc) func(ShellResult) {
			return func(r ShellResult) { results <- r }
		},
	})
	params := json.RawMessage(`{"code":"await tools.slow_probe({}); await tools.count_probe({}); text('all done');"}`)
	result, err := wrapped[0].Execute(ctx, "long", params, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Text()
	if count.calls.Load() != 0 || strings.Contains(text, "all done") || !strings.Contains(text, "slow_probe") || !strings.Contains(text, "background") {
		t.Fatalf("calls %d, result %q", count.calls.Load(), text)
	}
	close(slow.release)
	select {
	case r := <-results:
		if !strings.Contains(r.Output, "exported 4182") || r.Failed {
			t.Fatalf("background result %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the background call never reported its result")
	}
}

// gateTool returns only once two calls are in flight, so it finishes only when calls run at once.
type gateTool struct {
	active atomic.Int32
	both   chan struct{}
}

func (*gateTool) Name() string                           { return "gate_probe" }
func (*gateTool) Label() string                          { return "Gate probe" }
func (*gateTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*gateTool) Description() string                    { return "probe" }
func (*gateTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "gate_probe", Description: "probe", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}
}
func (t *gateTool) Execute(ctx context.Context, _ string, _ json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	if t.active.Add(1) == 2 {
		close(t.both)
	}
	select {
	case <-t.both:
		return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "ok"}}}, nil
	case <-time.After(5 * time.Second):
		return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "alone"}}}, nil
	case <-ctx.Done():
		return agent.AgentToolResult{}, ctx.Err()
	}
}

func TestCodemodeRunsBatchedCallsTogether(t *testing.T) {
	gate := &gateTool{both: make(chan struct{})}
	wrapped, err := wrapMCP([]agent.AgentTool{gate}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"code":"const r = await Promise.all([tools.gate_probe({}), tools.gate_probe({})]); text(r.map(x => x.content[0].text).join(','));"}`)
	result, err := wrapped[0].Execute(t.Context(), "batch", params, nil)
	if err != nil || !strings.Contains(result.Text(), "ok,ok") {
		t.Fatalf("batched calls ran one at a time: %q %v", result.Text(), err)
	}
}
