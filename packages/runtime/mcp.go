package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/packages/runtime/internal/untrusted"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/mcp"
)

type MCPServer struct {
	Name    string            `json:"name"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Headers are stored with secret references; Auth holds their filled values for one session.
	Headers    map[string]string `json:"headers,omitempty"`
	Auth       map[string]string `json:"-"`
	NativeOnly bool              `json:"nativeOnly,omitempty"`
	// ReadOnly is an operator-reviewed guarantee for every tool on this connection.
	ReadOnly bool `json:"readOnly,omitempty"`
}

// mcpTransport builds the transport one config describes, with the shell environment layered
// under a local server's own Env.
func mcpTransport(config MCPServer, cwd string, shell map[string]string) (mcp.Transport, error) {
	if config.URL != "" {
		return mcp.NewStreamableHTTPTransport(mcp.StreamableHTTPTransportOptions{URL: config.URL, Headers: config.Auth})
	}
	if config.Command != "" {
		env := maps.Clone(shell)
		if env == nil {
			env = map[string]string{}
		}
		maps.Copy(env, config.Env)
		return mcp.NewStdioTransport(mcp.StdioTransportOptions{Command: config.Command, Args: config.Args, Env: env, Cwd: cwd, MaxMessageBytes: 8 << 20, MaxStderrBytes: 64 << 10}), nil
	}
	return nil, fmt.Errorf("MCP server needs a URL or command")
}

// connectMCP starts local servers with shell layered under their own Env.
func connectMCP(ctx context.Context, servers []MCPServer, cwd string, shell map[string]string) ([]agent.AgentTool, []*mcp.Client, error) {
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
		transport, err := mcpTransport(config, cwd, shell)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		client := mcp.NewClient(mcp.ClientOptions{Implementation: mcp.Implementation{Name: "Polymux", Version: "0.1"}, RequestTimeoutMs: 30000})
		clients = append(clients, client)
		connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = client.Connect(connectCtx, transport)
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
			if !config.allows(tool.Name) {
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

func (config MCPServer) allows(name string) bool {
	browser := name == "get_browser_state" || name == "page" || strings.HasPrefix(name, "browser_")
	return !config.NativeOnly || !browser
}

// ProbeMCP lists the tools a session can use, then closes the connection.
func ProbeMCP(
	ctx context.Context,
	config MCPServer,
	cwd string,
	shell map[string]string,
) ([]string, error) {
	transport, err := mcpTransport(config, cwd, shell)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(mcp.ClientOptions{Implementation: mcp.Implementation{Name: "Polymux", Version: "0.1"}, RequestTimeoutMs: 30000})
	defer client.Close()
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := client.Connect(connectCtx, transport); err != nil {
		return nil, err
	}
	listed, err := client.ListTools(connectCtx, mcp.RequestOptions{})
	if err != nil {
		return nil, fmt.Errorf("list MCP tools: %w", err)
	}
	tools := make([]string, 0, len(listed))
	for _, tool := range listed {
		if config.allows(tool.Name) {
			tools = append(tools, tool.Name)
		}
	}
	return tools, nil
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
			out.Content = append(out.Content, ai.TextContent{Text: untrusted.Wrap(t.schema.Name, block.Text)})
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
