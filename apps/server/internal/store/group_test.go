package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestGroupMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}); err != nil {
		t.Fatal(err)
	}
	request := contract.SendRequest{ID: "queued", Text: "Keep this message", AttachmentIDs: []string{}}
	m, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Read(t.Context(), "a", m.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSession(t.Context(), "a", "individual.jsonl"); err != nil {
		t.Fatal(err)
	}
	before, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF;
DROP TABLE deliveries;
DROP TABLE runs;
DROP TABLE group_sessions;
DROP TABLE thread_members;
CREATE TABLE old_threads(id TEXT PRIMARY KEY,agent_id TEXT UNIQUE NOT NULL REFERENCES agents(id),last_read_seq INTEGER NOT NULL DEFAULT 0,last_seq INTEGER NOT NULL DEFAULT 0);
INSERT INTO old_threads(id,agent_id,last_read_seq,last_seq) SELECT id,agent_id,last_read_seq,last_seq FROM threads;
DROP TABLE threads;
ALTER TABLE old_threads RENAME TO threads;
PRAGMA user_version=2;`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	thread, err := s.Thread(t.Context(), "a")
	if err != nil || thread.Title != "A" || !reflect.DeepEqual(thread.AgentIDs, []string{"a"}) || thread.LastReadSeq != m.Seq {
		t.Fatalf("migrated thread %+v %v", thread, err)
	}
	after, err := s.Events(t.Context(), "a", 0, 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("migration changed replay %+v %v", after, err)
	}
	if id, err := s.Job(t.Context(), "a"); err != nil || id != "queued" {
		t.Fatalf("migration lost queued work %q %v", id, err)
	}
	if path, err := s.Session(t.Context(), "a"); err != nil || path != "individual.jsonl" {
		t.Fatal("migration lost individual session")
	}
	if _, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}}); err != nil {
		t.Fatalf("migration retained one-thread-per-agent constraint: %v", err)
	}
	rows, err := s.DB.QueryContext(t.Context(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration left broken foreign keys")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestGroupValidation(t *testing.T) {
	s := openTest(t)
	for _, members := range [][]string{{"a"}, {"a", "a"}, {"a", "missing"}, {}} {
		if _, err := s.CreateThread(t.Context(), "bad", contract.CreateThreadRequest{AgentIDs: members}); !errors.Is(err, ErrMembers) {
			t.Fatalf("invalid members %v: %v", members, err)
		}
	}
	threads, err := s.Threads(t.Context())
	if err != nil || len(threads) != 2 {
		t.Fatalf("failed creation leaked metadata: %+v %v", threads, err)
	}
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
	if err != nil || group.Title != "First, First" {
		t.Fatalf("default group title %+v %v", group, err)
	}
	request := contract.SendRequest{ID: "shared", Text: "Group text", Mentions: []string{"b"}}
	m, _, err := s.Send(t.Context(), group.ID, request, []contract.Part{{Kind: "text", Text: &request.Text}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Mentions, request.Mentions) {
		t.Fatal("explicit mention identity was lost")
	}
	if _, _, err := s.Send(t.Context(), group.ID, contract.SendRequest{ID: request.ID, Text: request.Text, Mentions: []string{"a"}}, m.Parts); !errors.Is(err, ErrConflict) {
		t.Fatalf("mention change reused an existing send identity: %v", err)
	}
	if _, _, err := s.DeleteAgent(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.Thread(t.Context(), group.ID)
	if err != nil || remaining.AgentID != "b" || !reflect.DeepEqual(remaining.AgentIDs, []string{"b"}) {
		t.Fatalf("remaining group was deleted %+v %v", remaining, err)
	}
	if loaded, err := s.Message(t.Context(), m.ID); err != nil || loaded.ThreadID != group.ID {
		t.Fatalf("remaining group history was deleted %+v %v", loaded, err)
	}
	if _, _, err := s.DeleteAgent(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Thread(t.Context(), group.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty group remained %v", err)
	}
}

func TestGroupUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	agents := []contract.Agent{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}}
	if err := s.Register(t.Context(), agents); err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, Title: stringPtr("Old name")})
	if err != nil {
		t.Fatal(err)
	}
	request := contract.SendRequest{ID: "queued", Text: "Keep our history", Mentions: []string{"b"}}
	user, _, err := s.Send(t.Context(), group.ID, request, []contract.Part{{Kind: "text", Text: &request.Text}})
	if err != nil {
		t.Fatal(err)
	}
	reply := contract.Message{ID: "reply", ThreadID: group.ID, Role: "assistant", Status: "done", Parts: []contract.Part{}, AgentID: stringPtr("a"), ReplyToID: &user.ID}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: group.ID, Type: "message.done", AgentID: reply.AgentID}, &reply); err != nil {
		t.Fatal(err)
	}
	if err := s.Read(t.Context(), group.ID, reply.Seq); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Thread(t.Context(), group.ID)
	events, _ := s.Events(t.Context(), group.ID, 0, 100)
	if err := s.SaveGroupSession(t.Context(), group.ID, "a", "a-session.jsonl"); err != nil {
		t.Fatal(err)
	}
	for _, members := range [][]string{{"c", "b"}, {"c"}} {
		if err := s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Title: new("New name"), AgentIDs: members}); err != nil {
			t.Fatal(err)
		}
		loaded, err := s.Thread(t.Context(), group.ID)
		if err != nil || loaded.ID != group.ID || loaded.AgentID != members[0] || loaded.Title != "New name" || !reflect.DeepEqual(loaded.AgentIDs, members) || loaded.LastSeq != before.LastSeq || loaded.LastReadSeq != before.LastReadSeq {
			t.Fatalf("updated group lost identity/history/read state %+v %v", loaded, err)
		}
	}
	if loaded, err := s.Message(t.Context(), reply.ID); err != nil || !reflect.DeepEqual(loaded, reply) {
		t.Fatalf("removed author's message was changed %+v %v", loaded, err)
	}
	if after, err := s.Events(t.Context(), group.ID, 0, 100); err != nil || !reflect.DeepEqual(events, after) {
		t.Fatalf("membership changed transcript replay %+v %v", after, err)
	}
	if queued, err := s.Request(t.Context(), request.ID); err != nil || !reflect.DeepEqual(queued, request) {
		t.Fatalf("membership changed queued work %+v %v", queued, err)
	}
	if loaded, err := s.Agents(t.Context()); err != nil || len(loaded) != 3 {
		t.Fatalf("removing group members deleted global agents %+v %v", loaded, err)
	}
	for _, agent := range agents {
		if direct, err := s.Thread(t.Context(), agent.ID); err != nil || direct.AgentID != agent.ID || direct.LastSeq != 0 || !reflect.DeepEqual(direct.AgentIDs, []string{agent.ID}) {
			t.Fatalf("direct chat changed %+v %v", direct, err)
		}
	}
	if err := s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Title: new("Renamed only")}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(t.Context(), group.ID, contract.UpdateThreadRequest{Title: new("Rejoined"), AgentIDs: []string{"b", "c", "a"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Thread(t.Context(), group.ID)
	if err != nil || loaded.Title != "Rejoined" || loaded.AgentID != "b" || !reflect.DeepEqual(loaded.AgentIDs, []string{"b", "c", "a"}) || loaded.LastSeq != before.LastSeq || loaded.LastReadSeq != before.LastReadSeq {
		t.Fatalf("membership order/read state lost on reopen %+v %v", loaded, err)
	}
}

func TestGroupUpdateValidation(t *testing.T) {
	s := openTest(t)
	ids := make([]string, 17)
	agents := make([]contract.Agent, 17)
	for index := range ids {
		ids[index] = fmt.Sprintf("member-%d", index)
		agents[index] = contract.Agent{ID: ids[index], Name: ids[index]}
	}
	if err := s.Register(t.Context(), agents); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, Title: stringPtr("Keep")}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, id string
		members  []string
		want     error
	}{
		{"empty", "group", []string{}, ErrMembers},
		{"duplicate", "group", []string{"a", "a"}, ErrMembers},
		{"unknown", "group", []string{"missing"}, ErrMembers},
		{"too-many", "group", ids, ErrMembers},
		{"direct", "a", []string{"b"}, ErrMembers},
		{"missing-chat", "missing", []string{"a"}, sql.ErrNoRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := s.UpdateThread(t.Context(), test.id, contract.UpdateThreadRequest{Title: new("Changed"), AgentIDs: test.members}); !errors.Is(err, test.want) {
				t.Fatalf("update error %v want %v", err, test.want)
			}
			loaded, err := s.Thread(t.Context(), "group")
			if err != nil || loaded.Title != "Keep" || !reflect.DeepEqual(loaded.AgentIDs, []string{"a", "b"}) {
				t.Fatalf("failed update modified metadata %+v %v", loaded, err)
			}
		})
	}
	if _, err := s.DB.ExecContext(t.Context(), `CREATE TEMP TRIGGER fail_update BEFORE UPDATE ON threads WHEN NEW.title='Fail' BEGIN SELECT RAISE(ABORT,'probe'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(t.Context(), "group", contract.UpdateThreadRequest{Title: new("Fail"), AgentIDs: ids[:16]}); err == nil {
		t.Fatal("expected transactional write failure")
	}
	loaded, err := s.Thread(t.Context(), "group")
	if err != nil || loaded.Title != "Keep" || !reflect.DeepEqual(loaded.AgentIDs, []string{"a", "b"}) {
		t.Fatalf("failed transaction did not restore members %+v %v", loaded, err)
	}
	if err := s.UpdateThread(t.Context(), "group", contract.UpdateThreadRequest{Title: new("Sixteen"), AgentIDs: slices.Clone(ids[:16])}); err != nil {
		t.Fatalf("maximum valid roster rejected: %v", err)
	}
}

func stringPtr(value string) *string { return &value }
