package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func peerFixture(t *testing.T) (*Store, contract.Run, contract.Run) {
	t.Helper()
	s := runStore(t)
	parent := NewRun("origin", "a", "a", "message")
	parent.Status = "running"
	if err := s.RunEvent(t.Context(), "run.start", &parent, nil); err != nil {
		t.Fatal(err)
	}
	text := "Explicit public task"
	work := Work{Trigger: "peer", Message: contract.Message{ID: "request", ThreadID: "b", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}, ParentRunID: &parent.ID, DeliveryID: "request"}
	delivery := contract.PeerDelivery{ID: "request", SourceAgentID: "a", TargetAgentID: "b", ConversationID: "a", ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
	if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
		t.Fatal(err)
	}
	parent.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
		t.Fatal(err)
	}
	parent, err := s.Run(t.Context(), parent.ID)
	if err != nil || parent.Status != "waiting_peer" {
		t.Fatalf("source did not wait: %+v %v", parent, err)
	}
	child, err := s.Run(t.Context(), NewRun("request", "b", "b", "peer").ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, parent, child
}

func TestPeerRemoval(t *testing.T) {
	for _, target := range []string{"a", "b"} {
		t.Run(target, func(t *testing.T) {
			s, parent, child := peerFixture(t)
			if _, _, err := s.DeleteAgent(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Job(t.Context(), "b"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("removed peer work remained executable", err)
			}
			if target == "a" {
				child, err := s.Run(t.Context(), child.ID)
				if err != nil || child.Status != "cancelled" {
					t.Fatalf("orphan request %+v %v", child, err)
				}
				if err := s.Finish(t.Context(), child.JobID); err != nil {
					t.Fatal("late worker cleanup after source deletion", err)
				}
			} else {
				parent, err := s.Run(t.Context(), parent.ID)
				if err != nil || parent.Status != "failed" {
					t.Fatalf("source still waits for deleted target %+v %v", parent, err)
				}
				var data string
				if err := s.DB.QueryRowContext(t.Context(), `SELECT data FROM deliveries WHERE id='request'`).Scan(&data); err != nil {
					t.Fatal(err)
				}
				var delivery contract.PeerDelivery
				if err := json.Unmarshal([]byte(data), &delivery); err != nil || delivery.Status != "cancelled" {
					t.Fatalf("missing terminal receipt %+v %v", delivery, err)
				}
			}
		})
	}
}

func TestParentCleanup(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			s, parent, child := runningChild(t)
			text := "Nested task"
			work := Work{Trigger: "peer", Message: contract.Message{ID: "nested", ThreadID: "a", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}, ParentRunID: &child.ID, DeliveryID: "nested"}
			delivery := contract.PeerDelivery{ID: "nested", SourceAgentID: "b", TargetAgentID: "a", ConversationID: "b", ParentRunID: child.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
			if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
				t.Fatal(err)
			}
			request := contract.SendRequest{ID: "unrelated", Text: "Independent work", AttachmentIDs: []string{}}
			if _, _, err := s.Send(t.Context(), "b", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "b"); err != nil {
				t.Fatal(err)
			}
			parent.Status = status
			parent.Error = &contract.Error{Code: "provider", Message: "The agent could not complete this reply."}
			if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
				t.Fatal(err)
			}
			stored, err := s.Run(t.Context(), parent.ID)
			if err != nil || stored.Status != status {
				t.Fatalf("parent status: %+v %v", stored, err)
			}
			for _, id := range []string{child.ID, NewRun("nested", "a", "a", "peer").ID} {
				run, err := s.Run(t.Context(), id)
				if err != nil || run.Status != "cancelled" {
					t.Fatalf("descendant not cancelled: %+v %v", run, err)
				}
			}
			for _, id := range []string{child.JobID, "nested"} {
				var state string
				if err := s.DB.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, id).Scan(&state); err != nil || state != "done" {
					t.Fatalf("job %s state %s: %v", id, state, err)
				}
			}
			if job, err := s.Job(t.Context(), "b"); err != nil || job != "unrelated" {
				t.Fatalf("unrelated work: %s %v", job, err)
			}
		})
	}
	t.Run("completed", func(t *testing.T) {
		s, parent, child := runningChild(t)
		parent.Status = "completed"
		if err := s.RunEvent(t.Context(), "run.done", &parent, nil); err != nil {
			t.Fatal(err)
		}
		stored, err := s.Run(t.Context(), parent.ID)
		if err != nil || stored.Status != "completed" {
			t.Fatalf("parent: %+v %v", stored, err)
		}
		stored, err = s.Run(t.Context(), child.ID)
		if err != nil || stored.Status != "running" {
			t.Fatalf("running child was cancelled: %+v %v", stored, err)
		}
		var state string
		if err := s.DB.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, child.JobID).Scan(&state); err != nil || state == "done" {
			t.Fatalf("completed parent closed the child job: %s %v", state, err)
		}
	})
}

