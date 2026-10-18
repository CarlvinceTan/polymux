package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type childFactory struct {
	target  string
	opened  chan struct{}
	release chan struct{}
}

func (f *childFactory) Open(_ context.Context, d agentruntime.Definition, _ string) (agentruntime.Session, error) {
	var spawn *spawnTool
	for _, tool := range d.ExtraTools {
		if candidate, ok := tool.(*spawnTool); ok {
			spawn = candidate
		}
	}
	return &childSession{factory: f, spawn: spawn, child: strings.HasPrefix(d.Scope, "child-")}, nil
}

type childSession struct {
	factory *childFactory
	spawn   *spawnTool
	child   bool
}

func (s *childSession) Send(ctx context.Context, message contract.Message, _ func(agentruntime.Delta) error) (agentruntime.Result, error) {
	if s.child {
		text := store.Text(message)
		if !strings.Contains(text, "Bounded child task") || strings.Contains(text, "Private parent context") {
			return agentruntime.Result{}, fmt.Errorf("child context was not isolated")
		}
		close(s.factory.opened)
		select {
		case <-s.factory.release:
			return agentruntime.Result{Text: "Child complete"}, nil
		case <-ctx.Done():
			return agentruntime.Result{}, ctx.Err()
		}
	}
	if s.spawn == nil {
		return agentruntime.Result{}, fmt.Errorf("spawn tool missing")
	}
	args, err := json.Marshal(map[string]string{"agent_id": s.factory.target, "task": "Bounded child task"})
	if err != nil {
		return agentruntime.Result{}, err
	}
	if hook := beforeTool(ctx, "spawn", "spawn_task", args); hook.Block {
		return agentruntime.Result{}, fmt.Errorf("spawn blocked: %s", hook.Reason)
	}
	result, err := s.spawn.Execute(ctx, "spawn", args, nil)
	if err != nil {
		return agentruntime.Result{}, err
	}
	if result.Terminate {
		return agentruntime.Result{}, fmt.Errorf("spawn ended the parent turn")
	}
	return agentruntime.Result{Text: "Parent continued"}, nil
}
func (*childSession) Abort(context.Context) error { return nil }
func (*childSession) Path() string                { return "fixture" }
func (*childSession) Close() error                { return nil }

func TestSpawnContinues(t *testing.T) {
	for _, target := range []string{"", "b"} {
		t.Run("target="+target, func(t *testing.T) {
			dir := t.TempDir()
			db, err := store.Open(t.Context(), filepath.Join(dir, "db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			factory := &childFactory{target: target, opened: make(chan struct{}), release: make(chan struct{})}
			api, err := New(t.Context(), db, Config{DataDir: dir, Factory: factory, Definitions: []agentruntime.Definition{
				{Agent: contract.Agent{ID: "a", Name: "A"}}, {Agent: contract.Agent{ID: "b", Name: "B"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { api.Close() })
			t.Cleanup(func() { close(factory.release) })
			text := "Private parent context"
			if _, _, err := db.Send(t.Context(), "a", contract.SendRequest{ID: "parent", Text: text}, []contract.Part{{Kind: "text", Text: &text}}, "a"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-factory.opened:
			case <-time.After(3 * time.Second):
				t.Fatal("isolated child did not start")
			}
			id := store.NewRun("parent", "a", "a", "message").ID
			end := time.Now().Add(3 * time.Second)
			for time.Now().Before(end) {
				run, err := db.Run(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				if run.Status == "completed" {
					if run.MessageID == nil {
						t.Fatal("parent missing message")
					}
					message, err := db.Message(t.Context(), *run.MessageID)
					if err != nil || store.Text(message) != "Parent continued" {
						t.Fatalf("parent did not continue: %+v %v", message, err)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("parent waited for child completion")
		})
	}
}
