package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestPendingHandoff(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Register(t.Context(), []contract.Agent{{ID: "a", Name: "Same"}, {ID: "b", Name: "Same"}, {ID: "c", Name: "C"}}); err != nil {
		t.Fatal(err)
	}
	group, err := db.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "c", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	chain := &chainState{seen: map[string]bool{"a": true, "c": true}, pending: map[string]string{}}
	tool := &peerTool{server: &Server{Store: db}, handoff: true}
	for index, source := range []string{"a", "c"} {
		run := store.NewRun("user", group.ID, source, "message")
		run.Status = "running"
		if err := db.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
			t.Fatal(err)
		}
		work := store.Work{Trigger: "message", Message: contract.Message{ID: "user", ThreadID: group.ID}}
		ctx := context.WithValue(context.WithValue(t.Context(), chainKey{}, chain), turnKey{}, &turnState{run: run, work: work})
		_, err := tool.Execute(ctx, "call", json.RawMessage(`{"agent_id":"b","message":"One bounded task"}`), nil)
		if index == 0 && err != nil {
			t.Fatal(err)
		}
		if index == 1 && err == nil {
			t.Fatal("second source overwrote an accepted handoff")
		}
	}
	var count int
	if err := db.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM deliveries`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate handoff %d %v", count, err)
	}
}

func TestToolBudget(t *testing.T) {
	state := &turnState{work: store.Work{Trigger: "message"}}
	chain := &chainState{seen: map[string]bool{}, pending: map[string]string{}}
	ctx := context.WithValue(context.WithValue(t.Context(), chainKey{}, chain), turnKey{}, state)
	for range 32 {
		if beforeTool(ctx, "call", "read", nil).Block {
			t.Fatal("budget blocked early")
		}
	}
	if result := beforeTool(ctx, "overflow", "read", nil); !result.Block || !result.Terminate {
		t.Fatal("tool budget did not end the stage")
	}
	state.tools = 0
	chain.tools = 64
	if result := beforeTool(ctx, "overflow", "read", nil); !result.Block || !result.Terminate {
		t.Fatal("group budget did not end the chain")
	}
}

func TestChildDelegation(t *testing.T) {
	for _, name := range []string{"spawn_task", "message_agent", "handoff_agent", "manage_tasks"} {
		t.Run(name, func(t *testing.T) {
			state := &turnState{work: store.Work{Trigger: "child"}}
			ctx := context.WithValue(t.Context(), turnKey{}, state)
			if result := beforeTool(ctx, "call", name, nil); !result.Block {
				t.Fatal("child delegation was allowed")
			}
		})
	}
}
