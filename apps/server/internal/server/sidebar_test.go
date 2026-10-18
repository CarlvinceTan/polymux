package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestSidebarAPI(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != status {
			t.Fatal(method, path, response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	var section contract.Section
	if err := json.Unmarshal(call("POST", "/sections", `{"name":"Projects"}`, 201), &section); err != nil {
		t.Fatal(err)
	}
	call("POST", "/sections", `{"name":"projects"}`, 200)
	call("PATCH", "/sections/"+section.ID, `{"collapsed":true}`, 200)
	call("PATCH", "/threads/a/placement", `{"sectionID":"`+section.ID+`"}`, 200)
	var thread contract.Thread
	if err := json.Unmarshal(call("PATCH", "/threads/a/placement", `{"sectionID":null}`, 200), &thread); err != nil || thread.SectionID != nil {
		t.Fatal(thread, err)
	}
	call("PATCH", "/threads/a/placement", `{"pinned":true}`, 200)
	call("PATCH", "/threads/a/placement", `{"sectionID":"`+section.ID+`"}`, 400)
	call("PATCH", "/threads/a/placement", `{"pinned":false,"sectionID":"`+section.ID+`"}`, 200)
	call("POST", "/sidebar/reorder", `{"kind":"chats","id":"a","sectionID":"`+section.ID+`","beforeID":"b"}`, 400)
	call("PATCH", "/threads/a/placement", `{"sectionID":42}`, 400)
	call("PATCH", "/threads/a/placement", `{}`, 400)
	call("PATCH", "/threads/missing/placement", `{"sectionID":null}`, 404)
	call("PATCH", "/sections/"+section.ID, `{}`, 400)
	call("POST", "/sections", `{"name":" "}`, 400)
	call("DELETE", "/sections/"+section.ID, "", 200)
	call("DELETE", "/sections/"+section.ID, "", 404)
	manifest := `{"schemaVersion":1,"source":"grokbot","sections":[{"id":"source-section","name":"Team","collapsed":false,"threadIDs":["a","orphan"]},{"id":"__agents__","name":"Unassigned","collapsed":false,"threadIDs":[]}],"pinnedThreadIDs":["b","missing-pin"]}`
	var report store.SidebarReport
	if err := json.Unmarshal(call("POST", "/sidebar/import", `{"manifest":`+manifest+`}`, 200), &report); err != nil || report.Applied || len(report.UnresolvedIDs) != 2 {
		t.Fatal(report, err)
	}
	call("GET", "/sidebar/sources/grokbot", "", 404)
	call("POST", "/sidebar/import", `{"manifest":`+manifest+`,"apply":true}`, 200)
	var source store.SidebarImport
	if err := json.Unmarshal(call("GET", "/sidebar/sources/grokbot", "", 200), &source); err != nil || source.Sections[0].ThreadIDs[1] != "orphan" {
		t.Fatal("source evidence missing", err)
	}
	var sections []contract.Section
	if err := json.Unmarshal(call("GET", "/sections", "", 200), &sections); err != nil || len(sections) != 1 || sections[0].ChatCount != 1 {
		t.Fatal(sections, err)
	}
}
