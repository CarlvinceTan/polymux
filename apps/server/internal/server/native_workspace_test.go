package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestChildWorkspace(t *testing.T) {
	repo := workspaceRepo(t)
	var api *Server
	var child contract.Run
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, message contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		if state.work.Trigger != "child" {
			receipt, err := botCall(ctx, d, "task", `{"action":"run","prompt":"Create the native child report"}`)
			if err == nil {
				err = json.Unmarshal([]byte(receipt.Text()), &child)
			}
			return agentruntime.Result{Text: "Child queued"}, err
		}
		if !strings.HasPrefix(d.Scope, "child-") || d.Worktree != api.worktrees.Path(d.Agent.ID, d.Scope) || d.Worktree == api.worktrees.Path(d.Agent.ID, d.Agent.ID) {
			return agentruntime.Result{}, fmt.Errorf("child did not receive its own checkout: scope=%q worktree=%q", d.Scope, d.Worktree)
		}
		if strings.Contains(store.Text(message), "PRIVATE_PARENT_CONTEXT") {
			return agentruntime.Result{}, errors.New("child inherited private parent history")
		}
		for _, name := range []string{"task", "message_agent", "handoff_agent", "manage_tasks"} {
			for _, tool := range d.ExtraTools {
				if tool.Name() == name {
					return agentruntime.Result{}, fmt.Errorf("child exposes forbidden delegation tool %s", name)
				}
			}
			if hook := d.BeforeTool(ctx, "forbidden-"+name, name, json.RawMessage(`{}`)); !hook.Block {
				return agentruntime.Result{}, fmt.Errorf("child may bypass its delegation limit using %s", name)
			}
		}
		err := nativeReport(ctx, d, repo, "Native child report")
		return agentruntime.Result{Text: "Child report ready"}, err
	}}
	api, _ = startTest(t, factory)
	patchWorkspace(t, api, "assistant", repo)
	text := "Create a bounded report. PRIVATE_PARENT_CONTEXT"
	parent := api.execute(t.Context(), t.Context(), api.actors["assistant"], store.Work{Trigger: "message", Message: contract.Message{
		ID: "native-child-parent", ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}},
	}})
	if parent.Status != "done" || child.ID == "" {
		t.Fatal("native child was not queued", parent, child)
	}
	run := awaitRun(t, api.Store, child.ID, "completed")
	if run.MessageID == nil {
		t.Fatal("child report message missing")
	}
	reply, err := api.Store.Message(t.Context(), *run.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	checkAttached(t, api, []contract.Message{reply}, "Native child report")
	factory.mu.Lock()
	definitions := append([]agentruntime.Definition(nil), factory.opened...)
	factory.mu.Unlock()
	if len(definitions) != 2 {
		t.Fatal("parent and child did not open separate sessions", len(definitions))
	}
	checkout := definitions[1].Worktree
	if data, err := os.ReadFile(filepath.Join(checkout, "native-report.txt")); err != nil || string(data) != "Native child report" {
		t.Fatal("completed child lost its generated workspace output", string(data), err)
	}
	if branch := git(t, checkout, "rev-parse", "--abbrev-ref", "HEAD"); branch != "HEAD" {
		t.Fatal("child checkout is not detached", branch)
	}
	nativeSource(t, repo)
}

