package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/memory"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestMemoryScope(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &memoryTool{store: api.Store, provider: api.memory, action: "save"}
	state := &turnState{run: contract.Run{ID: "run", AgentID: "assistant", ConversationID: "assistant", SourceMessageID: pointer("user")}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	if _, err := tool.Execute(ctx, "save", json.RawMessage(`{"name":"preference","text":"Explicit: concise answers","revision":""}`), nil); err != nil {
		t.Fatal(err)
	}
	documents, err := api.memory.List(t.Context(), "assistant", "assistant")
	if err != nil || len(documents) != 1 || documents[0].Source != "user" {
		t.Fatal("memory missing provenance", documents, err)
	}
	message, err := api.memoryContext(t.Context(), state.run, contract.Message{}, "")
	if err != nil || !strings.Contains(store.Text(message), "Explicit: concise answers") {
		t.Fatal("memory not injected", err)
	}
	group := state.run
	group.ConversationID = "group"
	message, err = api.memoryContext(t.Context(), group, contract.Message{}, "")
	if err != nil || strings.Contains(store.Text(message), "concise answers") {
		t.Fatal("group received private memory", err)
	}
	for _, trigger := range []string{"created", "peer"} {
		s := &turnState{run: state.run, work: store.Work{Trigger: trigger}}
		ctx := context.WithValue(t.Context(), turnKey{}, s)
		for _, action := range []string{"save", "recall", "forget"} {
			tool.action = action
			if _, err := tool.Execute(ctx, "read", json.RawMessage(`{}`), nil); err == nil {
				t.Fatal("private memory exposed", trigger, action)
			}
		}
	}
	for i := range 12 {
		if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: string(rune('a' + i)), Text: strings.Repeat("界", 1300)}); err != nil {
			t.Fatal(err)
		}
	}
	message, err = api.memoryContext(t.Context(), state.run, contract.Message{}, "")
	if err != nil || len(store.Text(message)) > 33000 || !utf8.ValidString(store.Text(message)) {
		t.Fatal("memory context exceeded valid byte budget", len(store.Text(message)), err)
	}
}

func TestMemoryContextOmitsEmptyBlock(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	run := contract.Run{ID: "run", AgentID: "assistant", ConversationID: "assistant"}
	message, err := api.memoryContext(t.Context(), run, contract.Message{}, "")
	if err != nil || len(message.Parts) != 0 {
		t.Fatalf("empty memory still produced a header: %q %v", store.Text(message), err)
	}
	group := run
	group.ConversationID = "group"
	message, err = api.memoryContext(t.Context(), group, contract.Message{}, "")
	if err != nil || len(message.Parts) != 0 {
		t.Fatalf("empty group produced a header: %q %v", store.Text(message), err)
	}
}

func TestHistoryTool(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	text := "A past decision"
	old := contract.Message{ID: "old", ThreadID: "assistant", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "assistant", Type: "message.start"}, &old); err != nil {
		t.Fatal(err)
	}
	new := old
	new.ID = "future"
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "assistant", Type: "message.start"}, &new); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message", Message: new}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &historyTool{store: api.Store}
	result, err := tool.Execute(ctx, "history", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(data), "old") || strings.Contains(string(data), "future") {
		t.Fatal("history crossed acceptance boundary", string(data), err)
	}
	state.work.Trigger = "peer"
	if _, err := tool.Execute(ctx, "history", json.RawMessage(`{}`), nil); err == nil {
		t.Fatal("peer retrieved private history")
	}
}

func TestMemoryRankBeforeBudget(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	run := contract.Run{AgentID: "assistant", ConversationID: "assistant"}
	if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: "old", Text: "Weekday university brief at 8:20am SGT"}); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: string(rune('a' + i)), Text: strings.Repeat("unrelated ", 400)}); err != nil {
			t.Fatal(err)
		}
	}
	query := "What time is the weekday university brief?"
	m, err := api.memoryContext(t.Context(), run, contract.Message{Parts: []contract.Part{{Kind: "text", Text: &query}}}, query)
	if err != nil || !strings.Contains(store.Text(m), "8:20am") {
		t.Fatal("old relevant fact lost to recent unrelated memory", err)
	}
	run.ConversationID = "group"
	m, err = api.memoryContext(t.Context(), run, contract.Message{}, "")
	if err != nil || strings.Contains(store.Text(m), "8:20am") {
		t.Fatal("ranking leaked private memory", err)
	}
}

type memoryFactory struct{ received chan contract.Message }

func (f *memoryFactory) Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error) {
	return &memorySession{factory: f}, nil
}

type memorySession struct{ factory *memoryFactory }

func (s *memorySession) Send(_ context.Context, m contract.Message, _ func(agentruntime.Delta) error) (agentruntime.Result, error) {
	s.factory.received <- m
	return agentruntime.Result{Text: "Noted"}, nil
}
func (*memorySession) Abort(context.Context) error { return nil }
func (*memorySession) Close() error                { return nil }
func (*memorySession) Path() string                { return "memory.jsonl" }

func TestMemoryRankingIgnoresOpenWork(t *testing.T) {
	f := &memoryFactory{received: make(chan contract.Message, 1)}
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}}}, Factory: f})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: "schedule", Text: "University brief at 8:20am SGT " + strings.Repeat("schedule ", 440)}); err != nil {
		t.Fatal(err)
	}
	distractions := "orchard meadow valley river forest ocean mountain garden desert island beach cliff waterfall stream plateau marsh prairie canyon glacier tundra "
	for i := range 12 {
		if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: string(rune('a' + i)), Text: strings.Repeat(distractions, 24)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := api.Store.SaveWork(t.Context(), store.NewRun("open", "assistant", "assistant", "message"), store.WorkItem{ID: "open", Text: distractions, State: "open"}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages", strings.NewReader(`{"id":"ranking","text":"University","attachmentIDs":[]}`)))
	if recorder.Code != http.StatusAccepted {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	received := <-f.received
	text := store.Text(received)
	if !strings.Contains(text, "Open work") || !strings.Contains(text, distractions) {
		t.Fatal("production request omitted the open work fixture")
	}
	if !strings.Contains(text, "University brief at 8:20am SGT") {
		t.Fatal("open work displaced memory relevant to the current request")
	}
	_, data, ok := strings.Cut(*received.Parts[0].Text, "\n")
	var selected []memory.Document
	if !ok || json.Unmarshal([]byte(data), &selected) != nil || len(selected) == 0 || selected[0].Name != "schedule" {
		t.Fatal("current request did not rank its memory first")
	}
}
