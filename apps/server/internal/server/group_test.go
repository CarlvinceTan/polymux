package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestGroupTargets(t *testing.T) {
	members := []contract.Agent{{ID: "ann", Name: "Ann"}, {ID: "anna", Name: "Anna"}, {ID: "writer", Name: "Research Writer"}, {ID: "edit", Name: "Éditeur"}}
	for _, test := range []struct {
		name, text     string
		explicit, want []string
	}{
		{"default", "Hello team", nil, []string{"ann"}},
		{"prefix", "Ask @Anna to review", nil, []string{"anna"}},
		{"email", "contact@everyone name@Anna", nil, []string{"ann"}},
		{"everyone", "@everyone review", nil, []string{"ann", "anna", "writer", "edit"}},
		{"unicode", "@Research Writer ask @ÉDITEUR", nil, []string{"writer", "edit"}},
		{"explicit", "Hello", []string{"edit", "outsider", "edit"}, []string{"edit"}},
		{"combined", "Ask @Ann", []string{"edit"}, []string{"edit", "ann"}},
		{"suffix", "Ask @Ann-more", nil, []string{"ann"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := groupTargets(test.text, members, test.explicit, true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("targets %v want %v", got, test.want)
			}
		})
	}
	if got := groupTargets("No handoff", members, nil, false); len(got) != 0 {
		t.Fatal("unmentioned reply caused an autonomous handoff")
	}
}

func TestBoundedNeverSplitsRunes(t *testing.T) {
	for _, test := range []struct {
		name  string
		text  string
		limit int
		want  int
	}{
		{"under limit untouched", "short", 16, 5},
		{"exact limit untouched", "exact", 5, 5},
		{"ascii clipped to limit", "abcdefgh", 4, 4},
		{"empty text", "", 8, 0},
		{"multibyte keeps whole runes", strings.Repeat("界", 8), 5, 3},
		{"multibyte lands on a boundary", strings.Repeat("界", 4) + "tail", 9, 9},
		{"multibyte splits mid-rune", strings.Repeat("界", 4) + "tail", 10, 9},
		{"emoji splits mid-rune", "🙂🙂🙂", 5, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := bounded(test.text, test.limit)
			if len(got) != test.want || !utf8.ValidString(got) {
				t.Fatalf("bounded(%d) = %q (%d bytes)", test.limit, got, len(got))
			}
			if got != test.text && !strings.HasPrefix(test.text, got) {
				t.Fatalf("clipped text is not a prefix: %q", got)
			}
		})
	}
}

func TestGroupPickerNames(t *testing.T) {
	members := []contract.Agent{{ID: "first", Name: "Éditeur"}, {ID: "second", Name: " éditeur "}, {ID: "writer", Name: "Research Writer"}}
	for _, test := range []struct {
		name, text     string
		explicit, want []string
	}{
		{"picked", "@ÉDITEUR review", []string{"second"}, []string{"second"}},
		{"typed", "@Éditeur review", nil, nil},
		{"other-name", "@Éditeur ask @Research Writer", []string{"second"}, []string{"second", "writer"}},
		{"both-picked", "@Éditeur review", []string{"second", "first"}, []string{"second", "first"}},
		{"everyone", "@everyone review", []string{"second"}, []string{"second", "first", "writer"}},
		{"outsider", "@Éditeur review", []string{"outsider"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := groupTargets(test.text, members, test.explicit, true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("targets %v want %v", got, test.want)
			}
		})
	}
	if got := groupTargets("@Éditeur take over", members, nil, false); len(got) != 0 {
		t.Fatalf("assistant handoff routing changed: %v", got)
	}
}

func TestGroupMentionSpans(t *testing.T) {
	members := []contract.Agent{{ID: "short", Name: "Alex"}, {ID: "long", Name: "Alex Smith"}, {ID: "same", Name: "alex smith"}, {ID: "unicode", Name: "Éditeur Principal"}}
	for _, test := range []struct {
		name, text     string
		explicit, want []string
	}{
		{"picked-long", "@Alex Smith review", []string{"long"}, []string{"long"}},
		{"typed-long", "@Alex Smith review", nil, nil},
		{"separate-short", "@Alex Smith ask @Alex", []string{"long"}, []string{"long", "short"}},
		{"typed-separate", "@Alex and @Alex Smith", nil, []string{"short"}},
		{"unicode", "@ÉDITEUR Principal ask @Alex Smith", nil, []string{"unicode"}},
		{"email", "person@Alex Smith", nil, []string{"short"}},
		{"everyone", "@everyone review", []string{"long"}, []string{"long", "short", "same", "unicode"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := groupTargets(test.text, members, test.explicit, true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("targets %v want %v", got, test.want)
			}
		})
	}
	if got := groupTargets("@Alex Smith take over", members, nil, false); len(got) != 0 {
		t.Fatalf("handoff matched a shorter-name prefix: %v", got)
	}
	reserved := append(slices.Clone(members), contract.Agent{ID: "reserved", Name: "Everyone Else"})
	if got := groupTargets("@everyone else, review", reserved, nil, true); len(got) != len(reserved) {
		t.Fatalf("member name shadowed @everyone: %v", got)
	}
}

