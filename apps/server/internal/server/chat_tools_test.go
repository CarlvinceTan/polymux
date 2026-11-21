package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestSendToUserSplitsBeats(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state := &turnState{run: contract.Run{ID: "run", AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: contract.RunTriggerMessage}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &chatTool{store: api.Store}
	if _, err := tool.Execute(ctx, "call", json.RawMessage(`{"text":"First beat.\n\nSecond beat."}`), nil); err != nil {
		t.Fatal(err)
	}
	messages, err := api.Store.Messages(t.Context(), "assistant", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || store.Text(messages[0]) != "First beat." || store.Text(messages[1]) != "Second beat." {
		t.Fatalf("messages = %+v", messages)
	}
	if state.posts != 2 {
		t.Fatalf("posted %d messages", state.posts)
	}
}
