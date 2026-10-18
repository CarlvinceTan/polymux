package store

import (
	"strconv"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestActivityUnread(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	message := contract.Message{ID: "peer-reply", ThreadID: "a", Role: "assistant", Status: "done", CreatedAt: Now()}
	if _, err := db.Append(ctx, contract.Event{ConversationID: "a", Type: "message.start"}, &message); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"memory.saved", "agent.messaged", "subagent.started", "subagent.finished"} {
		payload, err := NewActivity(kind, "object", "Object")
		if err != nil {
			t.Fatal(err)
		}
		if kind == "agent.messaged" {
			payload.MessageIDs = []string{message.ID}
		}
		if err := db.Activity(ctx, "a", "a", payload); err != nil {
			t.Fatal(err)
		}
	}
	threads, err := db.Threads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if threads[0].UnreadCount != 2 || threads[1].UnreadCount != 0 {
		t.Fatalf("only peer activity and child completion count, without hidden bubbles: %+v", threads)
	}
	if err := db.Read(ctx, "a", threads[0].LastSeq); err != nil {
		t.Fatal(err)
	}
	threads, err = db.Threads(ctx)
	if err != nil || threads[0].UnreadCount != 0 {
		t.Fatalf("reading must clear activity unread: %+v %v", threads, err)
	}
}

func TestChildMessages(t *testing.T) {
	db := openTest(t)
	for i, run := range []string{"child", "parent", "child"} {
		message := contract.Message{ID: run + strconv.Itoa(i), ThreadID: "a", Role: "assistant", Status: "done", CreatedAt: Now(), RunID: &run,
			Parts: []contract.Part{{Kind: "text", Text: &run}}}
		if _, err := db.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := db.ChildMessages(t.Context(), "child")
	if err != nil || len(messages) != 2 {
		t.Fatalf("child transcript: %v %v", messages, err)
	}
	for _, message := range messages {
		if Text(message) != "child" {
			t.Fatal("parent context exposed")
		}
	}
}
