package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type Device struct {
	Name string `json:"name"`
	Host string `json:"host"`
}

var deviceHost = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*(@[a-zA-Z0-9_][a-zA-Z0-9_.-]*)?$`)

func (d Device) Valid() bool {
	return len(d.Name) > 0 && len(d.Name) <= 64 && !strings.ContainsAny(d.Name, " /\\.:_") && len(d.Host) <= 256 && deviceHost.MatchString(d.Host)
}

type deviceTool struct{ devices []Device }

func (*deviceTool) Name() string                           { return "run_device" }
func (*deviceTool) Label() string                          { return "Run on a paired device" }
func (*deviceTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*deviceTool) Description() string {
	return "Run one executable with arguments over a preconfigured SSH target. Uses existing keys/known hosts without prompts or permission changes. Choose a registered device; filesystem paths refer to that remote host. No automatic retries: a timeout may have left remote effects running."
}
func (t *deviceTool) Schema() ai.ToolSchema {
	names := []string{}
	for _, device := range t.devices {
		names = append(names, device.Name)
	}
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"device": map[string]any{"type": "string", "enum": names}, "command": map[string]any{"type": "string"}, "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 64}}, "required": []string{"device", "command"}}}
}
func (t *deviceTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var input struct {
		Device  string   `json:"device"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	host := ""
	for _, device := range t.devices {
		if device.Name == input.Device && device.Valid() {
			host = device.Host
		}
	}
	if host == "" || input.Command == "" || len(input.Command) > 2048 || strings.ContainsAny(input.Command, "\x00\r\n\t ") || len(input.Args) > 64 {
		return agent.AgentToolResult{}, errors.New("invalid paired device command")
	}
	words := []string{input.Command}
	for _, arg := range input.Args {
		if len(arg) > 8192 || strings.ContainsRune(arg, 0) {
			return agent.AgentToolResult{}, errors.New("remote argument too long")
		}
		words = append(words, arg)
	}
	for i, word := range words {
		words[i] = "'" + strings.ReplaceAll(word, "'", "'\"'\"'") + "'"
	}
	call, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(call, "/usr/bin/ssh", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10", "--", host, strings.Join(words, " "))
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	text := output.String()
	if err != nil {
		text += "\nRemote command failed or interrupted; inspect remote state before retrying."
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: text}}, IsError: err != nil}, nil
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	b.data = append(b.data, data[:min(len(data), max(0, (64<<10)-len(b.data)))]...)
	return n, nil
}
func (b *boundedOutput) String() string { return strings.ToValidUTF8(string(b.data), "") }

func deviceTools(devices []Device) ([]agent.AgentTool, error) {
	for _, device := range devices {
		if !device.Valid() {
			return nil, fmt.Errorf("invalid paired device")
		}
	}
	if len(devices) == 0 {
		return nil, nil
	}
	return []agent.AgentTool{&deviceTool{devices: devices}}, nil
}
