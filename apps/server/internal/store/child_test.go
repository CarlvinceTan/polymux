package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func TestSpawn(t *testing.T) {
	s := runStore(t)
	text := "Private parent context"
	if _, _, err := s.Send(t.Context(), "a", contract.SendRequest{ID: "parent", Text: text}, []contract.Part{{Kind: "text", Text: &text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	parent := NewRun("parent", "a", "a", "message")
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.start", &parent, nil); err != nil {
		t.Fatal(err)
	}
	child, err := s.Spawn(t.Context(), parent.ID, "call", "b", "Bounded task")
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "queued" || child.Trigger != "child" || child.ParentRunID == nil || *child.ParentRunID != parent.ID || child.AgentID != "b" || child.JobID == parent.JobID {
		t.Fatalf("unexpected child: %+v", child)
	}
	replay, err := s.Spawn(t.Context(), parent.ID, "call", "b", "Bounded task")
	if err != nil || replay.ID != child.ID || replay.Seq != child.Seq {
		t.Fatalf("spawn replay: %+v %v", replay, err)
	}
	for _, change := range []struct{ name, target, task string }{
		{"task", "b", "Changed task"}, {"target", "a", "Bounded task"},
	} {
		t.Run(change.name, func(t *testing.T) {
			if _, err := s.Spawn(t.Context(), parent.ID, "call", change.target, change.task); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed replay: %v", err)
			}
		})
	}
	for _, invalid := range []struct{ name, target, task string }{
		{"unknown target", "missing", "Task"}, {"empty task", "b", " "}, {"oversize task", "b", strings.Repeat("x", 8193)},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if _, err := s.Spawn(t.Context(), parent.ID, invalid.name, invalid.target, invalid.task); err == nil {
				t.Fatal("invalid spawn accepted")
			}
		})
	}
	for i := 1; i < 8; i++ {
		if _, err := s.Spawn(t.Context(), parent.ID, fmt.Sprint(i), "b", "Task"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Spawn(t.Context(), parent.ID, "overflow", "b", "Task"); err == nil {
		t.Fatal("child limit not enforced")
	}
	parent.Status = "completed"
	if err := s.Finalize(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, nil, &parent, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), parent.JobID); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Run(t.Context(), parent.ID)
	if err != nil || stored.Status != "completed" {
		t.Fatalf("parent did not finish independently: %+v %v", stored, err)
	}
	stored, err = s.Run(t.Context(), child.ID)
	if err != nil || stored.Status != "queued" {
		t.Fatalf("finishing parent affected child: %+v %v", stored, err)
	}
	if _, err := s.Spawn(t.Context(), parent.ID, "late", "b", "Task"); err == nil {
		t.Fatal("finished parent spawned a child")
	}
	job, err := s.Job(t.Context(), "b")
	if err != nil || job != child.JobID {
		t.Fatalf("child job not claimable: %s %v", job, err)
	}
	work, err := s.Work(t.Context(), job)
	if err != nil || work.Task != "Bounded task" || work.Trigger != "child" || work.DeliveryID != "" {
		t.Fatalf("child input: %+v %v", work, err)
	}
}
