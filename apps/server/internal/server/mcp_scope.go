package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

var mcpName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func validateMCP(config agentruntime.MCPServer) bool {
	if !mcpName.MatchString(config.Name) || (config.URL == "") == (config.Command == "") || len(config.Command) > 2048 || strings.ContainsAny(config.Command, "\x00\r\n") || len(config.Args) > 32 {
		return false
	}
	for _, arg := range config.Args {
		if len(arg) > 2048 || strings.ContainsRune(arg, '\x00') {
			return false
		}
	}
	if config.URL != "" {
		u, err := url.Parse(config.URL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || len(config.URL) > 2048 || u.Scheme != "http" && u.Scheme != "https" {
			return false
		}
	}
	if len(config.Headers) > 16 || len(config.Headers) > 0 && config.URL == "" || len(config.Env) > 32 || len(config.Env) > 0 && config.Command == "" {
		return false
	}
	for name, value := range config.Env {
		if !envName.MatchString(name) || len(value) > 4096 || strings.ContainsRune(value, '\x00') {
			return false
		}
	}
	for name, value := range config.Headers {
		if !validHeader(name) || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return false
		}
	}
	return true
}

func (s *Server) effectiveMCP(ctx context.Context, id string, local []agentruntime.MCPServer) ([]agentruntime.MCPServer, error) {
	bindings, err := s.Store.MCPBindings(ctx, id)
	if err != nil {
		return nil, err
	}
	records, err := s.Store.MCPServers(ctx)
	if err != nil {
		return nil, err
	}
	shared := map[string]agentruntime.MCPServer{}
	for _, record := range records {
		var config agentruntime.MCPServer
		if err := json.Unmarshal(record.Data, &config); err != nil {
			return nil, err
		}
		shared[record.Name] = config
	}
	selected := map[string]agentruntime.MCPServer{}
	for _, config := range local {
		selected[config.Name] = config
	}
	for _, binding := range bindings {
		if binding.Hidden || !binding.Enabled {
			delete(selected, binding.Name)
			continue
		}
		if config, ok := shared[binding.Name]; ok {
			selected[binding.Name] = config
		} else if _, ok := selected[binding.Name]; !ok {
			return nil, store.ErrMCPUnknown
		}
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]agentruntime.MCPServer, 0, len(names))
	for _, name := range names {
		config := selected[name]
		if len(config.Headers) > 0 {
			config.Auth = map[string]string{}
			f := newFiller(ctx, s, id, config.URL)
			for header, value := range config.Headers {
				filled, err := f.fill(value, nil)
				if err != nil {
					// The connection fails its own auth and the run continues degraded.
					s.config.Log.Warn("MCP header unavailable", "connection", name, "error", err)
					continue
				}
				config.Auth[header] = filled
			}
		}
		result = append(result, config)
	}
	return result, nil
}

