package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/CarlvinceTan/polymux/apps/server/internal/testenv"
)

func findTaskTool(t *testing.T, d agentruntime.Definition, name string) *taskThreadTool {
	t.Helper()
	for _, tool := range d.ExtraTools {
		if candidate, ok := tool.(*taskThreadTool); ok && candidate.Name() == name {
			return candidate
		}
	}
	return nil
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if done() {
			return
		}
	}
	t.Fatal(what)
}

func TestTaskThread(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	turns := make(chan string, 8)
	factory := &voiceFactory{script: func(ctx context.Context, d agentruntime.Definition, message contract.Message) (agentruntime.Result, error) {
		text := store.Text(message)
		if d.Scope != "" {
			if findTaskTool(t, d, "create_task") != nil {
				return agentruntime.Result{}, errors.New("a task can start another task")
			}
			if strings.Contains(text, "group chat") || !strings.Contains(text, "Write the launch report") {
				return agentruntime.Result{}, errors.New("task turn missing its brief: " + text)
			}
			turns <- text
			return agentruntime.Result{Text: "Report written"}, nil
		}
		create := findTaskTool(t, d, "create_task")
		if create == nil {
			return agentruntime.Result{}, errors.New("main chat has no create_task")
		}
		args, _ := json.Marshal(map[string]string{"name": "Launch report", "description": "Write the launch report"})
		if _, err := create.Execute(ctx, "call", args, nil); err != nil {
			return agentruntime.Result{}, err
		}
		return agentruntime.Result{Text: "Started"}, nil
	}}
	api, err := New(t.Context(), db, Config{DataDir: dir, Factory: factory, Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "a", Name: "A"}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Close() })
	text := "Write the launch report"
	if _, _, err := db.Send(t.Context(), "a", contract.SendRequest{ID: "ask", Text: text}, []contract.Part{{Kind: "text", Text: &text}}, "a"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-turns:
	case <-time.After(5 * time.Second):
		t.Fatal("task did not start")
	}
	var task contract.Thread
	waitFor(t, "task reply did not arrive", func() bool {
		tasks, err := db.AgentTasks(t.Context(), "a")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 {
			task = tasks[0]
		}
		return task.UnreadCount > 0
	})
	if task.Title != "Launch report" || task.Task.Status != "open" || task.Task.Description != text || !slices.Equal(task.AgentIDs, []string{"a"}) {
		t.Fatalf("task %+v", task)
	}
	if _, err := db.GroupSession(t.Context(), task.ID, "a"); err != nil {
		t.Fatalf("task has no session of its own: %v", err)
	}
	main, err := db.Thread(t.Context(), "a")
	if err != nil || main.Task != nil {
		t.Fatalf("main chat %+v %v", main, err)
	}
	sidebar, err := db.Sidebar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(sidebar.Placements, func(p contract.SidebarPlacement) bool { return p.ThreadID == task.ID }) {
		t.Fatal("task has a place in the chat list")
	}
	if scope, err := memoryThread(t.Context(), db, contract.Run{AgentID: "a", ConversationID: task.ID}); err != nil || scope != "a" {
		t.Fatalf("memory scope %q %v", scope, err)
	}

	// A repeated tool call finds the same task.
	if _, created, err := db.CreateTask(t.Context(), task.ID, "a", "a", "Launch report", text); err != nil || created {
		t.Fatalf("repeat created=%v err=%v", created, err)
	}
	if err := db.UpdateThread(t.Context(), task.ID, contract.UpdateThreadRequest{AgentIDs: []string{"a"}}); !errors.Is(err, store.ErrMembers) {
		t.Fatalf("member edit: %v", err)
	}

	patch := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPatch, "/threads/"+task.ID+"/task", strings.NewReader(body))
		r.SetPathValue("id", task.ID)
		api.updateTask(w, r)
		return w
	}
	if w := patch(`{"status":"done","pinned":true}`); w.Code != 200 {
		t.Fatalf("mark done: %d %s", w.Code, w.Body)
	}
	if w := patch(`{"status":"lost"}`); w.Code != 400 {
		t.Fatalf("bad status: %d", w.Code)
	}
	listed := func() bool {
		chats, err := db.Chats(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(chats, func(c contract.Thread) bool { return c.ID == task.ID })
	}
	stale := time.Now().Add(-73 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := db.DB.ExecContext(t.Context(), `UPDATE task_threads SET updated_at=? WHERE thread_id=?`, stale, task.ID); err != nil {
		t.Fatal(err)
	}
	if !listed() {
		t.Fatal("pinned finished task left the list")
	}
	if w := patch(`{"pinned":false}`); w.Code != 200 {
		t.Fatalf("unpin: %d", w.Code)
	}
	if _, err := db.DB.ExecContext(t.Context(), `UPDATE task_threads SET updated_at=? WHERE thread_id=?`, stale, task.ID); err != nil {
		t.Fatal(err)
	}
	if listed() {
		t.Fatal("old finished task stayed listed")
	}

	// Messaging a finished task reopens it and reaches its own session.
	reply := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/threads/"+task.ID+"/messages", strings.NewReader(`{"id":"more","text":"Add the numbers"}`))
	request.SetPathValue("id", task.ID)
	api.send(reply, request)
	if reply.Code != 202 {
		t.Fatalf("send: %d %s", reply.Code, reply.Body)
	}
	select {
	case body := <-turns:
		if !strings.Contains(body, "Add the numbers") {
			t.Fatalf("follow-up %q", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow-up did not run")
	}
	if current, err := db.Thread(t.Context(), task.ID); err != nil || current.Task.Status != "open" {
		t.Fatalf("not reopened: %+v %v", current.Task, err)
	}
	waitFor(t, "follow-up did not finish", func() bool {
		current, err := db.Thread(t.Context(), task.ID)
		return err == nil && !slices.ContainsFunc(current.Runs, func(r contract.Run) bool { return !store.Terminal(r.Status) })
	})
	if err := api.eraseAgent(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if exists, err := db.IsTask(t.Context(), task.ID); err != nil || exists {
		t.Fatalf("task outlived its bot: %v %v", exists, err)
	}
}

// The model starts a task through the real runtime, and the task's turn runs in its own session.
func TestPiGCreateTask(t *testing.T) {
	testenv.PiG(t)
	var started atomic.Bool
	var brief atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		text := func(reply string) {
			fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", reply)
		}
		switch {
		case strings.Contains(string(body), "This thread is one task"):
			brief.Store(strings.Contains(string(body), "Write the launch report") && strings.Contains(string(body), `update_task`))
			text("Report written")
		case !started.Swap(true):
			call := map[string]any{"index": 0, "id": "start", "type": "function", "function": map[string]string{"name": "create_task", "arguments": `{"name":"Launch report","description":"Write the launch report"}`}}
			data, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{call}}}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
			fmt.Fprint(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		default:
			text("Started")
		}
	}))
	defer provider.Close()
	api, endpoint := startTest(t, durableRuntime(t, t.TempDir(), provider.URL+"/v1"))
	response, err := http.Post(endpoint.URL+"/threads/assistant/messages", "application/json", strings.NewReader(`{"id":"synthetic","text":"Please write the launch report","attachmentIDs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var task contract.Thread
	waitFor(t, "task did not reply", func() bool {
		tasks, err := api.Store.AgentTasks(t.Context(), "assistant")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 {
			task = tasks[0]
		}
		return task.UnreadCount > 0
	})
	waitFor(t, "task turn was not briefed", brief.Load)
	if path, err := api.Store.GroupSession(t.Context(), task.ID, "assistant"); err != nil || path == "" {
		t.Fatalf("session %q %v", path, err)
	}
	// Unread counts a reply while it streams, so wait for it to finish.
	var messages []contract.Message
	waitFor(t, "task reply did not finish", func() bool {
		messages, err = api.Store.TaskMessages(t.Context(), task.ID, 5)
		return err == nil && len(messages) > 0
	})
	if len(messages) != 1 || store.Text(messages[0]) != "Report written" {
		t.Fatalf("task messages %+v", messages)
	}
}
