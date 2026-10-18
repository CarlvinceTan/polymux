package server

import (
	"encoding/json"
	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAgentConfiguration(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	avatar := defaultAvatar()
	d := agentruntime.Definition{Agent: contract.Agent{ID: "a", Name: "A", Model: "live-model", Avatar: &avatar}, Prompt: "Preserved fixture instructions", Tools: []string{"read"}, MCP: []agentruntime.MCPServer{{Name: "notes", Command: "fixture-mcp", Env: map[string]string{"MOCK": "preserved"}}}}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	original["sourceProfile"] = json.RawMessage(`{"futureNumber":9223372036854775807}`)
	raw, err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDefinition(t.Context(), d.Agent, raw); err != nil {
		t.Fatal(err)
	}
	factory := &definitionFactory{opened: make(chan openedAgent, 8)}
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: factory, Definitions: []agentruntime.Definition{d}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	call := func(method, body string, status int) []byte {
		t.Helper()
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(method, "/agents/a/configuration", strings.NewReader(body)))
		if response.Code != status {
			t.Fatal(response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	turn := func(id string) {
		t.Helper()
		message := contract.Message{ID: id, ThreadID: "a", Role: "user", Parts: []contract.Part{{Kind: "text", Text: new("Hello")}}}
		result := api.execute(t.Context(), api.actors["a"], store.Work{Trigger: "message", Message: message})
		if result.Status != "done" {
			t.Fatal("fixture turn failed", result.Status)
		}
	}
	turn("before")
	<-factory.opened
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("Use the fixture skill."), 0600); err != nil {
		t.Fatal(err)
	}
	patch, err := json.Marshal(map[string]any{"tools": []string{"read", "ls"}, "skills": []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	call("PATCH", string(patch), 200)
	var config contract.AgentConfiguration
	data := call("GET", "", 200)
	if err := json.Unmarshal(data, &config); err != nil || !slices.Equal(config.Tools, []string{"read", "ls"}) || !slices.Equal(config.Skills, []string{path}) {
		t.Fatal(config, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 3 {
		t.Fatal("configuration inventory exposed unrelated profile data")
	}
	turn("after")
	second := <-factory.opened
	if second.path != "test.jsonl" || !slices.Equal(second.definition.Tools, config.Tools) || !slices.Equal(second.definition.Skills, config.Skills) {
		t.Fatal("next turn did not use saved configuration")
	}
	profile, _ := db.Profile(t.Context(), "a")
	if profile.Prompt != d.Prompt || profile.Agent.Model != d.Agent.Model || !reflect.DeepEqual(profile.Agent.Avatar, d.Agent.Avatar) {
		t.Fatal("configuration changed unrelated profile fields")
	}
	data, _ = db.Definition(t.Context(), "a")
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"sourceProfile", "mcp", "prompt", "agent"} {
		if !reflect.DeepEqual(saved[field], original[field]) {
			t.Fatal("configuration changed unrelated stored field", field)
		}
	}
	before := string(data)
	for _, body := range []string{`{}`, `{"tools":["unsupported"]}`, `{"tools":["read","read"]}`, `{"workspace":"relative"}`, `{"workspace":"/valid","skills":["/missing/SKILL.md"]}`, `{"model":"other"}`} {
		call("PATCH", body, 400)
		after, _ := db.Definition(t.Context(), "a")
		if string(after) != before {
			t.Fatal("rejected patch partially changed configuration")
		}
	}
	call("PATCH", `{"tools":[],"skills":[],"workspace":""}`, 200)
	if err := json.Unmarshal(call("GET", "", 200), &config); err != nil || len(config.Tools) != 0 || len(config.Skills) != 0 || config.Workspace != "" {
		t.Fatal(config, err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest("PATCH", "/agents/missing/configuration", strings.NewReader(`{"tools":[]}`)))
	if response.Code != 404 {
		t.Fatal(response.Code)
	}
}

func TestWorkspaceClearPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db")
	d := agentruntime.Definition{Agent: contract.Agent{ID: "a", Name: "A"}, Workspace: workspaceRepo(t), Tools: []string{"read"}}
	open := func() (*Server, *store.Store) {
		t.Helper()
		db, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Definitions: []agentruntime.Definition{d}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			t.Fatal(err)
		}
		return api, db
	}
	api, db := open()
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest("PATCH", "/agents/a/configuration", strings.NewReader(`{"workspace":""}`)))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest("PATCH", "/agents/a", strings.NewReader(`{"name":"Renamed","role":"","prompt":""}`)))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	api.Close()
	db.Close()
	api, db = open()
	defer db.Close()
	defer api.Close()
	if api.actors["a"].definition.Workspace != "" {
		t.Fatal("cleared workspace inherited configured default after restart")
	}
}

func TestConfigurationKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := agentruntime.Definition{Agent: contract.Agent{ID: "a", Name: "A"}, Tools: []string{"read"}}
	raw, _ := json.Marshal(d)
	if err := db.SaveDefinition(t.Context(), d.Agent, raw); err != nil {
		t.Fatal(err)
	}
	d.Workspace = workspaceRepo(t)
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Definitions: []agentruntime.Definition{d}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	for _, method := range []string{"GET", "PATCH"} {
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(method, "/agents/a/configuration", strings.NewReader(`{"tools":[]}`)))
		var value contract.AgentConfiguration
		if err := json.Unmarshal(response.Body.Bytes(), &value); response.Code != 200 || err != nil || value.Workspace != d.Workspace {
			t.Fatal("effective default was lost", method, response.Code, err)
		}
	}
	if api.actors["a"].definition.Workspace != d.Workspace {
		t.Fatal("omitted workspace patch changed runtime repo")
	}
}
