package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestRunStartTime(t *testing.T) {
	f := &voiceFactory{script: func(context.Context, agentruntime.Definition, contract.Message) (agentruntime.Result, error) {
		time.Sleep(25 * time.Millisecond)
		return agentruntime.Result{Text: "Synthetic response"}, nil
	}}
	api, _ := voiceTest(t, f)
	text := "Check elapsed duration"
	api.execute(t.Context(), t.Context(), api.actors["a"], store.Work{Trigger: "message", Message: contract.Message{ID: "audit-time", ThreadID: "a", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}}})
	run, err := api.Store.Run(t.Context(), store.NewRun("audit-time", "a", "a", "message").ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.StartedAt == nil || run.FinishedAt == nil || *run.StartedAt >= *run.FinishedAt {
		t.Fatalf("completed nonzero-duration run has start=%v finish=%v", value(run.StartedAt), value(run.FinishedAt))
	}
}

func TestConcurrentHandoff(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Register(t.Context(), []contract.Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}}); err != nil {
		t.Fatal(err)
	}
	group, err := db.CreateThread(t.Context(), "audit-group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	caps := DefaultGroups()
	tool := &peerTool{server: &Server{Store: db, config: Config{GroupCaps: &caps}}, handoff: true}
	for trial := range 25 {
		chain := &chainState{seen: map[string]bool{}, pending: map[string]string{}}
		contexts := []context.Context{}
		for _, source := range []string{"a", "c"} {
			run := store.NewRun(fmt.Sprintf("audit-race-%d", trial), group.ID, source, "message")
			run.Status = "running"
			if err := db.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
				t.Fatal(err)
			}
			work := store.Work{Trigger: "message", Message: contract.Message{ID: fmt.Sprintf("audit-race-%d", trial), ThreadID: group.ID}}
			contexts = append(contexts, context.WithValue(context.WithValue(t.Context(), chainKey{}, chain), turnKey{}, &turnState{run: run, work: work}))
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, ctx := range contexts {
			go func() {
				<-start
				_, err := tool.Execute(ctx, "audit-call", json.RawMessage(`{"agent_id":"b","message":"One bounded task"}`), nil)
				results <- err
			}()
		}
		close(start)
		accepted := 0
		for range 2 {
			if <-results == nil {
				accepted++
			}
		}
		if accepted > 1 {
			t.Fatalf("trial %d accepted %d simultaneous handoffs to one target", trial, accepted)
		}
	}
}

func TestPauseDuringSteer(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	if err := api.Store.SetProactivity(t.Context(), "assistant", contract.Proactivity{Enabled: true, IntervalSeconds: 14400}); err != nil {
		t.Fatal(err)
	}
	run := store.NewRun("audit-held", "assistant", "assistant", "message")
	run.Status = "running"
	if err := api.Store.RunEvent(t.Context(), "run.start", &run, nil); err != nil {
		t.Fatal(err)
	}
	text := store.PauseCommand
	_, _, err := api.Store.Send(t.Context(), "assistant", contract.SendRequest{ID: "audit-pause", Text: text, AttachmentIDs: []string{}}, []contract.Part{{Kind: "text", Text: &text}}, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	setting, err := api.Store.Proactivity(t.Context(), "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if setting.Enabled {
		t.Fatal("accepted pause proactivity as a steer but reviews remain enabled")
	}
}

func TestGroupTaskOwner(t *testing.T) {
	resumed := make(chan string, 2)
	f := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		if state.work.Trigger == "task" {
			resumed <- d.Agent.ID
			return agentruntime.Result{Text: "Continuation complete"}, nil
		}
		_, err := botCall(ctx, d, "manage_tasks", `{"action":"checkpoint","goal":"Audit specialist task","next":"Continue as the specialist","evidence":"Synthetic checkpoint","after_seconds":60}`)
		return agentruntime.Result{Text: "Checkpoint saved"}, err
	}}
	api, web := voiceTest(t, f)
	thread, err := api.Store.CreateThread(t.Context(), "audit-group", contract.CreateThreadRequest{AgentIDs: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	api.startGroup(thread.ID)
	response, err := http.Post(web.URL+"/threads/"+thread.ID+"/messages", "application/json", strings.NewReader(`{"id":"audit-group-start","text":"Checkpoint this specialist task","mentions":["b"],"attachmentIDs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatal(response.StatusCode)
	}
	awaitRun(t, api.Store, store.NewRun("audit-group-start", thread.ID, "b", "message").ID, "completed")
	waitGroup(t, api, "audit-group-start")
	tasks, err := api.Store.Tasks(t.Context(), "b", thread.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("checkpoint setup tasks=%+v error=%v", tasks, err)
	}
	if err := api.Store.ScheduleTasks(t.Context(), time.Now().Add(61*time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case owner := <-resumed:
		if owner != "b" {
			t.Fatalf("specialist b's task resumed as %s", owner)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task did not resume")
	}
}

func TestReplyNamespace(t *testing.T) {
	api, web := voiceTest(t, &voiceFactory{})
	future := "reply:" + store.NewRun("victim", "a", "a", "message").ID
	data, _ := json.Marshal(contract.SendRequest{ID: future, Text: "Claim a reply ID", AttachmentIDs: []string{}})
	response, err := http.Post(web.URL+"/threads/b/messages", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("server-owned message ID accepted: %d", response.StatusCode)
	}
	// Persistence refuses to move an existing message to another chat or role.
	text := "Original"
	m := contract.Message{ID: "owned", ThreadID: "b", Role: "user", Status: "done", Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "b", Type: "message.done"}, &m); err != nil {
		t.Fatal(err)
	}
	m.ThreadID, m.Role = "a", "assistant"
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &m); err == nil {
		t.Fatal("message overwritten from another chat")
	}
	got, err := api.Store.Message(t.Context(), "owned")
	if err != nil || got.ThreadID != "b" || got.Role != "user" {
		t.Fatalf("message changed: %+v %v", got, err)
	}
}

func TestVoiceDeviceTools(t *testing.T) {
	f := &voiceFactory{}
	api, web := voiceTest(t, f)
	api.clients.report("audit-phone", contract.ClientState{Platform: "ios", Capabilities: []string{"location", "home", "alarms"}})
	api.clients.attach("audit-phone")
	text := "Synthetic typed request"
	api.execute(t.Context(), t.Context(), api.actors["a"], store.Work{Trigger: "message", DeviceID: "audit-phone", Message: contract.Message{ID: "audit-typed", ThreadID: "a", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}}})
	code, _ := voicePost(t, web, "a", "audit-voice")
	if code != 200 {
		t.Fatal(code)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opened) != 2 {
		t.Fatal("expected typed and voice definitions", len(f.opened))
	}
	names := func(d agentruntime.Definition) map[string]bool {
		out := map[string]bool{}
		for _, tool := range d.ExtraTools {
			out[tool.Name()] = true
		}
		return out
	}
	typed, voice := names(f.opened[0]), names(f.opened[1])
	for _, name := range []string{"get_location", "control_home", "manage_alarms"} {
		if !typed[name] {
			t.Fatalf("setup: typed request lacks %s", name)
		}
		if !voice[name] {
			t.Errorf("voice request lacks typed device tool %s", name)
		}
	}
}
