package store

import (
	"database/sql"
	"errors"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"path/filepath"
	"testing"
	"time"
)

func beginStage(t *testing.T, s *Store, job string) contract.Run {
	t.Helper()
	request := contract.SendRequest{ID: job, Text: "Established goal", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "a", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "a"); err != nil {
		t.Fatal(err)
	}
	if id, err := s.Job(t.Context(), "a"); err != nil || id != job {
		t.Fatalf("claim %s %v", id, err)
	}
	run := NewRun(job, "a", "a", "message")
	run.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &run, nil); err != nil {
		t.Fatal(err)
	}
	return run
}
func sealStage(t *testing.T, s *Store, run *contract.Run, checkpoint *Checkpoint) error {
	t.Helper()
	text := "Stage finished"
	m := contract.Message{ID: "reply:" + run.ID, ThreadID: run.ConversationID, Role: "assistant", Status: "done", CreatedAt: Now(), RunID: &run.ID, AgentID: &run.AgentID, Parts: []contract.Part{{Kind: "text", Text: &text}}}
	run.Status = "completed"
	return s.FinalizeTask(t.Context(), contract.Event{ConversationID: m.ThreadID, Type: "message.done", MessageID: &m.ID}, &m, run, nil, checkpoint, 2, len(text))
}
func TestTaskSealedCheckpointSurvivesRestart(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), []contract.Agent{{ID: "a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	run := beginStage(t, s, "root")
	checkpoint := Checkpoint{Goal: "Original goal", Next: "Inspect the saved draft then continue", Evidence: "Draft path exists", After: 1}
	if err := sealStage(t, s, &run, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := sealStage(t, s, &run, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), "root"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ScheduleTasks(t.Context(), time.Now().Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	job, err := s.Job(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.Work(t.Context(), job)
	if err != nil || work.TaskID != "task:"+run.ID || work.Generation != 1 || len(work.Targets) != 1 || work.Targets[0] != "a" {
		t.Fatalf("successor %+v %v", work, err)
	}
	if _, err := s.StartTask(t.Context(), work); err != nil {
		t.Fatal(err)
	}
	next := NewRun(job, "a", "a", "task")
	next.TaskID = &work.TaskID
	next.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &next, nil); err != nil {
		t.Fatal(err)
	}
	if err := sealStage(t, s, &next, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	items, err := s.Tasks(t.Context(), "a", "a")
	if err != nil || len(items) != 1 || items[0].Status != "completed" || items[0].Tools != 4 {
		t.Fatalf("tasks %+v %v", items, err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("duplicate successor %v", err)
	}
}

func TestTaskUnsealedEffectIsNotReplayed(t *testing.T) {
	t.Parallel()
	s := runStore(t)
	run := beginStage(t, s, "root")
	if err := s.BeginEffect(t.Context(), run.ID, "effect", "remote", []byte(`{"secret":"not stored"}`), ""); err != nil {
		t.Fatal(err)
	}
	checkpoint := Checkpoint{Goal: "Goal", Next: "Next"}
	if err := sealStage(t, s, &run, &checkpoint); !errors.Is(err, ErrTask) {
		t.Fatalf("ambiguous effect sealed %v", err)
	}
	if err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.ScheduleTasks(t.Context(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ambiguous effect replayed %v", err)
	}
	items, err := s.Tasks(t.Context(), "a", "a")
	if err != nil || len(items) != 0 {
		t.Fatalf("unsealed checkpoint survived %+v %v", items, err)
	}
	var digest, status string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT digest,status FROM effects`).Scan(&digest, &status); err != nil || len(digest) != 64 || status != "unknown" {
		t.Fatalf("receipt %s %s %v", digest, status, err)
	}
}

func TestTaskInterruptCancelAndPark(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"interrupt", "cancel", "park"} {
		t.Run(kind, func(t *testing.T) {
			s := runStore(t)
			run := beginStage(t, s, "root")
			checkpoint := Checkpoint{Goal: "Goal", Next: "Next", Wait: kind == "park"}
			if err := sealStage(t, s, &run, &checkpoint); err != nil {
				t.Fatal(err)
			}
			if err := s.Finish(t.Context(), "root"); err != nil {
				t.Fatal(err)
			}
			id := "task:" + run.ID
			if kind == "park" {
				if err := s.ResumeTask(t.Context(), "b", "b", id, checkpoint); !errors.Is(err, ErrMembers) {
					t.Fatal(err)
				}
				if err := s.ScheduleTasks(t.Context(), time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("parked task ran")
				}
				return
			}
			if err := s.ScheduleTasks(t.Context(), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if kind == "interrupt" {
				job, err := s.Job(t.Context(), "a")
				if err != nil {
					t.Fatal(err)
				}
				work, err := s.Work(t.Context(), job)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.StartTask(t.Context(), work); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.CancelTask(t.Context(), "a", "a", id); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := s.ScheduleTasks(t.Context(), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("resurrected %v", err)
			}
			items, err := s.Tasks(t.Context(), "a", "a")
			if err != nil {
				t.Fatal(err)
			}
			want := "needs_review"
			if kind == "cancel" {
				want = "cancelled"
			}
			if items[0].Status != want {
				t.Fatalf("task %+v", items[0])
			}
		})
	}
}
