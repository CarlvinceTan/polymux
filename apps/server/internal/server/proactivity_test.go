package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
)

type reviewFactory struct {
	notify *bool
	text   string
	fail   bool
	opened chan agentruntime.Definition
	sent   chan contract.Message
}

func (f *reviewFactory) Open(_ context.Context, d agentruntime.Definition, _ string) (agentruntime.Session, error) {
	f.opened <- d
	return &reviewSession{factory: f, definition: d}, nil
}

type reviewSession struct {
	factory    *reviewFactory
	definition agentruntime.Definition
}

func (s *reviewSession) Send(ctx context.Context, m contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	f := s.factory
	f.sent <- m
	if err := emit(agentruntime.Delta{Text: "Ordinary review reasoning must stay private"}); err != nil {
		return agentruntime.Result{}, err
	}
	if f.fail {
		return agentruntime.Result{}, errors.New("fake provider failure")
	}
	if f.notify != nil {
		for _, tool := range s.definition.ExtraTools {
			if tool.Name() != "finish_review" {
				continue
			}
			args, _ := json.Marshal(map[string]any{"notify": *f.notify, "text": f.text})
			if err := emit(agentruntime.Delta{Kind: "tool.start", Tool: tool.Name(), ToolID: "finish"}); err != nil {
				return agentruntime.Result{}, err
			}
			if _, err := tool.Execute(ctx, "finish", args, nil); err != nil {
				return agentruntime.Result{}, err
			}
			if err := emit(agentruntime.Delta{Kind: "tool.done", Tool: tool.Name(), ToolID: "finish"}); err != nil {
				return agentruntime.Result{}, err
			}
		}
	}
	return agentruntime.Result{Text: "Undelivered review reasoning"}, nil
}

func (*reviewSession) Close() error                { return nil }
func (*reviewSession) Path() string                { return "temporary.jsonl" }
func (*reviewSession) Abort(context.Context) error { return nil }

func TestBackgroundReviews(t *testing.T) {
	quiet, notify := false, true
	for _, test := range []struct {
		name    string
		notify  *bool
		fail    bool
		outcome string
	}{
		{name: "silent", notify: &quiet, outcome: "quiet"},
		{name: "notice", notify: &notify, outcome: "notice"},
		{name: "no-decision", outcome: "quiet"},
		{name: "failure", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &reviewFactory{notify: test.notify, fail: test.fail, text: "A useful draft outline is ready to review.",
				opened: make(chan agentruntime.Definition, 1), sent: make(chan contract.Message, 1)}
			api, _ := startTest(t, f)
			if err := api.Store.SetProactivity(t.Context(), "assistant", contract.Proactivity{Enabled: true, IntervalSeconds: 14400}); err != nil {
				t.Fatal(err)
			}
			raw := "User's unresolved draft goal"
			_, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "assistant", Type: "message.start"},
				&contract.Message{ID: "evidence", ThreadID: "assistant", Role: "user", Status: "done", CreatedAt: store.Now(),
					Parts: []contract.Part{{Kind: "text", Text: &raw}}})
			if err != nil {
				t.Fatal(err)
			}
			work := store.Work{Trigger: "review", Message: contract.Message{ID: "review", ThreadID: "assistant", Role: "system"}}
			api.execute(t.Context(), api.actors["assistant"], work)
			run, err := api.Store.Run(t.Context(), store.NewRun("review", "assistant", "assistant", "review").ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.fail {
				if run.Status != "failed" {
					t.Fatal("review failure was hidden", run.Status)
				}
			} else if value(run.Outcome) != test.outcome {
				t.Fatal("wrong review outcome", value(run.Outcome))
			}
			d := <-f.opened
			if !d.Automatic || !d.Ephemeral || len(d.Tools) != 0 || len(d.MCP) != 0 {
				t.Fatal("automatic review inherited permissions")
			}
			for _, tool := range d.ExtraTools {
				if tool.Name() != "finish_review" && tool.Name() != "track_work" && tool.Name() != "recall_history" && tool.Name() != "save_memory" && tool.Name() != "recall_memory" && tool.Name() != "forget_memory" && tool.Name() != "manage_integrations" {
					t.Fatal("review exposed effect tool", tool.Name())
				}
			}
			if !strings.Contains(store.Text(<-f.sent), raw) {
				t.Fatal("review lacked conversation evidence")
			}
			messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
			want := 1
			if test.outcome == "notice" {
				want++
			}
			if err != nil || len(messages) != want {
				t.Fatal("review chat pollution", len(messages), err)
			}
			events, err := api.Store.Events(t.Context(), "assistant", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Type == "message.delta" || event.Type == "message.tool_call" {
					t.Fatal("review streamed hidden reasoning", event.Type)
				}
			}
			if _, err := api.Store.Session(t.Context(), "assistant"); err == nil {
				t.Fatal("review overwrote normal chat session")
			}
		})
	}
}

