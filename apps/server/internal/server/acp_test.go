package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/acptest"
	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv(acptest.Env) == "1" {
		acptest.Serve()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func externalAgent() *contract.AgentACP {
	return &contract.AgentACP{Command: os.Args[0], Args: []string{"-test.run=^$"}}
}

// awaitReplies waits for want completed replies containing text.
func awaitReplies(t *testing.T, api *Server, thread, text string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := api.Store.Messages(t.Context(), thread, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, m := range messages {
			if m.ReplyToID != nil && m.Status == contract.MessageStatusDone && strings.Contains(store.Text(m), text) {
				found++
			}
		}
		if found >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fewer than %d replies containing %q", want, text)
}

func awaitText(t *testing.T, api *Server, thread, text string) {
	t.Helper()
	awaitReplies(t, api, thread, text, 1)
}

func send(t *testing.T, api *Server, agent, id string) {
	t.Helper()
	sendText(t, api, agent, id, "do it")
}

func sendText(t *testing.T, api *Server, agent, id, text string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	body := `{"id":"` + id + `","text":"` + text + `","attachmentIDs":[]}`
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/"+agent+"/messages", strings.NewReader(body)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("send status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestExternalAgentRunsTheConversation(t *testing.T) {
	t.Setenv(acptest.Env, "1")
	dir := t.TempDir()
	definitions := []agentruntime.Definition{
		{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}, Tools: []string{}},
		{Agent: contract.Agent{ID: "worker", Name: "Worker", ACP: externalAgent()}, Tools: []string{}},
	}
	api, _ := durableServer(t, dir, "http://127.0.0.1:1", definitions...)

	// A durable conversation ID is never handed to the external agent.
	if err := api.Store.SaveSession(t.Context(), "worker", "durable:7"); err != nil {
		t.Fatal(err)
	}
	send(t, api, "worker", "first")
	awaitText(t, api, "worker", "Hello from ACP [allow-once]")
	if path, err := api.Store.Session(t.Context(), "worker"); err != nil || path != "acp:"+acptest.Created {
		t.Fatalf("saved session = %q, %v", path, err)
	}

	// Editing reopens the session; the agent loads the one it remembers.
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/agents/worker", strings.NewReader(`{"name":"Worker Two"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("edit status %d: %s", recorder.Code, recorder.Body.String())
	}
	send(t, api, "worker", "second")
	awaitReplies(t, api, "worker", "Hello from ACP [allow-once]", 2)
	if path, err := api.Store.Session(t.Context(), "worker"); err != nil || path != "acp:"+acptest.Created {
		t.Fatalf("reopened session = %q, %v", path, err)
	}

	recorder = httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/agents/worker", strings.NewReader(`{"acp":{"command":""}}`)))
	var profile contract.AgentProfile
	if err := json.Unmarshal(recorder.Body.Bytes(), &profile); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("detach status %d: %s", recorder.Code, recorder.Body.String())
	}
	if profile.Agent.ACP != nil || profile.Agent.Model != "" {
		t.Fatalf("detached agent = %+v", profile.Agent)
	}
}

func TestExternalAgentRecoversAfterItExits(t *testing.T) {
	t.Setenv(acptest.Env, "1")
	api, _ := durableServer(t, t.TempDir(), "http://127.0.0.1:1",
		agentruntime.Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}, Tools: []string{}},
		agentruntime.Definition{Agent: contract.Agent{ID: "worker", Name: "Worker", ACP: externalAgent()}, Tools: []string{}},
	)
	sendText(t, api, "worker", "crash", acptest.Crash)
	awaitRun(t, api.Store, store.NewRun("crash", "worker", "worker", contract.RunTriggerMessage).ID, contract.RunStatusFailed)

	// The next turn reopens the agent instead of reusing the exited process.
	send(t, api, "worker", "after")
	awaitText(t, api, "worker", "Hello from ACP [allow-once]")
}