func TestTaskWorkspace(t *testing.T) {
	repo := workspaceRepo(t)
	var api *Server
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, _ contract.Message) (agentruntime.Result, error) {
		state := ctx.Value(turnKey{}).(*turnState)
		if state.work.Trigger == "message" {
			if err := os.WriteFile(filepath.Join(d.Worktree, "draft.txt"), []byte("Prepared draft"), 0600); err != nil {
				return agentruntime.Result{}, err
			}
			_, err := botCall(ctx, d, "manage_tasks", `{"action":"checkpoint","goal":"Prepare the native report","next":"Read draft.txt and attach the report","evidence":"draft.txt is saved in this checkout","after_seconds":60}`)
			if err == nil && !d.BeforeTool(ctx, "after-checkpoint", "write", json.RawMessage(`{}`)).Block {
				err = errors.New("checkpoint allowed another tool in its sealed stage")
			}
			return agentruntime.Result{Text: "Draft checkpoint saved"}, err
		}
		if state.work.Trigger != "task" || !d.Automatic || !d.Ephemeral || d.Scope != "assistant" || d.Worktree != api.worktrees.Path("assistant", "assistant") {
			return agentruntime.Result{}, fmt.Errorf("continuation did not reuse its conversation checkout: %+v", d)
		}
		if data, err := os.ReadFile(filepath.Join(d.Worktree, "draft.txt")); err != nil || string(data) != "Prepared draft" {
			return agentruntime.Result{}, fmt.Errorf("continuation lost the prepared draft: %q %v", data, err)
		}
		receipt, err := botCall(ctx, d, "manage_tasks", `{"action":"list"}`)
		if err != nil || !strings.Contains(receipt.Text(), "Prepare the native report") {
			return agentruntime.Result{}, fmt.Errorf("continuation lost its resumable task state: %s %v", receipt.Text(), err)
		}
		err = nativeReport(ctx, d, repo, "Native continuation report")
		return agentruntime.Result{Text: "Task report ready"}, err
	}}
	api, _ = startTest(t, factory)
	patchWorkspace(t, api, "assistant", repo)
	text := "Prepare this report in two stages"
	first := api.execute(t.Context(), t.Context(), api.actors["assistant"], store.Work{Trigger: "message", Message: contract.Message{
		ID: "native-task-parent", ThreadID: "assistant", Role: "user", Parts: []contract.Part{{Kind: "text", Text: &text}},
	}})
	if first.Status != "done" {
		t.Fatal("first task stage failed", store.Text(first))
	}
	tasks, err := api.Store.Tasks(t.Context(), "assistant", "assistant")
	if err != nil || len(tasks) != 1 || tasks[0].Status != "sleeping" || tasks[0].Generation != 1 {
		t.Fatal("bounded continuation was not sealed", tasks, err)
	}
	if err := api.Store.ScheduleTasks(t.Context(), time.Now().Add(time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	job := fmt.Sprintf("task:%s:1", tasks[0].ID)
	run := awaitRun(t, api.Store, store.NewRun(job, "assistant", "assistant", "task").ID, "completed")
	if run.MessageID == nil {
		t.Fatal("continuation report message missing")
	}
	reply, err := api.Store.Message(t.Context(), *run.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	checkAttached(t, api, []contract.Message{reply}, "Native continuation report")
	tasks, err = api.Store.Tasks(t.Context(), "assistant", "assistant")
	if err != nil || len(tasks) != 1 || tasks[0].Status != "completed" || tasks[0].Generation != 1 {
		t.Fatal("continuation did not complete exactly one bounded stage", tasks, err)
	}
	nativeSource(t, repo)
}

func nativeReport(ctx context.Context, d agentruntime.Definition, repo, text string) error {
	if d.Workspace != repo || d.Worktree == "" || d.Worktree == repo {
		return fmt.Errorf("configured repository checkout missing: workspace=%q worktree=%q", d.Workspace, d.Worktree)
	}
	if data, err := os.ReadFile(filepath.Join(d.Worktree, "README.md")); err != nil || string(data) != "polymux\n" {
		return fmt.Errorf("checkout does not contain configured repository: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(d.Worktree, "native-report.txt"), []byte(text), 0600); err != nil {
		return err
	}
	_, err := botCall(ctx, d, "attach_file", `{"path":"native-report.txt"}`)
	return err
}

func nativeSource(t *testing.T, repo string) {
	t.Helper()
	if data, err := os.ReadFile(filepath.Join(repo, "README.md")); err != nil || string(data) != "polymux\n" {
		t.Fatal("native task modified the source repository", string(data), err)
	}
	for _, name := range []string{"draft.txt", "native-report.txt"} {
		if _, err := os.Stat(filepath.Join(repo, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("native output escaped its checkout", name, err)
		}
	}
}
