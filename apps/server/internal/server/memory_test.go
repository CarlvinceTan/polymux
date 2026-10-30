package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/packages/memory"
	"github.com/MichaelKinsy/PiG/agent"
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
	for _, trigger := range []string{"created"} {
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

func TestSharedMemory(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &memoryTool{store: api.Store, provider: api.memory, action: "save"}
	writer := &turnState{run: contract.Run{ID: "run", AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, writer)
	if _, err := tool.Execute(ctx, "save", json.RawMessage(`{"name":"timezone","text":"User lives in Sydney","revision":"","shared":true}`), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, "save", json.RawMessage(`{"name":"draft","text":"Private draft notes","revision":""}`), nil); err != nil {
		t.Fatal(err)
	}
	reader := contract.Run{ID: "other", AgentID: "researcher", ConversationID: "group"}
	message, err := api.memoryContext(t.Context(), reader, contract.Message{}, "")
	if err != nil || !strings.Contains(store.Text(message), "User lives in Sydney") || strings.Contains(store.Text(message), "Private draft") {
		t.Fatalf("shared memory not isolated from private memory: %q %v", store.Text(message), err)
	}
	tool.action = "recall"
	result, err := tool.Execute(ctx, "recall", json.RawMessage(`{"shared":true}`), nil)
	data, _ := json.Marshal(result)
	if err != nil || !strings.Contains(string(data), "Sydney") || strings.Contains(string(data), "Private draft") {
		t.Fatalf("shared recall returned the wrong scope: %s %v", data, err)
	}
	if strings.Contains(store.Text(message), `"agent":"assistant"`) || !strings.Contains(store.Text(message), `"agent":"`) {
		t.Fatalf("shared fact not tagged with the bot's name: %q", store.Text(message))
	}
	documents, err := api.memory.List(t.Context(), "assistant", sharedScope)
	if err != nil || len(documents) != 1 {
		t.Fatal("shared fact not in its writer's shard", documents, err)
	}
	if err := api.Store.SaveDefinition(t.Context(), contract.Agent{ID: "researcher", Name: "Researcher"}, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	other := &turnState{run: contract.Run{ID: "run2", AgentID: "researcher", ConversationID: "researcher"}, work: store.Work{Trigger: "message"}}
	ctx = context.WithValue(t.Context(), turnKey{}, other)
	for _, action := range []string{"save", "forget"} {
		tool.action = action
		args := `{"name":"timezone","text":"User lives in Perth","revision":"` + documents[0].Revision + `","shared":true}`
		if _, err := tool.Execute(ctx, action, json.RawMessage(args), nil); !errors.Is(err, errOwned) {
			t.Fatal("another bot changed a shared fact", action, err)
		}
	}
	deleteRequest(t, api, "assistant", 200)
	message, err = api.memoryContext(t.Context(), other.run, contract.Message{}, "")
	if err != nil || strings.Contains(store.Text(message), "Sydney") {
		t.Fatal("deleted bot's shared facts remain", err)
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

func TestFreshMemoryStatesFailure(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	run := contract.Run{ID: "run", AgentID: "assistant", ConversationID: "assistant"}
	if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: "tea", Text: "Prefers green tea."}); err != nil {
		t.Fatal(err)
	}
	build := api.freshMemory(run, "tea")
	texts, err := build(t.Context())
	if err != nil || len(texts) != 1 || !strings.Contains(texts[0], "Prefers green tea.") {
		t.Fatalf("fresh memory = %q, %v", texts, err)
	}
	if err := api.memory.Close(); err != nil {
		t.Fatal(err)
	}
	if texts, err := build(t.Context()); err != nil || len(texts) != 1 || texts[0] != memoryUnavailable {
		t.Fatalf("a failed read must say so, not look empty: %q, %v", texts, err)
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
	if _, err := tool.Execute(ctx, "history", json.RawMessage(`{}`), nil); err != nil {
		t.Fatal("peer could not retrieve its own history", err)
	}
}

func TestHistoryMatchesTail(t *testing.T) {
	for _, test := range []struct{ name, prefix string }{
		{name: "prose", prefix: strings.Repeat("Earlier conversation details. ", 300)},
		{name: "long token", prefix: strings.Repeat("x", 9000) + " "},
		{name: "unicode token", prefix: strings.Repeat("界", 3000) + " "},
	} {
		t.Run(test.name, func(t *testing.T) {
			api, _ := startTest(t, &testFactory{})
			text := test.prefix + "The flight is SQ322 on Friday."
			old := contract.Message{
				ID: "long", ThreadID: "assistant", Role: "assistant", Status: "done",
				Parts: []contract.Part{{Kind: "text", Text: &text}},
			}
			if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "assistant", Type: "message.start"}, &old); err != nil {
				t.Fatal(err)
			}
			if _, err := api.Store.DB.ExecContext(t.Context(), `INSERT INTO messages_fts(message_id,thread_id,body) VALUES(?,?,?)`, old.ID, old.ThreadID, text); err != nil {
				t.Fatal(err)
			}
			state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
			ctx := context.WithValue(t.Context(), turnKey{}, state)
			result, err := (&historyTool{store: api.Store}).Execute(ctx, "history", json.RawMessage(`{"query":"SQ322"}`), nil)
			if err != nil {
				t.Fatal(err)
			}
			var output struct{ Messages []struct{ Text string } }
			if err := json.Unmarshal([]byte(result.Text()), &output); err != nil {
				t.Fatal(err)
			}
			if len(output.Messages) != 1 {
				t.Fatal("missing history match", output)
			}
			excerpt := output.Messages[0].Text
			if !strings.Contains(excerpt, "The flight is SQ322 on Friday.") {
				t.Fatal("history match omitted the evidence beyond the initial text bound")
			}
			if len(excerpt) > 4096 || !utf8.ValidString(excerpt) {
				t.Fatal("history match exceeded its valid UTF-8 text budget", len(excerpt))
			}
			if strings.ContainsAny(excerpt, "\x01\x02") {
				t.Fatal("history match exposed highlight markers")
			}
		})
	}
}

func TestHistoryExcerptBudget(t *testing.T) {
	text := strings.Repeat("界", 3000) + "\x01SQ322\x02 on Friday."
	for _, limit := range []int{1, 2, 3, 16, 4096} {
		excerpt := historyExcerpt(text, limit)
		if len(excerpt) > limit || !utf8.ValidString(excerpt) || strings.ContainsAny(excerpt, "\x01\x02") {
			t.Fatalf("invalid excerpt with a %d byte budget: %q", limit, excerpt)
		}
		if limit >= 16 && !strings.Contains(excerpt, "SQ322") {
			t.Fatal("bounded excerpt omitted its match", limit)
		}
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

func (s *memorySession) Send(ctx context.Context, m contract.Message, _ func(agentruntime.Delta) error) (agentruntime.Result, error) {
	// The durable runtime adds this to each model request.
	fresh, err := agentruntime.Fresh(ctx)
	if err != nil {
		return agentruntime.Result{}, err
	}
	for _, text := range slices.Backward(fresh) {
		m.Parts = append([]contract.Part{{Kind: contract.PartKindText, Text: &text}}, m.Parts...)
	}
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

func TestMemoryTiers(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	for _, d := range []memory.Document{
		{Name: "trip", Text: "Planning a trip to Tokyo"},
		{Name: "snack", Text: "Liked the almonds", Tier: "note"},
		{Name: "name", Text: "User is called Sam", Tier: "profile"},
	} {
		if _, err := api.memory.Save(t.Context(), "assistant", "assistant", d); err != nil {
			t.Fatal(err)
		}
	}
	documents, err := api.memory.List(t.Context(), "assistant", "assistant")
	if err != nil {
		t.Fatal(err)
	}
	ordered := tierMemory(documents, time.Now())
	if ordered[0].Name != "name" || ordered[len(ordered)-1].Name != "snack" {
		t.Fatal("profile not first or note not last", ordered)
	}
	if faded := tierMemory(documents, time.Now().Add(noteFade+time.Hour)); slices.ContainsFunc(faded, func(d memory.Document) bool { return d.Name == "snack" }) {
		t.Fatal("old note stayed in the turn context")
	}
	trip := documents[slices.IndexFunc(documents, func(d memory.Document) bool { return d.Name == "trip" })]
	trip.Tier = "profile"
	if saved, err := api.memory.Save(t.Context(), "assistant", "assistant", trip); err != nil || saved.Tier != "profile" {
		t.Fatal("same text could not change tier", saved, err)
	}
	if _, err := api.memory.Save(t.Context(), "assistant", "assistant", memory.Document{Name: "x", Text: "y", Tier: "forever"}); err == nil {
		t.Fatal("unknown tier accepted")
	}
}

func TestProjectMemory(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	for _, id := range []string{"researcher", "outsider"} {
		if err := api.Store.SaveDefinition(t.Context(), contract.Agent{ID: id, Name: strings.ToUpper(id[:1]) + id[1:]}, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	turn := func(agent string) context.Context {
		return context.WithValue(t.Context(), turnKey{}, &turnState{run: contract.Run{ID: "run-" + agent, AgentID: agent, ConversationID: agent}, work: store.Work{Trigger: "message"}})
	}
	projects := &projectTool{store: api.Store}
	call := func(ctx context.Context, tool interface {
		Execute(context.Context, string, json.RawMessage, agent.ToolUpdateCallback) (agent.AgentToolResult, error)
	}, args string) error {
		_, err := tool.Execute(ctx, "call", json.RawMessage(args), nil)
		return err
	}
	if err := call(turn("assistant"), projects, `{"action":"create","slug":"launch","name":"Launch"}`); err != nil {
		t.Fatal(err)
	}
	if err := call(turn("researcher"), projects, `{"action":"join","slug":"launch"}`); err != nil {
		t.Fatal(err)
	}
	save := &memoryTool{store: api.Store, provider: api.memory, action: "save"}
	if err := call(turn("outsider"), save, `{"name":"date","text":"Launch is in May","revision":"","project":"launch"}`); err == nil {
		t.Fatal("non-member wrote project memory")
	}
	if err := call(turn("assistant"), save, `{"name":"date","text":"Launch is in May","revision":"","project":"launch"}`); err != nil {
		t.Fatal(err)
	}
	seen := func(agent string) string {
		message, err := api.memoryContext(t.Context(), contract.Run{AgentID: agent, ConversationID: agent}, contract.Message{}, "")
		if err != nil {
			t.Fatal(err)
		}
		return store.Text(message)
	}
	if !strings.Contains(seen("researcher"), "Launch is in May") || strings.Contains(seen("outsider"), "Launch is in May") {
		t.Fatal("project memory reached the wrong bots")
	}
	documents, err := api.memory.List(t.Context(), "assistant", projectScope("launch"))
	if err != nil || len(documents) != 1 {
		t.Fatal(documents, err)
	}
	if err := call(turn("researcher"), save, `{"name":"date","text":"Launch is in June","revision":"`+documents[0].Revision+`","project":"launch"}`); !errors.Is(err, errOwned) {
		t.Fatal("member changed another bot's project fact", err)
	}
	if err := call(turn("researcher"), projects, `{"action":"leave","slug":"launch"}`); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seen("researcher"), "Launch is in May") {
		t.Fatal("left project still in context")
	}
}
