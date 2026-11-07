package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestReact(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	text, bot := "Hello", "a"
	m := contract.Message{ID: "hello", ThreadID: "a", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}, CreatedAt: Now()}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		agent  *string
		remove bool
		count  int
		events int
	}{
		{"user adds", nil, false, 1, 2},
		{"retry adds once", nil, false, 1, 2},
		{"bot adds independently", &bot, false, 2, 3},
		{"user removes only theirs", nil, true, 1, 4},
		{"retry removes once", nil, true, 1, 4},
		{"bot removes theirs", &bot, true, 0, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := s.React(ctx, "a", m.ID, contract.ReactionRequest{Emoji: "👍", Remove: test.remove}, test.agent)
			if err != nil || len(got.Reactions) != test.count || got.Seq != m.Seq || Text(got) != text {
				t.Fatalf("reaction changed content/order or wrong count: %+v %v", got, err)
			}
			events, err := s.Events(ctx, "a", 0, 100)
			if err != nil || len(events) != test.events {
				t.Fatalf("retry created another event: %d %v", len(events), err)
			}
			last := events[len(events)-1]
			if last.Type != "message.reactions" || last.Message == nil || !reflect.DeepEqual(last.Message.Reactions, got.Reactions) {
				t.Fatalf("reactions missing from replay: %+v", last)
			}
			if test.name == "user removes only theirs" && (got.Reactions[0].AgentID == nil || *got.Reactions[0].AgentID != bot) {
				t.Fatal("user removed the bot's reaction")
			}
		})
	}
}

func TestReactValidation(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	text := "Hello"
	for _, status := range []string{"done", "streaming", "silent"} {
		m := contract.Message{ID: status, ThreadID: "a", Role: "assistant", Status: status, Parts: []contract.Part{{Kind: "text", Text: &text}}, CreatedAt: Now()}
		if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.start"}, &m); err != nil {
			t.Fatal(err)
		}
	}
	other, blank := "b", ""
	for _, test := range []struct {
		name, thread, id, emoji string
		agent                   *string
	}{
		{"missing", "a", "absent", "👍", nil},
		{"other chat", "b", "done", "👍", nil},
		{"nonmember", "a", "done", "👍", &other},
		{"empty actor", "a", "done", "👍", &blank},
		{"streaming", "a", "streaming", "👍", nil},
		{"silent", "a", "silent", "👍", nil},
		{"empty emoji", "a", "done", "", nil},
		{"invalid emoji", "a", "done", "hello", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := s.React(ctx, test.thread, test.id, contract.ReactionRequest{Emoji: test.emoji}, test.agent)
			if !errors.Is(err, ErrReaction) {
				t.Fatalf("invalid reaction accepted: %v", err)
			}
		})
	}
	events, err := s.Events(ctx, "a", 0, 100)
	if err != nil || len(events) != 3 {
		t.Fatal("invalid reactions changed history", events, err)
	}
}

func TestReactGroup(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	group, err := s.CreateThread(ctx, "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: "group-message", ThreadID: group.ID, Role: "user", Status: "done", Parts: []contract.Part{}, CreatedAt: Now()}
	if _, err := s.Append(ctx, contract.Event{ConversationID: group.ID, Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	for _, bot := range []string{"a", "b"} {
		if _, err := s.React(ctx, group.ID, m.ID, contract.ReactionRequest{Emoji: "🎉"}, &bot); err != nil {
			t.Fatal(err)
		}
	}
	a := "a"
	got, err := s.React(ctx, group.ID, m.ID, contract.ReactionRequest{Emoji: "🎉", Remove: true}, &a)
	if err != nil || len(got.Reactions) != 1 || got.Reactions[0].AgentID == nil || *got.Reactions[0].AgentID != "b" {
		t.Fatal("group bots shared a reacting identity", got, err)
	}
}

func TestReactRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "reactions.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(ctx, []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: "hello", ThreadID: "a", Role: "user", Status: "done", Parts: []contract.Part{}, CreatedAt: Now()}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	got, err := s.React(ctx, "a", m.ID, contract.ReactionRequest{Emoji: "👩🏽‍💻"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.Message(ctx, m.ID)
	if err != nil || !reflect.DeepEqual(restored, got) {
		t.Fatal("reaction did not survive restart", restored, err)
	}
}
