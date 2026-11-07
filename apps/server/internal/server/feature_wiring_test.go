package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestReusedAttachDirectory(t *testing.T) {
	repo := workspaceRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "report.txt"), []byte("Source report"), 0600); err != nil {
		t.Fatal(err)
	}
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, message contract.Message) (agentruntime.Result, error) {
		if message.ID == "first" {
			return agentruntime.Result{Text: "Created report"}, os.WriteFile(filepath.Join(d.Worktree, "report.txt"), []byte("Conversation report"), 0600)
		}
		_, err := botCall(ctx, d, "attach_file", `{"path":"report.txt"}`)
		return agentruntime.Result{Text: "Attached report"}, err
	}}
	api, _ := startTest(t, factory)
	patchWorkspace(t, api, "assistant", repo)
	for _, id := range []string{"first", "second"} {
		text := "Prepare and attach the report"
		message := api.execute(t.Context(), t.Context(), api.actors["assistant"], store.Work{Trigger: "message", Message: contract.Message{
			ID: id, ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}},
		}})
		if message.Status != "done" {
			t.Fatalf("%s failed: %s", id, store.Text(message))
		}
		if id == "second" {
			checkAttached(t, api, []contract.Message{message}, "Conversation report")
		}
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.opened) != 1 {
		t.Fatal("regression did not exercise a reused session", len(factory.opened))
	}
}

func TestPrivateAttachDirectory(t *testing.T) {
	repo := workspaceRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "report.txt"), []byte("Source report"), 0600); err != nil {
		t.Fatal(err)
	}
	var api *Server
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		cwd := agentruntime.AgentDir(api.config.DataDir, d.Agent.ID)
		if err := os.MkdirAll(cwd, 0700); err != nil {
			return agentruntime.Result{}, err
		}
		if err := os.WriteFile(filepath.Join(cwd, "report.txt"), []byte("Private report"), 0600); err != nil {
			return agentruntime.Result{}, err
		}
		_, err := botCall(ctx, d, "attach_file", `{"path":"report.txt"}`)
		return agentruntime.Result{Text: "Attached private report"}, err
	}}
	var web *httptest.Server
	api, web = startTest(t, factory)
	patchWorkspace(t, api, "assistant", repo)
	if code, _ := voicePost(t, web, "assistant", "private-report"); code != http.StatusOK {
		t.Fatal("private attachment failed", code)
	}
	messages, err := api.Store.Messages(t.Context(), "assistant", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	checkAttached(t, api, messages, "Private report")
}

func checkAttached(t *testing.T, api *Server, messages []contract.Message, want string) {
	t.Helper()
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Attachment == nil {
				continue
			}
			_, path, err := api.Store.Attachment(t.Context(), part.Attachment.ID)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatalf("attachment directory mismatch: %q, want %q: %v", data, want, err)
			}
			return
		}
	}
	t.Fatal("attachment missing")
}

func TestHTTPFailureBlocksCheckpoint(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "operation outcome unknown", http.StatusInternalServerError)
	}))
	defer endpoint.Close()
	api, _ := startTest(t, &voiceFactory{})
	run := store.NewRun("http-receipt", "assistant", "assistant", "message")
	if err := api.Store.PutPrivateRun(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	state := &turnState{run: run, work: store.Work{Trigger: "message"}, store: api.Store, external: map[string]bool{"http_request": true}}
	ctx := context.WithValue(t.Context(), turnKey{}, state)
	args, _ := json.Marshal(map[string]string{"url": endpoint.URL, "method": "POST"})
	if hook := beforeTool(ctx, "mutation", "http_request", args); hook.Block {
		t.Fatal("intent failed", hook.Reason)
	}
	result, err := (&httpTool{server: api}).Execute(ctx, "mutation", args, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Text(), `"status":500`) {
		t.Fatal("HTTP failure was not exposed with its receipt", result, err)
	}
	if err := api.Store.EndEffect(ctx, run.ID, "mutation", result.IsError); err != nil {
		t.Fatal(err)
	}
	if pending, err := api.Store.PendingEffects(ctx, run.ID); err != nil || pending != 1 {
		t.Fatal("failed mutation was sealed as successful", pending, err)
	}
	_, err = (&taskTool{server: api}).Execute(ctx, "next", json.RawMessage(`{"action":"checkpoint","goal":"Update service","next":"Continue the update","evidence":"HTTP response received","after_seconds":60}`), nil)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatal("continuation ignored uncertain mutation", err)
	}
}
