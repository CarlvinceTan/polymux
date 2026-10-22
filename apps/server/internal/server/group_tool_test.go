package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestGroupToolCreatesGroup(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definitions []agentruntime.Definition
	for _, id := range []string{"a", "b", "c"} {
		definitions = append(definitions, agentruntime.Definition{Agent: contract.Agent{ID: id, Name: id}})
	}
	api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: definitions, Factory: &testFactory{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	call := func(trigger, args string) (map[string]any, error) {
		state := &turnState{run: contract.Run{ID: "run", AgentID: "a", ConversationID: "a"}, work: store.Work{Trigger: trigger}, store: db}
		ctx := context.WithValue(t.Context(), turnKey{}, state)
		result, err := (&groupTool{server: api}).Execute(ctx, "call", json.RawMessage(args), nil)
		if err != nil {
			return nil, err
		}
		var value map[string]any
		return value, json.Unmarshal([]byte(result.Text()), &value)
	}

	value, err := call("message", `{"agent_ids":["b","c","b","a"],"title":" Trip "}`)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := db.Thread(t.Context(), value["chat_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(thread.AgentIDs, []string{"a", "b", "c"}) || thread.Title != "Trip" {
		t.Fatal("unexpected group", thread.AgentIDs, thread.Title)
	}
	api.actorsMu.RLock()
	started := api.groups[thread.ID] != nil
	api.actorsMu.RUnlock()
	if !started {
		t.Fatal("group worker did not start")
	}
	state := &turnState{run: contract.Run{AgentID: "a", ConversationID: thread.ID}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	if _, err := (&groupEdit{server: api}).Execute(ctx, "edit", json.RawMessage(`{"title":"Research","agent_ids":["a","b"],"lead_agent_id":"b"}`), nil); err != nil {
		t.Fatal(err)
	}
	updated, err := db.Thread(t.Context(), thread.ID)
	if err != nil || updated.Title != "Research" || !slices.Equal(updated.AgentIDs, []string{"a", "b"}) || (updated.LeadAgentID == nil || *updated.LeadAgentID != "b") {
		t.Fatal("group edits did not persist", updated, err)
	}

	if _, err := call("message", `{"agent_ids":["missing"]}`); err == nil {
		t.Fatal("created a group with an unknown agent")
	}
	if _, err := call("message", `{"agent_ids":["a"]}`); err == nil {
		t.Fatal("created a group with only the caller")
	}
	if _, err := call("routine", `{"agent_ids":["b"]}`); err != nil {
		t.Fatal("scheduled bot could not create a group", err)
	}
}
