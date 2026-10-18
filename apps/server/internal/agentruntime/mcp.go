package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/mcp"
)

type MCPServer struct {
	Name       string            `json:"name"`
	URL        string            `json:"url,omitempty"`
	Command    string            `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	NativeOnly bool              `json:"nativeOnly,omitempty"`
	// ReadOnly is an operator-reviewed guarantee for every tool on this connection.
	ReadOnly bool `json:"readOnly,omitempty"`
}

func connectMCP(ctx context.Context, servers []MCPServer, cwd string) ([]agent.AgentTool, []*mcp.Client, error) {
	tools := []agent.AgentTool{}
	clients := []*mcp.Client{}
	cleanup := func() {
		for _, c := range clients {
			c.Close()
		}
	}
	for _, config := range servers {
		if config.Name == "" {
			cleanup()
			return nil, nil, fmt.Errorf("MCP server needs a name")
		}
		var transport mcp.Transport
		if config.URL != "" {
			t, err := mcp.NewStreamableHTTPTransport(mcp.StreamableHTTPTransportOptions{URL: config.URL})
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			transport = t
		} else if config.Command != "" {
			transport = mcp.NewStdioTransport(mcp.StdioTransportOptions{Command: config.Command, Args: config.Args, Env: config.Env, Cwd: cwd, MaxMessageBytes: 8 << 20, MaxStderrBytes: 64 << 10})
		} else {
			cleanup()
			return nil, nil, fmt.Errorf("MCP server needs a URL or command")
		}
		client := mcp.NewClient(mcp.ClientOptions{Implementation: mcp.Implementation{Name: "Polymux", Version: "0.1"}, RequestTimeoutMs: 30000})
		clients = append(clients, client)
		connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := client.Connect(connectCtx, transport)
		if err != nil {
			cancel()
			cleanup()
			return nil, nil, err
		}
		listed, err := client.ListTools(connectCtx, mcp.RequestOptions{})
		cancel()
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		for _, tool := range listed {
			if config.NativeOnly && (tool.Name == "get_browser_state" || tool.Name == "page" || strings.HasPrefix(tool.Name, "browser_")) {
				continue
			}
			var params map[string]any
			if err := json.Unmarshal(tool.InputSchema, &params); err != nil {
				cleanup()
				return nil, nil, err
			}
			tools = append(tools, &mcpTool{client: client, tool: tool, readOnly: config.ReadOnly, schema: ai.ToolSchema{Name: config.Name + "__" + tool.Name, Description: tool.Description, Parameters: params}})
		}
	}
	return tools, clients, nil
}

type mcpTool struct {
	client   *mcp.Client
	tool     mcp.Tool
	schema   ai.ToolSchema
	readOnly bool
}

func (t *mcpTool) ToolClass() string {
	if t.readOnly {
		return "read"
	}
	return "consequential"
}

func (t *mcpTool) Name() string                           { return t.schema.Name }
func (t *mcpTool) Label() string                          { return t.tool.Title }
func (t *mcpTool) Schema() ai.ToolSchema                  { return t.schema }
func (t *mcpTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *mcpTool) Execute(ctx context.Context, _ string, params json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var args any
	if err := json.Unmarshal(params, &args); err != nil {
		return agent.AgentToolResult{}, err
	}
	result, err := t.client.CallTool(ctx, t.tool.Name, args, mcp.RequestOptions{})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	out := agent.AgentToolResult{Content: []ai.ToolResultMessageContent{}, StructuredContent: result.StructuredContent, IsError: result.IsError != nil && *result.IsError}
	for _, block := range mcp.ToLLMContent(*result) {
		if block.Type == "image" {
			out.Content = append(out.Content, ai.ImageContent{MimeType: block.MimeType, Data: block.Data})
		} else {
			out.Content = append(out.Content, ai.TextContent{Text: block.Text})
		}
	}
	return out, nil
}

func closeMCP(clients []*mcp.Client) error {
	var errs []error
	for _, c := range clients {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}
