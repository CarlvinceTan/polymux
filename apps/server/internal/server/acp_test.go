package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/acp"
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

// delegateFactory runs external tasks on the real runtime and every other turn
// through a stub parent that delegates once with args.
type delegateFactory struct {
	runtime *agentruntime.DurableRuntime
	args    string
}

func (f *delegateFactory) Open(ctx context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	if d.External != nil {
		return f.runtime.Open(ctx, d, path)
	}
	for _, tool := range d.ExtraTools {
		if spawn, ok := tool.(*delegateTool); ok {
			return &delegateParent{spawn: spawn, args: f.args}, nil
		}
	}
	return nil, errors.New("task tool missing")
}

type delegateParent struct {
	spawn *delegateTool
	args  string
}

func (s *delegateParent) Send(ctx context.Context, _ contract.Message, _ func(agentruntime.Delta) error) (agentruntime.Result, error) {
	if _, err := s.spawn.Execute(ctx, "spawn", json.RawMessage(s.args), nil); err != nil {
		return agentruntime.Result{}, err
	}
	return agentruntime.Result{Text: "Parent continued"}, nil
}
func (*delegateParent) Abort(context.Context) error { return nil }
func (*delegateParent) Path() string                { return "parent" }
func (*delegateParent) Close() error                { return nil }

// delegateServer starts a server whose assistant hands one task, args, to an external agent.
func delegateServer(t *testing.T, args string, registry *acp.Registry) *Server {
	t.Helper()
	t.Setenv(acptest.Env, "1")
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	api, err := New(t.Context(), db, Config{
		DataDir:     dir,
		Factory:     &delegateFactory{runtime: sqliteRuntime(t, dir, "http://127.0.0.1:1"), args: args},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		VaultKeys:   &vaultKeys{},
		Registry:    registry,
		Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Model: "local/test"}, Tools: []string{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close() })
	send(t, api, "assistant", "delegate")
	awaitText(t, api, "assistant", "Parent continued")
	return api
}

// awaitChild waits for the assistant's child task to reach a status that done accepts.
func awaitChild(t *testing.T, api *Server, done func(contract.Run) bool) contract.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		children, err := api.Store.Children(t.Context(), "assistant")
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range children {
			if done(child) {
				return child
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the child task did not reach the expected state")
	return contract.Run{}
}

// delegate has the assistant hand one task to an external agent and returns the finished child.
func delegate(t *testing.T, args string, registry *acp.Registry) contract.Run {
	t.Helper()
	api := delegateServer(t, args, registry)
	child := awaitChild(t, api, func(run contract.Run) bool { return run.Status == contract.RunStatusCompleted })
	messages, err := api.Store.ChildMessages(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(store.Text(message), "Hello from ACP [allow-once]") {
			return child
		}
	}
	t.Fatal("the external agent did not answer the delegated task")
	return contract.Run{}
}

// fakeCommand runs the test binary as the fake ACP agent with prompt as the task.
func fakeCommand(t *testing.T, prompt string) string {
	t.Helper()
	args, err := json.Marshal(map[string]any{"action": "run", "prompt": prompt,
		"external": map[string]any{"command": os.Args[0], "args": []string{"-test.run=^$"}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(args)
}

func TestDelegationRunsAnExternalCommand(t *testing.T) {
	child := delegate(t, fakeCommand(t, "Say hello"), nil)
	if child.External == nil || child.External.Command != os.Args[0] || child.External.Name != filepath.Base(os.Args[0]) {
		t.Fatalf("child external = %+v", child.External)
	}
}

// The test binary stands in for an installed registry agent: its file name is on PATH.
func TestDelegationRunsAnInstalledRegistryAgent(t *testing.T) {
	binary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	platform := runtime.GOOS + "-" + map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]
	registry := fmt.Sprintf(`{"version":"1","agents":[{"id":"stand-in","name":"Stand In","version":"1","description":"Test agent",
		"distribution":{"binary":{%q:{"archive":"https://example.invalid/a.zip","cmd":"./%s","args":["-test.run=^$"]}}}}]}`,
		platform, filepath.Base(binary))
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, registry) }))
	t.Cleanup(cdn.Close)

	child := delegate(t, `{"action":"run","prompt":"Say hello","external":{"agent":"stand-in"}}`, acp.NewRegistry(t.TempDir(), cdn.URL))
	if child.External == nil || child.External.Agent != "stand-in" || child.External.Name != "Stand In" {
		t.Fatalf("child external = %+v", child.External)
	}
}

func TestDelegationFailsWhenTheAgentCrashes(t *testing.T) {
	api := delegateServer(t, fakeCommand(t, acptest.Crash), nil)
	child := awaitChild(t, api, func(run contract.Run) bool { return store.Terminal(string(run.Status)) })
	if child.Status != contract.RunStatusFailed {
		t.Fatalf("crashed child ended %s", child.Status)
	}
}

// Stopping a running external child sends session/cancel and ends it cancelled.
func TestDelegationStopCancelsTheAgent(t *testing.T) {
	api := delegateServer(t, fakeCommand(t, acptest.Wait), nil)
	child := awaitChild(t, api, func(run contract.Run) bool { return run.Status == contract.RunStatusRunning })
	deadline := time.Now().Add(10 * time.Second)
	for {
		messages, err := api.Store.ChildMessages(t.Context(), child.ID)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(messages, func(m contract.Message) bool { return strings.Contains(store.Text(m), "Working") }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the agent never streamed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ids, err := api.Store.StopChild(t.Context(), "assistant", child.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if active, ok := api.turns.Load(id); ok {
			active.(*execution).cancel()
		}
	}
	stopped := awaitChild(t, api, func(run contract.Run) bool { return run.ID == child.ID && store.Terminal(string(run.Status)) })
	if stopped.Status != contract.RunStatusCancelled {
		t.Fatalf("stopped child ended %s", stopped.Status)
	}
}

func TestDelegationRejectsInvalidExternalAgents(t *testing.T) {
	platform := runtime.GOOS + "-" + map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]
	registry := fmt.Sprintf(`{"version":"1","agents":[{"id":"known","name":"Known","version":"1","description":"Test agent",
		"distribution":{"binary":{%q:{"archive":"https://example.invalid/a.zip","cmd":"./known"}}}}]}`, platform)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, registry) }))
	t.Cleanup(cdn.Close)
	tool := &delegateTool{server: &Server{config: Config{Registry: acp.NewRegistry(t.TempDir(), cdn.URL)}}}
	for name, in := range map[string]contract.ExternalAgent{
		"both":         {Agent: "known", Command: "agent"},
		"agent args":   {Agent: "known", Args: []string{"acp"}},
		"unknown":      {Agent: "missing"},
		"newline":      {Command: "agent\nrm -rf /"},
		"carriage":     {Command: "agent\rmore"},
		"nul command":  {Command: "agent\x00"},
		"nul argument": {Command: "agent", Args: []string{"acp\x00"}},
	} {
		if got, err := tool.external(t.Context(), &in); err == nil {
			t.Errorf("%s: accepted %+v", name, got)
		}
	}
	if got, err := tool.external(t.Context(), &contract.ExternalAgent{Agent: "known"}); err != nil || got.Name != "Known" {
		t.Fatalf("known agent = %+v, %v", got, err)
	}
}
