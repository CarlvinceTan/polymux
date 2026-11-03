package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func sectionFixture(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A", Model: "live-a"}, {ID: "b", Name: "B"}, {ID: "lead", Name: "Lead", Model: "live-lead"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}, Title: new("Live group")}); err != nil {
		t.Fatal(err)
	}
	message := contract.Message{ID: "live", ThreadID: "group", Role: "assistant", Status: "done", Parts: []contract.Part{{Kind: "text", Text: new("Live message")}}, CreatedAt: Now()}
	if _, err := s.Append(t.Context(), contract.Event{ConversationID: "group", Type: "message.done", MessageID: &message.ID}, &message); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestPinPlacement(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		id     string
		before *string
		after  *string
		want   []string
		fail   bool
	}{
		{name: "first slot", id: "b", before: new("a"), want: []string{"b", "a"}},
		{name: "last slot", id: "a", after: new("b"), want: []string{"b", "a"}},
		{name: "new pin", id: "group", before: new("a"), want: []string{"group", "a", "b"}},
		{name: "missing anchor", id: "group", before: new("missing"), fail: true},
		{name: "unpinned anchor", id: "group", before: new("lead"), fail: true},
		{name: "conflicting anchors", id: "group", before: new("a"), after: new("b"), fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, _ := sectionFixture(t)
			ctx := t.Context()
			if _, _, err := s.CreateSection(ctx, "work", "Work", nil); err != nil {
				t.Fatal(err)
			}
			if err := s.PlaceThread(ctx, "group", Placement{SectionSet: true, SectionID: new("work")}); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"a", "b"} {
				if err := s.PlaceThread(ctx, id, Placement{Pinned: new(true)}); err != nil {
					t.Fatal(err)
				}
			}
			previous, err := s.Sidebar(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = s.PlaceThread(ctx, test.id, Placement{Pinned: new(true), BeforeID: test.before, AfterID: test.after})
			if test.fail {
				if !errors.Is(err, ErrAnchor) {
					t.Fatalf("invalid anchor: %v", err)
				}
				next, err := s.Sidebar(ctx)
				if err != nil || !reflect.DeepEqual(previous, next) {
					t.Fatalf("failed drop changed sidebar: %v, %v", next, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			threads, err := s.Threads(ctx)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, thread := range threads {
				if thread.Pinned == nil || !*thread.Pinned {
					continue
				}
				if thread.PinOrder == nil || *thread.PinOrder != float64(len(ids)) || thread.SectionID != nil {
					t.Fatalf("invalid pin placement: %+v", thread)
				}
				ids = append(ids, thread.ID)
			}
			if !slices.Equal(ids, test.want) {
				t.Fatalf("pins = %v, want %v", ids, test.want)
			}
		})
	}
}

func TestUnassignedMigration(t *testing.T) {
	s, path := sectionFixture(t)
	ctx := t.Context()
	if _, _, err := s.CreateSection(ctx, "custom", "Unassigned", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceThread(ctx, "a", Placement{SectionSet: true, SectionID: new("custom")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sections WHERE id=?; DROP INDEX sections_custom_name; CREATE UNIQUE INDEX sections_name ON sections(name COLLATE NOCASE)`, UnassignedID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sections, err := s.Sections(ctx)
	if err != nil || len(sections) != 2 || sections[0].ID != "custom" || sections[1].ID != UnassignedID || sections[1].ChatCount != 3 {
		t.Fatal("migration lost an existing section or its placement", sections, err)
	}
	thread, err := s.Thread(ctx, "a")
	if err != nil || thread.SectionID == nil || *thread.SectionID != "custom" {
		t.Fatal("migration moved an assigned chat", thread, err)
	}
}

func TestSectionsPersist(t *testing.T) {
	s, path := sectionFixture(t)
	for _, id := range []string{"one", "two", "three"} {
		if _, created, err := s.CreateSection(t.Context(), id, id, nil); err != nil || !created {
			t.Fatal(created, err)
		}
	}
	if old, created, err := s.CreateSection(t.Context(), "other", "ONE", nil); err != nil || created || old.ID != "one" {
		t.Fatal(old, created, err)
	}
	if _, err := s.UpdateSection(t.Context(), "two", new("Career"), new(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReorderSidebar(t.Context(), SidebarOrder{Kind: "sections", ID: "three", BeforeID: new("one")}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "group"} {
		if err := s.PlaceThread(t.Context(), id, Placement{SectionSet: true, SectionID: new("two")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReorderSidebar(t.Context(), SidebarOrder{Kind: "chats", SectionID: new("two"), ID: "group", BeforeID: new("a")}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Thread(t.Context(), "group")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceThread(t.Context(), "group", Placement{SectionSet: true, SectionID: new("two")}); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Thread(t.Context(), "group")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("same placement changed chat metadata")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sections, err := s.Sections(t.Context())
	if err != nil || len(sections) != 4 || !slices.Equal([]string{sections[0].ID, sections[1].ID, sections[2].ID, sections[3].ID}, []string{UnassignedID, "three", "one", "two"}) || !sections[3].Collapsed || sections[3].Name != "Career" || sections[3].ChatCount != 2 {
		t.Fatal(sections, err)
	}
	if err := s.UpdateThread(t.Context(), "group", contract.UpdateThreadRequest{Pinned: new(true)}); err != nil {
		t.Fatal(err)
	}
	pinned, _ := s.Thread(t.Context(), "group")
	if pinned.SectionID != nil || pinned.SectionOrder != nil {
		t.Fatal("pin did not clear section")
	}
	if err := s.PlaceThread(t.Context(), "group", Placement{SectionSet: true, SectionID: new("two")}); !errors.Is(err, ErrPlacement) {
		t.Fatal("pinned chat moved", err)
	}
	if err := s.PlaceThread(t.Context(), "group", Placement{Pinned: new(false), SectionSet: true, SectionID: new("two")}); err != nil {
		t.Fatal(err)
	}
	returned, err := s.DeleteSection(t.Context(), "two")
	if err != nil || len(returned) != 2 {
		t.Fatal(returned, err)
	}
	group, err := s.Thread(t.Context(), "group")
	if err != nil || group.SectionID != nil || group.SectionOrder != nil || group.Title != "Live group" || group.LastSeq != before.LastSeq || group.LastReadSeq != before.LastReadSeq || !slices.Equal(group.AgentIDs, []string{"a", "b"}) {
		t.Fatal("section deletion changed chat", group, err)
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE id='live'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("message changed", count, err)
	}
}

func TestUnassignedSection(t *testing.T) {
	s, path := sectionFixture(t)
	ctx := t.Context()
	if _, _, err := s.CreateSection(ctx, "work", "Work", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceThread(ctx, "a", Placement{SectionSet: true, SectionID: new("work")}); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceThread(ctx, "b", Placement{Pinned: new(true)}); err != nil {
		t.Fatal(err)
	}
	section, err := s.UpdateSection(ctx, UnassignedID, nil, new(true))
	if err != nil || !section.Collapsed || section.ChatCount != 2 {
		t.Fatal("unassigned includes pinned or assigned chats", section, err)
	}
	if _, err := s.ReorderSidebar(ctx, SidebarOrder{Kind: "sections", ID: UnassignedID, AfterID: new("work")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSection(ctx, UnassignedID, new("Changed"), nil); !errors.Is(err, ErrSectionFixed) {
		t.Fatal("renamed unassigned", err)
	}
	if _, err := s.DeleteSection(ctx, UnassignedID); !errors.Is(err, ErrSectionFixed) {
		t.Fatal("deleted unassigned", err)
	}
	if err := s.PlaceThread(ctx, "a", Placement{SectionSet: true, SectionID: new(UnassignedID)}); err != nil {
		t.Fatal(err)
	}
	thread, err := s.Thread(ctx, "a")
	if err != nil || thread.SectionID != nil {
		t.Fatal("unassigned must keep null placement", thread, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sections, err := s.Sections(ctx)
	if err != nil || len(sections) != 2 || sections[1].ID != UnassignedID || !sections[1].Collapsed || sections[1].Name != "Unassigned" || sections[1].ChatCount != 3 {
		t.Fatal("unassigned state did not persist", sections, err)
	}
}

func TestSectionErrors(t *testing.T) {
	s, _ := sectionFixture(t)
	for _, name := range []string{"", "  ", "bad\nname", string(make([]byte, 41))} {
		if _, _, err := s.CreateSection(t.Context(), "x", name, nil); !errors.Is(err, ErrSectionName) {
			t.Fatal(name, err)
		}
	}
	for i := 0; i < 30; i++ {
		if _, _, err := s.CreateSection(t.Context(), fmt.Sprint(i), fmt.Sprint(i), nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.CreateSection(t.Context(), "overflow", "Overflow", nil); !errors.Is(err, ErrSectionLimit) {
		t.Fatal(err)
	}
	if _, err := s.UpdateSection(t.Context(), "0", new("1"), nil); !errors.Is(err, ErrSectionTaken) {
		t.Fatal(err)
	}
	if err := s.PlaceThread(t.Context(), "a", Placement{SectionSet: true, SectionID: new("0"), BeforeID: new("group")}); !errors.Is(err, ErrAnchor) {
		t.Fatal(err)
	}
	thread, _ := s.Thread(t.Context(), "a")
	if thread.SectionID != nil {
		t.Fatal("invalid anchor partially moved chat")
	}
	if _, err := s.ReorderSidebar(t.Context(), SidebarOrder{Kind: "sections", ID: "0", BeforeID: new("0")}); !errors.Is(err, ErrAnchor) {
		t.Fatal(err)
	}
}

func TestSidebarImport(t *testing.T) {
	s, path := sectionFixture(t)
	if _, _, err := s.CreateSection(t.Context(), "local", "Local", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceThread(t.Context(), "b", Placement{SectionSet: true, SectionID: new("local")}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(t.Context(), "lead", contract.UpdateThreadRequest{Pinned: new(true)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), []contract.Agent{{ID: "extra", Name: "Extra"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateSection(t.Context(), "source-projects", "Projects", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceThread(t.Context(), "extra", Placement{SectionSet: true, SectionID: new("source-projects")}); err != nil {
		t.Fatal(err)
	}
	source := SidebarImport{SchemaVersion: 1, Source: "grokbot", Sections: []SourceSection{{ID: "source-projects", Name: "Projects", Collapsed: new(true), ThreadIDs: []string{"group", "missing-group"}}, {ID: "source-team", Name: "Team", Collapsed: new(false), ThreadIDs: []string{"a", "orphan-source-id"}}, {ID: "__agents__", Name: "Unassigned", Collapsed: new(false), ThreadIDs: []string{}}}, PinnedThreadIDs: []string{"b", "missing-pin"}}
	before := dbFiles(t, path)
	report, err := s.ImportSidebar(t.Context(), source, false)
	if err != nil || report.Applied || report.Sections != 2 || report.Placements != 2 || report.Pins != 1 || !slices.Equal(report.UnresolvedIDs, []string{"missing-group", "orphan-source-id", "missing-pin"}) {
		t.Fatal(report, err)
	}
	if !reflect.DeepEqual(before, dbFiles(t, path)) {
		t.Fatal("preview changed database, WAL, or directory entries")
	}
	sections, _ := s.Sections(t.Context())
	if len(sections) != 3 {
		t.Fatal("preview mutated sections")
	}
	for i := 0; i < 2; i++ {
		report, err = s.ImportSidebar(t.Context(), source, true)
		if err != nil || !report.Applied {
			t.Fatal(report, err)
		}
	}
	sections, _ = s.Sections(t.Context())
	if len(sections) != 4 || sections[0].ID != "source-projects" || sections[1].ID != "source-team" || sections[2].ID != UnassignedID || sections[3].ID != "local" {
		t.Fatal("source order or extra section lost", sections)
	}
	saved, err := s.SidebarSource(t.Context(), "grokbot")
	if err != nil || !reflect.DeepEqual(saved, source) {
		t.Fatal("source evidence was lost", err)
	}
	threads, err := s.Threads(t.Context())
	if err != nil || len(threads) != 5 || threads[0].ID != "b" || threads[1].ID != "lead" {
		t.Fatal("extra chat or pin was lost", threads, err)
	}
	group, _ := s.Thread(t.Context(), "group")
	if group.SectionID == nil || *group.SectionID != "source-projects" || group.LastSeq != 1 || !slices.Equal(group.AgentIDs, []string{"a", "b"}) {
		t.Fatal("group was recreated", group)
	}
	extra, _ := s.Thread(t.Context(), "extra")
	if extra.SectionOrder == nil || *extra.SectionOrder < 2 {
		t.Fatal("extra chat interleaved source order", extra)
	}
	agents, _ := s.Agents(t.Context())
	for _, agent := range agents {
		if agent.ID == "lead" && agent.Model != "live-lead" {
			t.Fatal("model changed")
		}
	}
	var count int
	if err := s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE id='live'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved, err = s.SidebarSource(t.Context(), "grokbot")
	if err != nil || !reflect.DeepEqual(saved, source) {
		t.Fatal("source evidence did not survive restart", err)
	}
}

func TestSidebarImportRejectsAmbiguity(t *testing.T) {
	s, _ := sectionFixture(t)
	for _, source := range []SidebarImport{
		{SchemaVersion: 1, Source: "grokbot"},
		{SchemaVersion: 1, Source: "grokbot", Sections: []SourceSection{{ID: "s", Name: "Section", ThreadIDs: []string{"a"}}}, PinnedThreadIDs: []string{}},
		{SchemaVersion: 1, Source: "grokbot", Sections: []SourceSection{{ID: "s", Name: "Section", ThreadIDs: []string{"a", "a"}}}, PinnedThreadIDs: []string{}},
		{SchemaVersion: 1, Source: "grokbot", Sections: []SourceSection{{ID: "s", Name: "Section", ThreadIDs: []string{"a"}}}, PinnedThreadIDs: []string{"a"}},
	} {
		if _, err := s.ImportSidebar(t.Context(), source, true); !errors.Is(err, ErrSidebarImport) {
			t.Fatal(err)
		}
	}
	if _, _, err := s.CreateSection(t.Context(), "local", "Projects", nil); err != nil {
		t.Fatal(err)
	}
	source := SidebarImport{SchemaVersion: 1, Source: "grokbot", Sections: []SourceSection{{ID: "new", Name: "Projects", Collapsed: new(false), ThreadIDs: []string{"a"}}}, PinnedThreadIDs: []string{}}
	if _, err := s.ImportSidebar(t.Context(), source, true); !errors.Is(err, ErrSectionTaken) {
		t.Fatal(err)
	}
	thread, _ := s.Thread(t.Context(), "a")
	if thread.SectionID != nil {
		t.Fatal("rejected import changed placement")
	}
}

func dbFiles(t *testing.T, path string) map[string][32]byte {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][32]byte{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(path), entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		// Normal server reads may change SQLite shared-memory reader marks.
		if entry.Name() == filepath.Base(path)+"-shm" {
			data = []byte(fmt.Sprint(len(data)))
		}
		files[entry.Name()] = sha256.Sum256(data)
	}
	return files
}
