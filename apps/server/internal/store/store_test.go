package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "First"}, {ID: "b", Name: "First"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenEscapes(t *testing.T) {
	for _, name := range []string{"data#part?tail", "data?part#tail"} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			path := filepath.Join(root, name, "polymux.db")
			s, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.DB.ExecContext(ctx, `INSERT INTO agents(id,data) VALUES(?,?)`, "persisted", `{}`); err != nil {
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
			var data string
			if err := s.DB.QueryRowContext(ctx, `SELECT data FROM agents WHERE id=?`, "persisted").Scan(&data); err != nil {
				t.Fatal(err)
			}
			if data != `{}` {
				t.Fatalf("row data = %q, want {}", data)
			}
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("expected database file at %s: %v", path, err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != name || !entries[0].IsDir() {
				t.Fatalf("unexpected sibling paths: %v", entries)
			}
		})
	}
}

func TestDurableReplay(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "polymux.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(ctx, []contract.Agent{{ID: "assistant", Name: "Assistant"}}); err != nil {
		t.Fatal(err)
	}
	request := contract.SendRequest{ID: "u", Text: "remember the orange balloon", AttachmentIDs: []string{}}
	parts := []contract.Part{{Kind: "text", Text: &request.Text}}
	m, created, err := s.Send(ctx, "assistant", request, parts)
	if err != nil || !created || m.Seq != 1 {
		t.Fatalf("send: %+v %v %v", m, created, err)
	}
	if _, created, err := s.Send(ctx, "assistant", request, parts); err != nil || created {
		t.Fatalf("duplicate: %v %v", created, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events, err := s.Events(ctx, "assistant", 1, 10)
	if err != nil || len(events) != 1 || events[0].Seq != 2 || events[0].Message.ID != "u" {
		t.Fatalf("replay: %+v %v", events, err)
	}
	if id, err := s.Job(ctx, "assistant"); err != nil || id != "u" {
		t.Fatalf("durable queued job: %q %v", id, err)
	}
}

func TestStreamingKeepsOrder(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	m := contract.Message{ID: "reply", ThreadID: "a", Role: "assistant", Parts: []contract.Part{}, CreatedAt: Now(), Status: "streaming"}
	first, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.start"}, &m)
	if err != nil {
		t.Fatal(err)
	}
	u := contract.Message{ID: "next", ThreadID: "a", Role: "user", Parts: []contract.Part{}, CreatedAt: Now(), Status: "done"}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.start"}, &u); err != nil {
		t.Fatal(err)
	}
	text := "finished"
	m.Parts = append(m.Parts, contract.Part{Kind: "text", Text: &text})
	m.Status = "done"
	last, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done"}, &m)
	if err != nil {
		t.Fatal(err)
	}
	if m.Seq != first.Seq || last.Seq <= u.Seq {
		t.Fatalf("message order/cursor m=%d first=%d user=%d cursor=%d", m.Seq, first.Seq, u.Seq, last.Seq)
	}
	all, err := s.Messages(ctx, "a", 0, 10)
	if err != nil || len(all) != 2 || all[0].ID != "reply" || all[1].ID != "next" {
		t.Fatalf("stable ordering %+v %v", all, err)
	}
}

func TestReadAndIdentity(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	m := contract.Message{ID: "m", ThreadID: "a", Role: "assistant", Parts: []contract.Part{}, CreatedAt: Now(), Status: "done"}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.done", MessageID: &m.ID}, &m); err != nil {
		t.Fatal(err)
	}
	threads, err := s.Threads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if threads[0].UnreadCount != 1 || threads[1].UnreadCount != 0 {
		t.Fatalf("scoped unread %+v", threads)
	}
	if err := s.Read(ctx, "a", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.Read(ctx, "a", 0); err != nil {
		t.Fatal(err)
	}
	td, err := s.Thread(ctx, "a")
	if err != nil || td.LastReadSeq != 1 {
		t.Fatalf("monotonic clamped read %+v %v", td, err)
	}
	agents, err := s.Agents(ctx)
	if err != nil || len(agents) != 2 || agents[0].ID == agents[1].ID {
		t.Fatalf("duplicate display labels %+v %v", agents, err)
	}
}

