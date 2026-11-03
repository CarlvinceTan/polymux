package server

import (
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// seedLayout writes files; a path ending in a slash is an empty folder.
func seedLayout(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		full := filepath.Join(dir, path)
		if strings.HasSuffix(path, "/") {
			if err := os.MkdirAll(full, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// readLayout lists every file and empty folder under dir, leaving out the database.
func readLayout(t *testing.T, dir string) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, "polymux.db") {
			return err
		}
		if entry.IsDir() {
			if entries, err := os.ReadDir(path); err != nil || len(entries) > 0 {
				return err
			}
			found[relative+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		found[relative] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func layoutStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.ExecContext(t.Context(), `INSERT INTO deleted_agents(id,deleted_at) VALUES('gone','now'),('kept','now')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func layoutAgents(ids ...string) []agentruntime.Definition {
	definitions := make([]agentruntime.Definition, 0, len(ids))
	for _, id := range ids {
		definitions = append(definitions, agentruntime.Definition{Agent: contract.Agent{ID: id}})
	}
	return definitions
}

func TestMoveAgents(t *testing.T) {
	dir := t.TempDir()
	db := layoutStore(t, dir)
	seedLayout(t, dir, map[string]string{
		"full/notes/plan.md":  "plan",
		"full/.hidden":        "hidden",
		"empty/":              "",
		"agents/done/kept.md": "moved by an earlier run",
		"clash/old.md":        "old",
		"agents/clash/new.md": "new",
		"blank/old.md":        "old",
		"agents/blank/":       "",
		"stale/":              "",
		"agents/stale/new.md": "new",
		"gone/":               "",
		"kept/report.txt":     "report",
		"skills/x/SKILL.md":   "skill",
		"attachments/file":    "attachment",
		"settings.json":       "{}",
	})
	if err := os.Symlink("notes/plan.md", filepath.Join(dir, "full", "link")); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	agents := layoutAgents("full", "empty", "done", "clash", "blank", "stale")
	if err := moveAgents(t.Context(), db, dir, agents, log); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"agents/full/notes/plan.md": "plan",
		"agents/full/.hidden":       "hidden",
		"agents/full/link":          "plan",
		"agents/empty/":             "",
		"agents/done/kept.md":       "moved by an earlier run",
		"clash/old.md":              "old",
		"agents/clash/new.md":       "new",
		"agents/blank/old.md":       "old",
		"agents/stale/new.md":       "new",
		"agents/kept/report.txt":    "report",
		"skills/x/SKILL.md":         "skill",
		"attachments/file":          "attachment",
		"settings.json":             "{}",
	}
	if got := readLayout(t, dir); !maps.Equal(got, want) {
		t.Fatalf("layout = %v, want %v", got, want)
	}
	if err := moveAgents(t.Context(), db, dir, agents, log); err != nil {
		t.Fatal(err)
	}
	if got := readLayout(t, dir); !maps.Equal(got, want) {
		t.Fatalf("second run changed the layout: %v", got)
	}
}

func TestMoveAgentsContinuesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	db := layoutStore(t, dir)
	seedLayout(t, dir, map[string]string{"one/file": "1", "two/file": "2", "agents": "not a folder"})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	agents := layoutAgents("one", "two")
	if err := moveAgents(t.Context(), db, dir, agents, log); err == nil {
		t.Fatal("expected the blocked move to fail")
	}
	want := map[string]string{"one/file": "1", "two/file": "2", "agents": "not a folder"}
	if got := readLayout(t, dir); !maps.Equal(got, want) {
		t.Fatalf("failed run changed the layout: %v", got)
	}
	if err := os.Remove(filepath.Join(dir, "agents")); err != nil {
		t.Fatal(err)
	}
	if err := moveAgents(t.Context(), db, dir, agents, log); err != nil {
		t.Fatal(err)
	}
	want = map[string]string{"agents/one/file": "1", "agents/two/file": "2"}
	if got := readLayout(t, dir); !maps.Equal(got, want) {
		t.Fatalf("layout = %v, want %v", got, want)
	}
}

func TestStartupMovesAgentFolders(t *testing.T) {
	dir := t.TempDir()
	seedLayout(t, dir, map[string]string{"a/notes.md": "notes", "gone/": ""})
	db := layoutStore(t, dir)
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Definitions: layoutAgents("a")})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	data, err := os.ReadFile(filepath.Join(outputCWD(agentruntime.Definition{Agent: contract.Agent{ID: "a"}}, dir), "notes.md"))
	if err != nil || string(data) != "notes" {
		t.Fatalf("agent file not in its new folder: %q %v", data, err)
	}
	for _, path := range []string{"a", "gone"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
			t.Fatalf("old folder %s remains: %v", path, err)
		}
	}
}
