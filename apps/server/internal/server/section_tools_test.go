package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
)

func sectionCall(t *testing.T, api *Server, owner, name, args string) (agent.AgentToolResult, map[string]any) {
	t.Helper()
	state := &turnState{run: contract.Run{ID: "fixture-run", AgentID: owner, ConversationID: owner}, work: store.Work{Trigger: "message"}, store: api.Store}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	result, err := (&sectionTool{server: api, name: name}).Execute(ctx, "call", json.RawMessage(args), nil)
	if err != nil {
		t.Fatal(name, err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(result.Text()), &value); err != nil {
		t.Fatal(name, err)
	}
	return result, value
}

func TestSectionToolSequence(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	if err := api.Store.Register(t.Context(), []contract.Agent{{ID: "liam", Name: "Liam", Model: "retained/model"}, {ID: "other", Name: "Other"}}); err != nil {
		t.Fatal(err)
	}
	message := contract.Message{ID: "retained", ThreadID: "liam", Role: "assistant", Status: "done", Parts: []contract.Part{{Kind: "text", Text: new("Retained fixture message")}}}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "liam", Type: "message.done"}, &message); err != nil {
		t.Fatal(err)
	}
	before, err := api.Store.Thread(t.Context(), "liam")
	if err != nil {
		t.Fatal(err)
	}
	_, created := sectionCall(t, api, "assistant", "create_section", `{"name":"Career"}`)
	id := created["section_id"].(string)
	for _, item := range []struct{ name, args string }{
		{"move_chat", `{"chat_id":"liam","section_id":"` + id + `"}`},
		{"reorder", `{"kind":"chats","id":"other","before_id":"assistant"}`},
		{"rename_section", `{"section_id":"` + id + `","name":"Jobs"}`},
		{"delete_section", `{"section_id":"` + id + `"}`},
	} {
		result, value := sectionCall(t, api, "assistant", item.name, item.args)
		if result.IsError || value["changed"] != true {
			t.Fatal(item.name, value)
		}
	}
	after, err := api.Store.Thread(t.Context(), "liam")
	if err != nil || after.SectionID != nil || after.Title != before.Title || after.LastSeq != before.LastSeq || after.LastReadSeq != before.LastReadSeq || after.UnreadCount != before.UnreadCount || !slices.Equal(after.AgentIDs, before.AgentIDs) {
		t.Fatal("section sequence changed conversation data", err)
	}
	agents, err := api.Store.Agents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range agents {
		if value.ID == "liam" && value.Model != "retained/model" {
			t.Fatal("sidebar mutation changed model")
		}
	}
	events, err := api.Store.Events(t.Context(), "assistant", 0, 100)
	if err != nil || len(events) != 5 {
		t.Fatal("expected five activity rows", len(events), err)
	}
	labels := []string{"Created section 📁 Career", "Moved Liam to Career", "Reordered chats", "Renamed section 📁 Career → Jobs", "Deleted section 📁 Jobs · 1 chats returned"}
	for index, event := range events {
		if event.Type != "activity" || event.Summary == nil || *event.Summary != labels[index] || event.AgentID == nil || *event.AgentID != "assistant" {
			t.Fatal("activity order or actor mismatch", index)
		}
	}
	updates, err := api.Store.SidebarEvents(t.Context(), 0, 100)
	if err != nil || len(updates) != 5 || updates[1].Type != "sidebar.updated" || !slices.Equal(updates[1].ChangedThreadIDs, []string{"liam"}) || len(updates[4].Sections) != 0 || !slices.Equal(updates[4].ChangedThreadIDs, []string{"liam"}) {
		t.Fatal("expected five ordered layout events", len(updates), err)
	}
	var actor, snapshot, returned string
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT actor_id,section,returned_ids FROM sidebar_audit`).Scan(&actor, &snapshot, &returned); err != nil || actor != "assistant" || !strings.Contains(snapshot, `"name":"Jobs"`) || returned != `["liam"]` {
		t.Fatal("delete audit omitted actor or section evidence", err)
	}
	messages, err := api.Store.Messages(t.Context(), "liam", 0, 100)
	if err != nil || len(messages) != 1 || messages[0].ID != "retained" {
		t.Fatal("section tools changed existing messages", err)
	}
	_, retry := sectionCall(t, api, "assistant", "delete_section", `{"section_id":"`+id+`"}`)
	if retry["changed"] != false || retry["already_deleted"] != true {
		t.Fatal("repeat delete was not idempotent", retry)
	}
}

func TestSectionToolNoops(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	_, created := sectionCall(t, api, "assistant", "create_section", `{"name":"Career"}`)
	id := created["section_id"].(string)
	sectionCall(t, api, "assistant", "move_chat", `{"chat_id":"assistant","section_id":"`+id+`"}`)
	before, err := api.Store.Sidebar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, args string }{
		{"create_section", `{"name":"career"}`},
		{"rename_section", `{"section_id":"` + id + `","name":"Career"}`},
		{"move_chat", `{"chat_id":"assistant","section_id":"` + id + `"}`},
		{"reorder", `{"kind":"sections","id":"` + id + `"}`},
	} {
		result, value := sectionCall(t, api, "assistant", item.name, item.args)
		if result.IsError || value["changed"] != false {
			t.Fatal("no-op was counted as a mutation", item.name, value)
		}
	}
	after, err := api.Store.Sidebar(t.Context())
	if err != nil || after.Cursor != before.Cursor || !reflect.DeepEqual(after.Placements, before.Placements) {
		t.Fatal("no-op emitted an update", err)
	}
	sectionCall(t, api, "assistant", "pin_chat", `{"chat_id":"assistant"}`)
	for _, args := range []string{`{"chat_id":"assistant","section_id":"` + id + `"}`, `{"chat_id":"assistant","section_id":null}`} {
		result, value := sectionCall(t, api, "assistant", "move_chat", args)
		if !result.IsError || value["error"] != "chat_pinned" {
			t.Fatal("pinned chat moved without unpin", value)
		}
	}
	sectionCall(t, api, "assistant", "unpin_chat", `{"chat_id":"assistant"}`)
	sectionCall(t, api, "assistant", "delete_section", `{"section_id":"`+id+`"}`)
	result, value := sectionCall(t, api, "assistant", "move_chat", `{"chat_id":"assistant","section_id":"`+id+`"}`)
	if !result.IsError || value["error"] != "section_not_found" {
		t.Fatal("deleted section was accepted", value)
	}
}

func TestSectionToolRate(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	if err := api.Store.Register(t.Context(), []contract.Agent{{ID: "other", Name: "Other"}}); err != nil {
		t.Fatal(err)
	}
	for index := range 30 {
		name := "pin_chat"
		if index%2 == 1 {
			name = "unpin_chat"
		}
		result, value := sectionCall(t, api, "assistant", name, `{"chat_id":"assistant"}`)
		if result.IsError || value["changed"] != true {
			t.Fatal(index, value)
		}
	}
	before, err := api.Store.Sidebar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result, value := sectionCall(t, api, "assistant", "create_section", `{"name":"Blocked"}`)
	if !result.IsError || value["error"] != "rate_limit" {
		t.Fatal("mutation rate was not enforced across tools", value)
	}
	result, value = sectionCall(t, api, "assistant", "unpin_chat", `{"chat_id":"assistant"}`)
	if result.IsError || value["changed"] != false {
		t.Fatal("rate cap blocked harmless no-op", value)
	}
	after, err := api.Store.Sidebar(t.Context())
	if err != nil || before.Cursor != after.Cursor || len(after.Sections) != 0 {
		t.Fatal("rate rejection did not roll back", err)
	}
	result, value = sectionCall(t, api, "other", "create_section", `{"name":"Other agent"}`)
	if result.IsError || value["created"] != true {
		t.Fatal("one agent exhausted another agent's rate", value)
	}
	if _, _, err := api.Store.CreateSection(t.Context(), "user-section", "User section", nil); err != nil {
		t.Fatal("agent rate cap blocked native user", err)
	}
	if _, err := api.Store.DB.ExecContext(t.Context(), `UPDATE sidebar_events SET at=0 WHERE actor_id='assistant'`); err != nil {
		t.Fatal(err)
	}
	result, value = sectionCall(t, api, "assistant", "create_section", `{"name":"After window"}`)
	if result.IsError || value["created"] != true {
		t.Fatal("mutation rate did not reset", value)
	}
}

func TestSidebarCreation(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	if err := api.Store.Register(t.Context(), []contract.Agent{{ID: "other", Name: "Other"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Store.CreateSection(t.Context(), "career", "Career", nil); err != nil {
		t.Fatal(err)
	}
	call := func(path, body string, status int) []byte {
		t.Helper()
		response := httptest.NewRecorder()
		api.ServeHTTP(response, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if response.Code != status {
			t.Fatal(path, response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	// Create through the store first so its transaction can be checked without actors.
	thread, err := api.Store.CreateThread(t.Context(), "group", contract.CreateThreadRequest{AgentIDs: []string{"assistant", "other"}, SectionID: new("career")})
	if err != nil || thread.SectionID == nil || *thread.SectionID != "career" {
		t.Fatal("new chat lost section placement", err)
	}
	call("/threads", `{"agentIDs":["assistant"],"sectionID":"missing"}`, 404)
	call("/agents", `{"name":"Rejected","role":"","prompt":"","sectionID":"missing"}`, 404)
	var created contract.Agent
	if err := json.Unmarshal(call("/agents", `{"name":"Created","role":"","prompt":"","sectionID":"career"}`, 201), &created); err != nil {
		t.Fatal(err)
	}
	var group contract.Thread
	if err := json.Unmarshal(call("/threads", `{"agentIDs":["assistant","`+created.ID+`"],"sectionID":"career"}`, 201), &group); err != nil || group.SectionID == nil || *group.SectionID != "career" || group.SectionOrder == nil || *group.SectionOrder != 2 {
		t.Fatal("chat HTTP creation lost section placement", err)
	}
	value, err := api.Store.Thread(t.Context(), created.ID)
	if err != nil || value.SectionID == nil || *value.SectionID != "career" || value.SectionOrder == nil || *value.SectionOrder != 1 {
		t.Fatal("new agent did not append after existing source order", err)
	}
}

func TestSidebarStream(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state, err := api.Store.Sidebar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Store.CreateSection(t.Context(), "one", "One", nil); err != nil {
		t.Fatal(err)
	}
	updates, err := api.Store.SidebarEvents(t.Context(), state.Cursor, 100)
	if err != nil || len(updates) != 1 {
		t.Fatal("snapshot cursor missed a committed change", err)
	}
	web := httptest.NewServer(api)
	defer web.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", web.URL+fmt.Sprintf("/sidebar/stream?after=%d", state.Cursor), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", fmt.Sprint(updates[0].Seq))
	response, err := web.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, _, err := api.Store.CreateSection(t.Context(), "two", "Two", nil); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewScanner(response.Body)
	for reader.Scan() {
		line := reader.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event contract.SidebarEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.Seq != updates[0].Seq+1 || event.Type != "sidebar.updated" || len(event.Sections) != 2 {
			t.Fatal("SSE cursor replayed or skipped sidebar update")
		}
		return
	}
	t.Fatal("SSE did not deliver a live layout change", reader.Err())
}

func TestSectionToolPolicy(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	for _, trigger := range []string{"message", "routine", "peer"} {
		for _, tool := range api.sidebarTools() {
			state := &turnState{run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: trigger}, store: api.Store}
			ctx := context.WithValue(t.Context(), turnKey{}, state)
			if result := beforeTool(ctx, "call", tool.Name(), json.RawMessage(`{}`)); result.Block {
				t.Fatal("reversible sidebar tool was approval gated", trigger, tool.Name())
			}
		}
	}
	for _, tool := range api.sidebarTools() {
		state := &turnState{work: store.Work{Trigger: "review"}}
		ctx := context.WithValue(t.Context(), turnKey{}, state)
		if result := beforeTool(ctx, "call", tool.Name(), json.RawMessage(`{}`)); !result.Block {
			t.Fatal("review widened its tool scope", tool.Name())
		}
	}
	var count int
	if err := api.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM approvals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("sidebar tools created approval requests", count, err)
	}
}
