package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/memory"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func deleteRequest(t *testing.T, api *Server, id string, status int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/agents/"+id, nil))
	var result contract.DeleteResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if recorder.Code != status || status == 200 && (err != nil || !result.Deleted) {
		t.Fatalf("delete status=%d body=%s err=%v", recorder.Code, recorder.Body.String(), err)
	}
}

func TestDeleteAgentKeepsOthersAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "polymux.db")
	config := Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Prompt: "Original", Tools: []string{}}, {Agent: contract.Agent{ID: "other", Name: "Other"}, Prompt: "Keep other", Tools: []string{}}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	open := func() *Server {
		t.Helper()
		s, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		api, err := New(t.Context(), s, config)
		if err != nil {
			t.Fatal(err)
		}
		return api
	}
	api := open()
	for _, id := range []string{"exclusive", "shared", "unused"} {
		file := filepath.Join(dir, "attachments", id)
		if err := os.WriteFile(file, []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file+".jpg", []byte("poster"), 0600); err != nil {
			t.Fatal(err)
		}
		a := contract.Attachment{ID: id, Kind: "file", MimeType: "text/plain", IndexingState: "ready"}
		if err := api.Store.SaveAttachment(t.Context(), a, file); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		id, thread  string
		attachments []string
	}{{"one", "assistant", []string{"exclusive", "shared"}}, {"two", "other", []string{"shared"}}} {
		parts := []contract.Part{{Kind: "text", Text: &item.id}}
		for _, id := range item.attachments {
			a, _, _ := api.Store.Attachment(t.Context(), id)
			parts = append(parts, contract.Part{Kind: "attachment", Attachment: &a})
		}
		m := contract.Message{ID: item.id, ThreadID: item.thread, Role: "user", Status: "done", Parts: parts}
		if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: item.thread, Type: "message.done"}, &m); err != nil {
			t.Fatal(err)
		}
		if _, err := api.Store.DB.ExecContext(t.Context(), `INSERT INTO messages_fts(message_id,thread_id,body) VALUES(?,?,?)`, m.ID, m.ThreadID, item.id); err != nil {
			t.Fatal(err)
		}
		if _, err := api.Store.DB.ExecContext(t.Context(), `INSERT INTO media_index(message_id,text) VALUES(?,?)`, m.ID, item.id); err != nil {
			t.Fatal(err)
		}
		if _, err := api.Store.DB.ExecContext(t.Context(), `INSERT INTO media_vectors(message_id,thread_id,embedding,model) VALUES(?,?,X'0000803F0000000000000000','test')`, m.ID, m.ThreadID); err != nil {
			t.Fatal(err)
		}
	}
	sessions := filepath.Join(dir, "assistant", "sessions")
	if err := os.MkdirAll(sessions, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte("history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assistant", "keep.txt"), []byte("workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.SaveSession(t.Context(), "assistant", filepath.Join(sessions, "session.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: "private", Text: "Delete this memory"}); err != nil {
		t.Fatal(err)
	}
	deleteRequest(t, api, "assistant", 200)
	deleteRequest(t, api, "assistant", 200)
	for _, table := range []string{"messages", "events", "threads", "agents", "agent_definitions", "messages_fts", "media_index", "media_vectors"} {
		var count int
		if err := api.Store.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("delete scope table=%s count=%d err=%v", table, count, err)
		}
	}
	for _, table := range []string{"sessions", "jobs"} {
		var count int
		if err := api.Store.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("deleted session/job table=%s count=%d err=%v", table, count, err)
		}
	}
	for _, file := range []string{filepath.Join(dir, "attachments", "exclusive"), filepath.Join(dir, "attachments", "exclusive.jpg"), sessions, filepath.Join(dir, "memory", "assistant")} {
		if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted owned file remains %s %v", file, err)
		}
	}
	for _, file := range []string{filepath.Join(dir, "attachments", "shared"), filepath.Join(dir, "attachments", "unused"), filepath.Join(dir, "assistant", "keep.txt")} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("unrelated file removed %s %v", file, err)
		}
	}
	api.Close()
	api.Store.Close()
	api = open()
	defer api.Store.Close()
	defer api.Close()
	agents, err := api.Store.Agents(t.Context())
	if err != nil || len(agents) != 1 || agents[0].ID != "other" {
		t.Fatalf("deleted default reappeared %+v %v", agents, err)
	}
	profileRequest(t, api, http.MethodGet, "/agents/assistant", "", 404)
	other := profileRequest(t, api, http.MethodGet, "/agents/other", "", 200)
	if other.Prompt != "Keep other" {
		t.Fatalf("other profile changed %+v", other)
	}
	deleteRequest(t, api, "assistant", 200)
}

func TestDeleteStopsActiveReplyAndRejectsQueuedSends(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := &profileFactory{opened: make(chan openedAgent, 4), first: make(chan struct{})}
	api, err := New(t.Context(), s, Config{DataDir: dir, Factory: f, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant"}, Tools: []string{}}, {Agent: contract.Agent{ID: "other"}, Tools: []string{}}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	for _, id := range []string{"active", "queued"} {
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages", strings.NewReader(`{"id":"`+id+`","text":"hello","attachmentIDs":[]}`)))
		if recorder.Code != 202 {
			t.Fatalf("send status %d", recorder.Code)
		}
	}
	<-f.opened
	var parallel sync.WaitGroup
	for i := 0; i < 4; i++ {
		parallel.Go(func() { deleteRequest(t, api, "assistant", 200) })
	}
	parallel.Wait()
	if f.closed.Load() != 1 || f.unsafe.Load() || f.count.Load() != 1 {
		t.Fatalf("delete did not stop exactly one active session closed=%d unsafe=%v opens=%d", f.closed.Load(), f.unsafe.Load(), f.count.Load())
	}
	for _, table := range []string{"messages", "events", "jobs", "sessions"} {
		var count int
		if err := s.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("active reply resurrected table=%s count=%d err=%v", table, count, err)
		}
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages", strings.NewReader(`{"id":"late","text":"hello","attachmentIDs":[]}`)))
	if recorder.Code != 404 {
		t.Fatalf("send after deletion status=%d", recorder.Code)
	}
	profileRequest(t, api, http.MethodGet, "/agents/other", "", 200)
}

func TestDeleteMemoryFailureStillCleansFilesAndRestartsGroups(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Definitions: []agentruntime.Definition{
		{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}}, {Agent: contract.Agent{ID: "c", Name: "C"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if _, err := db.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b", "c"}}); err != nil {
		t.Fatal(err)
	}
	api.actorsMu.Lock()
	api.startGroup("group")
	api.actorsMu.Unlock()
	attachment := filepath.Join(dir, "attachments", "private")
	if err := os.WriteFile(attachment, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	a := contract.Attachment{ID: "private", Kind: "file", MimeType: "text/plain", IndexingState: "ready"}
	if err := db.SaveAttachment(t.Context(), a, attachment); err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: "owned", ThreadID: "a", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "attachment", Attachment: &a}}}
	if _, err := db.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(dir, "a", "sessions")
	if err := os.MkdirAll(sessions, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "memory")
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	deleteRequest(t, api, "a", 500)
	for _, path := range []string{attachment, sessions} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("memory failure skipped owned file cleanup", path, err)
		}
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/group/messages", strings.NewReader(`{"id":"survivor","text":"hello","mentions":["b"],"attachmentIDs":[]}`)))
	if recorder.Code != 202 {
		t.Fatal("surviving group not restarted", recorder.Code)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	deleteRequest(t, api, "a", 200)
}