func TestBotCannotAttachAnExternalAgent(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	state := &turnState{run: contract.Run{ID: "attach", AgentID: "assistant", ConversationID: "assistant"}, work: store.Work{Trigger: "message"}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	args := json.RawMessage(`{"name":"Coach","acp":{"command":"sh","args":["-c","true"]}}`)
	if _, err := (&agentTool{server: api, action: "update"}).Execute(ctx, "attach", args, nil); err != nil {
		t.Fatal(err)
	}
	profile, err := api.Store.Profile(t.Context(), "assistant")
	if err != nil || profile.Agent.Name != "Coach" || profile.Agent.ACP != nil {
		t.Fatalf("profile = %+v, %v", profile.Agent, err)
	}
}

func TestCreateAgentAttachesAnExternalAgent(t *testing.T) {
	api, _ := durableServer(t, t.TempDir(), "http://127.0.0.1:1",
		agentruntime.Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Tools: []string{}},
	)
	recorder := httptest.NewRecorder()
	body := `{"name":"Worker","role":"","prompt":"","acp":{"command":"npx","args":["@example/agent","--stdio"]}}`
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body)))
	var agent contract.Agent
	if err := json.Unmarshal(recorder.Body.Bytes(), &agent); err != nil || recorder.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", recorder.Code, recorder.Body.String())
	}
	if agent.ACP == nil || agent.ACP.Command != "npx" || len(agent.ACP.Args) != 2 || agent.Model != "" || agent.Provider != "" {
		t.Fatalf("created agent = %+v", agent)
	}
}

func TestCreateAgentRejectsAnInvalidACPCommand(t *testing.T) {
	api, _ := durableServer(t, t.TempDir(), "http://127.0.0.1:1",
		agentruntime.Definition{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Tools: []string{}},
	)
	recorder := httptest.NewRecorder()
	body := `{"name":"Worker","role":"","prompt":"","acp":{"command":"bad\ncommand"}}`
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create status %d: %s", recorder.Code, recorder.Body.String())
	}
}

// delegateFactory runs external turns on the real runtime and every other turn
// through a stub parent that delegates once to the worker.
type delegateFactory struct {
	runtime *agentruntime.DurableRuntime
}

func (f *delegateFactory) Open(ctx context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	if d.Agent.ACP != nil {
		return f.runtime.Open(ctx, d, path)
	}
	for _, tool := range d.ExtraTools {
		if spawn, ok := tool.(*delegateTool); ok {
			return &delegateParent{spawn: spawn}, nil
		}
	}
	return nil, errors.New("task tool missing")
}

type delegateParent struct {
	spawn *delegateTool
}

func (s *delegateParent) Send(ctx context.Context, _ contract.Message, _ func(agentruntime.Delta) error) (agentruntime.Result, error) {
	args := json.RawMessage(`{"action":"run","agent_id":"worker","prompt":"Say hello"}`)
	if _, err := s.spawn.Execute(ctx, "spawn", args, nil); err != nil {
		return agentruntime.Result{}, err
	}
	return agentruntime.Result{Text: "Parent continued"}, nil
}
func (*delegateParent) Abort(context.Context) error { return nil }
func (*delegateParent) Path() string                { return "parent" }
func (*delegateParent) Close() error                { return nil }

func TestDelegationReachesAnExternalAgent(t *testing.T) {
	t.Setenv(acptest.Env, "1")
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	api, err := New(t.Context(), db, Config{
		DataDir:   dir,
		Factory:   &delegateFactory{runtime: sqliteRuntime(t, dir, "http://127.0.0.1:1")},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		VaultKeys: &vaultKeys{},
		Definitions: []agentruntime.Definition{
			{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}, Tools: []string{}},
			{Agent: contract.Agent{ID: "worker", Name: "Worker", ACP: externalAgent()}, Tools: []string{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close() })

	send(t, api, "assistant", "delegate")
	awaitText(t, api, "assistant", "Parent continued")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		children, err := api.Store.Children(t.Context(), "assistant")
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range children {
			if child.Status != "completed" {
				continue
			}
			messages, err := api.Store.ChildMessages(t.Context(), child.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				if strings.Contains(store.Text(message), "Hello from ACP [allow-once]") {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the external agent did not answer the delegated task")
}
