package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
)

// A refused call stops before it counts as work or leaves an effect behind.
func TestGateRefusesBeforeAnyEffect(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	state := &turnState{run: store.NewRun("gated", "assistant", "assistant", "message"), store: api.Store,
		external: map[string]bool{"bash": true}}
	state.gate = func(_ context.Context, tool string, _ json.RawMessage) plugins.Decision {
		return plugins.Decision{Allow: tool != "bash", Reason: "Denied"}
	}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	result := beforeTool(ctx, "call", "bash", json.RawMessage(`{}`))
	if !result.Block || result.Terminate || result.Reason != "Denied" || state.tools != 0 {
		t.Fatalf("%+v tools %d", result, state.tools)
	}
	if pending, err := api.Store.PendingEffects(t.Context(), state.run.ID); err != nil || pending != 0 {
		t.Fatalf("pending %d %v", pending, err)
	}
	if result := beforeTool(ctx, "call", "read", json.RawMessage(`{}`)); result.Block || state.tools != 1 {
		t.Fatalf("%+v tools %d", result, state.tools)
	}
}

// An ask rule posts a card, holds the call, and runs it only after the person allows it.
func TestApprovalWaitsForTheAnswer(t *testing.T) {
	api, server := startTest(t, &voiceFactory{})
	ctx := t.Context()
	if _, err := api.Store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO threads(id,agent_id,title) VALUES('assistant','assistant','Assistant')`); err != nil {
		t.Fatal(err)
	}
	run := store.NewRun("asked", "assistant", "assistant", contract.RunTriggerMessage)
	state := &turnState{run: run, store: api.Store, chats: []string{"assistant"}}
	state.gate = func(context.Context, string, json.RawMessage) plugins.Decision {
		return plugins.Decision{Ask: &plugins.Ask{Title: "Run this command?", Detail: "ls", Options: []plugins.AskOption{
			{ID: "allow_once", Label: "Allow once", Outcome: "allow"},
			{ID: "deny", Label: "Deny", Outcome: "deny"},
		}}}
	}
	state.answer = func(ctx context.Context, _ string, _ json.RawMessage, answer, card string) plugins.Decision {
		status := contract.ApprovalStatusDenied
		if answer == "allow_once" {
			status = contract.ApprovalStatusAllowed
		}
		api.settleApproval(ctx, run.ConversationID, card, status, answer)
		if answer == "allow_once" {
			return plugins.Decision{Allow: true}
		}
		return plugins.Decision{Reason: "The person declined this call."}
	}
	state.ask = func(ctx context.Context, call string, ask plugins.Ask, tool string, args json.RawMessage) (string, string, error) {
		return api.askApproval(ctx, state, call, ask, tool, args)
	}
	result := make(chan agent.ToolCallHookResult, 1)
	go func() {
		result <- beforeTool(context.WithValue(t.Context(), turnKey{}, state), "call-1", "bash", json.RawMessage(`{"command":"ls"}`))
	}()
	id := approvalID(run.ID, "call-1")
	deadline := time.Now().Add(3 * time.Second)
	for {
		card, err := api.Store.ApprovalCard(ctx, run.ConversationID, id)
		if err == nil && card.Parts[0].Approval.Status == contract.ApprovalStatusPending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the approval card never appeared: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, err := http.Post(server.URL+"/threads/assistant/messages/"+id+"/approval", "application/json", strings.NewReader(`{"optionID":"allow_once"}`))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("answer: %v %d", err, response.StatusCode)
	}
	response.Body.Close()
	if hook := <-result; hook.Block || state.tools != 1 {
		t.Fatalf("%+v tools %d", hook, state.tools)
	}
	card, err := api.Store.ApprovalCard(ctx, run.ConversationID, id)
	if err != nil || card.Parts[0].Approval.Status != contract.ApprovalStatusAllowed || card.Parts[0].Approval.AnswerID == nil || *card.Parts[0].Approval.AnswerID != "allow_once" {
		t.Fatalf("card %+v %v", card.Parts[0].Approval, err)
	}
}

// Work a bot hands off stays under the rules of the chat that started it.
func TestRunsKeepTheirOriginsRules(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	ctx := t.Context()
	if _, err := api.Store.DB.ExecContext(ctx, `INSERT INTO threads(id,agent_id,title) VALUES('group','assistant','Group')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Store.CreateTask(ctx, "task-one", "assistant", "assistant", "Report", "Write it"); err != nil {
		t.Fatal(err)
	}
	parent := store.NewRun("origin", "group", "assistant", contract.RunTriggerMessage)
	if err := api.Store.PutPrivateRun(ctx, parent); err != nil {
		t.Fatal(err)
	}
	child := store.NewRun("child", "assistant", "assistant", contract.RunTriggerChild)
	child.ParentRunID = &parent.ID
	missing := "gone"
	orphan := store.NewRun("orphan", "assistant", "assistant", contract.RunTriggerChild)
	orphan.ParentRunID = &missing
	for name, test := range map[string]struct {
		run  contract.Run
		want []string
	}{
		"direct": {store.NewRun("direct", "assistant", "assistant", contract.RunTriggerMessage), []string{"assistant"}},
		"task":   {store.NewRun("task", "task-one", "assistant", contract.RunTriggerAssigned), []string{"task-one", "assistant"}},
		"child":  {child, []string{"assistant", "group"}},
		"orphan": {orphan, []string{"assistant"}},
	} {
		t.Run(name, func(t *testing.T) {
			chats, err := api.boundChats(ctx, test.run)
			if err != nil || !slices.Equal(chats, test.want) {
				t.Fatalf("%v %v", chats, err)
			}
		})
	}
}

