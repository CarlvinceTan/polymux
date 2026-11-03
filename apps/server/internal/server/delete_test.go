package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/packages/memory"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
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

func TestDeleteThreadKeepsItsMembers(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	created := httptest.NewRecorder()
	api.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{"agentIDs":["a","b"]}`)))
	var group contract.Thread
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &group) != nil {
		t.Fatal("group creation failed", created.Code)
	}
	for _, tc := range []struct {
		id     string
		status int
	}{
		{id: "a", status: http.StatusConflict},
		{id: "missing", status: http.StatusNotFound},
		{id: group.ID, status: http.StatusOK},
		{id: group.ID, status: http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/threads/"+tc.id, nil))
		if response.Code != tc.status {
			t.Fatalf("delete %s: %d %s", tc.id, response.Code, response.Body.String())
		}
	}
	threads, err := db.Threads(t.Context())
	if err != nil || len(threads) != 2 {
		t.Fatal("member chats changed", threads, err)
	}
	agents, err := db.Agents(t.Context())
	if err != nil || len(agents) != 2 {
		t.Fatal("group deletion removed its members", agents, err)
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
	sessions := filepath.Join(agentruntime.AgentDir(dir, "assistant"), "sessions")
	if err := os.MkdirAll(sessions, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte("history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentruntime.AgentDir(dir, "assistant"), "keep.txt"), []byte("workspace"), 0600); err != nil {
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
	for _, file := range []string{filepath.Join(dir, "attachments", "shared"), filepath.Join(dir, "attachments", "unused"), filepath.Join(agentruntime.AgentDir(dir, "assistant"), "keep.txt")} {
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

func createBot(t *testing.T, api *Server, name string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"`+name+`","role":"","prompt":"","model":"local/test"}`)))
	var bot contract.Agent
	if err := json.Unmarshal(recorder.Body.Bytes(), &bot); err != nil || recorder.Code != 201 {
		t.Fatalf("create status=%d body=%s err=%v", recorder.Code, recorder.Body.String(), err)
	}
	return bot.ID
}

func sqliteRuntime(t *testing.T, dir, url string) *agentruntime.DurableRuntime {
	t.Helper()
	storage, closeStorage, err := agentruntime.NewDurableStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeStorage() })
	runtime, err := agentruntime.NewDurable(t.Context(), dir, url, "local/test", "test-key", agentruntime.Client{}, storage)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}

// durableServer runs the API on the real runtime and SQLite storage.
func durableServer(t *testing.T, dir, url string, definitions ...agentruntime.Definition) (*Server, *store.Store) {
	t.Helper()
	runtime := sqliteRuntime(t, dir, url)
	s, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	api, err := New(t.Context(), s, Config{DataDir: dir, Factory: runtime, Definitions: definitions, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close() })
	return api, s
}

func requireNoDurableRows(t *testing.T, dir string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "durable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"conversations", "entries", "tasks", "submissions", "documents", "document_revisions", "record_ids"} {
		var count int
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("durable rows left table=%s count=%d err=%v", table, count, err)
		}
	}
}

func TestDeleteDuringReplyLeavesNoDurableRows(t *testing.T) {
	replying := make(chan struct{}, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only sees a disconnect once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		replying <- struct{}{}
		<-r.Context().Done()
	}))
	t.Cleanup(provider.Close)
	dir := t.TempDir()
	api, _ := durableServer(t, dir, provider.URL)
	reply := func() {
		t.Helper()
		select {
		case <-replying:
		case <-time.After(10 * time.Second):
			t.Fatal("no reply reached the provider")
		}
	}
	id := createBot(t, api, "Deleted")
	reply()
	deleteRequest(t, api, id, 200)
	requireNoDurableRows(t, dir)
	createBot(t, api, "Next")
	reply()
}

// heldTool ignores cancellation until released, like a tool that is slow to stop.
type heldTool struct {
	started, release chan struct{}
}

func (*heldTool) Name() string                           { return "held" }
func (*heldTool) Label() string                          { return "Held" }
func (*heldTool) Description() string                    { return "Holds its turn open." }
func (*heldTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*heldTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "held", Description: "Holds its turn open.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{},
	}}
}

func (t *heldTool) Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	close(t.started)
	<-t.release
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "held"}}}, nil
}