func TestAutomaticToolGate(t *testing.T) {
	for _, trigger := range []string{"review", "routine", "follow_up"} {
		state := &turnState{work: store.Work{Trigger: trigger}}
		ctx := context.WithValue(t.Context(), turnKey{}, state)
		for _, name := range []string{"bash", "read", "mcp_external_write", "message_agent", "manage_routines"} {
			if result := beforeTool(ctx, "call", name, nil); !result.Block {
				t.Fatal("automatic run allowed effect", trigger, name)
			}
		}
		if result := beforeTool(ctx, "call", "track_work", nil); result.Block {
			t.Fatal("automatic run cannot track work", trigger)
		}
		if result := beforeTool(ctx, "call", "show_html", nil); result.Block != (trigger == "review") {
			t.Fatal("wrong HTML permission", trigger)
		}
	}
}

func TestRoutineToolsNeedUserTurn(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	tool := &routineTool{server: api}
	for _, trigger := range []string{"created", "peer", "routine", "review", "follow_up"} {
		state := &turnState{work: store.Work{Trigger: trigger}, run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}}
		ctx := context.WithValue(t.Context(), turnKey{}, state)
		if _, err := tool.Execute(ctx, "call", json.RawMessage(`{"action":"list"}`), nil); err == nil {
			t.Fatal("automatic run obtained routine management", trigger)
		}
	}
	peer := &turnState{work: store.Work{Trigger: "peer"}, run: contract.Run{AgentID: "assistant", ConversationID: "assistant"}}
	ctx := context.WithValue(t.Context(), turnKey{}, peer)
	if _, err := (&workTool{store: api.Store}).Execute(ctx, "call", json.RawMessage(`{"action":"list"}`), nil); err == nil {
		t.Fatal("peer request read private scratchpad")
	}
}

func TestRoutineAPI(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		api.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
		return r
	}
	at := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	body, _ := json.Marshal(contract.RoutineRequest{At: at, Prompt: "Review my draft", IntervalSeconds: 86400})
	response := call(http.MethodPost, "/agents/assistant/routines", string(body))
	var routine contract.Routine
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &routine) != nil || routine.Status != "active" {
		t.Fatal("routine create failed", response.Code)
	}
	response = call(http.MethodGet, "/agents/assistant/routines", "")
	var routines []contract.Routine
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &routines) != nil || len(routines) != 1 {
		t.Fatal("routine listing failed", response.Code)
	}
	response = call(http.MethodDelete, "/agents/assistant/routines/"+routine.ID, "")
	if response.Code != 200 {
		t.Fatal("routine cancel failed", response.Code)
	}
	routine, _ = api.Store.Routine(t.Context(), "assistant", routine.ID)
	if routine.Status != "cancelled" {
		t.Fatal("cancel not durable")
	}
	response = call(http.MethodPatch, "/agents/assistant/proactivity", `{"enabled":false,"intervalSeconds":14400}`)
	if response.Code != 200 {
		t.Fatal("proactivity control failed", response.Code)
	}
	response = call(http.MethodPatch, "/agents/assistant/proactivity", `{"enabled":true,"intervalSeconds":1}`)
	if response.Code != 400 {
		t.Fatal("unbounded review frequency accepted")
	}
}

