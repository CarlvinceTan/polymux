package store

import (
	"encoding/json"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"reflect"
	"testing"
)

func TestSidebarRollback(t *testing.T) {
	for _, failure := range []string{"event", "activity", "audit"} {
		t.Run(failure, func(t *testing.T) {
			s, _ := sectionFixture(t)
			if _, _, err := s.CreateSection(t.Context(), "one", "One", nil); err != nil {
				t.Fatal(err)
			}
			if err := s.PlaceThread(t.Context(), "group", Placement{SectionSet: true, SectionID: new("one")}); err != nil {
				t.Fatal(err)
			}
			before, err := s.Sidebar(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			triggers := map[string]string{
				"event":    `CREATE TRIGGER reject_change BEFORE INSERT ON sidebar_events BEGIN SELECT RAISE(ABORT,'fixture event failure'); END`,
				"activity": `CREATE TRIGGER reject_change BEFORE INSERT ON events WHEN json_extract(NEW.data,'$.type')='activity' BEGIN SELECT RAISE(ABORT,'fixture activity failure'); END`,
				"audit":    `CREATE TRIGGER reject_change BEFORE INSERT ON sidebar_audit BEGIN SELECT RAISE(ABORT,'fixture audit failure'); END`,
			}
			if _, err := s.DB.ExecContext(t.Context(), triggers[failure]); err != nil {
				t.Fatal(err)
			}
			ctx, change := SidebarActor(t.Context(), "a", "fixture-run")
			if _, err := s.DeleteSection(ctx, "one"); err == nil {
				t.Fatal("failed journal write committed layout mutation")
			}
			after, err := s.Sidebar(t.Context())
			if err != nil || !reflect.DeepEqual(before, after) || change.Changed {
				t.Fatal("failed journal write changed saved layout or cursor", err)
			}
			var count int
			if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM sidebar_audit`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed delete retained an audit row", count, err)
			}
			events, err := s.Events(t.Context(), "a", 0, 100)
			if err != nil || len(events) != 0 {
				t.Fatal("failed delete retained an activity row", err)
			}
		})
	}
}

func TestSidebarReplayRestart(t *testing.T) {
	s, path := sectionFixture(t)
	before, err := s.Sidebar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateSection(t.Context(), "one", "One", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events, err := s.SidebarEvents(t.Context(), before.Cursor, 100)
	if err != nil || len(events) != 1 || events[0].Seq <= before.Cursor || events[0].Sections[0].ID != "one" {
		t.Fatal("layout event did not survive restart", err)
	}
	after, err := s.Sidebar(t.Context())
	if err != nil || after.Cursor != events[0].Seq {
		t.Fatal("snapshot cursor did not match committed state", err)
	}
	if _, err := json.Marshal(after); err != nil {
		t.Fatal(err)
	}
}

func TestSidebarCreationRollback(t *testing.T) {
	s, _ := sectionFixture(t)
	if _, _, err := s.CreateSection(t.Context(), "one", "One", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `UPDATE threads SET section_order=10 WHERE id='group'`); err != nil {
		t.Fatal(err)
	}
	thread, err := s.CreateThread(t.Context(), "new-main", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
	if err != nil || thread.SectionOrder == nil || *thread.SectionOrder != 11 {
		t.Fatal("new main-list chat did not append after saved source order", err)
	}
	before, err := s.Sidebar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(t.Context(), `CREATE TRIGGER reject_creation BEFORE INSERT ON sidebar_events BEGIN SELECT RAISE(ABORT,'fixture event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateThread(t.Context(), "rejected-chat", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, SectionID: new("one")}); err == nil {
		t.Fatal("chat creation ignored event failure")
	}
	agent := contract.Agent{ID: "rejected-agent", Name: "Rejected"}
	definition := json.RawMessage(`{"agent":{"id":"rejected-agent","name":"Rejected"},"prompt":"Fixture instructions"}`)
	if err := s.CreateDefinition(t.Context(), agent, definition, nil, new("one")); err == nil {
		t.Fatal("agent creation ignored event failure")
	}
	after, err := s.Sidebar(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed creation changed sidebar state", err)
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM agents WHERE id='rejected-agent')+(SELECT count(*) FROM jobs WHERE id='created:rejected-agent')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed creation retained profile or queued intro", count, err)
	}
}
