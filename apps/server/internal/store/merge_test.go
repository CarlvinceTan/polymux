package store

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mergeStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "polymux.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func exec(t *testing.T, s *Store, statement string, args ...any) {
	t.Helper()
	if _, err := s.DB.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func TestEveryTableHasAMergeRule(t *testing.T) {
	s, _ := mergeStore(t)
	rows, err := s.DB.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'messages_fts_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rules := MergeRules()
	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
		if !slices.Contains(rules, table) {
			t.Errorf("%s has no merge rule: copy it, keep this server's, or say why it's skipped", table)
		}
	}
	// These belong to the vault and secrets packages, so a server creates them, not Open.
	elsewhere := []string{"credentials", "vault_items", "vault_meta"}
	for _, rule := range rules {
		if !slices.Contains(tables, rule) && !slices.Contains(elsewhere, rule) {
			t.Errorf("merge rule for %s, which doesn't exist", rule)
		}
	}
}

func seedMerge(t *testing.T, s *Store, bot, name string) {
	t.Helper()
	exec(t, s, `INSERT INTO agents(id,data) VALUES(?,json_object('id',?,'name',?))`, bot, bot, name)
	exec(t, s, `INSERT INTO agent_definitions(agent_id,data) VALUES(?,json_object('agent',json_object('id',?,'name',?)))`, bot, bot, name)
	exec(t, s, `INSERT INTO threads(id,agent_id,section_id) VALUES(?,?,?)`, bot, bot, UnassignedID)
}

func TestMergeBringsChatsAndPausesSchedules(t *testing.T) {
	here, _ := mergeStore(t)
	there, path := mergeStore(t)
	seedMerge(t, here, "a1", "Coach")
	seedMerge(t, there, "b1", "Coach")
	seedMerge(t, there, "b2", "Scout")
	exec(t, there, `INSERT INTO sections(id,name,position,created_at,updated_at) VALUES('s1','Work',1,'2026-01-01','2026-01-01')`)
	exec(t, here, `INSERT INTO sections(id,name,position,created_at,updated_at) VALUES('s2','Work',1,'2026-01-01','2026-01-01')`)
	exec(t, there, `UPDATE threads SET section_id='s1' WHERE id='b2'`)
	exec(t, there, `INSERT INTO messages(id,thread_id,seq,role,status,data) VALUES('m1','b1',1,'user','done','{}')`)
	exec(t, there, `INSERT INTO messages_fts(message_id,thread_id,body) VALUES('m1','b1','needle')`)
	exec(t, there, `INSERT INTO routines(id,agent_id,next_at,data) VALUES('r1','b1',100,'{"status":"active"}')`)
	exec(t, there, `INSERT INTO triggers(id,agent_id,data) VALUES('t1','b1','{"enabled":true}')`)
	exec(t, there, `INSERT INTO proactivity(agent_id,next_at,data) VALUES('b1',100,'{"enabled":true}')`)
	exec(t, there, `INSERT INTO attachments(id,data,path) VALUES('f1','{}','/old/data/attachments/f1')`)
	exec(t, there, `INSERT INTO notices(thread_id,notice_key,data) VALUES('b1','k','{}')`)
	if _, err := there.DB.ExecContext(t.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	result, err := here.Merge(t.Context(), path, "Studio", "/old/data", "/new/data")
	if err != nil {
		t.Fatal(err)
	}
	if result.Bots != 2 || result.Chats != 2 {
		t.Fatalf("result %+v", result)
	}
	value := func(query string, args ...any) string {
		t.Helper()
		var out string
		if err := here.DB.QueryRowContext(t.Context(), query, args...).Scan(&out); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return out
	}
	if got := value(`SELECT json_extract(data,'$.name') FROM agents WHERE id='b1'`); got != "Coach (Studio)" {
		t.Errorf("clashing bot: %s", got)
	}
	if got := value(`SELECT json_extract(data,'$.agent.name') FROM agent_definitions WHERE agent_id='b1'`); got != "Coach (Studio)" {
		t.Errorf("clashing definition: %s", got)
	}
	if got := value(`SELECT json_extract(data,'$.name') FROM agents WHERE id='a1'`); got != "Coach" {
		t.Errorf("this server's bot changed: %s", got)
	}
	if got := value(`SELECT json_extract(data,'$.name') FROM agents WHERE id='b2'`); got != "Scout" {
		t.Errorf("unique bot changed: %s", got)
	}
	if got := value(`SELECT s.name FROM threads t JOIN sections s ON s.id=t.section_id WHERE t.id='b1'`); got != "Studio" {
		t.Errorf("loose chat landed in %s", got)
	}
	if got := value(`SELECT s.name FROM threads t JOIN sections s ON s.id=t.section_id WHERE t.id='b2'`); got != "Work (Studio)" {
		t.Errorf("sectioned chat landed in %s", got)
	}
	if got := value(`SELECT s.id FROM threads t JOIN sections s ON s.id=t.section_id WHERE t.id='a1'`); got != UnassignedID {
		t.Errorf("this server's chat moved to %s", got)
	}
	if got := value(`SELECT message_id FROM messages_fts WHERE messages_fts MATCH 'needle'`); got != "m1" {
		t.Errorf("search: %s", got)
	}
	if got := value(`SELECT json_extract(data,'$.status')||coalesce(next_at,'-') FROM routines WHERE id='r1'`); got != "paused-" {
		t.Errorf("routine: %s", got)
	}
	if got := value(`SELECT json_extract(data,'$.enabled') FROM triggers WHERE id='t1'`); got != "0" {
		t.Errorf("trigger: %s", got)
	}
	if got := value(`SELECT json_extract(data,'$.enabled')||coalesce(next_at,'-') FROM proactivity WHERE agent_id='b1'`); got != "0-" {
		t.Errorf("proactivity: %s", got)
	}
	if got := value(`SELECT path FROM attachments WHERE id='f1'`); got != "/new/data/attachments/f1" {
		t.Errorf("attachment: %s", got)
	}
	if got := value(`SELECT count(*) FROM notices`); got != "0" {
		t.Errorf("notices: %s", got)
	}
}

func TestMergeFailsWholeOnAClash(t *testing.T) {
	here, _ := mergeStore(t)
	there, path := mergeStore(t)
	seedMerge(t, here, "same", "Coach")
	seedMerge(t, there, "other", "Scout")
	seedMerge(t, there, "same", "Coach")
	if _, err := there.DB.ExecContext(t.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := here.Merge(t.Context(), path, "Studio", "", ""); err == nil || !strings.Contains(err.Error(), "agents") {
		t.Fatalf("a clash merged: %v", err)
	}
	var count int
	if err := here.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM agents`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("a failed merge left %d bots", count)
	}
}
