package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestWorkRoute(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, BuildSHA: "abc123", Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest("GET", "/work?limit=5", nil))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var work contract.WorkActivity
	if err := json.Unmarshal(response.Body.Bytes(), &work); err != nil {
		t.Fatal(err)
	}
	if work.Build != "abc123" || work.Runs == nil || work.Deliveries == nil {
		t.Fatalf("%+v", work)
	}
	response = httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest("GET", "/work?limit=none", nil))
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
}

func TestPluginToolNameIsSafeForModels(t *testing.T) {
	tool := pluginTool{tool: plugins.AgentTool{Plugin: "graph-visualiser", Tool: plugins.Tool{Name: "report_work"}}}
	if name := tool.Name(); name != "graph_visualiser__report_work" {
		t.Fatal(name)
	}
}