type groupSend struct {
	definition agentruntime.Definition
	message    contract.Message
}
type groupFactory struct {
	mu      sync.Mutex
	opened  []openedAgent
	sent    []groupSend
	block   bool
	started chan struct{}
	holdID  string
	release chan struct{}
	closed  []string
}

func (f *groupFactory) Open(_ context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	f.mu.Lock()
	f.opened = append(f.opened, openedAgent{definition: d, path: path})
	f.mu.Unlock()
	return &groupSession{factory: f, definition: d}, nil
}

type groupSession struct {
	factory    *groupFactory
	definition agentruntime.Definition
}

func (s *groupSession) Send(ctx context.Context, m contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	s.factory.mu.Lock()
	s.factory.sent = append(s.factory.sent, groupSend{s.definition, m})
	s.factory.mu.Unlock()
	text := "Reply from " + s.definition.Agent.ID
	prompt := ""
	if len(m.Parts) > 0 && m.Parts[len(m.Parts)-1].Text != nil {
		prompt = *m.Parts[len(m.Parts)-1].Text
	}
	if s.factory.holdID == m.ID {
		select {
		case s.factory.started <- struct{}{}:
		default:
		}
		select {
		case <-s.factory.release:
		case <-ctx.Done():
			return agentruntime.Result{}, ctx.Err()
		}
	}
	if s.factory.block {
		select {
		case s.factory.started <- struct{}{}:
		default:
		}
		if err := emit(agentruntime.Delta{Text: text}); err != nil {
			return agentruntime.Result{}, err
		}
		<-ctx.Done()
		return agentruntime.Result{}, ctx.Err()
	}
	if s.definition.Agent.ID == "writer" && strings.Contains(prompt, "handoff please") {
		for _, tool := range s.definition.ExtraTools {
			if tool.Name() != "handoff_agent" {
				continue
			}
			if err := emit(agentruntime.Delta{Kind: "tool.start", Tool: "handoff_agent", ToolID: "handoff"}); err != nil {
				return agentruntime.Result{}, err
			}
			if _, err := tool.Execute(ctx, "handoff", json.RawMessage(`{"agent_id":"edit","message":"Review the request."}`), nil); err != nil {
				return agentruntime.Result{}, err
			}
			if err := emit(agentruntime.Delta{Kind: "tool.done", Tool: "handoff_agent", ToolID: "handoff"}); err != nil {
				return agentruntime.Result{}, err
			}
		}
		text = "Passing to the editor."
	}
	if s.definition.Agent.ID == "edit" && strings.Contains(prompt, "handoff please") {
		text = "@Research Writer take over."
	}
	if err := emit(agentruntime.Delta{Text: text}); err != nil {
		return agentruntime.Result{}, err
	}
	return agentruntime.Result{Text: text}, nil
}
func (s *groupSession) Path() string {
	return filepath.Join(s.definition.Scope, s.definition.Agent.ID, "session.jsonl")
}
func (s *groupSession) Close() error {
	s.factory.mu.Lock()
	s.factory.closed = append(s.factory.closed, s.definition.Agent.ID)
	s.factory.mu.Unlock()
	return nil
}
func (*groupSession) Abort(context.Context) error { return nil }