// A hung tool keeps its rows until it ends, but the deletion answers and the runtime stays healthy.
func TestDeleteOutlastsHungTool(t *testing.T) {
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		delta := map[string]any{"role": "assistant", "content": "Hi"}
		finish := "stop"
		if hits.Add(1) == 1 {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call", "type": "function", "function": map[string]any{"name": "held", "arguments": "{}"}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": delta}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			data, _ := json.Marshal(map[string]any{"id": "test", "object": "chat.completion.chunk", "model": "test", "choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(provider.Close)
	tool := &heldTool{started: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(tool.release) }) })
	dir := t.TempDir()
	api, s := durableServer(t, dir, provider.URL, agentruntime.Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}, Tools: []string{}, ExtraTools: []agent.AgentTool{tool}})
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages", strings.NewReader(`{"id":"held","text":"hello","attachmentIDs":[]}`)))
	if recorder.Code != 202 {
		t.Fatalf("send status %d", recorder.Code)
	}
	select {
	case <-tool.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the tool never ran")
	}
	deleteRequest(t, api, "assistant", 200)
	steps, err := s.Cleanups(t.Context())
	if err != nil || len(steps) != 1 || steps[0].Kind != store.CleanupTranscript {
		t.Fatalf("hung transcript was not kept %+v %v", steps, err)
	}
	release.Do(func() { close(tool.release) })
	if api.sweep(t.Context()) {
		t.Fatal("cleanup did not finish once the tool ended")
	}
	requireNoDurableRows(t, dir)
	before := hits.Load()
	createBot(t, api, "Next")
	for deadline := time.Now().Add(10 * time.Second); hits.Load() == before; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the runtime stopped replying")
		}
	}
}

