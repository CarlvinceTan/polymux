package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestActivityTools(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir,
		Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}},
		Factory:     &testFactory{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	state := &turnState{run: contract.Run{ID: "run", AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	memory := &memoryTool{store: db, provider: api.memory, action: "save"}
	args := json.RawMessage(`{"name":"preference","text":"Concise answers","revision":""}`)
	if _, err := memory.Execute(ctx, "save", args, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Execute(ctx, "conflict", args, nil); err == nil {
		t.Fatal("conflicting save succeeded")
	}
	skill := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skill, []byte("Use concise answers."), 0600); err != nil {
		t.Fatal(err)
	}
	skillArgs, err := json.Marshal(map[string]string{"action": "skill", "path": skill})
	if err != nil {
		t.Fatal(err)
	}
	tool := &integrationTool{server: api}
	for _, input := range []json.RawMessage{
		skillArgs, skillArgs,
		json.RawMessage(`{"action":"mcp","name":"notes","command":"notes-mcp"}`),
		json.RawMessage(`{"action":"remove","name":"notes"}`),
		json.RawMessage(`{"action":"remove","name":"notes"}`),
	} {
		if _, err := tool.Execute(ctx, "setup", input, nil); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.Events(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	labels := []string{}
	for _, event := range events {
		if event.Type == "activity" {
			if event.Payload.Kind == "memory.saved" || event.Payload.Kind == "skill.saved" {
				recorder := httptest.NewRecorder()
				api.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/threads/assistant/activity/"+strconv.FormatInt(event.Seq, 10), nil))
				if recorder.Code != http.StatusOK {
					t.Fatalf("activity object status: %d", recorder.Code)
				}
				var content map[string]string
				if err := json.Unmarshal(recorder.Body.Bytes(), &content); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(content["text"], "oncise answers") {
					t.Fatalf("object content missing: %s", content["text"])
				}
			}
			kinds = append(kinds, event.Payload.Kind)
			labels = append(labels, event.Payload.ObjectLabel)
		}
	}
	if strings.Join(kinds, ",") != "memory.saved,skill.saved,connector.added,connector.removed" {
		t.Fatalf("successful mutations must emit once: %v", kinds)
	}
	if labels[0] != "Concise answers" || labels[1] != filepath.Base(dir) {
		t.Fatalf("memory and skill labels must describe the object: %v", labels)
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/threads/assistant/activity/0?path=/private/data.md", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatal("invalid activity cursor accepted")
	}
	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/agents/assistant", strings.NewReader(`{"name":"Assistant","prompt":"Updated instructions"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatal("instruction update failed", recorder.Code)
	}
	documents, err := api.memory.List(ctx, "assistant", "assistant")
	if err != nil || len(documents) != 1 {
		t.Fatal("memory missing", err)
	}
	forgetArgs, err := json.Marshal(map[string]string{"name": documents[0].Name, "revision": documents[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	memory.action = "forget"
	if _, err := memory.Execute(ctx, "forget", forgetArgs, nil); err != nil {
		t.Fatal(err)
	}
	removeArgs, err := json.Marshal(map[string]string{"action": "remove", "path": skill})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{removeArgs, removeArgs,
		json.RawMessage(`{"action":"device","name":"desktop","host":"cvmini"}`),
		json.RawMessage(`{"action":"remove","name":"desktop"}`)} {
		if _, err := tool.Execute(ctx, "setup", input, nil); err != nil {
			t.Fatal(err)
		}
	}
	events, err = db.Events(ctx, "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds = nil
	for _, event := range events {
		if event.Payload != nil {
			kinds = append(kinds, event.Payload.Kind)
		}
	}
	if strings.Join(kinds, ",") != "memory.saved,skill.saved,connector.added,connector.removed,instructions.updated,memory.deleted,skill.removed,device.added,device.removed" {
		t.Fatalf("missing activity producers: %v", kinds)
	}

}
