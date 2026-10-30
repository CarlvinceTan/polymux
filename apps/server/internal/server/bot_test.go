package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
)

func botCall(ctx context.Context, d agentruntime.Definition, name, args string) (agent.AgentToolResult, error) {
	for _, tool := range d.ExtraTools {
		if tool.Name() == name {
			return tool.Execute(ctx, name, json.RawMessage(args), nil)
		}
	}
	return agent.AgentToolResult{}, fmt.Errorf("missing tool %s", name)
}

func botSend(t *testing.T, api *Server, thread, id, text string) {
	t.Helper()
	body, err := json.Marshal(contract.SendRequest{ID: id, Text: text, AttachmentIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/"+thread+"/messages", strings.NewReader(string(body))))
	if r.Code != http.StatusAccepted {
		t.Fatal(r.Code, r.Body.String())
	}
}

func botReply(t *testing.T, api *Server, thread, text string) []contract.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := api.Store.Messages(t.Context(), thread, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range messages {
			if m.Status == "done" && store.Text(m) == text {
				return messages
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not receive %q", thread, text)
	return nil
}

func TestChatQuestions(t *testing.T) {
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, m contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		if strings.HasPrefix(state.work.Message.ID, "answer:") {
			return agentruntime.Result{Text: "Proceeding with " + store.Text(state.work.Message)}, nil
		}
		if _, err := botCall(ctx, d, "send_to_user", `{"text":"Checking the alternatives"}`); err != nil {
			return agentruntime.Result{}, err
		}
		_, err := botCall(ctx, d, "ask_user", `{"question":"Which approach?","options":[{"id":"first","label":"First"},{"id":"second","label":"Second"}],"allow_text":true}`)
		return agentruntime.Result{Text: "NO_RESPONSE"}, err
	}}
	api, _ := startTest(t, f)
	botSend(t, api, "assistant", "question-turn", "Choose an approach")
	run := awaitRun(t, api.Store, store.NewRun("question-turn", "assistant", "assistant", "message").ID, "completed")
	if run.Outcome == nil || *run.Outcome != "silent" || run.Error != nil {
		t.Fatal("silent turn failed", run)
	}
	messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil || len(messages) != 3 || store.Text(messages[1]) != "Checking the alternatives" || messages[2].Parts[0].Kind != "choice" {
		t.Fatal("missing immediate chat posts or leaked silent response", messages, err)
	}
	card := messages[2]
	for range 2 {
		r := httptest.NewRecorder()
		api.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/threads/assistant/messages/"+card.ID+"/answer", strings.NewReader(`{"optionID":"second"}`)))
		if r.Code != http.StatusOK {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	botReply(t, api, "assistant", "Proceeding with Second")
	var count int
	if err := api.Store.DB.QueryRow(`SELECT count(*) FROM jobs WHERE id=?`, "answer:"+card.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("answer did not queue exactly one turn", count, err)
	}
}

func TestProgressOrder(t *testing.T) {
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		_, err := botCall(ctx, d, "send_to_user", `{"text":"Working"}`)
		return agentruntime.Result{Text: "Finished"}, err
	}}
	api, _ := startTest(t, f)
	botSend(t, api, "assistant", "progress", "Do the work")
	messages := botReply(t, api, "assistant", "Finished")
	if len(messages) != 3 || store.Text(messages[1]) != "Working" || store.Text(messages[2]) != "Finished" {
		t.Fatal("final reply appeared before progress", messages)
	}
}

func TestNestedBots(t *testing.T) {
	var mu sync.Mutex
	var turns []string
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		id, trigger := d.Agent.ID, state.work.Trigger
		mu.Lock()
		turns = append(turns, id+":"+trigger)
		mu.Unlock()
		if trigger == "follow_up" {
			if id == "b" && !strings.Contains(store.Text(state.work.Message), "C result") || id == "a" && !strings.Contains(store.Text(state.work.Message), "B synthesis") {
				return agentruntime.Result{}, fmt.Errorf("%s did not receive its delegated result", id)
			}
			if id == "b" {
				return agentruntime.Result{Text: "B synthesis"}, nil
			}
			return agentruntime.Result{Text: "A final"}, nil
		}
		if id == "c" {
			return agentruntime.Result{Text: "C result"}, nil
		}
		target := "b"
		if id == "b" {
			target = "c"
		}
		_, err := botCall(ctx, d, "message_agent", `{"agent_id":"`+target+`","message":"Please research this","intent":"request"}`)
		return agentruntime.Result{Text: "Consulting " + target}, err
	}}
	api, _ := voiceTest(t, f, func(c *Config) {
		c.Definitions = append(c.Definitions, agentruntime.Definition{Agent: contract.Agent{ID: "c", Name: "C"}})
	})
	botSend(t, api, "a", "nested", "Research with the team")
	messages := botReply(t, api, "a", "A final")
	mu.Lock()
	got := slices.Clone(turns)
	mu.Unlock()
	if !slices.Equal(got, []string{"a:message", "b:peer", "c:peer", "b:follow_up", "a:follow_up"}) {
		t.Fatal("delegation did not return through its senders", got)
	}
	var pending int
	if err := api.Store.DB.QueryRow(`SELECT count(*) FROM deliveries WHERE json_extract(data,'$.status')='queued'`).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("delegation left an unfinished receipt", pending, err)
	}
	for _, id := range []string{"a", "b"} {
		history, err := api.Store.Messages(t.Context(), id, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(history, func(m contract.Message) bool { return strings.HasPrefix(m.ID, "sent:delivery:") }) {
			t.Fatal("outgoing peer message absent from chat", id, messages)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := map[string]int{}
	for _, opened := range f.opened {
		counts[opened.Agent.ID]++
		if opened.Scope != "" || opened.Automatic || opened.Ephemeral {
			t.Fatal("peer or follow-up used an isolated conversation", opened.Scope)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if counts[id] != 1 {
			t.Fatal("delegated results did not reuse the main session", id, counts[id])
		}
	}
}

func TestBotManagement(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state := &turnState{run: contract.Run{ID: "management", AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	if _, err := (&agentTool{server: api, action: "update"}).Execute(ctx, "rename", json.RawMessage(`{"name":"Coach","role":"Study guide","prompt":"Explain one step at a time","color":"bleu"}`), nil); err != nil {
		t.Fatal(err)
	}
	profile, err := api.Store.Profile(t.Context(), "assistant")
	if err != nil || profile.Agent.Name != "Coach" || profile.Agent.Role != "Study guide" || profile.Prompt != "Explain one step at a time" || profile.Agent.Avatar == nil || profile.Agent.Avatar.Color != "bleu" {
		t.Fatal("self configuration was not persisted", profile, err)
	}
	created, err := (&agentTool{server: api, action: "create"}).Execute(ctx, "hire", json.RawMessage(`{"role":"Researcher","prompt":"Find primary sources","initial_task":"Find the reference"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	var bot contract.Agent
	if err := json.Unmarshal([]byte(created.Text()), &bot); err != nil || bot.ID == "" {
		t.Fatal("blank-name hire failed", bot, err)
	}
	assertName(t, bot.Name)
	botReply(t, api, bot.ID, "Hello there")
	awaitRun(t, api.Store, store.NewRun("initial:"+bot.ID, bot.ID, bot.ID, "message").ID, "completed")
	if _, err := (&agentTool{server: api, action: "delete"}).Execute(ctx, "remove", json.RawMessage(`{}`), nil); err == nil {
		t.Fatal("deletion accepted a missing target")
	}
}

func TestGroupRevisit(t *testing.T) {
	var mu sync.Mutex
	var speakers []string
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		mu.Lock()
		speakers = append(speakers, d.Agent.ID)
		mu.Unlock()
		if state.work.FromAgentID != "" {
			if _, err := botCall(ctx, d, "handoff_agent", `{"agent_id":"`+state.work.FromAgentID+`","message":"Bounce"}`); err == nil {
				return agentruntime.Result{}, fmt.Errorf("immediate bounce accepted")
			}
		}
		next := map[string]string{"a": "b", "b": "c", "c": "a"}[d.Agent.ID]
		_, err := botCall(ctx, d, "handoff_agent", `{"agent_id":"`+next+`","message":"Next stage"}`)
		if state.work.Hops == 6 {
			if err == nil {
				return agentruntime.Result{}, fmt.Errorf("seventh hop accepted")
			}
			return agentruntime.Result{Text: "Group finished"}, nil
		}
		return agentruntime.Result{Text: "Passing the task"}, err
	}}
	api, _ := voiceTest(t, f, func(c *Config) {
		c.Definitions = append(c.Definitions, agentruntime.Definition{Agent: contract.Agent{ID: "c", Name: "C"}})
	})
	group, err := api.newGroup(t.Context(), contract.CreateThreadRequest{AgentIDs: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	botSend(t, api, group.ID, "group-cycle", "Work through this together")
	messages := botReply(t, api, group.ID, "Group finished")
	mu.Lock()
	got := slices.Clone(speakers)
	mu.Unlock()
	if !slices.Equal(got, []string{"a", "b", "c", "a", "b", "c", "a"}) {
		t.Fatal("group could not revisit a member", got)
	}
	for _, message := range messages {
		if message.Role == "assistant" && (message.ReplyToID == nil || *message.ReplyToID != "group-cycle") {
			t.Fatal("handoff reply lost the original user message", message)
		}
	}
}

func TestBotRemoval(t *testing.T) {
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		_, err := botCall(ctx, d, "delete_agent", `{"agent_id":"b"}`)
		return agentruntime.Result{Text: "Removed B"}, err
	}}
	api, _ := voiceTest(t, f)
	botSend(t, api, "a", "remove", "Remove B")
	botReply(t, api, "a", "Removed B")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := api.Store.Profile(t.Context(), "b"); errors.Is(err, sql.ErrNoRows) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bot removal did not finish after the reply")
}
