package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func paceState(run string) (*turnState, context.Context) {
	state := &turnState{run: contract.Run{ID: run, AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: contract.RunTriggerMessage}}
	return state, context.WithValue(context.Background(), turnKey{}, state)
}

func postBeat(t *testing.T, tool *chatTool, ctx context.Context, call, text string) error {
	t.Helper()
	_, err := tool.Execute(ctx, call, json.RawMessage(fmt.Sprintf(`{"text":%q}`, text)), nil)
	return err
}

func TestSendWall(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store}
	state, ctx := paceState("run")
	wall := strings.TrimSpace(strings.Repeat("word ", 50))
	err := postBeat(t, tool, ctx, "c1", wall)
	if err == nil || !strings.Contains(err.Error(), "wall of text") {
		t.Fatalf("wall send: %v", err)
	}
	if messages, _ := api.Store.Messages(t.Context(), "assistant", 0, 10); len(messages) != 0 {
		t.Fatalf("bounced wall posted %d messages", len(messages))
	}
	// The bounce is guidance: a retry in the same burst goes through.
	if err := postBeat(t, tool, ctx, "c2", wall); err != nil {
		t.Fatal(err)
	}
	if state.bubbles != 1 {
		t.Fatalf("bubbles = %d", state.bubbles)
	}
	// Real work resets the burst, so the next wall bounces again.
	beforeTool(ctx, "c3", "read", nil)
	if err := postBeat(t, tool, ctx, "c4", wall); err == nil || !strings.Contains(err.Error(), "wall of text") {
		t.Fatalf("wall after work: %v", err)
	}
}

func TestSendWallCodeExempt(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store}
	_, ctx := paceState("run")
	code := "Here's the fix:\n```go\n" + strings.Repeat("value := compute(input)\n", 20) + "```"
	if err := postBeat(t, tool, ctx, "c1", code); err != nil {
		t.Fatal(err)
	}
}

func TestSendCap(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store}
	state, ctx := paceState("run")
	if err := postBeat(t, tool, ctx, "c1", "One.\n\nTwo.\n\nThree."); err == nil || !strings.Contains(err.Error(), "two bubbles") {
		t.Fatalf("three beats: %v", err)
	}
	if err := postBeat(t, tool, ctx, "c2", "One.\n\nTwo."); err != nil {
		t.Fatal(err)
	}
	if err := postBeat(t, tool, ctx, "c3", "Three."); err == nil || !strings.Contains(err.Error(), "two bubbles") {
		t.Fatalf("third bubble: %v", err)
	}
	// A quiet tool doesn't start a new burst.
	beforeTool(ctx, "c4", "set_status", nil)
	if err := postBeat(t, tool, ctx, "c5", "Three."); err == nil {
		t.Fatal("cap held after a quiet tool")
	}
	beforeTool(ctx, "c6", "read", nil)
	if err := postBeat(t, tool, ctx, "c7", "Three."); err != nil {
		t.Fatal(err)
	}
	if state.bubbles != 1 {
		t.Fatalf("bubbles = %d after reset", state.bubbles)
	}
}

func TestSendParrot(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store}
	for _, run := range []string{"r1", "r2"} {
		_, ctx := paceState(run)
		if err := postBeat(t, tool, ctx, "c1", "Done, pushed."); err != nil {
			t.Fatalf("%s: %v", run, err)
		}
	}
	state, ctx := paceState("r3")
	err := postBeat(t, tool, ctx, "c1", "Done, pushed.")
	if err == nil || !strings.Contains(err.Error(), "opener") {
		t.Fatalf("third identical opener: %v", err)
	}
	if !state.parrotBounced {
		t.Fatal("parrot bounce not recorded")
	}
	if err := postBeat(t, tool, ctx, "c2", "Done, pushed."); err != nil {
		t.Fatal(err)
	}
	if messages, _ := api.Store.Messages(t.Context(), "assistant", 0, 10); len(messages) != 3 {
		t.Fatalf("posted %d messages, want 3", len(messages))
	}
}

func TestSendParrotWindow(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store}
	agent := "assistant"
	for i := 0; i < 12; i++ {
		text := "Alpha beta gamma rest."
		if i%2 == 1 {
			text = "Other opener here rest."
		}
		if _, err := api.Store.PostMessage(t.Context(), contract.Message{ID: fmt.Sprintf("seed%d", i), ThreadID: "assistant", Role: contract.MessageRoleAssistant, Status: contract.MessageStatusDone, CreatedAt: store.Now(), AgentID: &agent, Parts: []contract.Part{{Kind: contract.PartKindText, Text: &text}}}); err != nil {
			t.Fatal(err)
		}
	}
	_, ctx := paceState("run")
	// Six of the last twelve openers match, though the last two differ.
	err := postBeat(t, tool, ctx, "c1", "Alpha beta gamma again.")
	if err == nil || !strings.Contains(err.Error(), "opener") {
		t.Fatalf("repeated opener across the window: %v", err)
	}
}

func TestSendGroupUnpaced(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store}
	state, ctx := paceState("run")
	ctx = context.WithValue(ctx, chainKey{}, &chainState{seen: map[string]bool{}, pending: map[string]string{}})
	for i := 0; i < 5; i++ {
		if err := postBeat(t, tool, ctx, fmt.Sprintf("c%d", i), "A new finding"); err != nil {
			t.Fatalf("group post %d: %v", i, err)
		}
	}
	if state.posts != 5 {
		t.Fatalf("posted %d, want 5", state.posts)
	}
}

func TestAskSkipsPacing(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &chatTool{store: api.Store, question: true}
	state, ctx := paceState("run")
	question := strings.TrimSpace(strings.Repeat("word ", 60))
	args := fmt.Sprintf(`{"question":%q,"options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`, question)
	if _, err := tool.Execute(ctx, "c1", json.RawMessage(args), nil); err != nil {
		t.Fatal(err)
	}
	if state.bubbles != 0 {
		t.Fatalf("question counted as a bubble: %d", state.bubbles)
	}
}
