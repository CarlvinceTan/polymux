package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func configuration(d agentruntime.Definition) contract.AgentConfiguration {
	return contract.AgentConfiguration{Workspace: d.Workspace, Tools: append([]string{}, d.Tools...), Skills: append([]string{}, d.Skills...)}
}

func (s *Server) agentConfiguration(w http.ResponseWriter, r *http.Request) {
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
	write(w, 200, configuration(a.definition))
}

func (s *Server) configureAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !nativeRequest(w, r) {
		return
	}
	var patch contract.ConfigurationPatch
	if !decode(w, r, &patch) {
		return
	}
	if patch.Workspace == nil && patch.Tools == nil && patch.Skills == nil {
		fail(w, 400, "invalid_configuration", "Choose a configuration field to update.")
		return
	}
	if patch.Workspace != nil {
		value := strings.TrimSpace(*patch.Workspace)
		if len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") || value != "" && !filepath.IsAbs(value) {
			fail(w, 400, "invalid_workspace", "Enter an absolute repository path on the server.")
			return
		}
		patch.Workspace = &value
	}
	if patch.Tools != nil {
		seen := map[string]bool{}
		for _, name := range patch.Tools {
			if !slices.Contains([]string{"read", "write", "edit", "bash", "find", "ls"}, name) || seen[name] {
				fail(w, 400, "invalid_tools", "Choose different supported command tools.")
				return
			}
			seen[name] = true
		}
	}
	if patch.Skills != nil {
		if len(patch.Skills) > 64 {
			fail(w, 400, "invalid_skills", "Choose up to sixty-four skill files.")
			return
		}
		seen := map[string]bool{}
		for _, path := range patch.Skills {
			if !filepath.IsAbs(path) || len(path) > 2048 || strings.ContainsAny(path, "\x00\r\n") || filepath.Ext(path) != ".md" || seen[path] {
				fail(w, 400, "invalid_skills", "Choose different absolute Markdown skill paths.")
				return
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				fail(w, 400, "invalid_skills", "A selected skill file is unavailable.")
				return
			}
			seen[path] = true
		}
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
	if s.closing || s.ctx.Err() != nil {
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
		return
	}
	before := configuration(a.definition)
	raw, err := s.Store.ConfigureAgent(r.Context(), a.id, patch)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	var saved agentruntime.Definition
	if err := json.Unmarshal(raw, &saved); err != nil {
		s.respond(w, nil, errors.New("saved agent configuration is invalid"))
		return
	}
	if patch.Workspace != nil {
		a.definition.Workspace = saved.Workspace
		a.definition.WorkspaceSet = saved.WorkspaceSet
	}
	if patch.Tools != nil {
		a.definition.Tools = saved.Tools
	}
	if patch.Skills != nil {
		a.definition.Skills = saved.Skills
	}
	after := configuration(a.definition)
	if !reflect.DeepEqual(before, after) {
		a.revision++
	}
	write(w, 200, after)
}