func TestConflictingSend(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	request := contract.SendRequest{ID: "u", Text: "hello", AttachmentIDs: []string{}}
	if _, _, err := s.Send(ctx, "a", request, []contract.Part{}); err != nil {
		t.Fatal(err)
	}
	request.Text = "changed"
	if _, _, err := s.Send(ctx, "a", request, []contract.Part{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	request.Text = "hello"
	if _, _, err := s.Send(ctx, "b", request, []contract.Part{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected thread conflict: %v", err)
	}
}

func TestInterruptedReply(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := contract.Message{ID: "r", ThreadID: "a", Role: "assistant", Parts: []contract.Part{}, CreatedAt: Now(), Status: "streaming"}
	if _, err := s.Append(ctx, contract.Event{ConversationID: "a", Type: "message.start", MessageID: &m.ID}, &m); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := s.Message(ctx, "r")
	if err != nil || m.Status != "error" {
		t.Fatalf("recover %+v %v", m, err)
	}
	events, err := s.Events(ctx, "a", 1, 10)
	if err != nil || len(events) != 1 || events[0].Error.Code != "interrupted" {
		t.Fatalf("recover event %+v %v", events, err)
	}
}

func TestDefinitionCreationIsAtomic(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if _, err := s.DB.ExecContext(ctx, `CREATE TRIGGER reject_definition BEFORE INSERT ON agent_definitions WHEN NEW.agent_id='broken' BEGIN SELECT RAISE(ABORT,'reject'); END`); err != nil {
		t.Fatal(err)
	}
	definition := []byte(`{"prompt":"instructions","tools":[]}`)
	if err := s.SaveDefinition(ctx, contract.Agent{ID: "broken", Name: "Broken"}, definition); err == nil {
		t.Fatal("expected definition write to fail")
	}
	if _, err := s.Thread(ctx, "broken"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("partial conversation persisted: %v", err)
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE id='broken'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial agent persisted count=%d err=%v", count, err)
	}
	if err := s.SaveDefinition(ctx, contract.Agent{ID: "custom", Name: "Custom"}, definition); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Definitions(ctx)
	if err != nil || len(stored) != 1 || string(stored[0]) != string(definition) {
		t.Fatalf("definition roundtrip %+v %v", stored, err)
	}
}

func TestAvatarUpdateIsAtomicAndKeepsPresence(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	definition := []byte(`{"agent":{"id":"a","name":"First","role":"Guide","model":"provider/default","active":false},"prompt":"Keep instructions","tools":["read"]}`)
	if err := s.SaveDefinition(ctx, contract.Agent{ID: "a", Name: "First", Role: "Guide", Model: "provider/default"}, definition); err != nil {
		t.Fatal(err)
	}
	if err := s.Presence(ctx, "a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE TRIGGER reject_avatar BEFORE UPDATE ON agent_definitions WHEN NEW.agent_id='a' BEGIN SELECT RAISE(ABORT,'reject'); END`); err != nil {
		t.Fatal(err)
	}
	avatar := contract.AvatarDesign{Shape: "cercle", Color: "encre", Expression: "neutre", Blocks: []contract.AnimationBlock{}}
	if _, err := s.UpdateAvatar(ctx, "a", avatar); err == nil {
		t.Fatal("expected avatar definition write to fail")
	}
	profile, err := s.Profile(ctx, "a")
	if err != nil || profile.Agent.Avatar != nil || !profile.Agent.Active || profile.Prompt != "Keep instructions" {
		t.Fatalf("partial avatar change %+v %v", profile, err)
	}
	if _, err := s.DB.ExecContext(ctx, `DROP TRIGGER reject_avatar`); err != nil {
		t.Fatal(err)
	}
	agent, err := s.UpdateAvatar(ctx, "a", avatar)
	if err != nil || agent.Avatar == nil || !agent.Active || agent.Model != "provider/default" || agent.Role != "Guide" {
		t.Fatalf("avatar update changed profile/presence %+v %v", agent, err)
	}
	profile, err = s.Profile(ctx, "a")
	if err != nil || profile.Prompt != "Keep instructions" || profile.Agent.Avatar == nil {
		t.Fatalf("avatar update lost prompt %+v %v", profile, err)
	}
}
