package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type integrationTool struct{ server *Server }

func (*integrationTool) Name() string                           { return "manage_integrations" }
func (*integrationTool) Label() string                          { return "Configure integrations" }
func (*integrationTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*integrationTool) Description() string {
	return "List shared MCP setup, attach or detach this agent by name, register shared MCP setup, or remove this agent's MCP connections and trusted Markdown skill paths, or enable CLI tools. MCP setup is shared, but use is enabled only for this agent; attach reuses shared setup without copying credentials. Hidden connections cannot be attached. Changes persist and are available next turn, not immediately. Prefer CLI plus a skill for occasional/simple work; MCP for frequent structured calls. Decide whether to ask in normal text from the user's task and established preferences. Inspect existing setup; never copy credentials into arguments, URLs, skills or memory. This does not install software or alter OS permissions."
}
func (t *integrationTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"action":      map[string]any{"type": "string", "enum": []string{"list", "mcp", "skill", "remove", "enable_cli", "device", "attach", "detach"}},
			"host":        map[string]any{"type": "string", "description": "Existing SSH alias or user@tailnet-host; uses existing host keys"},
			"cua_binary":  map[string]any{"type": "string", "description": "Optional absolute Cua executable on the device; requires cua_socket too"},
			"cua_socket":  map[string]any{"type": "string", "description": "Existing Cua daemon socket on the device"},
			"native_only": map[string]any{"type": "boolean", "description": "Exclude browser tools from this MCP connection"},
			"name":        map[string]any{"type": "string", "description": "MCP connection name"},
			"url":         map[string]any{"type": "string", "description": "HTTP(S) MCP endpoint without inline credentials"},
			"command":     map[string]any{"type": "string", "description": "Stdio executable, not a shell expression"},
			"args":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 32},
			"path":        map[string]any{"type": "string", "description": "Absolute path to an existing trusted .md skill"},
		}, "required": []string{"action"},
	}}
}
func (t *integrationTool) Execute(ctx context.Context, _ string, args json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil || state.run.AgentID != state.run.ConversationID {
		if err == nil {
			err = errors.New("integration setup is direct-chat only")
		}
		return agent.AgentToolResult{}, err
	}
	// An unattended turn must not widen its own tool surface.
	if state.work.Trigger != "message" {
		return agent.AgentToolResult{}, errors.New("integration setup requires a user turn")
	}
	var input struct {
		Action     string   `json:"action"`
		Name       string   `json:"name"`
		URL        string   `json:"url"`
		Command    string   `json:"command"`
		Args       []string `json:"args"`
		Path       string   `json:"path"`
		Host       string   `json:"host"`
		CuaBinary  string   `json:"cua_binary"`
		CuaSocket  string   `json:"cua_socket"`
		NativeOnly *bool    `json:"native_only"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return agent.AgentToolResult{}, err
	}
	s := t.server
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	a := s.actors[state.run.AgentID]
	if a == nil || s.closing || s.deletes[a.id] != nil {
		return agent.AgentToolResult{}, errors.New("integration owner unavailable")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.definition
	previous, err := json.Marshal(d)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	d.MCP, d.Skills = slices.Clone(d.MCP), slices.Clone(d.Skills)
	d.Devices = slices.Clone(d.Devices)
	removedSkill := input.Action == "remove" && slices.Contains(d.Skills, input.Path)
	switch input.Action {
	case "list":
		inventory, err := s.mcpInventory(ctx, a.id, d.MCP, false)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		names := []string{}
		available := []string{}
		for _, item := range inventory {
			if item.Enabled {
				names = append(names, item.Name)
			}
			if item.Shared {
				available = append(available, item.Name)
			}
		}
		return toolResult(map[string]any{"mcp": names, "availableMCP": available, "skills": d.Skills, "tools": d.Tools, "devices": d.Devices})
	case "attach", "detach":
		legacy := slices.ContainsFunc(d.MCP, func(config agentruntime.MCPServer) bool { return config.Name == input.Name })
		enabled := input.Action == "attach"
		if err := s.Store.BindMCP(ctx, a.id, input.Name, &enabled, nil, legacy); err != nil {
			return agent.AgentToolResult{}, err
		}
		return toolResult(map[string]any{"configured": true, "available": "next turn"})
	case "device":
		device := agentruntime.Device{Name: input.Name, Host: input.Host}
		if !device.Valid() {
			return agent.AgentToolResult{}, errors.New("choose a simple device name and existing SSH host")
		}
		index := slices.IndexFunc(d.Devices, func(item agentruntime.Device) bool { return item.Name == device.Name })
		if index < 0 {
			d.Devices = append(d.Devices, device)
		} else {
			d.Devices[index] = device
		}
		if input.CuaBinary != "" || input.CuaSocket != "" {
			for _, path := range []string{input.CuaBinary, input.CuaSocket} {
				if !filepath.IsAbs(path) || len(path) > 2048 || strings.ContainsAny(path, "\x00\r\n") {
					return agent.AgentToolResult{}, errors.New("provide both existing absolute Cua paths")
				}
			}
			quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\"'\"'") + "'" }
			config := agentruntime.MCPServer{Name: input.Name, Command: "/usr/bin/ssh", Args: []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10", "--", input.Host, quote(input.CuaBinary) + " mcp --socket " + quote(input.CuaSocket)}, NativeOnly: true}
			index := slices.IndexFunc(d.MCP, func(item agentruntime.MCPServer) bool { return item.Name == config.Name })
			if index < 0 {
				d.MCP = append(d.MCP, config)
			} else {
				d.MCP[index] = config
			}
		}
	case "enable_cli":
		d.Tools = []string{"read", "write", "edit", "bash", "find", "ls"}
	case "skill":
		if !filepath.IsAbs(input.Path) || filepath.Ext(input.Path) != ".md" || len(input.Path) > 2048 {
			return agent.AgentToolResult{}, errors.New("use an absolute Markdown skill path")
		}
		info, err := os.Stat(input.Path)
		if err != nil || !info.Mode().IsRegular() {
			return agent.AgentToolResult{}, errors.New("skill file is unavailable")
		}
		if !slices.Contains(d.Skills, input.Path) {
			d.Skills = append(d.Skills, input.Path)
		}
	case "mcp":
		if input.Name == "" || len(input.Name) > 64 || strings.ContainsAny(input.Name, " /\\.:_") || (input.URL == "") == (input.Command == "") || len(input.Command) > 2048 || len(input.Args) > 32 {
			return agent.AgentToolResult{}, errors.New("use a unique simple name and exactly one MCP URL or executable")
		}
		for _, arg := range input.Args {
			if len(arg) > 2048 {
				return agent.AgentToolResult{}, errors.New("MCP argument too long")
			}
		}
		if input.URL != "" {
			u, err := url.Parse(input.URL)
			if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || len(input.URL) > 2048 || u.Scheme != "http" && u.Scheme != "https" {
				return agent.AgentToolResult{}, errors.New("use an HTTP(S) MCP URL without inline credentials or query parameters")
			}
		}
		config := agentruntime.MCPServer{Name: input.Name, URL: input.URL, Command: input.Command, Args: input.Args}
		index := slices.IndexFunc(d.MCP, func(server agentruntime.MCPServer) bool { return server.Name == input.Name })
		if index < 0 {
			d.MCP = append(d.MCP, config)
		} else {
			config.NativeOnly = d.MCP[index].NativeOnly
			if config.Command == d.MCP[index].Command {
				config.Env = d.MCP[index].Env
			}
			d.MCP[index] = config
		}
		if input.NativeOnly != nil {
			index = slices.IndexFunc(d.MCP, func(item agentruntime.MCPServer) bool { return item.Name == input.Name })
			d.MCP[index].NativeOnly = *input.NativeOnly
		}
	case "remove":
		d.MCP = slices.DeleteFunc(d.MCP, func(server agentruntime.MCPServer) bool { return server.Name == input.Name })
		d.Devices = slices.DeleteFunc(d.Devices, func(device agentruntime.Device) bool { return device.Name == input.Name })
		d.Skills = slices.DeleteFunc(d.Skills, func(path string) bool { return path == input.Path })
	default:
		return agent.AgentToolResult{}, errors.New("invalid integration action")
	}
	if len(d.MCP) > 64 || len(d.Skills) > 16 || len(d.Devices) > 16 {
		return agent.AgentToolResult{}, errors.New("integration limit reached")
	}
	data, err := json.Marshal(d)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	if input.Action == "mcp" || input.Action == "device" && input.CuaBinary != "" {
		index := slices.IndexFunc(d.MCP, func(config agentruntime.MCPServer) bool { return config.Name == input.Name })
		var devices []agentruntime.Device
		if input.Action == "device" {
			devices = d.Devices
		}
		nativeOnly := input.NativeOnly
		if input.Action == "device" {
			nativeOnly = new(true)
		}
		if err := s.registerMCP(ctx, d.MCP[index], a.id, d.MCP, devices, nativeOnly); err != nil {
			return agent.AgentToolResult{}, err
		}
	} else if input.Action == "remove" {
		mcp, err := json.Marshal(d.MCP)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		devices, err := json.Marshal(d.Devices)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		skills, err := json.Marshal(d.Skills)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		if err := s.Store.RemoveIntegrations(ctx, a.id, input.Name, mcp, devices, skills); err != nil {
			return agent.AgentToolResult{}, err
		}
	} else if bytes.Equal(previous, data) {
		return toolResult(map[string]any{"configured": true, "available": "next turn"})
	} else if _, err := s.Store.UpdateDefinition(ctx, d.Agent, data); err != nil {
		return agent.AgentToolResult{}, err
	}
	a.definition = d
	a.revision++
	type change struct{ kind, id, label string }
	changes := []change{}
	if input.Action == "mcp" {
		changes = append(changes, change{"connector.added", input.Name, input.Name})
	}
	if input.Action == "device" {
		changes = append(changes, change{"device.added", input.Name, input.Name})
	}
	if input.Action == "skill" || removedSkill {
		label := filepath.Base(input.Path)
		if label == "SKILL.md" {
			label = filepath.Base(filepath.Dir(input.Path))
		}
		kind := "skill.saved"
		if input.Action == "remove" {
			kind = "skill.removed"
		}
		changes = append(changes, change{kind, input.Path, label})
	}
	if input.Action == "remove" && input.Name != "" {
		var prior agentruntime.Definition
		if err := json.Unmarshal(previous, &prior); err != nil {
			return agent.AgentToolResult{}, err
		}
		if slices.ContainsFunc(prior.MCP, func(item agentruntime.MCPServer) bool { return item.Name == input.Name }) {
			changes = append(changes, change{"connector.removed", input.Name, input.Name})
		}
		if slices.ContainsFunc(prior.Devices, func(item agentruntime.Device) bool { return item.Name == input.Name }) {
			changes = append(changes, change{"device.removed", input.Name, input.Name})
		}
	}
	for _, change := range changes {
		payload, err := store.NewActivity(change.kind, change.id, change.label)
		if err != nil {
			return agent.AgentToolResult{}, err
		}
		if err := s.Store.Activity(ctx, state.run.ConversationID, state.run.AgentID, payload); err != nil {
			return agent.AgentToolResult{}, err
		}
	}
	return toolResult(map[string]any{"configured": true, "available": "next turn"})
}