// A new bot's first task has no parent run, so a bound agent can't use one to work around its rules.
func TestBoundAgentCantStartAFreeBot(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	guard := `{"manifest":{"name":"guard","provides":["gate"]},"state":"ready","title":"Guard","kind":"scoped"}`
	if err := api.Store.SavePlugin(t.Context(), "guard", json.RawMessage(guard)); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.SetPluginChats(t.Context(), "guard", []string{"assistant"}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", contract.RunTriggerMessage), chats: []string{"assistant"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &agentTool{server: api, action: "create"}
	if _, err := tool.Execute(ctx, "free", json.RawMessage(`{"role":"Helper","prompt":"Help","initial_task":"Read the secret"}`), nil); err == nil {
		t.Fatal("a bound agent started a bot outside its rules")
	}
	if _, err := tool.Execute(ctx, "plain", json.RawMessage(`{"role":"Helper","prompt":"Help"}`), nil); err != nil {
		t.Fatal(err)
	}
}

// Voice opens its own sessions, so it has to bind the gate itself.
func TestVoiceTurnsAreBound(t *testing.T) {
	var result agent.ToolCallHookResult
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		result = d.BeforeTool(ctx, "call", "read", json.RawMessage(`{}`))
		return agentruntime.Result{Text: "Private result"}, nil
	}}
	api, web := voiceTest(t, factory)
	guard := `{"manifest":{"name":"guard","provides":["gate"]},"state":"failed","title":"Guard","kind":"scoped"}`
	if err := api.Store.SavePlugin(t.Context(), "guard", json.RawMessage(guard)); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.SetPluginChats(t.Context(), "guard", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if status, _ := voicePost(t, web, "a", "voice-gated"); status != 200 {
		t.Fatal(status)
	}
	if !result.Block || result.Reason != "Guard isn't running, so the call was blocked." {
		t.Fatalf("voice got past the gate: %+v", result)
	}
}

// A bound agent could read the local token from disk, so it can't reach the API to lift its own rules.
func TestGatedRequestStaysOffThisMachine(t *testing.T) {
	var hits atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer local.Close()
	api, _ := startTest(t, &voiceFactory{})
	guard := `{"manifest":{"name":"guard","provides":["gate"]},"state":"ready","title":"Guard","kind":"scoped"}`
	if err := api.Store.SavePlugin(t.Context(), "guard", json.RawMessage(guard)); err != nil {
		t.Fatal(err)
	}
	if err := api.Store.SetPluginChats(t.Context(), "guard", []string{"assistant"}); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: store.NewRun("test", "assistant", "assistant", contract.RunTriggerMessage), work: store.Work{Trigger: contract.RunTriggerMessage}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &httpTool{server: api}
	args, _ := json.Marshal(map[string]string{"url": local.URL})
	if _, err := tool.Execute(ctx, "free", args, nil); err != nil || hits.Load() != 1 {
		t.Fatalf("unbound request %d %v", hits.Load(), err)
	}
	state.chats = []string{"assistant"}
	if _, err := tool.Execute(ctx, "bound", args, nil); err == nil || hits.Load() != 1 {
		t.Fatalf("bound request reached this machine: %d %v", hits.Load(), err)
	}
}
