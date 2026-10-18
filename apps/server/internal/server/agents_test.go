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
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type openedAgent struct {
	definition agentruntime.Definition
	path       string
}

type definitionFactory struct {
	testFactory
	opened chan openedAgent
}

func (f *definitionFactory) Open(_ context.Context, d agentruntime.Definition, path string) (agentruntime.Session, error) {
	f.opened <- openedAgent{definition: d, path: path}
	return &testSession{factory: &f.testFactory}, nil
}

func awaitReply(t *testing.T, api *Server, thread, user string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := api.Store.Messages(t.Context(), thread, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range messages {
			if m.ReplyToID != nil && *m.ReplyToID == user && m.Status == "done" {
				if store.Text(m) != "Hello there" {
					t.Fatalf("unexpected reply %+v", m)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("custom agent did not reply")
}

func TestCreateAgentSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "polymux.db")
	definitions := []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant", Role: "Personal assistant", Model: "provider/default"}, Prompt: "Default instructions", Tools: []string{"read"}}}
	config := Config{DataDir: dir, Model: "provider/default", Definitions: definitions, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	open := func() (*Server, *httptest.Server, *definitionFactory) {
		t.Helper()
		s, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		f := &definitionFactory{opened: make(chan openedAgent, 4)}
		config.Factory = f
		api, err := New(t.Context(), s, config)
		if err != nil {
			s.Close()
			t.Fatal(err)
		}
		return api, httptest.NewServer(api), f
	}
	api, httpServer, factory := open()
	request := `{"name":"  Coach  ","role":"Study guide","prompt":"Remember my learning goals."}`
	response, err := http.Post(httpServer.URL+"/agents", "application/json", strings.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	var created contract.Agent
	err = json.NewDecoder(response.Body).Decode(&created)
	response.Body.Close()
	if err != nil || response.StatusCode != 201 || created.ID == "" || created.ID == "assistant" || created.Name != "Coach" || created.Model != config.Model || created.Active {
		t.Fatalf("create response %+v status=%d err=%v", created, response.StatusCode, err)
	}
	avatar := defaultAvatar()
	if !reflect.DeepEqual(created.Avatar, &avatar) {
		t.Fatal("creation omitted the server default avatar")
	}
	threads, err := api.Store.Threads(t.Context())
	if err != nil || len(threads) != 2 {
		t.Fatalf("one thread per agent %+v %v", threads, err)
	}
	thread, err := api.Store.Thread(t.Context(), created.ID)
	if err != nil || thread.AgentID != created.ID {
		t.Fatalf("custom conversation %+v %v", thread, err)
	}
	send := func(user string) {
		t.Helper()
		body := `{"id":"` + user + `","text":"hello","attachmentIDs":[]}`
		response, err := http.Post(httpServer.URL+"/threads/"+created.ID+"/messages", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 202 {
			t.Fatalf("send status %d", response.StatusCode)
		}
		awaitReply(t, api, created.ID, user)
	}
	send("first")
	first := <-factory.opened
	if !first.definition.Intro || len(first.definition.ExtraTools) != 0 || len(first.definition.MCP) != 0 {
		t.Fatalf("creation was not tool-free: %+v", first)
	}
	ordinary := <-factory.opened
	if ordinary.definition.Intro || ordinary.path != "test.jsonl" || len(ordinary.definition.Tools) != 6 {
		t.Fatalf("first task did not reopen normal tools: %+v", ordinary)
	}
	if first.definition.Prompt != "Remember my learning goals." || first.definition.Tools == nil || len(first.definition.Tools) != 0 || first.definition.Agent.Model != config.Model || first.path != "" {
		t.Fatalf("runtime definition %+v", first)
	}
	thread, _ = api.Store.Thread(t.Context(), created.ID)
	if err := api.Store.Read(t.Context(), created.ID, thread.LastSeq); err != nil {
		t.Fatal(err)
	}
	httpServer.Close()
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	api.Store.Close()
	api, httpServer, factory = open()
	defer httpServer.Close()
	defer api.Store.Close()
	defer api.Close()
	restored, err := api.Store.Thread(t.Context(), created.ID)
	if err != nil || restored.LastReadSeq != thread.LastSeq {
		t.Fatalf("read cursor lost %+v %v", restored, err)
	}
	api.actorsMu.RLock()
	a := api.actors[created.ID]
	api.actorsMu.RUnlock()
	if a == nil || a.definition.Prompt != first.definition.Prompt || len(a.definition.Tools) != 6 || a.definition.Tools == nil || !reflect.DeepEqual(a.definition.Agent, created) {
		t.Fatalf("custom definition not restored %+v", a)
	}
	send("second")
	second := <-factory.opened
	if second.path != "test.jsonl" || second.definition.Prompt != first.definition.Prompt || second.definition.Agent.Model != config.Model {
		t.Fatalf("session did not resume same definition %+v", second)
	}
	agents, err := api.Store.Agents(t.Context())
	if err != nil || len(agents) != 2 || agents[0].ID != "assistant" || agents[0].Name != "Assistant" {
		t.Fatalf("defaults not preserved %+v %v", agents, err)
	}
}

func TestCreateAgentValidation(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	for _, body := range []string{
		`{}`, `{"name":" ","role":"guide","prompt":"help"}`,
		`{"name":"` + strings.Repeat("n", 129) + `","role":"","prompt":""}`,
		`{"name":"Guide","role":"guide","prompt":"help","model":" "}`,
		`{"name":"Guide","role":"guide","prompt":"help","tools":["bash"]}`,
	} {
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body)))
		if recorder.Code != 400 {
			t.Errorf("invalid create accepted: %s status=%d", body, recorder.Code)
		}
	}
	agents, err := api.Store.Agents(t.Context())
	if err != nil || len(agents) != 1 {
		t.Fatalf("invalid creates mutated store %+v %v", agents, err)
	}
	custom := httptest.NewRecorder()
	api.ServeHTTP(custom, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Guide","role":"guide","prompt":"help","model":"provider/custom"}`)))
	var a contract.Agent
	if err := json.Unmarshal(custom.Body.Bytes(), &a); err != nil || custom.Code != 201 || a.Model != "provider/custom" {
		t.Fatalf("custom model %+v status=%d err=%v", a, custom.Code, err)
	}
}

func TestCreateAgentShutdownRace(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	var succeeded atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		workers.Go(func() {
			<-start
			recorder := httptest.NewRecorder()
			api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Same name","role":"Guide","prompt":"help"}`)))
			switch recorder.Code {
			case 201:
				succeeded.Add(1)
			case 503:
			default:
				t.Errorf("create during shutdown status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
		workers.Go(func() {
			<-start
			recorder := httptest.NewRecorder()
			api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/assistant/stop", strings.NewReader(`{}`)))
		})
		workers.Go(func() {
			<-start
			recorder := httptest.NewRecorder()
			api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/agents/assistant", strings.NewReader(`{"name":"Edited","role":"","prompt":"New instructions"}`)))
			if recorder.Code != 200 && recorder.Code != 503 {
				t.Errorf("edit during shutdown status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
		workers.Go(func() {
			<-start
			recorder := httptest.NewRecorder()
			body, _ := json.Marshal(avatarDesign())
			api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/agents/assistant/avatar", strings.NewReader(string(body))))
			if recorder.Code != 200 && recorder.Code != 503 {
				t.Errorf("avatar edit during shutdown status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	workers.Go(func() { <-start; api.Close() })
	close(start)
	workers.Wait()
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Late","role":"Guide","prompt":"help"}`)))
	if recorder.Code != 503 {
		t.Fatalf("worker accepted after shutdown: %d", recorder.Code)
	}
	agents, err := api.Store.Agents(t.Context())
	if err != nil || len(agents) != int(succeeded.Load())+1 {
		t.Fatalf("atomic creates count=%d successful=%d err=%v", len(agents), succeeded.Load(), err)
	}
	definitions, err := api.Store.Definitions(t.Context())
	if err != nil || len(definitions) != len(agents) {
		t.Fatalf("definition count=%d agents=%d err=%v", len(definitions), len(agents), err)
	}
}

// workspaceRepo seeds a repository with an origin carrying main, so the server
// can create a conversation worktree from origin/main.
func workspaceRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "polymux@example.test")
	git(t, repo, "config", "user.name", "Polymux")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("polymux\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-m", "Seed")
	origin := t.TempDir()
	git(t, origin, "init", "--bare", "-b", "main")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "origin", "main")
	return repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func patchWorkspace(t *testing.T, api *Server, id, workspace string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"name": "Assistant", "role": "Guide", "prompt": "Help.", "workspace": workspace})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/agents/"+id, strings.NewReader(string(body))))
	if recorder.Code != 200 {
		t.Fatalf("workspace patch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	data, err := api.Store.Definition(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var d agentruntime.Definition
	if err := json.Unmarshal(data, &d); err != nil || d.Workspace != workspace {
		t.Fatalf("workspace was not saved: %q %v", d.Workspace, err)
	}
}

func TestWorkspaceChatGetsCheckout(t *testing.T) {
	repo := workspaceRepo(t)
	api, server := startTest(t, &testFactory{})
	patchWorkspace(t, api, "assistant", repo)
	body := `{"id":"first","text":"hello","attachmentIDs":[]}`
	response, err := http.Post(server.URL+"/threads/assistant/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("send status %d", response.StatusCode)
	}
	awaitReply(t, api, "assistant", "first")
	checkout := filepath.Join(api.config.DataDir, "worktrees", "assistant--assistant")
	if branch := git(t, checkout, "rev-parse", "--abbrev-ref", "HEAD"); branch != "HEAD" {
		t.Fatalf("chat checkout is not detached: %q", branch)
	}
	if branches := git(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads"); branches != "main" {
		t.Fatalf("chat checkout created a branch: %s", branches)
	}
	request, err := http.NewRequest(http.MethodDelete, server.URL+"/agents/assistant", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("delete status %d", response.StatusCode)
	}
	if _, err := os.Stat(checkout); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleting the chat left its checkout: %v", err)
	}
}

func TestWorkspaceChatFailureExplained(t *testing.T) {
	api, server := startTest(t, &testFactory{})
	patchWorkspace(t, api, "assistant", t.TempDir()) // exists but is not a git repository
	body := `{"id":"first","text":"hello","attachmentIDs":[]}`
	response, err := http.Post(server.URL+"/threads/assistant/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("failing workspace turned the send into %d", response.StatusCode)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range messages {
			if m.Status != "error" || m.RunID == nil {
				continue
			}
			run, err := api.Store.Run(t.Context(), *m.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Error == nil || run.Error.Code != "workspace" || !strings.HasPrefix(run.Error.Message, "Can't open repo:") {
				t.Fatalf("run error %+v", run.Error)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn did not end on its own")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkspaceRequestLatency(t *testing.T) {
	repo := workspaceRepo(t)
	upload := filepath.Join(t.TempDir(), "upload-pack")
	if err := os.WriteFile(upload, []byte("#!/bin/sh\nsleep 1\nexec git-upload-pack \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "remote.origin.uploadpack", upload)
	api, _ := startTest(t, &testFactory{})
	patchWorkspace(t, api, "assistant", repo)
	var durations []time.Duration
	for i := 0; i < 20; i++ {
		body := fmt.Sprintf(`{"id":"latency-%d","text":"hello","attachmentIDs":[]}`, i)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/threads/assistant/messages", strings.NewReader(body))
		start := time.Now()
		api.ServeHTTP(recorder, request)
		durations = append(durations, time.Since(start))
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("send status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	if p95 := durations[18]; p95 >= 50*time.Millisecond {
		t.Fatalf("message POST p95=%s, want below 50ms with slow fetch", p95)
	}
	awaitReply(t, api, "assistant", "latency-0")
}