func (s *Server) mcpInventory(ctx context.Context, id string, local []agentruntime.MCPServer, includeHidden bool) ([]contract.MCPConnection, error) {
	records, err := s.Store.MCPServers(ctx)
	if err != nil {
		return nil, err
	}
	values := map[string]contract.MCPConnection{}
	for _, record := range records {
		values[record.Name] = contract.MCPConnection{Name: record.Name, Shared: true}
	}
	for _, config := range local {
		item := values[config.Name]
		item.Name = config.Name
		item.Enabled = true
		values[config.Name] = item
	}
	if id != "" {
		bindings, err := s.Store.MCPBindings(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			item := values[binding.Name]
			item.Name = binding.Name
			item.Enabled = binding.Enabled && !binding.Hidden
			item.Hidden = binding.Hidden
			values[binding.Name] = item
		}
	}
	result := []contract.MCPConnection{}
	for _, item := range values {
		if includeHidden || !item.Hidden {
			result = append(result, item)
		}
	}
	slices.SortFunc(result, func(a, b contract.MCPConnection) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (s *Server) registerMCP(ctx context.Context, config agentruntime.MCPServer, owner string, local []agentruntime.MCPServer, devices []agentruntime.Device, nativeOnly *bool) error {
	if !validateMCP(config) {
		return errors.New("invalid MCP configuration")
	}
	records, err := s.Store.MCPServers(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Name == config.Name {
			var old agentruntime.MCPServer
			if err := json.Unmarshal(record.Data, &old); err != nil {
				return err
			}
			if config.Command != "" && config.Command == old.Command && config.Env == nil {
				config.Env = old.Env
			}
			if config.URL != "" && config.URL == old.URL && config.Headers == nil {
				config.Headers = old.Headers
			}
			if nativeOnly == nil {
				config.NativeOnly = old.NativeOnly
			}
		}
	}
	if nativeOnly != nil {
		config.NativeOnly = *nativeOnly
	}
	for index := range local {
		if local[index].Name == config.Name {
			local[index] = config
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	var legacy json.RawMessage
	if local != nil {
		legacy, err = json.Marshal(local)
		if err != nil {
			return err
		}
	}
	var deviceJSON json.RawMessage
	if devices != nil {
		deviceJSON, err = json.Marshal(devices)
		if err != nil {
			return err
		}
	}
	return s.Store.RegisterMCP(ctx, config.Name, raw, owner, legacy, deviceJSON)
}

func (s *Server) mcpRoutes() {
	s.mux.HandleFunc("GET /mcp/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		items, err := s.mcpInventory(r.Context(), "", nil, true)
		s.respond(w, items, err)
	})
	s.mux.HandleFunc("POST /mcp/servers", s.setupMCP)
	s.mux.HandleFunc("GET /agents/{id}/mcp", s.agentMCP)
	s.mux.HandleFunc("GET /agents/{id}/mcp/status", s.agentMCPStatus)
	s.mux.HandleFunc("PATCH /agents/{id}/mcp/{name}", s.bindMCP)
}

func (s *Server) setupMCP(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var request struct {
		Name       string   `json:"name"`
		URL        string   `json:"url,omitempty"`
		Command    string   `json:"command,omitempty"`
		Args       []string `json:"args,omitempty"`
		NativeOnly *bool    `json:"nativeOnly,omitempty"`
		ReadOnly   bool     `json:"readOnly,omitempty"`
	}
	if !decode(w, r, &request) {
		return
	}
	config := agentruntime.MCPServer{Name: request.Name, URL: request.URL, Command: request.Command, Args: request.Args, ReadOnly: request.ReadOnly}
	if !validateMCP(config) {
		fail(w, 400, "invalid_mcp", "Choose a simple name and one MCP endpoint or executable without inline credentials.")
		return
	}
	if err := s.registerMCP(r.Context(), config, "", nil, nil, request.NativeOnly); err != nil {
		if !s.mcpError(w, err) {
			s.respond(w, nil, err)
		}
		return
	}
	write(w, 200, contract.MCPConnection{Name: config.Name, Shared: true})
}

func (s *Server) agentMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	a := s.actors[r.PathValue("id")]
	if a == nil {
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	inventory, err := s.mcpInventory(r.Context(), a.id, a.definition.MCP, true)
	s.respond(w, inventory, err)
}

func (s *Server) agentMCPStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.actorsMu.RLock()
	a := s.actors[r.PathValue("id")]
	if a == nil {
		s.actorsMu.RUnlock()
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	a.mu.Lock()
	local, workdir := a.definition.MCP, a.sessionDir
	a.mu.Unlock()
	s.actorsMu.RUnlock()
	// Probes run unlocked; one slow connection must not stall the actor.
	if workdir == "" {
		workdir = agentruntime.AgentDir(s.config.DataDir, a.id)
	}
	items, err := s.mcpStatus(r.Context(), a.id, local, workdir)
	s.respond(w, items, err)
}

// mcpStatus resolves each active connection the way a turn would and probes it with
// the session handshake, in parallel.
func (s *Server) mcpStatus(
	ctx context.Context,
	id string,
	local []agentruntime.MCPServer,
	cwd string,
) ([]*contract.MCPStatus, error) {
	inventory, err := s.mcpInventory(ctx, id, local, false)
	if err != nil {
		return nil, err
	}
	bindings, err := s.Store.MCPBindings(ctx, id)
	if err != nil {
		return nil, err
	}
	records, err := s.Store.MCPServers(ctx)
	if err != nil {
		return nil, err
	}
	shared := map[string]agentruntime.MCPServer{}
	for _, record := range records {
		var config agentruntime.MCPServer
		if err := json.Unmarshal(record.Data, &config); err != nil {
			return nil, err
		}
		shared[record.Name] = config
	}
	definitions := map[string]agentruntime.MCPServer{}
	for _, config := range local {
		definitions[config.Name] = config
	}
	shell := func(string) map[string]string { return nil }
	if s.proxy != nil || s.ssh != nil {
		shell = s.shellEnv
	}
	items := []*contract.MCPStatus{}
	var wait sync.WaitGroup
	for _, connection := range inventory {
		if !connection.Enabled {
			continue
		}
		// An enabled binding carries the shared setup; without one, the agent's own definition runs.
		config, resolvable := shared[connection.Name]
		if !slices.ContainsFunc(bindings, func(binding store.MCPBinding) bool {
			return binding.Name == connection.Name && binding.Enabled && !binding.Hidden
		}) {
			config, resolvable = definitions[connection.Name]
		}
		status := &contract.MCPStatus{Name: connection.Name, Tools: []string{}}
		items = append(items, status)
		if !resolvable {
			continue
		}
		if len(config.Headers) > 0 {
			config.Auth = map[string]string{}
			f := newFiller(ctx, s, id, config.URL)
			for header, value := range config.Headers {
				filled, err := f.fill(value, nil)
				if err != nil {
					// The connection fails its own auth and the run continues degraded.
					s.config.Log.Warn("MCP header unavailable", "connection", connection.Name, "error", err)
					resolvable = false
					break
				}
				config.Auth[header] = filled
			}
		}
		if !resolvable {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			tools, err := agentruntime.ProbeMCP(
				ctx,
				config,
				cwd,
				shell(id),
			)
			if err != nil {
				return
			}
			status.Tools = tools
			status.Connected = true
		}()
	}
	wait.Wait()
	return items, nil
}

func (s *Server) bindMCP(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var patch struct {
		Enabled *bool `json:"enabled,omitempty"`
		Hidden  *bool `json:"hidden,omitempty"`
	}
	if !decode(w, r, &patch) {
		return
	}
	if patch.Enabled == nil && patch.Hidden == nil {
		fail(w, 400, "invalid_mcp", "Choose an MCP visibility field to update.")
		return
	}
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	a := s.actors[r.PathValue("id")]
	if a == nil {
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	legacy := slices.ContainsFunc(a.definition.MCP, func(config agentruntime.MCPServer) bool { return config.Name == r.PathValue("name") })
	if err := s.Store.BindMCP(r.Context(), a.id, r.PathValue("name"), patch.Enabled, patch.Hidden, legacy); err != nil {
		if !s.mcpError(w, err) {
			s.respond(w, nil, err)
		}
		return
	}
	items, err := s.mcpInventory(r.Context(), a.id, a.definition.MCP, true)
	s.respond(w, items, err)
}

func (s *Server) mcpError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrMCPHidden):
		fail(w, 403, "mcp_hidden", "This connection is hidden from the agent.")
	case errors.Is(err, store.ErrMCPUnknown), errors.Is(err, sql.ErrNoRows):
		fail(w, 404, "not_found", "MCP connection not found.")
	case errors.Is(err, store.ErrMCPLimit):
		fail(w, 409, "mcp_limit", "You can configure up to sixty-four MCP connections.")
	default:
		return false
	}
	return true
}
