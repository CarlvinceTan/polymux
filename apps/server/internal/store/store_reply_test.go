package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestReplySnapshotSurvivesRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "reply.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(ctx, []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	text := "We chose Friday."
	attachment := contract.Attachment{ID: "image", Name: "Friday.png", Kind: "image"}
	original := contract.Message{ID: "original", ThreadID: "a", Role: "assistant", Status: "done", CreatedAt: Now(),
		Parts: []contract.Part{{Kind: "text", Text: &text}, {Kind: "attachment", Attachment: &attachment}}}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &original); err != nil {
		t.Fatal(err)
	}
	request := contract.SendRequest{ID: "reply", Text: "What time?", AttachmentIDs: []string{}, ReplyToID: &original.ID}
	m, created, err := s.Send(ctx, "a", request, []contract.Part{{Kind: "text", Text: &request.Text}})
	if err != nil || !created || m.Reply == nil || m.Reply.Text != text || m.Reply.Attachment == nil || m.Reply.Attachment.ID != attachment.ID {
		t.Fatalf("reply snapshot: %+v %v", m.Reply, err)
	}
	changed := "Changed after the reply."
	original.Parts[0].Text = &changed
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &original); err != nil {
		t.Fatal(err)
	}
	duplicate, created, err := s.Send(ctx, "a", request, nil)
	if err != nil || created || duplicate.Reply == nil || duplicate.Reply.Text != text {
		t.Fatalf("idempotent snapshot: %+v %v", duplicate.Reply, err)
	}
	request.ReplyToID = nil
	if _, _, err := s.Send(ctx, "a", request, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed target must conflict: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err = s.Message(ctx, "reply")
	if err != nil || m.Reply == nil || m.Reply.Text != text || m.Reply.MessageID != original.ID {
		t.Fatalf("reopened reply: %+v %v", m.Reply, err)
	}
	events, err := s.Events(ctx, "a", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Message != nil && event.Message.ID == "reply" {
			found = true
			if event.Message.Reply == nil || event.Message.Reply.Text != text {
				t.Fatalf("replayed quote: %+v", event.Message)
			}
		}
	}
	if !found {
		t.Fatal("reply missing from replay")
	}
}

func TestReplyRejectsUnavailableTarget(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	text := "Private to B."
	original := contract.Message{ID: "b-only", ThreadID: "b", Role: "assistant", Status: "done", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "b", Type: "message.done"}, &original); err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ name, target string }{{"missing", "absent"}, {"empty", ""}, {"other conversation", original.ID}} {
		t.Run(value.name, func(t *testing.T) {
			request := contract.SendRequest{ID: "invalid", Text: "Reply", ReplyToID: &value.target}
			if _, _, err := s.Send(ctx, "a", request, nil); !errors.Is(err, ErrReply) {
				t.Fatalf("target accepted: %v", err)
			}
			messages, err := s.Messages(ctx, "a", 0, 100)
			if err != nil || len(messages) != 0 {
				t.Fatalf("invalid reply changed history: %+v %v", messages, err)
			}
		})
	}
}