func TestGroupHistory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db")
	definitions := []agentruntime.Definition{{Agent: contract.Agent{ID: "writer", Name: "Research Writer"}, Prompt: "Writer instructions"}, {Agent: contract.Agent{ID: "edit", Name: "Éditeur"}, Prompt: "Editor instructions"}, {Agent: contract.Agent{ID: "ann", Name: "Ann"}, Prompt: "Ann instructions"}}
	config := Config{DataDir: dir, Definitions: definitions, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	open := func() (*Server, *httptest.Server, *groupFactory) {
		t.Helper()
		db, err := store.Open(t.Context(), dbPath)
		if err != nil {
			t.Fatal(err)
		}
		f := &groupFactory{}
		config.Factory = f
		api, err := New(t.Context(), db, config)
		if err != nil {
			t.Fatal(err)
		}
		return api, httptest.NewServer(api), f
	}
	api, web, factory := open()
	create, err := http.Post(web.URL+"/threads", "application/json", strings.NewReader(`{"agentIDs":["writer","edit","ann"],"title":"Trip"}`))
	if err != nil {
		t.Fatal(err)
	}
	var thread contract.Thread
	err = json.NewDecoder(create.Body).Decode(&thread)
	create.Body.Close()
	if err != nil || create.StatusCode != 201 || thread.ID == "writer" || thread.Title != "Trip" || !reflect.DeepEqual(thread.AgentIDs, []string{"writer", "edit", "ann"}) {
		t.Fatalf("group create %+v %v", thread, err)
	}
	send := func(id, text string, mentions []string, want []string) {
		t.Helper()
		data, _ := json.Marshal(contract.SendRequest{ID: id, Text: text, AttachmentIDs: []string{}, Mentions: mentions})
		response, err := http.Post(web.URL+"/threads/"+thread.ID+"/messages", "application/json", strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 202 {
			t.Fatalf("group send %d", response.StatusCode)
		}
		deadline := time.Now().Add(4 * time.Second)
		settled := false
		for time.Now().Before(deadline) {
			var state string
			err := api.Store.DB.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, id).Scan(&state)
			if err == nil && state == "done" {
				settled = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !settled {
			t.Fatal("group turn did not settle")
		}
		messages, err := api.Store.Messages(t.Context(), thread.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		var authors []string
		for _, message := range messages {
			if message.Role == "assistant" && message.ReplyToID != nil && *message.ReplyToID == id {
				if message.Status != "done" || message.AgentID == nil {
					t.Fatalf("group attribution %+v", message)
				}
				authors = append(authors, *message.AgentID)
			}
		}
		if !reflect.DeepEqual(authors, want) {
			t.Fatalf("authors %v want %v", authors, want)
		}
	}
	send("first", "Hello team", nil, []string{"writer"})
	send("second", "@Éditeur review this", nil, []string{"edit"})
	send("third", "@everyone compare", nil, []string{"writer", "edit", "ann"})
	send("fourth", "handoff please", nil, []string{"writer", "edit"})
	send("fifth", "Selected member", []string{"ann"}, []string{"ann"})
	for _, definition := range definitions {
		messages, err := api.Store.Messages(t.Context(), definition.Agent.ID, 0, 100)
		if err != nil || len(messages) != 0 {
			t.Fatalf("individual history was contaminated %s %+v %v", definition.Agent.ID, messages, err)
		}
		if _, err := api.Store.Session(t.Context(), definition.Agent.ID); err == nil {
			t.Fatal("group overwrote an individual session path")
		}
	}
	factory.mu.Lock()
	for _, opened := range factory.opened {
		if opened.definition.Scope != thread.ID || opened.definition.Prompt == "" {
			t.Fatalf("group runtime lost isolation/config %+v", opened)
		}
	}
	if len(factory.opened) != 3 {
		t.Fatalf("expected one cached session per group member, got %d", len(factory.opened))
	}
	context := store.Text(factory.sent[1].message)
	factory.mu.Unlock()
	if !strings.Contains(context, "Reply from writer") || !strings.Contains(context, "Writer instructions") && !strings.Contains(context, "Research Writer") {
		t.Fatalf("shared transcript/member names missing: %q", context)
	}
	current, _ := api.Store.Thread(t.Context(), thread.ID)
	if err := api.Store.Read(t.Context(), thread.ID, current.LastSeq); err != nil {
		t.Fatal(err)
	}
	web.Close()
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	api.Store.Close()
	api, web, factory = open()
	defer web.Close()
	defer api.Store.Close()
	defer api.Close()
	restored, err := api.Store.Thread(t.Context(), thread.ID)
	if err != nil || restored.LastReadSeq != current.LastSeq || restored.Title != "Trip" {
		t.Fatalf("group metadata lost on restart %+v %v", restored, err)
	}
	send("sixth", "@Éditeur after restart", nil, []string{"edit"})
	factory.mu.Lock()
	opened := factory.opened[0]
	factory.mu.Unlock()
	if opened.path != filepath.Join(thread.ID, "edit", "session.jsonl") {
		t.Fatalf("group session was not resumed %q", opened.path)
	}
	request, _ := http.NewRequest(http.MethodDelete, web.URL+"/agents/writer", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("delete group member %d", response.StatusCode)
	}
	restored, err = api.Store.Thread(t.Context(), thread.ID)
	if err != nil || restored.AgentID != "edit" || !reflect.DeepEqual(restored.AgentIDs, []string{"edit", "ann"}) {
		t.Fatalf("remaining group members/history were lost %+v %v", restored, err)
	}
	send("seventh", "Hello remaining team", nil, []string{"edit"})
	request, _ = http.NewRequest(http.MethodPatch, web.URL+"/threads/"+thread.ID, strings.NewReader(`{"title":"New trip"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("rename status %d", response.StatusCode)
	}
	restored, err = api.Store.Thread(t.Context(), thread.ID)
	if err != nil || restored.Title != "New trip" {
		t.Fatal("rename did not persist")
	}
}

func TestGroupStop(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f := &groupFactory{block: true, started: make(chan struct{}, 1)}
	api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}}}, Factory: f, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	web := httptest.NewServer(api)
	defer web.Close()
	response, err := http.Post(web.URL+"/threads", "application/json", strings.NewReader(`{"agentIDs":["a","b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var group contract.Thread
	json.NewDecoder(response.Body).Decode(&group)
	response.Body.Close()
	response, err = http.Post(web.URL+"/threads/"+group.ID+"/messages", "application/json", strings.NewReader(`{"id":"stopped","text":"@everyone hello","attachmentIDs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case <-f.started:
	case <-time.After(3 * time.Second):
		t.Fatal("group reply did not begin")
	}
	response, err = http.Post(web.URL+"/threads/"+group.ID+"/stop", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := db.Messages(t.Context(), group.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) == 2 && messages[1].Status == "error" {
			f.mu.Lock()
			sent := slices.Clone(f.sent)
			f.mu.Unlock()
			if len(sent) != 1 {
				t.Fatal("stop started a subsequent member reply")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("group stop did not persist its final error")
}

func TestGroupMembership(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db")
	definitions := []agentruntime.Definition{
		{Agent: contract.Agent{ID: "a", Name: "Alpha"}},
		{Agent: contract.Agent{ID: "b", Name: "Beta"}},
		{Agent: contract.Agent{ID: "c", Name: "Gamma"}},
	}
	factory := &groupFactory{holdID: "active", started: make(chan struct{}, 1), release: make(chan struct{})}
	open := func() (*Server, *httptest.Server) {
		t.Helper()
		db, err := store.Open(t.Context(), dbPath)
		if err != nil {
			t.Fatal(err)
		}
		api, err := New(t.Context(), db, Config{DataDir: dir, Definitions: definitions, Factory: factory, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			t.Fatal(err)
		}
		return api, httptest.NewServer(api)
	}
	api, web := open()
	defer func() { web.Close(); api.Close(); api.Store.Close() }()
	response, err := http.Post(web.URL+"/threads", "application/json", strings.NewReader(`{"agentIDs":["a","b"],"title":"Old"}`))
	if err != nil {
		t.Fatal(err)
	}
	var group contract.Thread
	err = json.NewDecoder(response.Body).Decode(&group)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusCreated {
		t.Fatalf("create group %+v %v", group, err)
	}
	patch := func(id, body string, status int) contract.Thread {
		t.Helper()
		request, _ := http.NewRequest(http.MethodPatch, web.URL+"/threads/"+id, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			data, _ := io.ReadAll(response.Body)
			t.Fatalf("PATCH status %d want %d: %s", response.StatusCode, status, data)
		}
		var thread contract.Thread
		if status == 200 {
			if err := json.NewDecoder(response.Body).Decode(&thread); err != nil {
				t.Fatal(err)
			}
		}
		return thread
	}
	send := func(id, text string, mentions []string) {
		t.Helper()
		data, _ := json.Marshal(contract.SendRequest{ID: id, Text: text, AttachmentIDs: []string{}, Mentions: mentions})
		response, err := http.Post(web.URL+"/threads/"+group.ID+"/messages", "application/json", strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("send status %d", response.StatusCode)
		}
	}
	settled := func(id string, want []string) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		var state string
		for time.Now().Before(deadline) {
			err := api.Store.DB.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, id).Scan(&state)
			if err == nil && state == "done" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if state != "done" {
			t.Fatal("group turn did not finish")
		}
		messages, err := api.Store.Messages(t.Context(), group.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		var authors []string
		for _, message := range messages {
			if message.Role == "assistant" && message.ReplyToID != nil && *message.ReplyToID == id {
				if message.AgentID == nil || message.Status != "done" {
					t.Fatalf("unfinished/anonymous reply %+v", message)
				}
				authors = append(authors, *message.AgentID)
			}
		}
		if !reflect.DeepEqual(authors, want) {
			t.Fatalf("turn %s authors %v want %v", id, authors, want)
		}
	}
	send("active", "@everyone hello", nil)
	select {
	case <-factory.started:
	case <-time.After(3 * time.Second):
		t.Fatal("active member reply did not start")
	}
	send("queued", "@everyone next", nil)
	send("old-mention", "@Beta review this", []string{"b"})
	send("named", "@Alpha review this", nil)
	profileRequest(t, api, http.MethodPatch, "/agents/a", `{"name":"Delta","role":"","prompt":""}`, 200)
	profileRequest(t, api, http.MethodPatch, "/agents/c", `{"name":"Alpha","role":"","prompt":""}`, 200)
	updated := patch(group.ID, `{"title":"Solo group","agentIDs":["c"]}`, 200)
	if updated.ID != group.ID || updated.AgentID != "c" || !reflect.DeepEqual(updated.AgentIDs, []string{"c"}) {
		t.Fatalf("one-member group lost identity %+v", updated)
	}
	close(factory.release)
	settled("active", []string{"a"})
	settled("queued", []string{"c"})
	settled("old-mention", nil)
	settled("named", nil)
	rejected := httptest.NewRecorder()
	api.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/threads/"+group.ID+"/messages", strings.NewReader(`{"id":"stale-selection","text":"@Alpha review this","attachmentIDs":[],"mentions":["a"]}`)))
	if rejected.Code != 400 || !strings.Contains(rejected.Body.String(), "membership") {
		t.Fatal("stale mention was silently retargeted", rejected.Code, rejected.Body.String())
	}
	factory.mu.Lock()
	closed := slices.Clone(factory.closed)
	factory.mu.Unlock()
	if !slices.Contains(closed, "a") {
		t.Fatal("removed member runtime was retained after the active turn")
	}
	updated = patch(group.ID, `{"title":"Back together","agentIDs":["b","c"]}`, 200)
	if updated.AgentID != "b" || !reflect.DeepEqual(updated.AgentIDs, []string{"b", "c"}) {
		t.Fatalf("rejoined order %+v", updated)
	}
	send("rejoined", "@everyone hello again", nil)
	settled("rejoined", []string{"b", "c"})
	send("new-first", "Hello", nil)
	settled("new-first", []string{"c"})
	updated = patch(group.ID, `{"title":"Renamed only"}`, 200)
	if !reflect.DeepEqual(updated.AgentIDs, []string{"b", "c"}) {
		t.Fatal("title-only update cleared members")
	}
	for _, test := range []struct{ name, id, body string }{
		{"empty", group.ID, `{"title":"Bad","agentIDs":[]}`},
		{"duplicate", group.ID, `{"title":"Bad","agentIDs":["b","b"]}`},
		{"unknown", group.ID, `{"title":"Bad","agentIDs":["missing"]}`},
		{"direct", "a", `{"title":"Bad","agentIDs":["b"]}`},
		{"title", group.ID, `{"title":" ","agentIDs":["a"]}`},
	} {
		t.Run(test.name, func(t *testing.T) { patch(test.id, test.body, 400) })
	}
	before, _ := api.Store.Events(t.Context(), group.ID, 0, 1000)
	web.Close()
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	api.Store.Close()
	factory = &groupFactory{}
	api, web = open()
	restored, err := api.Store.Thread(t.Context(), group.ID)
	if err != nil || restored.Title != "Renamed only" || restored.AgentID != "b" || !reflect.DeepEqual(restored.AgentIDs, []string{"b", "c"}) {
		t.Fatalf("membership did not survive restart %+v %v", restored, err)
	}
	after, err := api.Store.Events(t.Context(), group.ID, 0, 1000)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("membership/restart changed transcript replay %+v %v", after, err)
	}
	send("after-restart", "@everyone hello", nil)
	settled("after-restart", []string{"b", "c"})
	for _, definition := range definitions {
		if messages, err := api.Store.Messages(t.Context(), definition.Agent.ID, 0, 100); err != nil || len(messages) != 0 {
			t.Fatalf("membership touched individual history %+v %v", messages, err)
		}
	}
	if agents, err := api.Store.Agents(t.Context()); err != nil || len(agents) != 3 {
		t.Fatalf("membership removed global agents %+v %v", agents, err)
	}
}
