package acp

import "encoding/json"

// Wire types for the subset of ACP v1 this client speaks. Field names follow the
// protocol's JSON keys; only the pieces Polymux uses are modelled.

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponseEnvelope struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      int64     `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type fsCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type clientCapabilities struct {
	FS       fsCapabilities `json:"fs"`
	Terminal bool           `json:"terminal"`
}

type initializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
	ClientInfo         implementation     `json:"clientInfo"`
}

type agentCapabilities struct {
	LoadSession bool `json:"loadSession"`
}

type initializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
}

type newSessionRequest struct {
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

type newSessionResponse struct {
	SessionID string `json:"sessionId"`
}

type loadSessionRequest struct {
	Cwd        string `json:"cwd"`
	SessionID  string `json:"sessionId"`
	MCPServers []any  `json:"mcpServers"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type promptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type promptResponse struct {
	StopReason string `json:"stopReason"`
}

type cancelNotification struct {
	SessionID string `json:"sessionId"`
}

// sessionUpdate covers the union of session/update variants. Content is raw
// because it is a single block for message chunks but an array for tool calls.
type sessionUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Name          *string         `json:"name"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
}

type toolCallContent struct {
	Type    string       `json:"type"`
	Content contentBlock `json:"content"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}

type permissionRequest struct {
	Options []permissionOption `json:"options"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

type permissionResponse struct {
	Outcome permissionOutcome `json:"outcome"`
}