func runningChild(t *testing.T) (*Store, contract.Run, contract.Run) {
	t.Helper()
	s := runStore(t)
	text := "Parent turn"
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
	if _, err := s.Job(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	child.Status = "running"
	if err := s.RunEvent(t.Context(), "run.update", &child, nil); err != nil {
		t.Fatal(err)
	}
	return s, parent, child
}

func TestStopRuns(t *testing.T) {
	for _, status := range []string{"running", "waiting_peer", "waiting_input"} {
		t.Run(status, func(t *testing.T) {
			s, parent, child := peerFixture(t)
			parent.Status = status
			if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
				t.Fatal(err)
			}
			text := "Nested task"
			work := Work{Trigger: "peer", Message: contract.Message{ID: "nested", ThreadID: "a", Role: "user", CreatedAt: Now(), Parts: []contract.Part{{Kind: "text", Text: &text}}}, ParentRunID: &parent.ID, DeliveryID: "nested"}
			delivery := contract.PeerDelivery{ID: "nested", SourceAgentID: "b", TargetAgentID: "a", ConversationID: "b", ParentRunID: parent.ID, Intent: "request", Status: "queued", CreatedAt: Now()}
			if _, err := s.Deliver(t.Context(), delivery, work, true); err != nil {
				t.Fatal(err)
			}
			child.Status = "failed"
			if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
				t.Fatal(err)
			}
			request := contract.SendRequest{ID: "unrelated", Text: "Independent work", AttachmentIDs: []string{}}
			if _, _, err := s.Send(t.Context(), "b", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "b"); err != nil {
				t.Fatal(err)
			}
			ids, err := s.StopRuns(t.Context(), "a")
			if err != nil || len(ids) != 2 {
				t.Fatalf("stop tree: %v %v", ids, err)
			}
			for _, id := range []string{parent.ID, NewRun("nested", "a", "a", "peer").ID} {
				run, err := s.Run(t.Context(), id)
				if err != nil || run.Status != "cancelled" || run.Error == nil || run.Error.Code != "stopped" {
					t.Fatalf("run not stopped: %+v %v", run, err)
				}
			}
			stored, err := s.Run(t.Context(), child.ID)
			if err != nil || stored.Status != "failed" || stored.Seq != child.Seq {
				t.Fatalf("terminal intermediate changed: %+v %v", stored, err)
			}
			if job, err := s.Job(t.Context(), "b"); err != nil || job != "unrelated" {
				t.Fatalf("unrelated work lost: %s %v", job, err)
			}
			if _, err := s.Job(t.Context(), "a"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("nested work remains executable", err)
			}
			if _, err := s.Work(t.Context(), "followup:"+parent.ID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("stopped parent resumed", err)
			}
			work.Message.ID, work.DeliveryID, delivery.ID = "late", "late", "late"
			work.ParentRunID, delivery.ParentRunID = &parent.ID, parent.ID
			if _, err := s.Deliver(t.Context(), delivery, work, true); !errors.Is(err, ErrConflict) {
				t.Fatal("stopped parent created new work", err)
			}
			if ids, err := s.StopRuns(t.Context(), "a"); err != nil || len(ids) != 0 {
				t.Fatalf("repeated stop changed terminal runs: %v %v", ids, err)
			}
		})
	}
}

func TestStoppedStream(t *testing.T) {
	s, parent, _ := peerFixture(t)
	parent.Status = "running"
	message := contract.Message{ID: "stream", ThreadID: "a", Role: "assistant", Status: "streaming", CreatedAt: Now(), RunID: &parent.ID, Parts: []contract.Part{}}
	parent.MessageID = &message.ID
	if err := s.RunEvent(t.Context(), "run.update", &parent, nil); err != nil {
		t.Fatal(err)
	}
	event := contract.Event{ConversationID: "a", Type: "message.start", MessageID: &message.ID, RunID: &parent.ID}
	if _, err := s.Append(t.Context(), event, &message); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopRuns(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	event.Type = "message.delta"
	if _, err := s.Append(t.Context(), event, &message); !errors.Is(err, context.Canceled) {
		t.Fatal("stopped run accepted a late stream update", err)
	}
	stored, err := s.Message(t.Context(), message.ID)
	if err != nil || stored.Status != "error" {
		t.Fatalf("stopped message reopened: %+v %v", stored, err)
	}
}

func TestStopWaiting(t *testing.T) {
	s, parent, child := peerFixture(t)
	request := contract.SendRequest{ID: "unrelated", Text: "Unrelated user message", AttachmentIDs: []string{}}
	if _, _, err := s.Send(t.Context(), "b", request, []contract.Part{{Kind: "text", Text: &request.Text}}, "b"); err != nil {
		t.Fatal(err)
	}
	ids, err := s.StopWaiting(t.Context(), "a")
	if err != nil || len(ids) != 1 || ids[0] != child.ID {
		t.Fatalf("scoped stop %v %v", ids, err)
	}
	for _, id := range []string{parent.ID, child.ID} {
		run, err := s.Run(t.Context(), id)
		if err != nil || run.Status != "cancelled" {
			t.Fatalf("run not cancelled %+v %v", run, err)
		}
	}
	if id, err := s.Job(t.Context(), "b"); err != nil || id != "unrelated" {
		t.Fatal("stop removed unrelated work", id, err)
	}
}

func TestReceiptRequiresOutcome(t *testing.T) {
	s, parent, child := peerFixture(t)
	child.Status = "completed"
	if err := s.Receipt(t.Context(), "request", child, contract.Message{}); !errors.Is(err, ErrConflict) {
		t.Fatal("unpersisted success accepted", err)
	}
	if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Receipt(t.Context(), "request", child, contract.Message{}); err != nil {
		t.Fatal(err)
	}
	parent, err := s.Run(t.Context(), parent.ID)
	if err != nil || parent.Status != "completed" {
		t.Fatalf("durable result did not resolve wait %+v %v", parent, err)
	}
}

func TestFinishRepairsReceipt(t *testing.T) {
	s, parent, child := peerFixture(t)
	child.Status = "completed"
	if err := s.RunEvent(t.Context(), "run.done", &child, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), child.JobID); err != nil {
		t.Fatal(err)
	}
	parent, err := s.Run(t.Context(), parent.ID)
	if err != nil || parent.Status != "completed" {
		t.Fatalf("job completion left its source waiting: %+v %v", parent, err)
	}
}
