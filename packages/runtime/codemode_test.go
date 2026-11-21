package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
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

	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "cm1", Name: "CM1"}, MCP: servers}, "")
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
	wrapped, err := wrapMCP([]agent.AgentTool{probe}, session.beforeMCP, session.afterMCP)
	if err != nil {
		t.Fatal(err)
	}
	tool := wrapped[0]
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

func TestCM1DiscoveryRuns(t *testing.T) {
	probe := &stateTool{}
	wrapped, err := wrapMCP([]agent.AgentTool{probe}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"code":"text(await searchTools('state')); text(await describeTool('state_probe')); text(await tools.state_probe({}));"}`)
	result, err := wrapped[0].Execute(t.Context(), "discover", params, nil)
	if err != nil || result.IsError || !probe.ran.Load() {
		t.Fatalf("discover and call: %+v, %v", result, err)
	}
	if !strings.Contains(result.Text(), "state_probe") || !strings.Contains(result.Text(), "probed") {
		t.Fatalf("discovery output: %s", result.Text())
	}
}

func TestCodemodeEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		failed bool
	}{{name: "success"}, {name: "mcp error", failed: true}} {
		t.Run(test.name, func(t *testing.T) {
			probe := &resultTool{result: agent.AgentToolResult{
				Content: []ai.ToolResultMessageContent{
					ai.TextContent{Text: "MCP reply"}, ai.ImageContent{Data: "synthetic-image", MimeType: "image/png"},
				},
				StructuredContent: json.RawMessage(`{"answer":42}`), IsError: test.failed,
			}}
			settled := 0
			wrapped, err := wrapMCP([]agent.AgentTool{probe}, nil, func(_ context.Context, id, name string, unknown bool) {
				if id != "envelope/1" || name != "state_probe" || unknown {
					t.Errorf("incorrect effect settlement: %s %s %v", id, name, unknown)
				}
				settled++
			})
			if err != nil {
				t.Fatal(err)
			}
			params := json.RawMessage(`{"code":"const r = await tools.state_probe({}); text(JSON.stringify({text:r.content[0].text,image:r.content[1].data,answer:r.structuredContent.answer,failed:r.isError}));"}`)
			result, err := wrapped[0].Execute(t.Context(), "envelope", params, nil)
			if err != nil || result.IsError || settled != 1 {
				t.Fatalf("nested result or settlement lost: %v %v %d", result, err, settled)
			}
			for _, part := range []string{"MCP reply", "synthetic-image", `"answer":42`, fmt.Sprintf(`"failed":%v`, test.failed)} {
				if !strings.Contains(result.Text(), part) {
					t.Fatalf("MCP envelope lost %q: %s", part, result.Text())
				}
			}
		})
	}
}

func TestCodemodeStopped(t *testing.T) {
	probe := &stateTool{}
	calls := 0
	wrapped, err := wrapMCP([]agent.AgentTool{probe}, func(context.Context, string, string, json.RawMessage) agent.ToolCallHookResult {
		calls++
		return agent.ToolCallHookResult{Block: true, Terminate: true, Reason: "Waiting for approval"}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"code":"try { await tools.state_probe({}); } catch(e) { text(String(e)); } try { await tools.state_probe({}); } catch(e) { text(String(e)); }"}`)
	result, err := wrapped[0].Execute(t.Context(), "stopped", params, nil)
	if err != nil || probe.ran.Load() || calls != 1 || !strings.Contains(result.Text(), "turn stopped") {
		t.Fatalf("caught failure bypassed the stopped turn: %v %v calls=%d", result, err, calls)
	}
}

// shotTool stands in for a plugin tool that answers with a screenshot.
type shotTool struct{}

func (shotTool) Name() string                           { return "camera__shot" }
func (shotTool) Label() string                          { return "Camera" }
func (shotTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (shotTool) Description() string                    { return "Take a picture" }
func (shotTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "camera__shot", Description: "Take a picture", Parameters: map[string]any{"type": "object"}}
}
func (shotTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "shot"}, ai.ImageContent{Data: png, MimeType: "image/png"}}}, nil
}

// Code tools reach the model only through codemode, beside MCP tools, and their pictures can come back out.
func TestCodeToolsJoinCodemode(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct{ Name, Description string }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Function.Name != "codemode" ||
			!strings.Contains(body.Tools[0].Function.Description, "camera__shot") || !strings.Contains(body.Tools[0].Function.Description, "notes__echo0") {
			t.Errorf("tools: %+v", body.Tools)
		}
		sse(w, "Done", nil)
	}))
	defer provider.Close()
	runtime := openDurable(t, provider)
	session, err := runtime.Open(t.Context(), Definition{Agent: Agent{ID: "code", Name: "Code"},
		MCP: []MCPServer{{Name: "notes", URL: fakeMCP(t, 1).URL}}, CodeTools: []agent.AgentTool{shotTool{}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Send(t.Context(), turn("look"), nil); err != nil {
		t.Fatal(err)
	}

	wrapped, err := wrapMCP([]agent.AgentTool{shotTool{}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"code":"const r = await tools.camera__shot({}); image(r.content.find(c => c.type === 'image'))"}`)
	result, err := wrapped[0].Execute(t.Context(), "call", params, nil)
	if err != nil || result.IsError {
		t.Fatalf("result %+v %v", result, err)
	}
	if !slices.ContainsFunc(result.Content, func(c ai.ToolResultMessageContent) bool { _, ok := c.(ai.ImageContent); return ok }) {
		t.Fatalf("no picture in %+v", result.Content)
	}
}