func TestRoutineAndUserContexts(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	text := "The second recommendation is to shorten the introduction."
	run := store.NewRun("automatic", "assistant", "assistant", "follow_up")
	run.Status = "completed"
	if err := api.Store.RunEvent(t.Context(), "run.done", &run, nil); err != nil {
		t.Fatal(err)
	}
	m := contract.Message{ID: "automatic-result", ThreadID: "assistant", Role: "assistant", Status: "done",
		CreatedAt: store.Now(), RunID: &run.ID, AgentID: pointer("assistant"), Parts: []contract.Part{{Kind: "text", Text: &text}}}
	if _, err := api.Store.Append(t.Context(), contract.Event{ConversationID: "assistant", Type: "message.start"}, &m); err != nil {
		t.Fatal(err)
	}
	user := contract.Message{ID: "next", ThreadID: "assistant", Role: "user", Seq: m.Seq + 1}
	context, err := api.resultContext(t.Context(), run, user)
	if err != nil || !strings.Contains(store.Text(context), text) {
		t.Fatal("user turn could not refer to automatic recommendations", err)
	}
	old := user
	old.Seq = m.Seq
	context, err = api.resultContext(t.Context(), run, old)
	if err != nil || strings.Contains(store.Text(context), text) {
		t.Fatal("future result leaked into earlier user turn", err)
	}
	r, err := api.Store.CreateRoutine(t.Context(), contract.Routine{
		ID: "draft", AgentID: "assistant", Prompt: "Summarize the draft discussed here",
		At: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	context, err = api.autoContext(t.Context(), run, store.Work{Trigger: "routine", RoutineID: &r.ID}, user)
	if err != nil || !strings.Contains(store.Text(context), text) {
		t.Fatal("routine lacked conversation evidence", err)
	}
}

func TestRoutineConversationalCreation(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state := &turnState{work: store.Work{Trigger: "message"}, run: contract.Run{ID: "user-task", AgentID: "assistant", ConversationID: "assistant"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	tool := &routineTool{server: api}
	for _, action := range []string{"propose", "create"} {
		args, _ := json.Marshal(map[string]any{"action": action, "prompt": "Review my draft", "at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		if _, err := tool.Execute(ctx, action, args, nil); err != nil {
			t.Fatal(err)
		}
	}
	routines, err := api.Store.Routines(t.Context(), "assistant")
	if err != nil || len(routines) != 2 {
		t.Fatal("chat routines", routines, err)
	}
	for _, routine := range routines {
		if routine.Status == "pending" {
			args, _ := json.Marshal(map[string]string{"action": "activate", "id": routine.ID})
			if _, err := tool.Execute(ctx, "normal-chat-answer", args, nil); err != nil {
				t.Fatal("ordinary answer could not activate", err)
			}
		}
		saved, err := api.Store.Routine(t.Context(), "assistant", routine.ID)
		if err != nil || saved.Status != "active" {
			t.Fatal("routine still required exact confirmation", saved, err)
		}
	}
}

type initiativeFactory struct{}

func (*initiativeFactory) Open(_ context.Context, d agentruntime.Definition, _ string) (agentruntime.Session, error) {
	return &initiativeSession{definition: d}, nil
}

type initiativeSession struct{ definition agentruntime.Definition }

func (s *initiativeSession) Send(ctx context.Context, m contract.Message, emit func(agentruntime.Delta) error) (agentruntime.Result, error) {
	text := "Independent task completed."
	if m.ID == "question" {
		for _, tool := range s.definition.ExtraTools {
			if tool.Name() != "track_work" {
				continue
			}
			for _, args := range []string{
				`{"action":"save","id":"blocked","text":"Waiting for the user's chosen project","state":"parked","waiting_for":"user"}`,
				`{"action":"save","id":"independent","text":"Prepared an outline","state":"done"}`,
			} {
				if _, err := tool.Execute(ctx, "track", json.RawMessage(args), nil); err != nil {
					return agentruntime.Result{}, err
				}
			}
		}
		text = "Which project should I use? I prepared the outline meanwhile."
	}
	return agentruntime.Result{Text: text}, emit(agentruntime.Delta{Text: text})
}
func (*initiativeSession) Close() error                { return nil }
func (*initiativeSession) Path() string                { return "initiative.jsonl" }
func (*initiativeSession) Abort(context.Context) error { return nil }

func TestWaitingDoesNotPauseAgent(t *testing.T) {
	api, _ := startTest(t, &initiativeFactory{})
	for _, id := range []string{"question", "independent"} {
		text := "User goal"
		work := store.Work{Trigger: "message", Message: contract.Message{ID: id, ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}}}}
		m := api.execute(t.Context(), api.actors["assistant"], work)
		run, err := api.Store.Run(t.Context(), value(m.RunID))
		if err != nil || run.Status != "completed" || m.Status != "done" {
			t.Fatal("execution remained frozen", run, err)
		}
		if id == "question" && value(run.Outcome) != "waiting_input" {
			t.Fatal("question did not describe user wait", run)
		}
		if id == "independent" && value(run.Outcome) != "reply" {
			t.Fatal("independent work failed", run)
		}
	}
	items, err := api.Store.OpenWork(t.Context(), "assistant", "assistant")
	if err != nil || len(items) != 2 || items[0].WaitingFor != "user" {
		t.Fatal("blocked task was not retained", items, err)
	}
}

func morningWork(t *testing.T, s *store.Store, id, job string) store.Work {
	t.Helper()
	routine, err := s.CreateRoutine(t.Context(), contract.Routine{
		ID: id, AgentID: "assistant", Prompt: "Morning brief", Status: "active",
		At: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := routine.Prompt
	return store.Work{Trigger: "routine", RoutineID: &routine.ID, Message: contract.Message{
		ID: job, ThreadID: "assistant", Role: "system", Parts: []contract.Part{{Kind: "text", Text: &prompt}},
	}}
}

func failureNotices(t *testing.T, s *store.Store) []contract.Message {
	t.Helper()
	messages, err := s.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	notes := []contract.Message{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Kind == "routine_failed" {
				notes = append(notes, message)
			}
		}
	}
	return notes
}

func TestR4(t *testing.T) {
	testenv.PiG(t)
	const attempts = 3
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-should-retry", "false")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal server error","type":"server_error"}}`))
	}))
	t.Cleanup(provider.Close)
	runtime := durableRuntime(t, t.TempDir(), provider.URL+"/v1")
	api, _ := startTest(t, runtime)
	if err := api.Store.RegisterPush(t.Context(), contract.PushDevice{
		ID: "fixture-installation", Token: strings.Repeat("ab", 32),
		Environment: "sandbox", Server: "http://server.test:7337", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	work := morningWork(t, api.Store, "morning", "morning-job")
	for range attempts {
		api.execute(t.Context(), api.actors["assistant"], work)
	}
	if hits.Load() < 1 {
		t.Fatal("model did not return 500")
	}
	run, err := api.Store.Run(t.Context(), store.NewRun(work.Message.ID, "assistant", "assistant", "routine").ID)
	notes := failureNotices(t, api.Store)
	if err != nil || run.Status != "failed" || run.Error == nil || len(notes) != 1 || notes[0].Role != "system" {
		t.Fatalf("run %+v notices %d hits %d err %v", run, len(notes), hits.Load(), err)
	}
	part := notes[0].Parts[0]
	var body map[string]string
	if part.Kind != "routine_failed" || value(part.Text) != "Routine \u201cMorning brief\u201d failed: model error. Retry" || value(part.Name) != value(work.RoutineID) || json.Unmarshal([]byte(value(part.Args)), &body) != nil || body["title"] != "Morning brief" || body["reason"] != "model error" || body["routineID"] != value(work.RoutineID) {
		t.Fatalf("notice %+v", notes[0])
	}
	push, err := api.Store.ClaimPush(t.Context())
	if err != nil || push.Notice.MessageID != notes[0].ID {
		t.Fatalf("notice was not pushed: %+v %v", push.Notice, err)
	}
	if _, err := api.Store.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failure notice pushed twice", err)
	}
	okAPI, _ := startTest(t, &testFactory{})
	okWork := morningWork(t, okAPI.Store, "morning-ok", "morning-ok")
	okAPI.execute(t.Context(), okAPI.actors["assistant"], okWork)
	okRun, err := okAPI.Store.Run(t.Context(), store.NewRun(okWork.Message.ID, "assistant", "assistant", "routine").ID)
	if err != nil || okRun.Status != "completed" || len(failureNotices(t, okAPI.Store)) != 0 {
		t.Fatalf("successful routine %+v notices %d %v", okRun, len(failureNotices(t, okAPI.Store)), err)
	}
}

type failModel struct{ calls atomic.Int32 }

func (f *failModel) Open(context.Context, agentruntime.Definition, string) (agentruntime.Session, error) {
	return &failSession{factory: f}, nil
}

type failSession struct{ factory *failModel }

func (s *failSession) Send(context.Context, contract.Message, func(agentruntime.Delta) error) (agentruntime.Result, error) {
	s.factory.calls.Add(1)
	return agentruntime.Result{}, errors.New("500 internal server error")
}
func (*failSession) Close() error                { return nil }
func (*failSession) Path() string                { return "fail.jsonl" }
func (*failSession) Abort(context.Context) error { return nil }

func TestR4CancelIsNotFailure(t *testing.T) {
	model := &failModel{}
	api, _ := startTest(t, model)
	work := morningWork(t, api.Store, "morning", "morning-job")
	if _, err := api.Store.CancelRoutine(t.Context(), "assistant", value(work.RoutineID)); err != nil {
		t.Fatal(err)
	}
	api.execute(t.Context(), api.actors["assistant"], work)
	run, err := api.Store.Run(t.Context(), store.NewRun(work.Message.ID, "assistant", "assistant", "routine").ID)
	notes := failureNotices(t, api.Store)
	if err != nil || run.Status != "cancelled" || len(notes) != 0 || model.calls.Load() != 0 {
		t.Fatalf("cancel treated as failure: status %s notices %d model %d err %v", run.Status, len(notes), model.calls.Load(), err)
	}
}
