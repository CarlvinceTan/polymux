package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestPinAPI(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	for _, body := range []string{`{"pinned":true}`, `{"pinned":true,"pinOrder":0}`, `{"pinned":false}`} {
		request := httptest.NewRequest(http.MethodPatch, "/threads/a", strings.NewReader(body))
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatal("pin-only PATCH rejected", response.Code, response.Body.String())
		}
		var thread contract.Thread
		if err := json.Unmarshal(response.Body.Bytes(), &thread); err != nil {
			t.Fatal(err)
		}
		if thread.Title != "A" || len(thread.AgentIDs) != 1 || thread.AgentIDs[0] != "a" {
			t.Fatal("pin-only PATCH changed title or members")
		}
	}
	for _, body := range []string{`{}`, `{"title":""}`, `{"pinned":false,"pinOrder":0}`, `{"pinned":true,"pinOrder":-1}`} {
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/threads/a", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatal("invalid pin PATCH accepted", response.Code)
		}
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/threads/missing", strings.NewReader(`{"pinned":true}`)))
	if response.Code != 404 {
		t.Fatal("missing pinned thread was not rejected", response.Code)
	}
	response = httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/threads/b", strings.NewReader(`{"pinned":true}`)))
	if response.Code != 200 {
		t.Fatal("pin failed", response.Code)
	}
	response = httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/threads", nil))
	var threads []contract.Thread
	if err := json.Unmarshal(response.Body.Bytes(), &threads); err != nil || len(threads) != 2 || threads[0].ID != "b" || (threads[0].Pinned == nil || !*threads[0].Pinned) {
		t.Fatal("thread list did not expose ordered pins", err)
	}
}