func TestReplyParts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parts.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}); err != nil {
		t.Fatal(err)
	}
	first, second, tool := "First text", "Second text", "Tool data"
	images := []contract.Attachment{{ID: "one", Kind: "image", Name: "Same.png"}, {ID: "two", Kind: "image", Name: "Same.png"}}
	original := contract.Message{ID: "original", ThreadID: "a", Role: "assistant", Status: "done", CreatedAt: Now(), AgentID: stringPtr("b"),
		Parts: []contract.Part{{Kind: "text", Text: &first}, {Kind: "attachment", Attachment: &images[0]},
			{Kind: "tool", Text: &tool}, {Kind: "text", Text: &second}, {Kind: "attachment", Attachment: &images[1]}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &original); err != nil {
		t.Fatal(err)
	}
	var replies []contract.Message
	for _, test := range []struct {
		name string
		part int64
		text string
		file *contract.Attachment
	}{
		{"first-text", 0, first, nil},
		{"second-text", 3, second, nil},
		{"first-image", 1, "", &images[0]},
		{"second-image", 4, "", &images[1]},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := contract.SendRequest{ID: test.name, Text: "Reply", ReplyToID: &original.ID, ReplyPart: &test.part}
			message, created, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}})
			if err != nil || !created || message.Reply == nil || message.Reply.PartIndex == nil || *message.Reply.PartIndex != test.part || message.Reply.Text != test.text || !reflect.DeepEqual(message.Reply.Attachment, test.file) || message.Reply.AgentID == nil || *message.Reply.AgentID != "b" {
				t.Fatalf("selected quote %+v %v", message.Reply, err)
			}
			replies = append(replies, message)
			duplicate, created, err := s.Send(t.Context(), "a", request, nil)
			if err != nil || created || !reflect.DeepEqual(duplicate.Reply, message.Reply) {
				t.Fatalf("selector idempotency %+v %v", duplicate, err)
			}
			changed := int64(0)
			if test.part == 0 {
				changed = 4
			}
			request.ReplyPart = &changed
			if _, _, err := s.Send(t.Context(), "a", request, nil); !errors.Is(err, ErrConflict) {
				t.Fatalf("same-id selector change accepted: %v", err)
			}
		})
	}
	changed := "Changed source text"
	original.Parts[0].Text = &changed
	original.Parts[4].Attachment = &contract.Attachment{ID: "changed", Kind: "image", Name: "Changed.png"}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &original); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range replies {
		loaded, err := s.Message(t.Context(), message.ID)
		if err != nil || !reflect.DeepEqual(loaded.Reply, message.Reply) {
			t.Fatalf("selector snapshot changed after source edit/reopen %+v %v", loaded, err)
		}
		request, err := s.Request(t.Context(), message.ID)
		if err != nil || request.ReplyPart == nil || *request.ReplyPart != *message.Reply.PartIndex {
			t.Fatalf("durable request lost selector %+v %v", request, err)
		}
	}
	events, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, event := range events {
		for _, message := range replies {
			if event.Message != nil && event.Message.ID == message.ID {
				counts[message.ID]++
				if !reflect.DeepEqual(event.Message.Reply, message.Reply) {
					t.Fatalf("event replay changed selected quote %+v", event.Message.Reply)
				}
			}
		}
	}
	for _, message := range replies {
		if counts[message.ID] != 2 {
			t.Fatalf("duplicate or missing reply events %s: %d", message.ID, counts[message.ID])
		}
	}
}

func TestReplyPartValidation(t *testing.T) {
	s := openTest(t)
	empty := ""
	target := contract.Message{ID: "target", ThreadID: "a", Role: "assistant", Status: "done", Parts: []contract.Part{
		{Kind: "text", Text: stringPtr("Allowed")}, {Kind: "tool", Text: stringPtr("Not quotable")},
		{Kind: "text", Text: &empty}, {Kind: "text"}, {Kind: "attachment"}}}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &target); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		part int64
	}{
		{"negative", -1}, {"beyond-end", 5}, {"overflow", 1<<63 - 1},
		{"tool", 1}, {"empty-text", 2}, {"missing-text", 3}, {"missing-attachment", 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := contract.SendRequest{ID: fmt.Sprintf("invalid-%s", test.name), Text: "Reply", ReplyToID: &target.ID, ReplyPart: &test.part}
			if _, _, err := s.Send(t.Context(), "a", request, nil); !errors.Is(err, ErrReply) {
				t.Fatalf("invalid part accepted: %v", err)
			}
		})
	}
	part := int64(0)
	if _, _, err := s.Send(t.Context(), "a", contract.SendRequest{ID: "no-message", Text: "Reply", ReplyPart: &part}, nil); !errors.Is(err, ErrReply) {
		t.Fatalf("selector without message accepted: %v", err)
	}
	if messages, err := s.Messages(t.Context(), "a", 0, 100); err != nil || len(messages) != 1 {
		t.Fatalf("invalid reply altered history %+v %v", messages, err)
	}
	var jobs int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("invalid reply enqueued work %d %v", jobs, err)
	}
}