func TestChildConversationEndsWithItsTurn(t *testing.T) {
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		delta := map[string]any{"role": "assistant", "content": "Hi"}
		finish := "stop"
		if hits.Add(1) == 1 {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call", "type": "function", "function": map[string]any{"name": "task", "arguments": `{"action":"run","prompt":"Bounded child task"}`}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": delta}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			data, _ := json.Marshal(map[string]any{"id": "test", "object": "chat.completion.chunk", "model": "test", "choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(provider.Close)
	dir := t.TempDir()
	api, s := durableServer(t, dir, provider.URL, agentruntime.Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}, Tools: []string{}})
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages", strings.NewReader(`{"id":"parent","text":"hello","attachmentIDs":[]}`)))
	if recorder.Code != 202 {
		t.Fatalf("send status %d", recorder.Code)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "durable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var children, conversations int
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		_ = s.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM runs WHERE json_extract(data,'$.trigger')='child' AND status='completed'`).Scan(&children)
		_ = db.QueryRowContext(t.Context(), `SELECT count(*) FROM conversations`).Scan(&conversations)
		if children == 1 && conversations == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("children=%d conversations=%d, want the child's forgotten", children, conversations)
		}
	}
}

// An ephemeral session has no Path, but a failed forget on close must still be retried.
func TestDiscardCoversEphemeralSessions(t *testing.T) {
	dir := t.TempDir()
	api, s := durableServer(t, dir, "http://localhost")
	session, err := api.config.Factory.Open(t.Context(), agentruntime.Definition{Agent: contract.Agent{ID: "a", Name: "A", Model: "local/test"}, Tools: []string{}, Ephemeral: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Path() != "" {
		t.Fatalf("ephemeral session has path %q", session.Path())
	}
	api.discard(t.Context(), session)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		steps, err := s.Cleanups(t.Context())
		if err == nil && len(steps) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanups left %+v %v", steps, err)
		}
	}
	requireNoDurableRows(t, dir)
}

func TestStartupForgetsOrphans(t *testing.T) {
	for _, test := range []struct {
		name  string
		saved bool
		want  int
	}{{name: "saved session proves the store matches", saved: true, want: 1}, {name: "unrelated store proves nothing", want: 2}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			runtime := sqliteRuntime(t, dir, "http://localhost")
			s, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
				t.Fatal(err)
			}
			definition := agentruntime.Definition{Agent: contract.Agent{ID: "a", Name: "A", Model: "local/test"}, Tools: []string{}}
			var kept string
			for range 2 {
				session, err := runtime.Open(t.Context(), definition, "")
				if err != nil {
					t.Fatal(err)
				}
				kept = session.Path()
				if err := session.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if test.saved {
				if err := s.SaveSession(t.Context(), "a", kept); err != nil {
					t.Fatal(err)
				}
			}
			api, err := New(t.Context(), s, Config{DataDir: dir, Factory: runtime, Definitions: []agentruntime.Definition{definition}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			defer api.Close()
			if orphans := api.orphans(t.Context()); !test.saved && len(orphans) != 0 {
				t.Fatalf("an unrelated store orphaned %v", orphans)
			}
			for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
				paths, err := runtime.Transcripts(t.Context())
				if err == nil && len(paths) == test.want && (!test.saved || paths[0] == kept) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("transcripts %v %v, want %d keeping %s", paths, err, test.want, kept)
				}
			}
		})
	}
}

func TestBlockedFilesRetry(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: &testFactory{}, Definitions: []agentruntime.Definition{
		{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}},
	}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if _, err := db.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
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
	folder, shared := agentruntime.AgentDir(dir, "a"), filepath.Join(dir, "groups", "group", "a")
	for _, path := range []string{folder, shared} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	blocked := []string{filepath.Dir(attachment), filepath.Dir(folder), filepath.Dir(shared)}
	chmod := func(mode os.FileMode) {
		for _, path := range blocked {
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	chmod(0500)
	t.Cleanup(func() { chmod(0700) })
	deleteRequest(t, api, "a", 200)
	steps, err := db.Cleanups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, step := range steps {
		kinds = append(kinds, step.Kind)
	}
	slices.Sort(kinds)
	if want := []string{store.CleanupAgent, store.CleanupAttachment, store.CleanupMember}; !slices.Equal(kinds, want) {
		t.Fatalf("kept %v, want %v", kinds, want)
	}
	chmod(0700)
	if api.sweep(t.Context()) {
		t.Fatal("cleanup did not finish once unblocked")
	}
	for _, path := range []string{attachment, folder, shared} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("cleanup left", path, err)
		}
	}
}

// forgetFactory stands in for a runtime whose transcripts can't be forgotten yet.
type forgetFactory struct {
	testFactory
	failing   atomic.Bool
	forgotten sync.Map
	stored    []string
}

func (f *forgetFactory) Transcripts(context.Context) ([]string, error) { return f.stored, nil }

func (f *forgetFactory) Forget(_ context.Context, path string) error {
	if f.failing.Load() {
		return errors.New("still in use")
	}
	f.forgotten.Store(path, true)
	return nil
}

func TestFailedOrphanRetries(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSession(t.Context(), "a", "durable:1"); err != nil {
		t.Fatal(err)
	}
	f := &forgetFactory{stored: []string{"durable:1", "durable:2"}}
	f.failing.Store(true)
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: f, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		steps, err := db.Cleanups(t.Context())
		if err == nil && slices.Equal(steps, []store.Cleanup{{Kind: store.CleanupTranscript, Target: "durable:2"}}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphan was not queued %+v %v", steps, err)
		}
	}
	f.failing.Store(false)
	if api.sweep(t.Context()) {
		t.Fatal("orphan was not forgotten once it could be")
	}
	if _, ok := f.forgotten.Load("durable:2"); !ok {
		t.Fatal("orphan was not forgotten")
	}
	if _, ok := f.forgotten.Load("durable:1"); ok {
		t.Fatal("saved conversation was forgotten")
	}
}

func TestCleanupSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f := &forgetFactory{}
	f.failing.Store(true)
	config := Config{DataDir: dir, Factory: f, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	config.Definitions = []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}}}
	api, err := New(t.Context(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if _, err := db.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(db.SaveSession(t.Context(), "a", "durable:1"), db.SaveGroupSession(t.Context(), "group", "b", "durable:2")); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/threads/group", nil))
	if recorder.Code != 200 {
		t.Fatalf("delete group status %d", recorder.Code)
	}
	deleteRequest(t, api, "a", 200)
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	f.failing.Store(false)
	config.Definitions = config.Definitions[1:]
	restarted, err := New(t.Context(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		steps, err := db.Cleanups(t.Context())
		if err == nil && len(steps) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart left cleanups %+v %v", steps, err)
		}
	}
	for _, path := range []string{"durable:1", "durable:2"} {
		if _, ok := f.forgotten.Load(path); !ok {
			t.Fatalf("%s was not forgotten", path)
		}
	}
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
	folder, shared := agentruntime.AgentDir(dir, "a"), filepath.Join(dir, "groups", "group", "a")
	for _, path := range []string{folder, shared} {
		if err := os.MkdirAll(filepath.Join(path, "sessions"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "memory")
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	deleteRequest(t, api, "a", 200)
	if steps, err := db.Cleanups(t.Context()); err != nil || !slices.Contains(steps, store.Cleanup{Kind: store.CleanupAgent, Target: "a"}) {
		t.Fatalf("failed cleanup was not kept %+v %v", steps, err)
	}
	for _, path := range []string{attachment, folder, shared} {
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
	if api.sweep(t.Context()) {
		t.Fatal("cleanup did not finish once its failure cleared")
	}
}
