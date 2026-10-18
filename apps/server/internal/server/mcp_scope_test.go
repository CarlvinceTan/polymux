package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type scopeFactory struct {
	testFactory
	mu     sync.Mutex
	opened []openedAgent
	closed int
}
type scopeSession struct {
	*testSession
	owner *scopeFactory
}

func (f *scopeFactory) Open(_ context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, openedAgent{definition: d, path: path})
	return &scopeSession{testSession: &testSession{factory: &f.testFactory}, owner: f}, nil
}
func (s *scopeSession) Close() error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	s.owner.closed++
	return nil
}
func (f *scopeFactory) snapshots() []openedAgent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.opened)
}

func TestMCPScopeRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db")
	defs := []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}, MCP: []agentruntime.MCPServer{{Name: "private", Command: "fixture-private"}}}, {Agent: contract.Agent{ID: "b", Name: "B"}}}
	factory := &scopeFactory{}
	open := func() (*Server, *store.Store) {
		t.Helper()
		db, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		api, err := New(t.Context(), db, Config{DataDir: dir, Factory: factory, Definitions: defs, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			t.Fatal(err)
		}
		return api, db
	}
	api, db := open()
	defer func() { api.Close(); db.Close() }()
	call := func(method, url, body string, status int) []byte {
		t.Helper()
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(method, url, strings.NewReader(body)))
		if response.Code != status {
			t.Fatal(method, url, response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	turn := func(id, message string) {
		t.Helper()
		result := api.execute(t.Context(), api.actors[id], store.Work{Trigger: "message", Message: contract.Message{ID: message, ThreadID: id, Role: "user", Parts: []contract.Part{{Kind: "text", Text: new("Hello")}}}})
		if result.Status != "done" {
			t.Fatal("fixture turn failed", result.Status)
		}
	}
	turn("a", "first-a")
	turn("b", "first-b")
	call("POST", "/mcp/servers", `{"name":"notes","command":"fixture-notes","args":["serve"]}`, 200)
	turn("b", "setup-does-not-enable")
	opened := factory.snapshots()
	if len(opened) != 2 || len(opened[0].definition.MCP) != 1 || len(opened[1].definition.MCP) != 0 {
		t.Fatal("shared setup enabled an unassigned agent")
	}
	inventory := call("GET", "/mcp/servers", "", 200)
	if strings.Contains(string(inventory), "fixture-notes") || strings.Contains(string(inventory), "serve") {
		t.Fatal("inventory exposed connection configuration")
	}
	call("PATCH", "/agents/b/mcp/notes", `{"enabled":true}`, 200)
	turn("b", "enabled")
	opened = factory.snapshots()
	if len(opened) != 3 || opened[2].path != "test.jsonl" || len(opened[2].definition.MCP) != 1 || opened[2].definition.MCP[0].Name != "notes" {
		t.Fatal("enabled MCP did not reopen with retained history")
	}
	turn("a", "still-private")
	if len(factory.snapshots()) != 3 {
		t.Fatal("another agent inherited shared MCP")
	}
	call("PATCH", "/agents/b/mcp/notes", `{"hidden":true}`, 200)
	turn("b", "hidden")
	opened = factory.snapshots()
	if len(opened) != 4 || len(opened[3].definition.MCP) != 0 {
		t.Fatal("hidden connection remained in runtime")
	}
	state := &turnState{run: contract.Run{AgentID: "b", ConversationID: "b"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	if _, err := (&integrationTool{server: api}).Execute(ctx, "attach", json.RawMessage(`{"action":"attach","name":"notes"}`), nil); !errors.Is(err, store.ErrMCPHidden) {
		t.Fatal("agent bypassed user hidden mask", err)
	}
	result, err := (&integrationTool{server: api}).Execute(ctx, "list", json.RawMessage(`{"action":"list"}`), nil)
	if err != nil || strings.Contains(result.Text(), "notes") {
		t.Fatal("hidden connection leaked into agent inventory", err)
	}
	call("PATCH", "/agents/b/mcp/notes", `{"hidden":false,"enabled":true}`, 200)
	turn("b", "unhidden")
	call("POST", "/mcp/servers", `{"name":"notes","command":"fixture-notes-v2"}`, 200)
	turn("b", "updated-shared-setup")
	opened = factory.snapshots()
	last := opened[len(opened)-1]
	if len(opened) != 6 || last.path != "test.jsonl" || last.definition.MCP[0].Command != "fixture-notes-v2" {
		t.Fatal("shared setup edit did not refresh enabled agent")
	}
	factory.mu.Lock()
	closed := factory.closed
	factory.mu.Unlock()
	if closed != 4 {
		t.Fatal("stale MCP sessions were not closed", closed)
	}
	call("PATCH", "/agents/a/mcp/private", `{"hidden":true}`, 200)
	turn("a", "masked-existing")
	opened = factory.snapshots()
	if len(opened[len(opened)-1].definition.MCP) != 0 {
		t.Fatal("existing per-agent setup bypassed hidden mask")
	}
	api.Close()
	db.Close()
	api, db = open()
	effective, err := api.effectiveMCP(t.Context(), "b", nil)
	if err != nil || len(effective) != 1 || effective[0].Command != "fixture-notes-v2" {
		t.Fatal("shared setup/scope did not survive restart", err)
	}
	effective, err = api.effectiveMCP(t.Context(), "a", defs[0].MCP)
	if err != nil || len(effective) != 0 {
		t.Fatal("legacy mask did not survive restart", err)
	}
	call("PATCH", "/agents/b/mcp/unknown", `{"enabled":true}`, 404)
	call("PATCH", "/agents/missing/mcp/notes", `{"enabled":true}`, 404)
	call("PATCH", "/agents/b/mcp/notes", `{}`, 400)
	for _, body := range []string{`{"name":"bad_name","command":"fixture"}`, `{"name":"notes","url":"https://credential@example.com/mcp"}`, `{"name":"notes","url":"https://example.com/mcp?token=fixture"}`, `{"name":"notes","command":"fixture","env":{"TOKEN":"fixture"}}`} {
		call("POST", "/mcp/servers", body, 400)
	}
}

func TestMCPSetupReuse(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	owner := api.actors["assistant"]
	owner.mu.Lock()
	owner.definition.MCP = []agentruntime.MCPServer{{Name: "notes", Command: "fixture-notes", Env: map[string]string{"MOCK_CONFIG": "preserve"}}}
	owner.mu.Unlock()
	state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	if _, err := api.Store.DB.ExecContext(t.Context(), `UPDATE agent_definitions SET data=json_set(data,'$.sourceEvidence',json('{"id":9223372036854775807}')) WHERE agent_id='assistant'`); err != nil {
		t.Fatal(err)
	}
	tool := &integrationTool{server: api}
	for _, action := range []string{`{"action":"mcp","name":"notes","command":"fixture-notes","args":["serve"]}`, `{"action":"detach","name":"notes"}`, `{"action":"attach","name":"notes"}`} {
		if _, err := tool.Execute(ctx, "setup", json.RawMessage(action), nil); err != nil {
			t.Fatal(err)
		}
	}
	effective, err := api.effectiveMCP(t.Context(), "assistant", nil)
	if err != nil || len(effective) != 1 || effective[0].Env["MOCK_CONFIG"] != "preserve" {
		t.Fatal("shared setup did not reuse existing environment", err)
	}
	records, err := api.Store.MCPServers(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatal("repeat setup duplicated shared connection", err)
	}
	var evidence string
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT json_extract(data,'$.sourceEvidence') FROM agent_definitions WHERE agent_id='assistant'`).Scan(&evidence); err != nil || evidence != `{"id":9223372036854775807}` {
		t.Fatal("MCP registration replaced source metadata", err)
	}
	before := records[0].Data
	if err := api.Store.BindMCP(t.Context(), "assistant", "notes", nil, new(true), true); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, "setup", json.RawMessage(`{"action":"mcp","name":"notes","command":"changed"}`), nil); !errors.Is(err, store.ErrMCPHidden) {
		t.Fatal("hidden setup was edited by agent", err)
	}
	records, err = api.Store.MCPServers(t.Context())
	if err != nil || !reflect.DeepEqual(before, records[0].Data) {
		t.Fatal("rejected setup changed shared configuration", err)
	}
}

func TestMCPSharedFlags(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	config := agentruntime.MCPServer{Name: "notes", Command: "fixture-notes", NativeOnly: true, Env: map[string]string{"MOCK_CONFIG": "preserve"}}
	if err := api.registerMCP(t.Context(), config, "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.BindMCP(t.Context(), "assistant", "notes", new(true), nil, false); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &integrationTool{server: api}
	for index, action := range []string{`{"action":"mcp","name":"notes","command":"fixture-notes","args":["serve"]}`, `{"action":"mcp","name":"notes","command":"fixture-notes","native_only":false}`} {
		if _, err := tool.Execute(ctx, "update", json.RawMessage(action), nil); err != nil {
			t.Fatal(err)
		}
		effective, err := api.effectiveMCP(t.Context(), "assistant", nil)
		if err != nil || len(effective) != 1 || effective[0].NativeOnly != (index == 0) || effective[0].Env["MOCK_CONFIG"] != "preserve" {
			t.Fatal("shared-only update lost omitted flags or private environment", err)
		}
	}
}

func TestMCPRemoveRollback(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &integrationTool{server: api}
	if _, err := tool.Execute(ctx, "setup", json.RawMessage(`{"action":"mcp","name":"notes","command":"fixture-notes"}`), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Store.DB.ExecContext(t.Context(), `UPDATE agent_definitions SET data=json_set(data,'$.sourceEvidence',json('{"id":9223372036854775807}')) WHERE agent_id='assistant'; CREATE TRIGGER reject_profile BEFORE UPDATE ON agent_definitions BEGIN SELECT RAISE(ABORT,'fixture failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, "remove", json.RawMessage(`{"action":"remove","name":"notes"}`), nil); err == nil {
		t.Fatal("profile failure was ignored")
	}
	effective, err := api.effectiveMCP(t.Context(), "assistant", api.actors["assistant"].definition.MCP)
	if err != nil || len(effective) != 1 || effective[0].Name != "notes" {
		t.Fatal("failed removal changed active MCP binding", err)
	}
	if _, err := api.Store.DB.ExecContext(t.Context(), `DROP TRIGGER reject_profile`); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, "remove", json.RawMessage(`{"action":"remove","name":"notes"}`), nil); err != nil {
		t.Fatal(err)
	}
	effective, err = api.effectiveMCP(t.Context(), "assistant", api.actors["assistant"].definition.MCP)
	if err != nil || len(effective) != 0 {
		t.Fatal("successful removal left connection attached", err)
	}
	records, err := api.Store.MCPServers(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatal("agent removal deleted account-wide setup", err)
	}
	var evidence string
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT json_extract(data,'$.sourceEvidence') FROM agent_definitions WHERE agent_id='assistant'`).Scan(&evidence); err != nil || evidence != `{"id":9223372036854775807}` {
		t.Fatal("integration removal replaced source metadata", err)
	}
}

func TestMCPVoiceScope(t *testing.T) {
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		names := []string{}
		reads := []string{}
		for _, config := range d.MCP {
			names = append(names, config.Name+"__")
			if config.ReadOnly {
				reads = append(reads, config.Name+"__")
			}
		}
		if !slices.Equal(state.prefixes, names) || !slices.Equal(state.readPrefixes, reads) {
			return agentruntime.Result{}, errors.New("voice policy used a different MCP scope")
		}
		return agentruntime.Result{Text: "Private result"}, nil
	}}
	api, web := voiceTest(t, factory, func(config *Config) {
		config.Definitions[0].MCP = []agentruntime.MCPServer{{Name: "private", Command: "fixture-private"}}
	})
	if err := api.registerMCP(t.Context(), agentruntime.MCPServer{Name: "shared", Command: "fixture-shared", ReadOnly: true}, "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.BindMCP(t.Context(), "a", "private", nil, new(true), true); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.BindMCP(t.Context(), "a", "shared", new(true), nil, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := voicePost(t, web, "a", "voice-scoped"); status != 200 {
		t.Fatal(status)
	}
	if err := api.Store.BindMCP(t.Context(), "a", "shared", new(false), nil, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := voicePost(t, web, "a", "voice-detached"); status != 200 {
		t.Fatal(status)
	}
	factory.mu.Lock()
	if len(factory.opened) != 2 || len(factory.opened[0].MCP) != 1 || factory.opened[0].MCP[0].Name != "shared" || len(factory.opened[1].MCP) != 0 {
		t.Error("voice did not honor shared, hidden, and detached MCP scope")
	}
	factory.mu.Unlock()
	privateChat(t, api, "a")
}

func TestMCPAutomaticScope(t *testing.T) {
	factory := &scopeFactory{}
	api, _ := startTest(t, factory)
	if err := api.registerMCP(t.Context(), agentruntime.MCPServer{Name: "notes", Command: "fixture-notes"}, "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for index, enabled := range []bool{true, false} {
		if err := api.Store.BindMCP(t.Context(), "assistant", "notes", &enabled, nil, false); err != nil {
			t.Fatal(err)
		}
		result := api.execute(t.Context(), api.actors["assistant"], store.Work{Trigger: "follow_up", Message: contract.Message{ID: []string{"automatic-enabled", "automatic-disabled"}[index], ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: new("Continue")}}}})
		if result.Status != "done" {
			t.Fatal("automatic fixture turn failed", result.Status)
		}
		opened := factory.snapshots()
		definition := opened[len(opened)-1].definition
		if !definition.Automatic || enabled != (len(definition.MCP) == 1) || enabled && definition.MCP[0].Name != "notes" {
			t.Fatal("automatic turn did not honor agent-specific MCP scope")
		}
	}
}
