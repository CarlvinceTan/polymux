package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestIntegrationSetup(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &integrationTool{server: api}
	state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	if err := api.Store.Presence(t.Context(), "assistant", true); err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{
		`{"action":"enable_cli"}`, `{"action":"mcp","name":"notes","command":"notes-mcp","args":["serve"]}`,
	} {
		if _, err := tool.Execute(ctx, "setup", json.RawMessage(args), nil); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte("Use the notes CLI for occasional queries."), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"action": "skill", "path": path})
	if _, err := tool.Execute(ctx, "skill", args, nil); err != nil {
		t.Fatal(err)
	}
	data, err := api.Store.Definition(t.Context(), "assistant")
	var d agentruntime.Definition
	if err != nil || json.Unmarshal(data, &d) != nil || len(d.MCP) != 1 || len(d.Tools) != 6 || len(d.Skills) != 1 {
		t.Fatal("setup was not persisted", err)
	}
	profile, err := api.Store.Profile(t.Context(), "assistant")
	if err != nil || !profile.Agent.Active {
		t.Fatal("integration update erased live presence", err)
	}
	api.actors["assistant"].mu.Lock()
	api.actors["assistant"].definition.MCP[0].Env = map[string]string{"MOCK_CONFIG": "keep"}
	api.actors["assistant"].mu.Unlock()
	if _, err := tool.Execute(ctx, "update", json.RawMessage(`{"action":"mcp","name":"notes","command":"notes-mcp","args":["serve","--quiet"]}`), nil); err != nil {
		t.Fatal(err)
	}
	data, err = api.Store.Definition(t.Context(), "assistant")
	if err != nil || json.Unmarshal(data, &d) != nil || d.MCP[0].Env["MOCK_CONFIG"] != "keep" {
		t.Fatal("same-command edit removed configured environment", err)
	}
	for _, args := range []string{
		`{"action":"mcp","name":"notes","url":"https://token@example.com/mcp"}`,
		`{"action":"mcp","name":"notes","url":"https://example.com/mcp?token=secret"}`,
		`{"action":"skill","path":"relative.md"}`,
		`{"action":"mcp","name":"notes","command":"a","url":"https://example.com/mcp"}`,
	} {
		if _, err := tool.Execute(ctx, "bad", json.RawMessage(args), nil); err == nil {
			t.Fatal("unsafe configuration accepted", args)
		}
	}
	state.work.Trigger = "peer"
	if _, err := tool.Execute(ctx, "setup", json.RawMessage(`{"action":"list"}`), nil); err == nil {
		t.Fatal("peer read private integration setup")
	}
	for _, trigger := range []string{"review", "routine", "follow_up", "task", "event"} {
		state.work.Trigger = trigger
		if _, err := tool.Execute(ctx, "setup", json.RawMessage(`{"action":"enable_cli"}`), nil); err == nil {
			t.Fatal("unattended turn changed its own tool surface", trigger)
		}
	}
}

func TestConfiguredAutomaticTools(t *testing.T) {
	for _, trigger := range []string{"review", "routine", "follow_up"} {
		state := &turnState{work: store.Work{Trigger: trigger}, external: map[string]bool{"bash": true}, prefixes: []string{"notes__"}}
		ctx := context.WithValue(t.Context(), turnKey{}, state)
		for _, name := range []string{"bash", "notes__search", "recall_history", "save_memory"} {
			if result := beforeTool(ctx, "call", name, nil); result.Block {
				t.Fatal("configured initiative denied", trigger, name)
			}
		}
		for _, name := range []string{"other__send", "message_agent", "manage_routines", "manage_integrations", "manage_triggers"} {
			if result := beforeTool(ctx, "call", name, nil); !result.Block {
				t.Fatal("unconfigured or recursive work allowed", trigger, name)
			}
		}
	}
}

type degradedFactory struct {
	opens  atomic.Int32
	closed atomic.Int32
	path   string
}

func (f *degradedFactory) Open(_ context.Context, _ agentruntime.Definition, path string) (agentruntime.Session, error) {
	f.path = path
	return &degradedSession{Session: &testSession{factory: &testFactory{}}, factory: f, reusable: f.opens.Add(1) > 1}, nil
}

type degradedSession struct {
	agentruntime.Session
	factory  *degradedFactory
	reusable bool
}

func (s *degradedSession) Reusable() bool { return s.reusable }
func (s *degradedSession) Close() error {
	s.factory.closed.Add(1)
	return s.Session.Close()
}

func TestDegradedSessionReopensWithItsTranscript(t *testing.T) {
	factory := &degradedFactory{}
	api, _ := startTest(t, factory)
	for _, id := range []string{"first", "second", "third"} {
		text := "User task"
		m := api.execute(t.Context(), api.actors["assistant"], store.Work{Trigger: "message", Message: contract.Message{
			ID: id, ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}},
		}})
		if m.Status != "done" {
			t.Fatal("turn failed", m.Status)
		}
	}
	if factory.opens.Load() != 2 || factory.closed.Load() != 1 || factory.path != "test.jsonl" {
		t.Fatal("degraded MCP session did not refresh and preserve transcript", factory.opens.Load(), factory.closed.Load(), factory.path)
	}
}
